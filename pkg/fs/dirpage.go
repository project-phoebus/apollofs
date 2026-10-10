package fs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/domainos-archeology/apollofs/pkg/uid"
)

// SR10 directories are B-trees of 1024-byte pages (domain_os/dir/
// dir_internal.h, dir_page_hdr_t).  Every page starts with an 0x12-byte
// header:
//
//	+0x00 u16 flags: (kind<<14) | version; kind 0 = leaf, 1 = interior;
//	          bit 13 = page holds deleted entries; version is 5
//	+0x02 uid split stamp (shared by the pages one split wrote; 0 on a
//	          directory that never split)
//	+0x0A u16 page number (0 = root)
//	+0x0C u16 next leaf page (0xFFFF = none)
//	+0x0E u16 end of the index table
//	+0x10 u16 lowest entry offset (entries are packed down from 0x400)
//
// The root page has a "root area" between the header and the index table:
// 0x12 u16 0x0100, 0x14 u16 root area length (0x70), 0x16 u16 0x0068,
// 0x18 u16 0, then two default protections (DirDefProt at 0x1A, FileDefProt
// at 0x4E, 0x34 bytes each).  The index table is an array of u16 page
// offsets, one per entry, sorted by name (unsigned bytes, a proper prefix
// sorting first).  Entries are 4-byte aligned; byte 0 is the type (low 3
// bits; 0x80 marks a deleted entry), byte 1 the name length:
//
//	type 1 (interior): +2 u16 child page, +4 name
//	type 2 (object):   +4 uid, +0xC u32 0, +0x10 name
//	type 3 (hard link/node entry): +4 uid, +0xC u32, +0x14 name
//	type 4 (soft link): +2 u16 text length, +4 u16 0xFFFF (text inline),
//	                   +6 6 bytes 0, +0xC name, then the link text
//
// A leaf root page (and an interior root's first entry) carries a sentinel
// entry named "\x00" that sorts before every real name.

const (
	DirPageSize        = 1024
	DirPageHdrSize     = 0x12
	DirRootAreaLen     = 0x70
	DirPageVersion     = 5
	DirKindLeaf        = 0
	DirKindInterior    = 1
	DirEntryDeleted    = 0x80
	DirEntryInterior   = 1
	DirEntryObject     = 2
	DirEntryHardLink   = 3
	DirEntrySoftLink   = 4
	dirIndexBaseRoot   = DirPageHdrSize + DirRootAreaLen
	dirIndexBaseNormal = DirPageHdrSize
)

var dirNameOffset = [8]int{0, 4, 0x10, 0x14, 0xC, 0, 0, 0}

// DirDefProt is one default-protection slot of a root page (dir_$def_prot_t):
// the 44-byte protection image (owner/group/org UIDs, rights, flags) and the
// UID of the ACL it came from.
type DirDefProt struct {
	Prot   [44]byte
	ACLUID uid.UID
}

// DirRootArea is the root page's area between the header and index table.
type DirRootArea struct {
	Word12      uint16 // 0x0100
	AreaLen     uint16 // 0x0070
	Word16      uint16 // 0x0068 (size of the two slots)
	Word18      uint16 // 0
	DirDefProt  DirDefProt
	FileDefProt DirDefProt
}

// NewDirRootArea returns the root area with the constant words filled in.
func NewDirRootArea(dirProt, fileProt DirDefProt) DirRootArea {
	return DirRootArea{Word12: 0x0100, AreaLen: DirRootAreaLen, Word16: 0x0068,
		DirDefProt: dirProt, FileDefProt: fileProt}
}

// DirEnt is one live directory entry.
type DirEnt struct {
	Type     uint8 // DirEntryObject, DirEntryHardLink or DirEntrySoftLink
	Name     []byte
	UID      uid.UID // types 2 and 3
	Extra    uint32  // +0xC longword (types 2, 3)
	LinkText []byte  // type 4
}

func (e DirEnt) String() string {
	switch e.Type {
	case DirEntrySoftLink:
		return fmt.Sprintf("%s -> %s", e.Name, e.LinkText)
	default:
		return fmt.Sprintf("%s (%08x.%08x)", e.Name, e.UID.Hi, e.UID.Lo)
	}
}

func dirEntrySize(typ uint8, nameLen, linkLen int) int {
	size := dirNameOffset[typ&7] + nameLen
	if typ&7 == DirEntrySoftLink {
		size += linkLen
	}
	return (size + 3) &^ 3
}

func (e DirEnt) size() int { return dirEntrySize(e.Type, len(e.Name), len(e.LinkText)) }

func (e DirEnt) encode() []byte {
	b := make([]byte, e.size())
	b[0] = e.Type
	b[1] = byte(len(e.Name))
	switch e.Type {
	case DirEntryObject, DirEntryHardLink:
		binary.BigEndian.PutUint32(b[4:], e.UID.Hi)
		binary.BigEndian.PutUint32(b[8:], e.UID.Lo)
		binary.BigEndian.PutUint32(b[0xC:], e.Extra)
		copy(b[dirNameOffset[e.Type]:], e.Name)
	case DirEntrySoftLink:
		binary.BigEndian.PutUint16(b[2:], uint16(len(e.LinkText)))
		binary.BigEndian.PutUint16(b[4:], 0xFFFF)
		copy(b[0xC:], e.Name)
		copy(b[0xC+len(e.Name):], e.LinkText)
	default:
		panic(fmt.Sprintf("bad dir entry type %d", e.Type))
	}
	return b
}

// CompareDirNames orders names the way dir_$find_entry does.
func CompareDirNames(a, b []byte) int { return bytes.Compare(a, b) }

var dirSentinelName = []byte{0}

// page builder
type dirPage struct {
	buf   [DirPageSize]byte
	base  int // index table start
	iend  int
	heap  int
	count int
}

func newDirPage(kind int, pageNo uint16, stamp uid.UID, root *DirRootArea) *dirPage {
	p := &dirPage{}
	binary.BigEndian.PutUint16(p.buf[0:], uint16(kind<<14)|DirPageVersion)
	binary.BigEndian.PutUint32(p.buf[2:], stamp.Hi)
	binary.BigEndian.PutUint32(p.buf[6:], stamp.Lo)
	binary.BigEndian.PutUint16(p.buf[0xA:], pageNo)
	binary.BigEndian.PutUint16(p.buf[0xC:], 0xFFFF)
	p.base = dirIndexBaseNormal
	if root != nil {
		var w bytes.Buffer
		if err := binary.Write(&w, binary.BigEndian, root); err != nil {
			panic(err)
		}
		copy(p.buf[DirPageHdrSize:], w.Bytes())
		p.base = dirIndexBaseRoot
	}
	p.iend = p.base
	p.heap = DirPageSize
	p.flush()
	return p
}

func (p *dirPage) fits(size int) bool { return p.iend+2 <= p.heap-size }

func (p *dirPage) add(raw []byte) {
	if !p.fits(len(raw)) {
		panic("dir page overflow")
	}
	p.heap -= len(raw)
	copy(p.buf[p.heap:], raw)
	binary.BigEndian.PutUint16(p.buf[p.iend:], uint16(p.heap))
	p.iend += 2
	p.count++
	p.flush()
}

func (p *dirPage) flush() {
	binary.BigEndian.PutUint16(p.buf[0xE:], uint16(p.iend))
	binary.BigEndian.PutUint16(p.buf[0x10:], uint16(p.heap))
}

func (p *dirPage) setNext(n uint16) { binary.BigEndian.PutUint16(p.buf[0xC:], n) }

func sentinelEntry() []byte {
	return DirEnt{Type: DirEntrySoftLink, Name: dirSentinelName}.encode()
}

func interiorEntry(name []byte, child uint16) []byte {
	b := make([]byte, dirEntrySize(DirEntryInterior, len(name), 0))
	b[0] = DirEntryInterior
	b[1] = byte(len(name))
	binary.BigEndian.PutUint16(b[2:], child)
	copy(b[4:], name)
	return b
}

// dirLeafFill is how full EncodeDirectory packs a non-root leaf page, so
// the system can add a few names before it has to split.
const dirLeafFill = DirPageSize * 7 / 8

// DirMaxDepth is the deepest B-tree dir_$insert_entry builds: it refuses
// to split a root that already sits on 8 levels (max_depth == 8 ->
// status_$directory_is_full, domain_os/dir/insert_entry.c 0xE4F55A).
const DirMaxDepth = 8

var ErrDirTooBig = errors.New("directory too large for an 8-level B-tree")

// dirNode is a page of the level being built: the smallest name below it
// and its page number.
type dirNode struct {
	first []byte
	page  *dirPage
}

// EncodeDirectory lays the entries out as directory pages: one leaf root
// page when they fit, otherwise leaf pages 1..n chained through next-page,
// as many levels of interior pages as it takes (numbered after the
// leaves, each level chained like the leaves), and an interior root
// (page 0).  An interior entry names the smallest name of its child, as
// dir_$insert_entry copies the first name of the new child into the
// parent; the first entry of the root is the "\0" sentinel the root split
// writes (dir_$find_entry starts comparing interior pages at entry 2).
// stamp is written into every page of a multi-page directory (the kernel
// writes a fresh UID per split).
func EncodeDirectory(ents []DirEnt, root DirRootArea, stamp uid.UID) ([][DirPageSize]byte, error) {
	sorted := make([]DirEnt, len(ents))
	copy(sorted, ents)
	sort.SliceStable(sorted, func(i, j int) bool { return CompareDirNames(sorted[i].Name, sorted[j].Name) < 0 })
	for i := range sorted {
		if len(sorted[i].Name) == 0 || len(sorted[i].Name) > 255 {
			return nil, fmt.Errorf("bad name %q", sorted[i].Name)
		}
		if i > 0 && bytes.Equal(sorted[i].Name, sorted[i-1].Name) {
			return nil, fmt.Errorf("duplicate name %q", sorted[i].Name)
		}
	}
	raws := make([][]byte, len(sorted))
	for i, e := range sorted {
		raws[i] = e.encode()
	}

	// single leaf root?
	{
		p := newDirPage(DirKindLeaf, 0, uid.Empty, &root)
		ok := true
		p.add(sentinelEntry())
		for _, r := range raws {
			if !p.fits(len(r)) {
				ok = false
				break
			}
			p.add(r)
		}
		if ok {
			return [][DirPageSize]byte{p.buf}, nil
		}
	}

	var all []*dirPage // pages 1.. in page-number order
	full := func(p *dirPage, size int) bool {
		return p.iend+2 > p.heap-size || (DirPageSize-p.heap)+(p.iend-p.base)+size+2 > dirLeafFill
	}
	chain := func(level []dirNode) {
		for i := 0; i+1 < len(level); i++ {
			level[i].page.setNext(binary.BigEndian.Uint16(level[i+1].page.buf[0xA:]))
		}
	}
	newPage := func(kind int) *dirPage {
		p := newDirPage(kind, uint16(len(all)+1), stamp, nil)
		all = append(all, p)
		return p
	}

	// leaves
	var level []dirNode
	for i, r := range raws {
		if len(level) == 0 || full(level[len(level)-1].page, len(r)) {
			level = append(level, dirNode{first: sorted[i].Name, page: newPage(DirKindLeaf)})
		}
		level[len(level)-1].page.add(r)
	}
	chain(level)

	// interior levels until the root page can hold one entry per node
	depth := 2
	for {
		rootPage := newDirPage(DirKindInterior, 0, stamp, &root)
		ok := true
		for i, n := range level {
			name := n.first
			if i == 0 {
				name = dirSentinelName
			}
			r := interiorEntry(name, binary.BigEndian.Uint16(n.page.buf[0xA:]))
			if !rootPage.fits(len(r)) {
				ok = false
				break
			}
			rootPage.add(r)
		}
		if ok {
			pages := [][DirPageSize]byte{rootPage.buf}
			for _, p := range all {
				pages = append(pages, p.buf)
			}
			return pages, nil
		}
		if depth++; depth > DirMaxDepth || len(all) >= 0xFFFF {
			return nil, ErrDirTooBig
		}
		var up []dirNode
		for _, n := range level {
			r := interiorEntry(n.first, binary.BigEndian.Uint16(n.page.buf[0xA:]))
			if len(up) == 0 || full(up[len(up)-1].page, len(r)) {
				up = append(up, dirNode{first: n.first, page: newPage(DirKindInterior)})
			}
			up[len(up)-1].page.add(r)
		}
		chain(up)
		level = up
	}
}

// DirPageInfo is a decoded page header.
type DirPageInfo struct {
	Flags     uint16
	Kind      int
	Stamp     uid.UID
	PageNo    uint16
	Next      uint16
	IndexEnd  uint16
	HeapBase  uint16
	IndexBase int
	Offsets   []uint16
}

// ParseDirPage decodes a page header and its index table.
func ParseDirPage(p []byte) (DirPageInfo, error) {
	if len(p) < DirPageSize {
		return DirPageInfo{}, errors.New("short dir page")
	}
	var info DirPageInfo
	info.Flags = binary.BigEndian.Uint16(p[0:])
	info.Kind = int(p[0] >> 6)
	info.Stamp = uid.UID{Hi: binary.BigEndian.Uint32(p[2:]), Lo: binary.BigEndian.Uint32(p[6:])}
	info.PageNo = binary.BigEndian.Uint16(p[0xA:])
	info.Next = binary.BigEndian.Uint16(p[0xC:])
	info.IndexEnd = binary.BigEndian.Uint16(p[0xE:])
	info.HeapBase = binary.BigEndian.Uint16(p[0x10:])
	info.IndexBase = dirIndexBaseNormal
	if info.PageNo == 0 {
		info.IndexBase = DirPageHdrSize + int(binary.BigEndian.Uint16(p[0x14:]))
	}
	if info.Flags&0x3F == 0 || info.Kind > 1 || int(info.IndexEnd) > DirPageSize || int(info.IndexEnd) < info.IndexBase {
		return info, fmt.Errorf("bad dir page header (flags %04x index %d..%d)", info.Flags, info.IndexBase, info.IndexEnd)
	}
	for o := info.IndexBase; o+2 <= int(info.IndexEnd); o += 2 {
		info.Offsets = append(info.Offsets, binary.BigEndian.Uint16(p[o:]))
	}
	return info, nil
}

// ParseDirEntry decodes the entry at off.  Interior entries come back with
// Type 1 and the child page in Extra.
func ParseDirEntry(p []byte, off int) (DirEnt, bool, error) {
	if off < DirPageHdrSize || off+2 > DirPageSize {
		return DirEnt{}, false, fmt.Errorf("bad entry offset %#x", off)
	}
	t := p[off]
	deleted := t&DirEntryDeleted != 0
	typ := t & 7
	nl := int(p[off+1])
	e := DirEnt{Type: typ}
	no := off + dirNameOffset[typ]
	if typ == 0 || typ > 4 || no+nl > DirPageSize {
		return e, deleted, fmt.Errorf("bad entry at %#x (type %#x)", off, t)
	}
	e.Name = append([]byte(nil), p[no:no+nl]...)
	switch typ {
	case DirEntryInterior:
		e.Extra = uint32(binary.BigEndian.Uint16(p[off+2:]))
	case DirEntryObject, DirEntryHardLink:
		e.UID = uid.UID{Hi: binary.BigEndian.Uint32(p[off+4:]), Lo: binary.BigEndian.Uint32(p[off+8:])}
		e.Extra = binary.BigEndian.Uint32(p[off+0xC:])
	case DirEntrySoftLink:
		ll := int(binary.BigEndian.Uint16(p[off+2:]))
		if binary.BigEndian.Uint16(p[off+4:]) != 0xFFFF {
			return e, deleted, fmt.Errorf("link %q text lives on page %d (not supported)", e.Name, binary.BigEndian.Uint16(p[off+4:]))
		}
		if no+nl+ll > DirPageSize {
			return e, deleted, fmt.Errorf("bad link entry at %#x", off)
		}
		e.LinkText = append([]byte(nil), p[no+nl:no+nl+ll]...)
	}
	return e, deleted, nil
}

// DecodeDirectory returns the live entries reachable from the root page
// (sentinel and deleted entries dropped) and the root area of page 0.
// Pages the B-tree no longer references (left over from splits) are
// ignored, as dir_$find_entry ignores them.
func DecodeDirectory(pages [][]byte) ([]DirEnt, DirRootArea, error) {
	var root DirRootArea
	if len(pages) == 0 || pages[0] == nil {
		return nil, root, errors.New("empty directory")
	}
	if err := binary.Read(bytes.NewReader(pages[0][DirPageHdrSize:]), binary.BigEndian, &root); err != nil {
		return nil, root, err
	}
	var out []DirEnt
	visited := map[int]bool{}
	var walk func(n, depth int) error
	walk = func(n, depth int) error {
		if n >= len(pages) || pages[n] == nil {
			return fmt.Errorf("directory references missing page %d", n)
		}
		if visited[n] || depth > 16 {
			return fmt.Errorf("directory page %d referenced twice", n)
		}
		visited[n] = true
		p := pages[n]
		info, err := ParseDirPage(p)
		if err != nil {
			return fmt.Errorf("page %d: %w", n, err)
		}
		if n != 0 && int(info.PageNo) != n {
			return fmt.Errorf("page %d says it is page %d", n, info.PageNo)
		}
		for _, off := range info.Offsets {
			e, deleted, err := ParseDirEntry(p, int(off))
			if err != nil {
				return fmt.Errorf("page %d: %w", n, err)
			}
			if deleted {
				continue
			}
			if info.Kind == DirKindInterior {
				if e.Type != DirEntryInterior {
					return fmt.Errorf("page %d: entry type %d on an interior page", n, e.Type)
				}
				if err := walk(int(e.Extra), depth+1); err != nil {
					return err
				}
				continue
			}
			if bytes.Equal(e.Name, dirSentinelName) {
				continue
			}
			out = append(out, e)
		}
		return nil
	}
	if err := walk(0, 0); err != nil {
		return nil, root, err
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareDirNames(out[i].Name, out[j].Name) < 0 })
	return out, root, nil
}

package fs

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/project-phoebus/apollofs/pkg/uid"
)

func pageBytes(p [DirPageSize]byte) []byte { return p[:] }

// Bytes taken from the "/" root page of an SR10.3 volume.
func TestDirEntryEncoding(t *testing.T) {
	sau2 := DirEnt{Type: DirEntryObject, Name: []byte("sau2"), UID: uid.UID{Hi: 0x6c972c45, Lo: 0xd0012345}}
	require.Equal(t, "020400006c972c45d00123450000000073617532", hex.EncodeToString(sau2.encode()))
	tmp := DirEnt{Type: DirEntrySoftLink, Name: []byte("tmp"), LinkText: []byte("`node_data/tmp")}
	require.Equal(t, "0403000effff000000000000746d70606e6f64655f646174612f746d70000000", hex.EncodeToString(tmp.encode()))
	require.Equal(t, "04010000ffff0000000000000000000"+"0", hex.EncodeToString(sentinelEntry()))
	node := DirEnt{Type: DirEntryHardLink, Name: []byte("node_1"), UID: uid.UID{Hi: 0x6c95ba45, Lo: 0x40012345}}
	require.Equal(t, "030600006c95ba454001234500000000000000006e6f64655f310000", hex.EncodeToString(node.encode()))
}

func TestDirSingleLeafRoot(t *testing.T) {
	ents := []DirEnt{
		{Type: DirEntryObject, Name: []byte("sau2"), UID: uid.UID{Hi: 1, Lo: 2}},
		{Type: DirEntrySoftLink, Name: []byte("bin"), LinkText: []byte("$(SYSTYPE)/bin")},
		{Type: DirEntryObject, Name: []byte("lib"), UID: uid.UID{Hi: 3, Lo: 4}},
	}
	root := NewDirRootArea(DirDefProt{}, DirDefProt{})
	pages, err := EncodeDirectory(ents, root, uid.UID{Hi: 9, Lo: 9})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	p := pageBytes(pages[0])
	// header: leaf version 5, no stamp, page 0, no next
	require.Equal(t, "000500000000000000000000ffff", hex.EncodeToString(p[:0x0e]))
	require.Equal(t, "0100007000680000", hex.EncodeToString(p[0x12:0x1a]))
	info, err := ParseDirPage(p)
	require.NoError(t, err)
	require.Equal(t, 0x82, info.IndexBase)
	require.Len(t, info.Offsets, 4)
	require.Equal(t, uint16(0x3f0), info.Offsets[0]) // sentinel first, at the top
	got, _, err := DecodeDirectory([][]byte{p})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "bin", string(got[0].Name))
	require.Equal(t, "$(SYSTYPE)/bin", string(got[0].LinkText))
	require.Equal(t, "lib", string(got[1].Name))
	require.Equal(t, "sau2", string(got[2].Name))
}

func TestDirMultiPage(t *testing.T) {
	var ents []DirEnt
	for i := 0; i < 400; i++ {
		ents = append(ents, DirEnt{Type: DirEntryObject, Name: []byte(fmt.Sprintf("file_%04d_with_a_long_name", i)), UID: uid.UID{Hi: uint32(i), Lo: 7}})
	}
	stamp := uid.UID{Hi: 0x11, Lo: 0x22}
	pages, err := EncodeDirectory(ents, NewDirRootArea(DirDefProt{}, DirDefProt{}), stamp)
	require.NoError(t, err)
	require.Greater(t, len(pages), 2)
	var raw [][]byte
	for i := range pages {
		raw = append(raw, pageBytes(pages[i]))
	}
	root, err := ParseDirPage(raw[0])
	require.NoError(t, err)
	require.Equal(t, DirKindInterior, root.Kind)
	require.Equal(t, uint16(0x4005), root.Flags)
	require.Equal(t, stamp, root.Stamp)
	require.Len(t, root.Offsets, len(pages)-1)
	first, _, err := ParseDirEntry(raw[0], int(root.Offsets[0]))
	require.NoError(t, err)
	require.Equal(t, []byte{0}, first.Name)
	require.Equal(t, uint32(1), first.Extra)
	// leaves chain 1 -> 2 -> ... -> 0xffff and each child key is its first name
	for i := 1; i < len(pages); i++ {
		li, err := ParseDirPage(raw[i])
		require.NoError(t, err)
		require.Equal(t, uint16(i), li.PageNo)
		if i == len(pages)-1 {
			require.Equal(t, uint16(0xffff), li.Next)
		} else {
			require.Equal(t, uint16(i+1), li.Next)
		}
		if i > 1 {
			key, _, _ := ParseDirEntry(raw[0], int(root.Offsets[i-1]))
			fe, _, _ := ParseDirEntry(raw[i], int(li.Offsets[0]))
			require.Equal(t, fe.Name, key.Name)
		}
		require.Equal(t, uint16(binary.BigEndian.Uint16(raw[i][0x10:])), li.HeapBase)
	}
	got, _, err := DecodeDirectory(raw)
	require.NoError(t, err)
	require.Len(t, got, 400)
	for i := range got {
		require.Equal(t, fmt.Sprintf("file_%04d_with_a_long_name", i), string(got[i].Name))
	}
}

func TestDirDuplicateName(t *testing.T) {
	ents := []DirEnt{{Type: DirEntryObject, Name: []byte("a")}, {Type: DirEntryObject, Name: []byte("a")}}
	_, err := EncodeDirectory(ents, DirRootArea{}, uid.Empty)
	require.Error(t, err)
}

// Pages a split left behind are not part of the directory.
func TestDirDecodeIgnoresUnreferencedPages(t *testing.T) {
	var ents []DirEnt
	for i := 0; i < 200; i++ {
		ents = append(ents, DirEnt{Type: DirEntryObject, Name: []byte(fmt.Sprintf("n%03d_padding_padding", i))})
	}
	pages, err := EncodeDirectory(ents, NewDirRootArea(DirDefProt{}, DirDefProt{}), uid.UID{Hi: 1})
	require.NoError(t, err)
	var raw [][]byte
	for i := range pages {
		raw = append(raw, pageBytes(pages[i]))
	}
	stray := append([]byte(nil), raw[1]...)
	binary.BigEndian.PutUint16(stray[0xA:], uint16(len(raw)))
	raw = append(raw, stray)
	got, _, err := DecodeDirectory(raw)
	require.NoError(t, err)
	require.Len(t, got, 200)
}

// kernelFind mirrors dir_$find_entry (domain_os/dir/find_entry.c): binary
// search each page by unsigned bytes, a proper prefix first; interior
// pages from entry 2 (falling back to the entry before the first larger
// name), the root page indexing after its root area and every other page
// from 0x12.  It returns the leaf entry found and the depth descended.
func kernelFind(t *testing.T, pages [][DirPageSize]byte, name []byte) (DirEnt, bool, int) {
	p := pages[0][:]
	base := 0x12 + int(binary.BigEndian.Uint16(p[0x14:]))
	depth := 0
	for {
		depth++
		n := (int(binary.BigEndian.Uint16(p[0xE:])) - base) / 2
		off := func(i int) int { return int(binary.BigEndian.Uint16(p[base+2*i-2:])) }
		interior := p[0]>>6 == 1
		lo, hi := 1, n
		if interior {
			lo = 2
		}
		mid, cmp := lo, -1
		found := false
		for lo <= hi {
			mid = (lo + hi) / 2
			e, _, err := ParseDirEntry(p, off(mid))
			require.NoError(t, err)
			cmp = CompareDirNames(name, e.Name)
			if cmp == 0 {
				found = true
				break
			}
			if cmp < 0 {
				hi = mid - 1
			} else {
				lo = mid + 1
			}
		}
		if !found && cmp < 0 {
			mid--
		}
		e, _, err := ParseDirEntry(p, off(mid))
		require.NoError(t, err)
		if !interior {
			return e, found, depth
		}
		p = pages[e.Extra][:]
		base = 0x12
	}
}

// A directory too big for a two-level tree gets more interior levels, as
// dir_$insert_entry splits interior pages (up to 8 levels); every name is
// then found by the kernel's search and the leaf chain lists them in order.
func TestDirMultiLevel(t *testing.T) {
	var ents []DirEnt
	for i := 0; i < 2000; i++ {
		ents = append(ents, DirEnt{Type: DirEntryObject, Name: []byte(fmt.Sprintf("a_rather_long_file_name_%04d", 1999-i)), UID: uid.UID{Hi: uint32(i + 1), Lo: 7}})
	}
	ents = append(ents, DirEnt{Type: DirEntrySoftLink, Name: []byte("A"), LinkText: []byte("x")},
		DirEnt{Type: DirEntryObject, Name: []byte("a"), UID: uid.UID{Hi: 9, Lo: 9}},
		DirEnt{Type: DirEntryObject, Name: []byte("a_rather"), UID: uid.UID{Hi: 9, Lo: 10}})
	pages, err := EncodeDirectory(ents, NewDirRootArea(DirDefProt{}, DirDefProt{}), uid.UID{Hi: 5, Lo: 6})
	require.NoError(t, err)
	maxDepth := 0
	for _, want := range ents {
		e, found, depth := kernelFind(t, pages, want.Name)
		require.True(t, found, "%q", want.Name)
		require.Equal(t, want.UID, e.UID)
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	require.Equal(t, 3, maxDepth)
	_, found, _ := kernelFind(t, pages, []byte("a_rather_long_file_name_0500x"))
	require.False(t, found)
	var raw [][]byte
	for i := range pages {
		raw = append(raw, pageBytes(pages[i]))
	}
	got, _, err := DecodeDirectory(raw)
	require.NoError(t, err)
	require.Len(t, got, len(ents))
	require.Equal(t, "A", string(got[0].Name))
	require.Equal(t, "a", string(got[1].Name))
	require.Equal(t, "a_rather", string(got[2].Name))
	require.Equal(t, "a_rather_long_file_name_0000", string(got[3].Name))
	// leaves chain 1 -> 2 -> ... in name order and end with 0xFFFF
	var chained []string
	for n := 1; n != 0xFFFF; {
		info, err := ParseDirPage(raw[n])
		require.NoError(t, err)
		require.Equal(t, DirKindLeaf, info.Kind)
		for _, o := range info.Offsets {
			e, _, _ := ParseDirEntry(raw[n], int(o))
			chained = append(chained, string(e.Name))
		}
		n = int(info.Next)
	}
	require.Len(t, chained, len(ents))
	require.Equal(t, "a_rather_long_file_name_1999", chained[len(chained)-1])
}

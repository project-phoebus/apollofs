package managers

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

// CheckReport is what Check found.
type CheckReport struct {
	Objects, Dirs, Links int
	ExtraNames           int // objects catalogued under more than one name
	BlocksInUse          int
	Problems             []string
}

func (r *CheckReport) problem(f string, a ...any) {
	r.Problems = append(r.Problems, fmt.Sprintf(f, a...))
}

// Check walks the volume from "/" and "//" and cross-checks the VTOC
// buckets, every VTOCE's file map and block headers, directory pages and
// parent pointers, and the BAT bitmap, partition counts and free count.
func (v *Volume) Check() (*CheckReport, error) {
	r := &CheckReport{}
	owner := map[fs.DAddr]string{}
	claim := func(d fs.DAddr, who string) {
		if prev, ok := owner[d]; ok {
			r.problem("block %d used by both %s and %s", d, prev, who)
			return
		}
		owner[d] = who
	}
	l := &v.LV.Label
	bat := &l.BATHeader
	claim(fs.DAddr(v.PV.Label.AltLVLabelDAddr[0])-v.LV.StartDAddr(), "alternate LV label")
	for i := 0; i < v.BAT.NumBATBlocks(); i++ {
		claim(bat.FirstBATBlockDAddr+fs.DAddr(i), "BAT")
	}

	// bucket area and overflow chains: collect (uid -> vtocx)
	slots := map[uid.UID]fs.VTOCX{}
	vtoceBlocks := map[fs.DAddr]bool{}
	visitBkt := map[fs.DAddr]bool{}
	var bucketBlocks []fs.DAddr
	for _, ext := range v.LV.VTOCMap {
		for i := 0; i < int(ext.NumBlocks); i++ {
			bucketBlocks = append(bucketBlocks, ext.FirstBlockDAddr+fs.DAddr(i))
		}
	}
	for len(bucketBlocks) > 0 {
		d := bucketBlocks[0]
		bucketBlocks = bucketBlocks[1:]
		if visitBkt[d] {
			continue
		}
		visitBkt[d] = true
		claim(d, "VTOC bucket block")
		b, err := v.LV.ReadBlock(d)
		if err != nil {
			return nil, err
		}
		if b.Header.ObjectUID != uid.UIDvtoc_bkt || binary.BigEndian.Uint32(b.Data[0x3F8:]) != bucketBlockMagic || fs.DAddr(binary.BigEndian.Uint32(b.Data[0x3FC:])) != d {
			r.problem("bucket block %d: bad header/magic/self", d)
		}
		for k := 0; k < bucketsPerBlock; k++ {
			if nb := fs.DAddr(binary.BigEndian.Uint32(b.Data[k*bucketSize:])); nb != 0 {
				bucketBlocks = append(bucketBlocks, nb)
			}
			for s := 0; s < bucketSlots; s++ {
				o := slotOff(k, s)
				x := fs.VTOCX(binary.BigEndian.Uint32(b.Data[o+8:]))
				if x == 0 {
					continue
				}
				u := uid.UID{Hi: binary.BigEndian.Uint32(b.Data[o:]), Lo: binary.BigEndian.Uint32(b.Data[o+4:])}
				if _, dup := slots[u]; dup {
					r.problem("UID %v indexed twice", u)
				}
				slots[u] = x
				vtoceBlocks[x.BlockDAddr()] = true
			}
		}
	}
	for i := 0; i < int(l.NumPartitions); i++ {
		d := fs.DAddr(l.Partitions[i].StatusVTOCE & 0xFFFFFF)
		chained := map[fs.DAddr]bool{}
		for hops := 0; d != 0 && hops < 100000 && !chained[d]; hops++ {
			chained[d] = true
			vtoceBlocks[d] = true
			b, err := v.LV.ReadBlock(d)
			if err != nil {
				return nil, err
			}
			d = fs.DAddr(binary.BigEndian.Uint32(b.Data[0:]))
		}
	}
	for d := range vtoceBlocks {
		claim(d, "VTOCE block")
		b, err := v.LV.ReadBlock(d)
		if err != nil {
			return nil, err
		}
		if b.Header.ObjectUID != uid.UIDvtoc || binary.BigEndian.Uint32(b.Data[0x3F8:]) != vtoceBlockMagic || fs.DAddr(binary.BigEndian.Uint32(b.Data[0x3FC:])) != d {
			r.problem("VTOCE block %d: bad header/magic/self", d)
		}
		inUse := 0
		for k := 0; k < vtocesPerBlock; k++ {
			if binary.BigEndian.Uint16(b.Data[vtoceBlockEntryOff+k*vtoceSize+2:])&fs.VTOCEFlagInUse != 0 {
				inUse++
			}
		}
		if c := int(binary.BigEndian.Uint16(b.Data[4:])); c != inUse {
			r.problem("VTOCE block %d: count %d but %d entries in use", d, c, inUse)
		}
	}

	seen := map[uid.UID]bool{}
	var walk func(u uid.UID, parent uid.UID, p string, depth int)
	checkObject := func(u uid.UID, parent uid.UID, p string) *fs.VTOCE {
		x, ok := slots[u]
		if !ok {
			r.problem("%s: UID %v has no VTOC bucket entry", p, u)
			return nil
		}
		e, err := v.VTOC.GetEntry(x)
		if err != nil {
			r.problem("%s: %v", p, err)
			return nil
		}
		if e.Header.ObjectUID != u || e.Header.Flags&fs.VTOCEFlagInUse == 0 {
			r.problem("%s: VTOCE %v holds %v (flags %04x)", p, x, e.Header.ObjectUID, e.Header.Flags)
			return nil
		}
		if seen[u] {
			// Domain/OS lets one object have several names (an installer
			// catalogs authorized-area objects again under the node's
			// tree); only the first name owns the blocks
			if e.IsDirectory() {
				r.problem("%s: directory reached twice", p)
			}
			r.ExtraNames++
			return nil
		}
		if parent != uid.Empty && e.Header.DirectoryUID != parent {
			r.problem("%s: parent pointer %v, catalogued in %v", p, e.Header.DirectoryUID, parent)
		}
		seen[u] = true
		m, fmb, err := v.VTOC.FileMap(e)
		if err != nil {
			r.problem("%s: file map: %v", p, err)
			return e
		}
		pages := (int(e.Header.CurrentLength) + 1023) / 1024
		used := len(fmb)
		for i, d := range m {
			if d == 0 {
				continue
			}
			used++
			if d < FirstDataBlock {
				if e.Header.ObjectTypeDefUID != uid.UIDsysboot {
					r.problem("%s: page %d in reserved block %d", p, i, d)
				}
			} else {
				claim(d, p)
			}
			b, err := v.LV.ReadBlock(d)
			if err != nil {
				r.problem("%s: %v", p, err)
				continue
			}
			if b.Header.ObjectUID != u || b.Header.PageWithinObject != int32(i) {
				r.problem("%s: block %d header %v/%d, expected page %d", p, d, b.Header.ObjectUID, b.Header.PageWithinObject, i)
			}
		}
		for _, d := range fmb {
			claim(d, p+" (file map)")
		}
		if len(m) < pages {
			r.problem("%s: map covers %d of %d pages", p, len(m), pages)
		}
		if int(e.Header.BlocksUsed) != used {
			r.problem("%s: blocks used %d, map has %d", p, e.Header.BlocksUsed, used)
		}
		r.Objects++
		return e
	}
	_ = checkObject
	walk = func(u uid.UID, parent uid.UID, p string, depth int) {
		e := checkObject(u, parent, p)
		if e == nil || !e.IsDirectory() {
			return
		}
		r.Dirs++
		d, err := v.Names.ReadDir(u)
		if err != nil {
			r.problem("%s: %v", p, err)
			return
		}
		for _, ent := range d.Entries {
			name := p + "/" + string(ent.Name)
			if p == "/" {
				name = "/" + string(ent.Name)
			}
			switch ent.Type {
			case fs.DirEntrySoftLink:
				r.Links++
			case fs.DirEntryObject:
				if depth > 64 {
					r.problem("%s: too deep", name)
					continue
				}
				walk(ent.UID, u, name, depth+1)
			case fs.DirEntryHardLink:
				if !seen[ent.UID] {
					walk(ent.UID, uid.Empty, name, depth+1)
				}
			}
		}
	}
	root, err := v.Names.RootUID()
	if err != nil {
		return nil, err
	}
	walk(root, uid.Empty, "/", 0)
	if net, err := v.Names.NetRootUID(); err == nil {
		walk(net, uid.Empty, "//", 0)
	} else {
		r.problem("network root: %v", err)
	}
	if x := l.VTOCHeader.SysbootVTOCX; x != 0 {
		if e, err := v.VTOC.GetEntry(x); err != nil || e.Header.ObjectTypeDefUID != uid.UIDsysboot {
			r.problem("sysboot VTOCX %v does not name a sysboot object", x)
		}
	}
	var orphans []string
	for u := range slots {
		if !seen[u] {
			orphans = append(orphans, u.String())
		}
	}
	sort.Strings(orphans)
	for _, o := range orphans {
		r.problem("VTOCE for %s is not reachable from / or //", o)
	}

	// BAT
	first := bat.BlockNumOfFirstBATBit
	last := first + fs.DAddr(bat.NumBlocksRepresented)
	partFree := make([]int, l.NumPartitions)
	free := 0
	for d := first; d < last; d++ {
		_, used := owner[d]
		isFree := v.BAT.IsFree(d)
		if used && isFree {
			r.problem("block %d (%s) is marked free", d, owner[d])
		}
		if !used && !isFree {
			r.problem("block %d is allocated but unowned", d)
		}
		if isFree {
			free++
			if i := v.BAT.PartitionIndex(d); i >= 0 {
				partFree[i]++
			}
		}
	}
	for d := range owner {
		if d >= last {
			r.problem("block %d (%s) past the end of the BAT", d, owner[d])
		}
	}
	if free != int(bat.NumFreeBlocks) {
		r.problem("BAT header says %d free, bitmap has %d", bat.NumFreeBlocks, free)
	}
	for i := range partFree {
		if partFree[i] != int(l.Partitions[i].FreeCount) {
			r.problem("partition %d: free count %d, bitmap has %d", i, l.Partitions[i].FreeCount, partFree[i])
		}
	}
	r.BlocksInUse = len(owner)
	return r, nil
}

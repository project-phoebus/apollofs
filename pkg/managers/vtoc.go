package managers

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
	"golang.org/x/exp/slices"
)

// On-disk VTOC layout of a new-format (SR10) volume:
//
// bucket blocks (tagged vtoc_bkt_$uid): four 0xF8-byte buckets, each a
// {next block u32, next bucket u16, pad u16} chain link and twenty
// {uid, VTOCX} slots; the block ends with magic 0xFEDCA985 and its own
// daddr.  UID hashes pick a bucket in the contiguous bucket-block area
// named by the VTOC map; full buckets chain to overflow buckets handed out
// from the header's CurBktBlock/CurBktIdx (VTOC_$ALLOCATE).
//
// VTOCE blocks (tagged vtoc_$uid): {next u32, count u16, pad u16}, three
// 0x150-byte VTOCEs at +8, magic 0xFEDCA984 and own daddr at the end.  Each
// BAT partition remembers the VTOCE block it is filling (BAT_$ALLOC_VTOCE).
const (
	bucketSize         = 0xF8
	bucketsPerBlock    = 4
	bucketSlots        = 20
	bucketBlockMagic   = 0xFEDCA985
	vtoceBlockMagic    = 0xFEDCA984
	vtoceSize          = 0x150
	vtocesPerBlock     = 3
	vtoceBlockEntryOff = 8
)

var errNotFound = errors.New("not found")

// ErrNotFound is returned for missing names and UIDs.
var ErrNotFound = errNotFound

type VTOCManager struct {
	lvol *fs.LogicalVolume
	bat  *BATManager
	now  func() uint32
}

func NewVTOCManager(lvol *fs.LogicalVolume, bat *BATManager) *VTOCManager {
	return &VTOCManager{lvol: lvol, bat: bat, now: func() uint32 { return 0 }}
}

func (vm *VTOCManager) Mount() error { return nil }

func (vm *VTOCManager) hashUID(u uid.UID) uint32 {
	vtocVersion := vm.lvol.Label.VTOCHeader.Version
	vtocSize := uint32(uint16(vm.lvol.Label.VTOCHeader.VTOCSizeInBlocks))

	switch {
	case vtocVersion < 2:
		// UID_$HASH (domain_os/uid/hash.c): fold hi^lo to 16 bits, then
		// divu.w by the table size; the remainder is the hash.
		tmp := u.Hi ^ u.Lo
		return uint32(uint16(tmp>>16)^uint16(tmp)) % vtocSize
	case vtocVersion == 2:
		x := u.Hi * 2
		if int32(u.Lo) < 0 {
			x = x + 1
		}
		return (x>>0x10 ^ x&0xffff) % vtocSize
	case vtocVersion == 3:
		return uint32(uint16(u.Hi>>0x10)^uint16(u.Hi)) % vtocSize
	default:
		panic("unhandled vtoc version")
	}
}

// bucketFor returns the bucket block and bucket index a UID hashes to.
func (vm *VTOCManager) bucketFor(u uid.UID) (fs.DAddr, int, error) {
	h := vm.hashUID(u)
	idx := h >> 2
	for _, ext := range vm.lvol.VTOCMap {
		if idx < uint32(ext.NumBlocks) {
			return ext.FirstBlockDAddr + fs.DAddr(idx), int(h & 3), nil
		}
		idx -= uint32(ext.NumBlocks)
	}
	return 0, 0, fmt.Errorf("hash %d past the VTOC map", h)
}

func (vm *VTOCManager) header(objUID uid.UID, d fs.DAddr) fs.BlockHeader {
	return fs.BlockHeader{ObjectUID: objUID, PageWithinObject: int32(d), LastWritten: vm.now(),
		PVBlockDAddr: d + vm.lvol.StartDAddr()}
}

type bucketSlot struct {
	block fs.DAddr
	bkt   int
	slot  int
}

func slotOff(bkt, slot int) int { return bkt*bucketSize + 8 + slot*12 }

// walkBuckets visits every slot of the chain starting at u's bucket.
func (vm *VTOCManager) walkBuckets(u uid.UID, fn func(b *fs.Block, s bucketSlot, slotUID uid.UID, x fs.VTOCX) (bool, error)) error {
	blk, bkt, err := vm.bucketFor(u)
	if err != nil {
		return err
	}
	for hops := 0; blk != 0; hops++ {
		if hops > 100000 {
			return errors.New("VTOC bucket chain loops")
		}
		b, err := vm.lvol.ReadBlock(blk)
		if err != nil {
			return err
		}
		for k := 0; k < bucketSlots; k++ {
			o := slotOff(bkt, k)
			su := uid.UID{Hi: binary.BigEndian.Uint32(b.Data[o:]), Lo: binary.BigEndian.Uint32(b.Data[o+4:])}
			x := fs.VTOCX(binary.BigEndian.Uint32(b.Data[o+8:]))
			stop, err := fn(b, bucketSlot{blk, bkt, k}, su, x)
			if err != nil || stop {
				return err
			}
		}
		o := bkt * bucketSize
		blk = fs.DAddr(binary.BigEndian.Uint32(b.Data[o:]))
		bkt = int(binary.BigEndian.Uint16(b.Data[o+4:]))
	}
	return nil
}

// GetIndexForUID returns the VTOCX of an object.
func (vm *VTOCManager) GetIndexForUID(u uid.UID) (fs.VTOCX, error) {
	var found fs.VTOCX
	err := vm.walkBuckets(u, func(_ *fs.Block, _ bucketSlot, su uid.UID, x fs.VTOCX) (bool, error) {
		if x != 0 && su == u {
			found = x
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return 0, err
	}
	if found == 0 {
		return 0, errNotFound
	}
	return found, nil
}

// GetEntry reads the VTOCE at x.
func (vm *VTOCManager) GetEntry(x fs.VTOCX) (*fs.VTOCE, error) {
	if x.Index() >= vtocesPerBlock {
		return nil, fmt.Errorf("bad VTOCX %v", x)
	}
	b, err := vm.lvol.ReadBlock(x.BlockDAddr())
	if err != nil {
		return nil, err
	}
	o := vtoceBlockEntryOff + x.Index()*vtoceSize
	return fs.VTOCEFromBytes(b.Data[o : o+vtoceSize])
}

func (vm *VTOCManager) GetEntryForUID(u uid.UID) (*fs.VTOCE, error) {
	x, err := vm.GetIndexForUID(u)
	if err != nil {
		return nil, err
	}
	return vm.GetEntry(x)
}

// PutEntry writes the VTOCE at x.
func (vm *VTOCManager) PutEntry(x fs.VTOCX, e *fs.VTOCE) error {
	b, err := vm.lvol.ReadBlock(x.BlockDAddr())
	if err != nil {
		return err
	}
	o := vtoceBlockEntryOff + x.Index()*vtoceSize
	copy(b.Data[o:o+vtoceSize], e.Bytes())
	b.Header.LastWritten = vm.now()
	return vm.lvol.WriteBlock(x.BlockDAddr(), b)
}

// InitBucketBlock writes an empty bucket block at d.
func (vm *VTOCManager) InitBucketBlock(d fs.DAddr) error {
	var b fs.Block
	b.Header = vm.header(uid.UIDvtoc_bkt, d)
	binary.BigEndian.PutUint32(b.Data[0x3F8:], bucketBlockMagic)
	binary.BigEndian.PutUint32(b.Data[0x3FC:], uint32(d))
	return vm.lvol.WriteBlock(d, &b)
}

// insertBucket records (u, x) in u's bucket chain, extending it with an
// overflow bucket when every slot is taken.
func (vm *VTOCManager) insertBucket(u uid.UID, x fs.VTOCX) error {
	blk, bkt, err := vm.bucketFor(u)
	if err != nil {
		return err
	}
	vh := &vm.lvol.Label.VTOCHeader
	for {
		b, err := vm.lvol.ReadBlock(blk)
		if err != nil {
			return err
		}
		for k := 0; k < bucketSlots; k++ {
			o := slotOff(bkt, k)
			if binary.BigEndian.Uint32(b.Data[o+8:]) == 0 {
				binary.BigEndian.PutUint32(b.Data[o:], u.Hi)
				binary.BigEndian.PutUint32(b.Data[o+4:], u.Lo)
				binary.BigEndian.PutUint32(b.Data[o+8:], uint32(x))
				b.Header.LastWritten = vm.now()
				return vm.lvol.WriteBlock(blk, b)
			}
		}
		o := bkt * bucketSize
		next := fs.DAddr(binary.BigEndian.Uint32(b.Data[o:]))
		nextBkt := int(binary.BigEndian.Uint16(b.Data[o+4:]))
		if next == 0 {
			if vh.CurBktBlock == 0 {
				nb, err := vm.bat.Allocate(blk)
				if err != nil {
					return err
				}
				if err := vm.InitBucketBlock(nb); err != nil {
					return err
				}
				vh.CurBktBlock = uint32(nb)
				vh.CurBktIdx = 0
				vh.VTOCBlocksUsed++
			}
			next = fs.DAddr(vh.CurBktBlock)
			nextBkt = int(vh.CurBktIdx)
			vh.CurBktIdx++
			if vh.CurBktIdx == bucketsPerBlock {
				vh.CurBktBlock = 0
			}
			binary.BigEndian.PutUint32(b.Data[o:], uint32(next))
			binary.BigEndian.PutUint16(b.Data[o+4:], uint16(nextBkt))
			if err := vm.lvol.WriteBlock(blk, b); err != nil {
				return err
			}
		}
		blk, bkt = next, nextBkt
	}
}

// allocVTOCESlot finds a free VTOCE slot the way BAT_$ALLOC_VTOCE picks
// a block: the hint's partition if it still has 1/8 of its blocks free,
// else a VTOCE (status 2) partition, else the emptiest one.
func (vm *VTOCManager) allocVTOCESlot(hint fs.DAddr) (fs.VTOCX, error) {
	l := &vm.lvol.Label
	part := -1
	if l.NumPartitions > 0 {
		threshold := l.PartitionSize >> 3
		if hint != 0 {
			if p := vm.bat.PartitionIndex(hint); p >= 0 && l.Partitions[p].FreeCount >= threshold {
				part = p
			}
		}
		if part < 0 {
			n := int(l.NumPartitions)
			idx := n >> 1
			best := int64(-1)
			dir := 1
			for remaining := n - 1; remaining >= 0; remaining-- {
				if idx >= 0 && idx < n {
					p := l.Partitions[idx]
					if p.StatusVTOCE>>24 == 2 && p.FreeCount > threshold {
						part = idx
						break
					}
					if int64(p.FreeCount) > best {
						part, best = idx, int64(p.FreeCount)
					}
				}
				if dir&1 != 0 {
					idx -= dir
				} else {
					idx += dir
				}
				dir++
			}
			if part > 0 {
				hint = fs.DAddr(uint32(part)*l.PartitionSize + uint32(l.PartitionStartOffset))
			} else {
				hint = 0
			}
		}
	}
	var blk fs.DAddr
	if part >= 0 {
		blk = fs.DAddr(l.Partitions[part].StatusVTOCE & 0xFFFFFF)
	}
	var b *fs.Block
	if blk == 0 {
		nb, err := vm.bat.Allocate(hint)
		if err != nil {
			return 0, err
		}
		blk = nb
		b = &fs.Block{Header: vm.header(uid.UIDvtoc, blk)}
		binary.BigEndian.PutUint32(b.Data[0x3F8:], vtoceBlockMagic)
		binary.BigEndian.PutUint32(b.Data[0x3FC:], uint32(blk))
		if part >= 0 {
			l.Partitions[part].StatusVTOCE = l.Partitions[part].StatusVTOCE&0xFF000000 | uint32(blk)
		}
	} else {
		var err error
		if b, err = vm.lvol.ReadBlock(blk); err != nil {
			return 0, err
		}
	}
	idx := -1
	for k := 0; k < vtocesPerBlock; k++ {
		o := vtoceBlockEntryOff + k*vtoceSize
		if binary.BigEndian.Uint16(b.Data[o+2:])&fs.VTOCEFlagInUse == 0 {
			idx = k
			break
		}
	}
	if idx < 0 {
		return 0, fmt.Errorf("VTOCE block %d has no free entry", blk)
	}
	count := binary.BigEndian.Uint16(b.Data[4:]) + 1
	binary.BigEndian.PutUint16(b.Data[4:], count)
	if count >= vtocesPerBlock && part >= 0 {
		l.Partitions[part].StatusVTOCE = l.Partitions[part].StatusVTOCE&0xFF000000 | binary.BigEndian.Uint32(b.Data[0:])
	}
	// reserve the slot so a second allocation before the caller writes it
	// does not hand it out again
	o := vtoceBlockEntryOff + idx*vtoceSize
	binary.BigEndian.PutUint16(b.Data[o+2:], fs.VTOCEFlagInUse)
	if err := vm.lvol.WriteBlock(blk, b); err != nil {
		return 0, err
	}
	return fs.NewVTOCX(blk, idx), nil
}

// AllocateEntry stores a new VTOCE for e.Header.ObjectUID near hint and
// indexes it in the bucket chain.
func (vm *VTOCManager) AllocateEntry(e *fs.VTOCE, hint fs.DAddr) (fs.VTOCX, error) {
	if _, err := vm.GetIndexForUID(e.Header.ObjectUID); err == nil {
		return 0, fmt.Errorf("duplicate UID %v", e.Header.ObjectUID)
	}
	x, err := vm.allocVTOCESlot(hint)
	if err != nil {
		return 0, err
	}
	e.Header.Version = 1
	e.Header.Flags |= fs.VTOCEFlagInUse
	if err := vm.PutEntry(x, e); err != nil {
		return 0, err
	}
	if err := vm.insertBucket(e.Header.ObjectUID, x); err != nil {
		return 0, err
	}
	return x, nil
}

// FreeEntry drops an object's bucket slot and clears its VTOCE.
func (vm *VTOCManager) FreeEntry(u uid.UID) error {
	var x fs.VTOCX
	err := vm.walkBuckets(u, func(b *fs.Block, s bucketSlot, su uid.UID, sx fs.VTOCX) (bool, error) {
		if sx != 0 && su == u {
			x = sx
			o := slotOff(s.bkt, s.slot)
			for i := 0; i < 12; i++ {
				b.Data[o+i] = 0
			}
			return true, vm.lvol.WriteBlock(s.block, b)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if x == 0 {
		return errNotFound
	}
	b, err := vm.lvol.ReadBlock(x.BlockDAddr())
	if err != nil {
		return err
	}
	o := vtoceBlockEntryOff + x.Index()*vtoceSize
	for i := 0; i < vtoceSize; i++ {
		b.Data[o+i] = 0
	}
	if c := binary.BigEndian.Uint16(b.Data[4:]); c > 0 {
		binary.BigEndian.PutUint16(b.Data[4:], c-1)
		// VTOCE_$TRUNCATE: a block that just stopped being full goes on
		// its partition's chain of blocks with free entries
		// (BAT_$ADD_PART_VTOCE)
		if c-1 == vtocesPerBlock-1 {
			if p := vm.bat.PartitionIndex(x.BlockDAddr()); p >= 0 {
				part := &vm.lvol.Label.Partitions[p]
				binary.BigEndian.PutUint32(b.Data[0:], part.StatusVTOCE&0xFFFFFF)
				part.StatusVTOCE = part.StatusVTOCE&0xFF000000 | uint32(x.BlockDAddr())
			}
		}
	}
	return vm.lvol.WriteBlock(x.BlockDAddr(), b)
}

// GetFMBlocks returns the daddrs of an object's pages in order (holes and
// the end of the map are 0-terminated, as before).
func (vm VTOCManager) GetFMBlocks(e *fs.VTOCE) ([]fs.DAddr, error) {
	m, _, err := vm.FileMap(e)
	if err != nil {
		return nil, err
	}
	if i := slices.Index(m, fs.DAddr(0)); i >= 0 {
		m = m[:i]
	}
	return m, nil
}

// FileMap returns one daddr per logical page (0 for a hole, flag bit 31
// masked off) and the daddrs of the file-map blocks themselves.  The map is
// walked whole, the way VTOCE_$TRUNCATE frees it, so blocks past
// CurrentLength (which the kernel can leave behind) are returned too; the
// page list covers at least CurrentLength.
func (vm VTOCManager) FileMap(e *fs.VTOCE) ([]fs.DAddr, []fs.DAddr, error) {
	pages := int((e.Header.CurrentLength + 1023) / 1024)
	var out, fmBlocks []fs.DAddr
	set := func(page int, d fs.DAddr) {
		for len(out) <= page {
			out = append(out, 0)
		}
		out[page] = d
	}
	for i, d := range e.FileMap0 {
		if d &= 0x7FFFFFFF; d != 0 {
			set(i, d)
		}
	}
	seen := map[fs.DAddr]bool{}
	var walk func(level int, d fs.DAddr, first int) error
	walk = func(level int, d fs.DAddr, first int) error {
		d &= 0x7FFFFFFF
		if d == 0 {
			return nil
		}
		if seen[d] {
			return fmt.Errorf("file-map block %d referenced twice", d)
		}
		seen[d] = true
		fmBlocks = append(fmBlocks, d)
		b, err := vm.lvol.ReadBlock(d)
		if err != nil {
			return err
		}
		span := 1
		for i := 1; i < level; i++ {
			span *= 256
		}
		for i := 0; i < 256; i++ {
			c := fs.DAddr(binary.BigEndian.Uint32(b.Data[i*4:])) & 0x7FFFFFFF
			if c == 0 {
				continue
			}
			if level == 1 {
				set(first+i, c)
			} else if err := walk(level-1, c, first+i*span); err != nil {
				return err
			}
		}
		return nil
	}
	first := 32
	for level, d := range []fs.DAddr{e.FileMap1Ptr, e.FileMap2Ptr, e.FileMap3Ptr} {
		if err := walk(level+1, d, first); err != nil {
			return nil, nil, err
		}
		span := 256
		for i := 0; i < level; i++ {
			span *= 256
		}
		first += span
	}
	for len(out) < pages {
		out = append(out, 0)
	}
	return out, fmBlocks, nil
}

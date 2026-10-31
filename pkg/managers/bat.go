package managers

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
)

// BATManager keeps the volume's block allocation table in memory.  A SET
// bit means the block is free (BAT_$FREE sets it, BAT_$ALLOCATE clears
// it).  Block b (LV-relative) is bit (b-first)&31 of longword
// ((b-first)>>5)&0xFF of BAT block FirstBATBlockDAddr+((b-first)>>13).
type BATManager struct {
	lvol   *fs.LogicalVolume
	words  []uint32
	dirty  bool
	cursor fs.DAddr
	now    func() uint32
}

var ErrNoSpace = errors.New("no free blocks")

const batBitsPerBlock = 8192

func NewBATManager(lvol *fs.LogicalVolume) *BATManager {
	return &BATManager{lvol: lvol, now: func() uint32 { return 0 }}
}

func (bm *BATManager) hdr() *fs.BATHeader { return &bm.lvol.Label.BATHeader }

// NumBATBlocks is the number of BAT blocks the header implies.
func (bm *BATManager) NumBATBlocks() int {
	return (int(bm.hdr().NumBlocksRepresented) + batBitsPerBlock - 1) / batBitsPerBlock
}

// Mount reads the bitmap.
func (bm *BATManager) Mount() error {
	n := bm.NumBATBlocks()
	bm.words = make([]uint32, n*256)
	for i := 0; i < n; i++ {
		b, err := bm.lvol.ReadBlock(bm.hdr().FirstBATBlockDAddr + fs.DAddr(i))
		if err != nil {
			return err
		}
		for j := 0; j < 256; j++ {
			bm.words[i*256+j] = binary.BigEndian.Uint32(b.Data[j*4:])
		}
	}
	bm.cursor = bm.hdr().BlockNumOfFirstBATBit
	return nil
}

// InitEmpty sets up an all-allocated bitmap (used by Format).
func (bm *BATManager) InitEmpty() {
	bm.words = make([]uint32, bm.NumBATBlocks()*256)
	bm.cursor = bm.hdr().BlockNumOfFirstBATBit
	bm.dirty = true
}

func (bm *BATManager) represented(d fs.DAddr) bool {
	first := bm.hdr().BlockNumOfFirstBATBit
	return d >= first && d < first+fs.DAddr(bm.hdr().NumBlocksRepresented)
}

func (bm *BATManager) pos(d fs.DAddr) (int, uint32) {
	rel := uint32(d - bm.hdr().BlockNumOfFirstBATBit)
	return int(rel >> 5), 1 << (rel & 31)
}

// IsFree reports whether d is free.  Blocks the BAT does not represent are
// never free.
func (bm *BATManager) IsFree(d fs.DAddr) bool {
	if !bm.represented(d) {
		return false
	}
	w, m := bm.pos(d)
	return bm.words[w]&m != 0
}

// PartitionIndex is the BAT partition a block belongs to (BAT_$FREE's
// arithmetic: blocks below the start offset are partition 0).
func (bm *BATManager) PartitionIndex(d fs.DAddr) int {
	l := &bm.lvol.Label
	if l.NumPartitions == 0 || l.PartitionSize == 0 {
		return -1
	}
	if uint32(d) < uint32(l.PartitionStartOffset) {
		return 0
	}
	i := int((uint32(d) - uint32(l.PartitionStartOffset)) / l.PartitionSize)
	if i >= int(l.NumPartitions) {
		return -1
	}
	return i
}

func (bm *BATManager) adjust(d fs.DAddr, delta int32) {
	bm.hdr().NumFreeBlocks += delta
	if i := bm.PartitionIndex(d); i >= 0 {
		bm.lvol.Label.Partitions[i].FreeCount = uint32(int32(bm.lvol.Label.Partitions[i].FreeCount) + delta)
	}
	bm.dirty = true
}

// MarkUsed allocates a specific block.
func (bm *BATManager) MarkUsed(d fs.DAddr) error {
	if !bm.represented(d) {
		return fmt.Errorf("block %d is not in the BAT", d)
	}
	w, m := bm.pos(d)
	if bm.words[w]&m == 0 {
		return fmt.Errorf("block %d already allocated", d)
	}
	bm.words[w] &^= m
	bm.adjust(d, -1)
	return nil
}

// Free releases a block.
func (bm *BATManager) Free(d fs.DAddr) error {
	if !bm.represented(d) {
		return fmt.Errorf("block %d is not in the BAT", d)
	}
	w, m := bm.pos(d)
	if bm.words[w]&m != 0 {
		return fmt.Errorf("block %d already free", d)
	}
	bm.words[w] |= m
	bm.adjust(d, 1)
	return nil
}

func (bm *BATManager) lastBlock() fs.DAddr {
	return bm.hdr().BlockNumOfFirstBATBit + fs.DAddr(bm.hdr().NumBlocksRepresented) - 1
}

func (bm *BATManager) wrap(d fs.DAddr) fs.DAddr {
	if !bm.represented(d) {
		return bm.hdr().BlockNumOfFirstBATBit
	}
	return d
}

// Allocate takes the first free block at or after hint (wrapping).
func (bm *BATManager) Allocate(hint fs.DAddr) (fs.DAddr, error) {
	if hint == 0 {
		hint = bm.cursor
	}
	start := bm.wrap(hint)
	d := start
	for {
		if bm.IsFree(d) {
			if err := bm.MarkUsed(d); err != nil {
				return 0, err
			}
			bm.cursor = d + 1
			return d, nil
		}
		d++
		if d > bm.lastBlock() {
			d = bm.hdr().BlockNumOfFirstBATBit
		}
		if d == start {
			return 0, ErrNoSpace
		}
	}
}

// AllocateRun allocates n blocks, contiguous when a big enough run exists
// at or after hint, otherwise wherever they fit.
func (bm *BATManager) AllocateRun(n int, hint fs.DAddr) ([]fs.DAddr, error) {
	if n == 0 {
		return nil, nil
	}
	if int(bm.hdr().NumFreeBlocks) < n {
		return nil, ErrNoSpace
	}
	if hint == 0 {
		hint = bm.cursor
	}
	first := bm.hdr().BlockNumOfFirstBATBit
	last := bm.lastBlock()
	for pass := 0; pass < 2; pass++ {
		d := bm.wrap(hint)
		if pass == 1 {
			d = first
		}
		run := 0
		for ; d <= last; d++ {
			if bm.IsFree(d) {
				run++
				if run == n {
					out := make([]fs.DAddr, n)
					for i := 0; i < n; i++ {
						out[i] = d - fs.DAddr(n-1-i)
						if err := bm.MarkUsed(out[i]); err != nil {
							return nil, err
						}
					}
					bm.cursor = d + 1
					return out, nil
				}
			} else {
				run = 0
			}
		}
	}
	out := make([]fs.DAddr, 0, n)
	for len(out) < n {
		d, err := bm.Allocate(hint)
		if err != nil {
			for _, o := range out {
				bm.Free(o)
			}
			return nil, err
		}
		out = append(out, d)
		hint = d + 1
	}
	return out, nil
}

// CountFree counts the free bits.
func (bm *BATManager) CountFree() int {
	n := 0
	for d := bm.hdr().BlockNumOfFirstBATBit; d <= bm.lastBlock(); d++ {
		if bm.IsFree(d) {
			n++
		}
	}
	return n
}

// Flush writes the bitmap back.
func (bm *BATManager) Flush() error {
	if !bm.dirty {
		return nil
	}
	for i := 0; i < bm.NumBATBlocks(); i++ {
		d := bm.hdr().FirstBATBlockDAddr + fs.DAddr(i)
		var b fs.Block
		b.Header = fs.BlockHeader{ObjectUID: uid.UIDbat, PageWithinObject: int32(d), LastWritten: bm.now(),
			PVBlockDAddr: d + bm.lvol.StartDAddr()}
		for j := 0; j < 256; j++ {
			binary.BigEndian.PutUint32(b.Data[j*4:], bm.words[i*256+j])
		}
		if err := bm.lvol.WriteBlock(d, &b); err != nil {
			return err
		}
	}
	bm.dirty = false
	return nil
}

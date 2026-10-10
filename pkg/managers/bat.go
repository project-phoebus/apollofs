package managers

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

type BATManager struct {
	lvol      *fs.LogicalVolume
	batBlocks []fs.BATBlock
}

func NewBATManager(lvol *fs.LogicalVolume) *BATManager {
	return &BATManager{
		lvol: lvol,
	}
}

func readBATBlocks(lvol *fs.LogicalVolume) ([]fs.BATBlock, error) {
	var batBlocks []fs.BATBlock

	lvdaddr := lvol.Label.BATHeader.FirstBATBlockDAddr
	for {
		block, err := lvol.ReadBlock(lvdaddr)
		if err != nil {
			return nil, err
		}

		if block.Header.ObjectUID != uid.UIDbat {
			// break out of this loop
			break
		}

		var batBlock fs.BATBlock
		err = block.ReadInto(&batBlock)
		if err != nil {
			return nil, err
		}

		batBlocks = append(batBlocks, batBlock)
		lvdaddr = lvdaddr + 1
	}

	return batBlocks, nil
}

func writeBATBlocks(lvol *fs.LogicalVolume, batBlocks []fs.BATBlock) error {
	for i, batBlock := range batBlocks {
		blockDAddr := lvol.Label.BATHeader.FirstBATBlockDAddr + fs.DAddr(i)
		block, err := lvol.ReadBlock(blockDAddr)
		if err != nil {
			return err
		}

		// overwrite the block's data with our fs.BATBlock
		var buf bytes.Buffer

		err = binary.Write(&buf, binary.BigEndian, batBlock)
		if err != nil {
			return err
		}

		if buf.Len() > len(block.Data) {
			panic("BATBlock too big")
		}

		copy(block.Data[:], buf.Bytes())

		err = lvol.WriteBlock(blockDAddr, block)
		if err != nil {
			return err
		}
	}

	return nil
}

func (bm *BATManager) Mount() error {
	batBlocks, err := readBATBlocks(bm.lvol)
	if err != nil {
		return err
	}

	bm.batBlocks = batBlocks
	return nil
}

func (bm *BATManager) flush() error {
	writeBATBlocks(bm.lvol, bm.batBlocks)
	// TODO(toshok) write out the lvol.Label.BATHeader so NumFreeBlocks is updated
	return nil
}

func (bm *BATManager) AllocateBlock() (fs.DAddr, error) {
	defer bm.flush()

	daddr := bm.lvol.Label.BATHeader.BlockNumOfFirstBATBit
	for i, batBlock := range bm.batBlocks {
		for j, entry := range batBlock.Entries {
			if entry == 0xffffffff {
				// this entry is full.  skip it
				daddr += 32
				continue
			}

			// there's a spot in this entry we can allocate.  figure out
			// which bit it is, set it, and return the DAddr.
			bit := 31
			for k := 31; k >= 0; k-- {
				if entry&(1<<uint(k)) == 0 {
					bit = k
					break
				}
			}

			entry |= 1 << uint(bit)
			bm.batBlocks[i].Entries[j] = entry
			bm.lvol.Label.BATHeader.NumFreeBlocks -= 1

			return daddr + fs.DAddr(bit), nil
		}
	}

	return 0, errors.New("no free blocks")
}

func (bm *BATManager) DeallocateBlock(daddr fs.DAddr) error {
	defer bm.flush()

	daddr = daddr - bm.lvol.Label.BATHeader.BlockNumOfFirstBATBit
	for i, batBlock := range bm.batBlocks {
		for j, entry := range batBlock.Entries {
			if daddr > 31 {
				daddr -= 31
				continue
			}

			// we're at the block
			if entry&(1<<uint(31-daddr)) == 0 {
				return errors.New("block already deallocated")
			}

			entry &^= 1 << uint(31-daddr)
			bm.batBlocks[i].Entries[j] = entry
			bm.lvol.Label.BATHeader.NumFreeBlocks += 1
			return nil
		}
	}

	return errors.New("daddr out of range")
}

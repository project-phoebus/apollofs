package fs

import "fmt"

// BATHeader is LV label +0x2C..+0x4B (bat_$label_t in domain_os/bat/bat.h).
// A set bit in the BAT bitmap means the block is FREE; block b is bit
// (b-BlockNumOfFirstBATBit)&31 (LSB = bit 0) of longword
// ((b-first)>>5)&0xFF of BAT block FirstBATBlockDAddr+((b-first)>>13).
type BATHeader struct {
	NumBlocksRepresented  int32  // 0x2C
	NumFreeBlocks         int32  // 0x30
	FirstBATBlockDAddr    DAddr  // 0x34
	BlockNumOfFirstBATBit DAddr  // 0x38
	VolumeTrouble         uint16 // 0x3C bit 12: needs salvage (new format)
	StepBlocks            uint16 // 0x3E
	BatStep               uint16 // 0x40 (kernel defaults step pair to 0/3)
	Reserved42            uint16 // 0x42
	ReservedBlocks        uint32 // 0x44
	Unknown48             uint32 // 0x48
}

// BATPartition is one of the 64 eight-byte allocation partitions in the
// LV label (label+0x108, bat_$partition_t).
type BATPartition struct {
	FreeCount uint32 // free blocks in this partition
	// high byte: 1 = data partition, 2 = VTOCE blocks may be placed here;
	// low 24 bits: the partition's current VTOCE block with free entries
	StatusVTOCE uint32
}

func (h BATHeader) Print() {
	fmt.Println("BATHeader:")
	fmt.Println("  NumBlocksRepresented:", h.NumBlocksRepresented)
	fmt.Println("  NumFreeBlocks:", h.NumFreeBlocks)
	fmt.Println("  FirstBATBlockDAddr:", h.FirstBATBlockDAddr)
	fmt.Println("  BlockNumOfFirstBATBit:", h.BlockNumOfFirstBATBit)
	fmt.Println("  VolumeTrouble:", h.VolumeTrouble)
	fmt.Println("  BatStep:", h.BatStep)
}

type BATBlock struct {
	Entries [256]uint32
}

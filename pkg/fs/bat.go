package fs

import "fmt"

type BATHeader struct {
	NumBlocksRepresented  int32
	NumFreeBlocks         int32
	FirstBATBlockDAddr    DAddr
	BlockNumOfFirstBATBit DAddr
	VolumeTrouble         uint16
	Unused1               int16
	BatStep               uint32
	Unused2               [8]byte // padding to get us to 32 bytes
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

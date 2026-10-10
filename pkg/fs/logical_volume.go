package fs

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/uid"
	"github.com/domainos-archeology/apollofs/pkg/util"
)

type LogicalVolume struct {
	pvol       *PhysicalVolume
	startDAddr DAddr
	Label      LVLabel

	// there will be 8 of these
	VTOCMap VTOCMap
}

// LVLabel is LV block 0 (bat_$label_t, domain_os/bat/bat.h), 0x308 bytes.
type LVLabel struct {
	Version              int16            // 0x00 0 = pre-SR10, 1 = SR10
	Ignore1              int16            // 0x02
	Name                 [32]byte         // 0x04
	UID                  uid.UID          // 0x24
	BATHeader            BATHeader        // 0x2C
	VTOCHeader           VTOCHeader       // 0x4C
	LabelWritten         uint32           // 0xB0 time LV label written (mount time)
	LastMountedNode      uint32           // 0xB4 top 12 bits unused, node id below
	BootTime             uint32           // 0xB8
	DismountedTime       uint32           // 0xBC
	CurrentTime          uint32           // 0xC0
	Reserved1            [10]byte         // 0xC4
	SalvageFlag          int16            // 0xCE 1 = mounted / needs salvage
	Reserved2            [0x2c]byte       // 0xD0 (calendar data; last valid time at 0xE6)
	NumPartitions        uint16           // 0xFC
	PartitionStartOffset uint16           // 0xFE
	PartitionSize        uint32           // 0x100
	Unknown104           uint32           // 0x104
	Partitions           [64]BATPartition // 0x108
}

func (l LVLabel) Print() {
	var versionExtra string
	if l.Version == 0 {
		versionExtra = "(pre-sr10)"
	} else {
		versionExtra = "(sr10)"
	}

	fmt.Printf("Version: %d %s\n", l.Version, versionExtra)
	fmt.Printf("Name: %s\n", string(l.Name[:]))
	fmt.Printf("UID: %s\n", l.UID)
	fmt.Printf("Label Written: %s\n", util.FormatTimestamp(l.LabelWritten))
	fmt.Printf("Boot Time: %s\n", util.FormatTimestamp(l.BootTime))
	fmt.Printf("Dismounted Time: %s\n", util.FormatTimestamp(l.DismountedTime))
	l.BATHeader.Print()
	l.VTOCHeader.Print()
}

func NewLogicalVolume(pvol *PhysicalVolume, startDAddr DAddr) (*LogicalVolume, error) {
	lvol := &LogicalVolume{
		pvol:       pvol,
		startDAddr: startDAddr,
	}

	block, err := pvol.ReadBlock(startDAddr)
	if err != nil {
		return nil, err
	}

	// XXX validate the block header?
	err = block.ReadInto(&lvol.Label)
	if err != nil {
		return nil, err
	}

	// parse out the VTOCMapData into our VTOCMap
	var numExtents int
	switch {
	case lvol.Label.Version == 0:
		numExtents = 8
	case lvol.Label.Version == 1:
		numExtents = 10
	default:
		panic("unknown LV label version")
	}

	dataIdx := 0
	for i := 0; i < numExtents; i++ {
		var extent VTOCMapExtent

		extent.NumBlocks = lvol.Label.VTOCHeader.VTOCMapData.Uint16At(dataIdx)
		dataIdx += 2

		extent.FirstBlockDAddr = DAddr(lvol.Label.VTOCHeader.VTOCMapData.Uint32At(dataIdx))
		dataIdx += 4

		lvol.VTOCMap = append(lvol.VTOCMap, extent)
	}

	// not yet
	// err = lvol.Label.validate()
	// if err != nil {
	// 	return nil, err
	// }

	return lvol, nil
}

func (lvol *LogicalVolume) PrintLabel() {
	fmt.Println("LV Label:")
	lvol.Label.Print()
	// now print out our parsed vtoc map:
	fmt.Println("VTOC Map:")
	lvol.VTOCMap.Print()
}

// StartDAddr is the physical block of LV block 0 (the LV label).
func (lvol *LogicalVolume) StartDAddr() DAddr { return lvol.startDAddr }

// PhysicalVolume returns the volume this LV lives on.
func (lvol *LogicalVolume) PhysicalVolume() *PhysicalVolume { return lvol.pvol }

// WriteLabel stores Label into LV block 0, keeping the block header.
func (lvol *LogicalVolume) WriteLabel() error {
	block, err := lvol.ReadBlock(0)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, &lvol.Label); err != nil {
		return err
	}
	copy(block.Data[:], buf.Bytes())
	return lvol.WriteBlock(0, block)
}

func (lvol *LogicalVolume) ReadBlock(blockNum DAddr) (*Block, error) {
	return lvol.pvol.ReadBlock(lvol.startDAddr + blockNum)
}

func (lvol *LogicalVolume) WriteBlock(blockNum DAddr, block *Block) error {
	return lvol.pvol.WriteBlock(lvol.startDAddr+blockNum, block)
}

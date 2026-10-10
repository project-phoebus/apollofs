package fs

import (
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

type LVLabel struct {
	Version         int16
	Ignore1         int16
	Name            [32]byte
	UID             uid.UID
	BATHeader       BATHeader
	VTOCHeader      VTOCHeader
	LabelWritten    uint32 // time LV label writtern
	Ignore2         int16
	LastMountedNode int16
	BootTime        uint32
	DismountedTime  uint32
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

func (lvol *LogicalVolume) ReadBlock(blockNum DAddr) (*Block, error) {
	return lvol.pvol.ReadBlock(lvol.startDAddr + blockNum)
}

func (lvol *LogicalVolume) WriteBlock(blockNum DAddr, block *Block) error {
	return lvol.pvol.WriteBlock(lvol.startDAddr+blockNum, block)
}

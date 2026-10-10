package fs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/domainos-archeology/apollofs/pkg/drives"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

const MaxLogicalVolumes = 10

type PhysicalVolume struct {
	Label PVLabel
	LV    *LogicalVolume

	file *os.File
}

type PVLabel struct {
	Version             int16
	APOLLO              [6]byte
	Name                [32]byte
	UID                 uid.UID
	Ignore1             int16
	DriveType           int16
	TotalBlocksInVolume int32
	BlocksPerTrack      int16
	TracksPerCylinder   int16
	LVDAddr             [10]DAddr
	AltLVLabelDAddr     [10]DAddr

	// start of phys bad spot cylinder
	PhysBadspotDAddr DAddr

	// start of phys diag cylinder
	PhysDiagDAddr DAddr

	SectorStart     uint16
	SectorSize      uint16
	PreCompCylinder uint16
}

func (l PVLabel) validate() error {
	if !bytes.Equal(l.APOLLO[:], []byte("APOLLO")) {
		return fmt.Errorf("expected 'APOLLO', got '%s'", string(l.APOLLO[:]))
	}

	// the first lv daddr/alt-daddr should be non-zero, but the rest should be zero
	if l.LVDAddr[0] == 0 {
		return fmt.Errorf("expected LVDAddr[0] to be non-zero")
	}
	if l.AltLVLabelDAddr[0] == 0 {
		return fmt.Errorf("expected AltLVLabelDAddr[0] to be non-zero")
	}

	for i := 1; i < MaxLogicalVolumes; i++ {
		if l.LVDAddr[i] != 0 {
			return fmt.Errorf("expected LVDAddr[%d] to be zero", i)
		}
		if l.AltLVLabelDAddr[i] != 0 {
			return fmt.Errorf("expected AltLVLabelDAddr[%d] to be zero", i)
		}
	}

	return nil
}

func (l PVLabel) Print() {
	dtName := "unknown"
	dt, err := drives.GetDriveType(l.DriveType)
	if err == nil {
		dtName = dt.Name
	}
	fmt.Printf("Version: %d\n", l.Version)
	fmt.Printf("Name: %s\n", string(l.Name[:]))
	fmt.Printf("UID: %s\n", l.UID.String())
	fmt.Printf("DriveType: %x (%s)\n", l.DriveType, dtName)
	fmt.Printf("TotalBlocksInVolume: %d\n", l.TotalBlocksInVolume)
	fmt.Printf("BlocksPerTrack: %d\n", l.BlocksPerTrack)
	fmt.Printf("TracksPerCylinder: %d\n", l.TracksPerCylinder)
	fmt.Printf("SectorStart: %d\n", l.SectorStart)
	fmt.Printf("SectorSize: %d\n", l.SectorSize)
	fmt.Printf("PreComp: %d\n", l.PreCompCylinder)
	fmt.Printf("Logical Volumes:\n")
	for i := 0; i < MaxLogicalVolumes; i++ {
		fmt.Printf("  LV%d: block %d / %d (alt)\n", i, l.LVDAddr[i], l.AltLVLabelDAddr[i])
	}
}

func Mount(diskImage string) (*PhysicalVolume, error) {
	file, err := os.Open(diskImage)
	if err != nil {
		return nil, err
	}

	pvol := &PhysicalVolume{file: file}
	block0, err := pvol.ReadBlock(0)
	if err != nil {
		return nil, err
	}

	// XXX validate the block header?
	err = block0.ReadInto(&pvol.Label)
	if err != nil {
		return nil, err
	}

	err = pvol.Label.validate()
	if err != nil {
		return nil, err
	}

	pvol.LV, err = NewLogicalVolume(pvol, pvol.Label.LVDAddr[0])
	if err != nil {
		return nil, err
	}

	return pvol, nil
}

func (pvol *PhysicalVolume) Unmount() error {
	if pvol.file != nil {
		return pvol.file.Close()
	}
	return nil
}

func (pvol *PhysicalVolume) PrintLabel() {
	fmt.Println("PV Label:")
	pvol.Label.Print()
}

func (pvol *PhysicalVolume) LogicalVolumes() []int {
	var lvs []int
	for i := 0; i < MaxLogicalVolumes; i++ {
		if pvol.Label.LVDAddr[i] != 0 {
			lvs = append(lvs, i)
		}
	}
	return lvs
}

func (pvol *PhysicalVolume) ReadBlock(blockNum DAddr) (*Block, error) {
	_, err := pvol.file.Seek(int64(blockNum*BlockSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	var block Block
	err = binary.Read(pvol.file, binary.BigEndian, &block)
	if err != nil {
		return nil, err
	}

	return &block, nil
}

func (pvol *PhysicalVolume) WriteBlock(blockNum DAddr, block *Block) error {
	_, err := pvol.file.Seek(int64(blockNum*BlockSize), io.SeekStart)
	if err != nil {
		return err
	}

	err = binary.Write(pvol.file, binary.BigEndian, block)
	if err != nil {
		return err
	}

	return nil
}

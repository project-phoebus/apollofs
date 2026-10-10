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

	SectorStart     uint16 // 0x94
	SectorSize      uint16 // 0x96
	PreCompCylinder uint16 // 0x98

	// SR10 extension (disk_$pv_label_t in domain_os/disk/disk_internal.h)
	Reserved9A      [6]byte   // 0x9A (an SR10.3 invol writes 0,0,1 here)
	AddrEnd         uint32    // 0xA0 0 = no end
	ReservedA4      uint32    // 0xA4
	NumParts        uint16    // 0xA8 volumes in a striped set (1)
	PartNum         uint16    // 0xAA this volume's place in the set (1)
	PartDev         [8]uint16 // 0xAC
	Interleave      uint16    // 0xBC striping mode
	SectorsPerBlock uint16    // 0xBE hardware sectors per block (1)
	WordC0          uint16    // 0xC0 (1 on an SR10.3 volume)
	RevisionLen     uint16    // 0xC2
	Revision        [32]byte  // 0xC4 invol's "revision 10.x" text
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
	fmt.Printf("AddrEnd: %d NumParts: %d PartNum: %d Interleave: %d SectorsPerBlock: %d\n", l.AddrEnd, l.NumParts, l.PartNum, l.Interleave, l.SectorsPerBlock)
	if n := int(l.RevisionLen); n > 0 && n <= len(l.Revision) {
		fmt.Printf("Revision: %s\n", string(l.Revision[:n]))
	}
	fmt.Printf("Logical Volumes:\n")
	for i := 0; i < MaxLogicalVolumes; i++ {
		fmt.Printf("  LV%d: block %d / %d (alt)\n", i, l.LVDAddr[i], l.AltLVLabelDAddr[i])
	}
}

func Mount(diskImage string) (*PhysicalVolume, error) {
	return mount(diskImage, false)
}

// MountWritable opens the image read-write.
func MountWritable(diskImage string) (*PhysicalVolume, error) {
	return mount(diskImage, true)
}

func mount(diskImage string, writable bool) (*PhysicalVolume, error) {
	flag := os.O_RDONLY
	if writable {
		flag = os.O_RDWR
	}
	file, err := os.OpenFile(diskImage, flag, 0)
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
		err := pvol.file.Close()
		pvol.file = nil
		return err
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

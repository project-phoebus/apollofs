package fs

import (
	"bytes"
	"encoding/binary"
	"errors"
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

	// Headers tells the image's layout.  A disk whose controller keeps
	// Apollo block headers (the ANSI, SMD, ST-506 and ESDI controllers:
	// 1056-byte sectors) is an array of 1056-byte blocks, header first; a
	// SCSI disk keeps none (WIN_SCSI_$CINIT's unit flags 0x8000 turn off
	// DISK_IO's header stamp and check) and is an array of 1024-byte
	// blocks.  Mount tells the two apart by where the PV label is; a
	// headerless volume's blocks read back with a zero header and only
	// their data is written.
	Headers bool

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
	SysbootDAddr    uint32    // 0xA4 sysboot's first block (invol: 2)
	NumParts        uint16    // 0xA8 volumes in a striped set (1)
	PartNum         uint16    // 0xAA this volume's place in the set (1)
	PartDev         [8]uint16 // 0xAC
	Interleave      uint16    // 0xBC striping mode
	SectorsPerBlock uint16    // 0xBE hardware sectors per block (1)
	WordC0          uint16    // 0xC0 (1 on an SR10.3 volume)
	RevisionLen     uint16    // 0xC2
	Revision        [32]byte  // 0xC4 invol's "revision 10.x" text
	ReservedE4      [0x12]byte
	// The reserved areas past TotalBlocksInVolume, in blocks: the SR10.2
	// invol stores, for a SCSI disk, one cylinder for the badspot list and
	// two for the diagnostic area here (0 on the other disks).
	BadspotLen uint16 // 0xF6
	DiagLen    uint16 // 0xF8
	// The blocks of sysboot from SysbootDAddr: invol clears it, then
	// counts the pages it installs (the MD9 PROM loads that many blocks
	// and takes 0 or more than 255 as "No boot program on this disk").
	SysbootBlocks uint16 // 0xFA
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
	dtName := drives.Name(l.DriveType)
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
	if l.BadspotLen != 0 || l.DiagLen != 0 {
		fmt.Printf("BadspotLen: %d DiagLen: %d\n", l.BadspotLen, l.DiagLen)
	}
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
	pvol.Headers, err = detectHeaders(file)
	if err != nil {
		file.Close()
		return nil, err
	}
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

// detectHeaders finds the PV label's "APOLLO" (label bytes 2..7): at image
// byte 2 on a headerless (SCSI) volume, at byte 34, behind the 32-byte
// block header, on a volume with headers.
func detectHeaders(f *os.File) (bool, error) {
	var b [BlockSize]byte
	n, err := f.ReadAt(b[:], 0)
	if err != nil && err != io.EOF {
		return false, err
	}
	apollo := []byte("APOLLO")
	switch {
	case n >= 8 && bytes.Equal(b[2:8], apollo):
		return false, nil
	case n >= BlockHeaderSize+8 && bytes.Equal(b[BlockHeaderSize+2:BlockHeaderSize+8], apollo):
		return true, nil
	}
	return false, errors.New("no PV label ('APOLLO') at block 0, with or without a block header")
}

// BlockBytes is the size of one block in the image: 1056 with headers,
// 1024 without.
func (pvol *PhysicalVolume) BlockBytes() int64 { return BlockBytes(pvol.Headers) }

// BlockBytes is the size of one block in an image of either layout.
func BlockBytes(headers bool) int64 {
	if headers {
		return BlockSize
	}
	return BlockDataSize
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
	if pvol.Headers {
		fmt.Println("Layout: 1056-byte blocks with block headers")
	} else {
		fmt.Println("Layout: 1024-byte blocks, no block headers (SCSI)")
	}
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
	_, err := pvol.file.Seek(int64(blockNum)*pvol.BlockBytes(), io.SeekStart)
	if err != nil {
		return nil, err
	}

	var block Block
	if pvol.Headers {
		err = binary.Read(pvol.file, binary.BigEndian, &block)
	} else {
		_, err = io.ReadFull(pvol.file, block.Data[:])
	}
	if err != nil {
		return nil, err
	}

	return &block, nil
}

// WriteBlock writes a block; on a headerless volume only its data.
func (pvol *PhysicalVolume) WriteBlock(blockNum DAddr, block *Block) error {
	return WriteBlockTo(pvol.file, pvol.Headers, blockNum, block)
}

// WriteBlockTo writes a block into an image file of either layout.
func WriteBlockTo(f *os.File, headers bool, blockNum DAddr, block *Block) error {
	_, err := f.Seek(int64(blockNum)*BlockBytes(headers), io.SeekStart)
	if err != nil {
		return err
	}

	if headers {
		return binary.Write(f, binary.BigEndian, block)
	}
	_, err = f.Write(block.Data[:])
	return err
}

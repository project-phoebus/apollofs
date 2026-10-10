package fs

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/uid"
)

// VTOCHeader is the 100-byte "VTOC Header" of the LV label (label+0x4C),
// the kernel's per-volume vtoc_$vol_t (domain_os/vtoc/vtoc_internal.h).
type VTOCHeader struct {
	Version             int16       // 0x00 hash_type: 0/1 UID_$HASH, 2 shift-xor, 3 xor
	VTOCSizeInBlocks    int16       // 0x02 hash_size: number of hash buckets (4 per bucket block)
	VTOCBlocksUsed      int32       // 0x04 blocks_added: bucket blocks allocated
	NetworkRootDirVTOCX VTOCX       // 0x08 name_dir1
	DiskEntryDirVTOCX   VTOCX       // 0x0C name_dir2
	OSPagingFileVTOCX   VTOCX       // 0x10 current_vtoce
	SysbootVTOCX        VTOCX       // 0x14
	VTOCMapData         VTOCMapData // 0x18 ten {count u16, base u32} bucket-block extents
	CurBktBlock         uint32      // 0x54 bucket block being handed out for overflow
	CurBktIdx           uint16      // 0x58 next bucket in it (wraps at 4)
	Unused              [10]byte    // 0x5A
}
type VTOCMapDataSR9 [48]byte
type VTOCMapData [60]byte

func (v VTOCMapData) Uint16At(index int) uint16 {
	return binary.BigEndian.Uint16(v[index : index+2])
}

func (v VTOCMapData) Uint32At(index int) uint32 {
	return binary.BigEndian.Uint32(v[index : index+4])
}

// the next two are the parsed form of VTOCMapData. check logical_volume.go
type VTOCMap []VTOCMapExtent
type VTOCMapExtent struct {
	NumBlocks       uint16
	FirstBlockDAddr DAddr
}

type VTOCBlockSR9 struct {
	NextBlockDAddr DAddr
	Entries        [5]VTOCESR9
}

// VTOCBlock is a new-format VTOCE block (vtoc_$vtoce_block_t, 0x3F8 bytes
// in domain_os/vtoc/vtoc_internal.h): the partition's chain of blocks with
// free entries at +0 and the in-use count BAT_$ALLOC_VTOCE maintains at +4
// (0xE3B09C addq.w #0x1,(0x4,A0)).
type VTOCBlock struct {
	NextBlockDAddr DAddr  // 0x00 next block with free entries (0 = end)
	EntryCount     uint16 // 0x04 entries in use (0..3)
	Reserved06     uint16 // 0x06
	Entries        [3]VTOCE
}

// this seem to be a struct in SR10 only, and not present in SR9?
type VTOCBucketBlock struct {
	Buckets [4]VTOCBucket
}

type VTOCBucket struct {
	// bucket header (next daddr/index)
	NextBlockDAddr DAddr
	NextIndex      uint16
	Padding        uint16

	// bucket entries
	Entries [20]VTOCBucketEntry
}

type VTOCBucketEntry struct {
	UID   uid.UID
	VTOCX VTOCX
}

type VTOCEHeaderSR9 struct {
	// "VTOCE & Object Info" in the docs
	Version    uint8
	SystemType uint8
	Flags      uint16

	ObjectUID        uid.UID
	ObjectTypeDefUID uid.UID
	ObjectACLUID     uid.UID
	CurrentLength    int32
	BlocksUsed       int32
	LastUsedTime     int32
	LastModifiedTime int32
	DirectoryUID     uid.UID // "UID of Directory in which object catalogued" in the docs.  containing dir?
	MoreStuff        int32   // "DTM Ext. / Unused / Ref. Count" in the docs
	ObjectLockKey    int32

	Padding [4]byte // Aegis Internals doesn't have this, but EH'87 does.
}

// VTOCEHeader is the first 0xC4 bytes of an SR10 VTOCE.  Bytes 0x00..0x8F
// are the object attributes the AST caches (aote_t +0x0C..+0x9B in
// domain_os/ast/ast.h); an rbak/wbak tape stores 0x00..0x77 of them
// verbatim in its file and directory records.
type VTOCEHeader struct {
	Version          uint8   // 0x00 object kind (always 1; VTOC_$ALLOCATE forces it)
	SystemType       uint8   // 0x01 sub type: 0 file, 1 directory, 2 root directory, 4/5 device
	Flags            uint16  // 0x02 0x8000 = entry in use
	ObjectUID        uid.UID // 0x04
	ObjectTypeDefUID uid.UID // 0x0C type UID (0x321 unstruct, 0x322 coff, 0x311 uasc, ...)
	CurrentLength    uint32  // 0x14 bytes
	BlocksUsed       uint32  // 0x18 data pages + file-map blocks
	DTM              [8]byte // 0x1C 48-bit clock + pad: date/time modified
	DTU              [8]byte // 0x24 date/time used
	DTC              [8]byte // 0x2C
	DTA              [8]byte // 0x34 attributes modified
	DirectoryUID     uid.UID // 0x3C parent directory
	Node             uint32  // 0x44
	OwnerUID         uid.UID // 0x48 person
	GroupUID         uid.UID // 0x50
	OrgUID           uid.UID // 0x58
	Rights           [4]byte // 0x60 owner, group, org, world
	ACLFlags         uint32  // 0x64
	ACLExt           [3]uint32
	RefCount         uint16   // 0x74
	U76              uint16   // 0x76
	Reserved78       [16]byte // 0x78
	ObjectACLUID     uid.UID  // 0x88
	Reserved90       [52]byte // 0x90
}

// VTOCEAttrSize is how much of a VTOCE header an rbak tape record carries.
const VTOCEAttrSize = 0x78

// VTOCEFlagInUse marks an allocated VTOCE.
const VTOCEFlagInUse = 0x8000

// Bytes returns the 0x150-byte on-disk image.
func (e *VTOCE) Bytes() []byte {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, e); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// VTOCEFromBytes decodes an on-disk VTOCE.
func VTOCEFromBytes(b []byte) (*VTOCE, error) {
	var e VTOCE
	if err := binary.Read(bytes.NewReader(b), binary.BigEndian, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// SetAttrs copies an rbak-style 0x78-byte attribute image over the header.
func (e *VTOCE) SetAttrs(attrs [VTOCEAttrSize]byte) {
	b := e.Bytes()
	copy(b, attrs[:])
	ne, err := VTOCEFromBytes(b)
	if err != nil {
		panic(err)
	}
	*e = *ne
}

// Attrs returns header bytes 0x00..0x77.
func (e *VTOCE) Attrs() [VTOCEAttrSize]byte {
	var a [VTOCEAttrSize]byte
	copy(a[:], e.Bytes())
	return a
}

type VTOCESR9 struct {
	Header      VTOCEHeaderSR9
	FileMap0    [32]DAddr
	FileMap1Ptr DAddr
	FileMap2Ptr DAddr
	FileMap3Ptr DAddr
}

type VTOCE struct {
	Header      VTOCEHeader
	FileMap1Ptr DAddr
	FileMap2Ptr DAddr
	FileMap3Ptr DAddr
	FileMap0    [32]DAddr // daddrs for the first 32 pages of an object
}

type VTOCX uint32

func NewVTOCX(blockDAddr DAddr, index int) VTOCX {
	return VTOCX(blockDAddr<<4 + DAddr(index))
}

func (x VTOCX) Index() int {
	return int(uint32(x) & 0xF)
}

func (x VTOCX) BlockDAddr() DAddr {
	return DAddr(uint32(x) >> 4)
}

func (x VTOCX) String() string {
	return fmt.Sprintf("vtoc blk %d, index %d", x.BlockDAddr(), x.Index())
}

func (h VTOCHeader) Print() {
	fmt.Println("VTOCHeader:")
	fmt.Println("  Version:", h.Version)
	fmt.Println("  VTOCSizeInBlocks:", h.VTOCSizeInBlocks)
	fmt.Println("  VTOCBlocksUsed:", h.VTOCBlocksUsed)
	fmt.Println("  NetworkRootDirVTOCX:", h.NetworkRootDirVTOCX)
	fmt.Println("  DiskEntryDirVTOCX:", h.DiskEntryDirVTOCX)
	fmt.Println("  OSPagingFileVTOCX:", h.OSPagingFileVTOCX)
	fmt.Println("  SysbootVTOCX:", h.SysbootVTOCX)
}

func (b VTOCBucketBlock) Print() {
	fmt.Println("VTOCBucketBlock:")
	for i, bucket := range b.Buckets {
		fmt.Printf("  Bucket %d:\n", i)
		fmt.Println("    NextBlockDAddr:", bucket.NextBlockDAddr)
		fmt.Println("    NextIndex:", bucket.NextIndex)

		for j, entry := range bucket.Entries {
			if entry.VTOCX == 0 {
				continue
			}
			fmt.Printf("    Entry %d:\n", j)
			fmt.Println("      UID:", entry.UID)
			fmt.Println("      VTOCX:", entry.VTOCX)
		}
	}
}

func (b VTOCBlock) Print() {
	fmt.Println("VTOCBlock:")
	fmt.Println(" NextBlockDAddr:", b.NextBlockDAddr, "EntryCount:", b.EntryCount)
	for i, e := range b.Entries {
		fmt.Printf(" VTOCE %d:\n", i)
		e.Print()
	}
}

func (e VTOCE) IsDirectory() bool {
	return e.Header.SystemType == 2 || e.Header.SystemType == 1
}

func (e VTOCE) Print() {
	fmt.Println("  Info: version", e.Header.Version, "systype", e.Header.SystemType, "flags", e.Header.Flags)
	fmt.Println("  ObjectUID:", e.Header.ObjectUID)
	fmt.Println("  ObjectTypeDefUID:", e.Header.ObjectTypeDefUID)
	fmt.Println("  ObjectACLUID:", e.Header.ObjectACLUID)
	fmt.Println("  CurrentLength:", e.Header.CurrentLength)
	fmt.Println("  BlocksUsed:", e.Header.BlocksUsed)
	fmt.Printf("  DTM: %x DTU: %x\n", e.Header.DTM[:6], e.Header.DTU[:6])
	fmt.Println("  DirectoryUID:", e.Header.DirectoryUID)
	fmt.Printf("  Owner/Group/Org: %s %s %s rights %x flags %08x\n", e.Header.OwnerUID, e.Header.GroupUID, e.Header.OrgUID, e.Header.Rights[:], e.Header.ACLFlags)
	fmt.Println("  FileMap0:")
	for i, daddr := range e.FileMap0 {
		if daddr == 0 {
			continue
		}
		fmt.Println("    map idx:", i, " daddr:", daddr)
	}
	fmt.Println("  FileMap1Ptr:", e.FileMap1Ptr)
	fmt.Println("  FileMap2Ptr:", e.FileMap2Ptr)
	fmt.Println("  FileMap3Ptr:", e.FileMap3Ptr)
}

func (m VTOCMap) Print() {
	for i, extent := range m {
		fmt.Printf("  extent %d: %d blocks at %d\n", i, extent.NumBlocks, extent.FirstBlockDAddr)
	}
}

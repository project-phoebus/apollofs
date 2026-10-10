package fs

import (
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

type VTOCBlock struct {
	Padding        uint32 // not sure what this is, but VTOCE_$READ uses 8 bytes to offset to the entries instead of 4
	NextBlockDAddr DAddr
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

type VTOCEHeader struct {
	Version          uint8
	SystemType       uint8
	Flags            uint16
	ObjectUID        uid.UID
	ObjectTypeDefUID uid.UID
	CurrentLength    uint32
	BlocksUsed       uint32
	LastModifiedTime uint32
	ExtDtm           uint16
	RefCount         uint16
	LastUsedTime     uint32
	U5               uint32
	U6               uint32
	U7               uint32
	U8               uint32
	U9               uint32
	DirectoryUID     uid.UID
	ObjectLockKey    uint32
	ObjectUserUID    uid.UID
	ObjectGroupUID   uid.UID
	ObjectOrgUID     uid.UID
	U19              uint32
	U20              uint32
	U21              uint32
	U22              uint32
	U23              uint32
	U24              uint32
	U25              uint32
	U26              uint32
	U27              uint32
	U28              uint32
	ObjectACLUID     uid.UID
	Padding          [52]byte
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
	fmt.Println(" NextBlockDAddr:", b.NextBlockDAddr)
	for i, e := range b.Entries {
		fmt.Printf(" VTOCE %d:\n", i)
		e.Print()
	}
}

func (e VTOCE) IsDirectory() bool {
	return e.Header.ObjectTypeDefUID == uid.UIDdirectory ||
		e.Header.ObjectTypeDefUID == uid.UIDunix_directory ||
		e.Header.SystemType == 2 ||
		e.Header.SystemType == 1
}

func (e VTOCE) Print() {
	fmt.Println("  Info: version", e.Header.Version, "systype", e.Header.SystemType, "flags", e.Header.Flags)
	fmt.Println("  ObjectUID:", e.Header.ObjectUID)
	fmt.Println("  ObjectTypeDefUID:", e.Header.ObjectTypeDefUID)
	fmt.Println("  ObjectACLUID:", e.Header.ObjectACLUID)
	fmt.Println("  CurrentLength:", e.Header.CurrentLength)
	fmt.Println("  BlocksUsed:", e.Header.BlocksUsed)
	fmt.Println("  LastUsedTime:", e.Header.LastUsedTime)
	fmt.Println("  LastModifiedTime:", e.Header.LastModifiedTime)
	fmt.Println("  DirectoryUID:", e.Header.DirectoryUID)
	// fmt.Println("  MoreStuff:", e.Header.MoreStuff)
	fmt.Println("  ObjectLockKey:", e.Header.ObjectLockKey)
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

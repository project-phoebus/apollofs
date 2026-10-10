package fs

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// On-disk sizes are what encoding/binary reads and writes (packed,
// big-endian).  Every record below is also checked for having no Go
// alignment padding (unsafe.Sizeof == binary.Size) so the unsafe.Offsetof
// assertions describe disk offsets too.
func requireDiskSize(t *testing.T, want int, v any, sz uintptr) {
	t.Helper()
	require.Equal(t, want, binary.Size(v), "on-disk size")
	require.Equal(t, want, int(sz), "Go struct carries padding")
}

// The VTOC header is the kernel's 100-byte vtoc_$vol_t: the map at 0x18
// holds ten six-byte {count, base} extents (60 bytes), then the overflow
// bucket cursor at 0x54/0x58.
func TestVTOCHeaderLayout(t *testing.T) {
	var h VTOCHeader
	requireDiskSize(t, 100, h, unsafe.Sizeof(h))
	require.Equal(t, 0x00, int(unsafe.Offsetof(h.Version)))
	require.Equal(t, 0x02, int(unsafe.Offsetof(h.VTOCSizeInBlocks)))
	require.Equal(t, 0x04, int(unsafe.Offsetof(h.VTOCBlocksUsed)))
	require.Equal(t, 0x08, int(unsafe.Offsetof(h.NetworkRootDirVTOCX)))
	require.Equal(t, 0x0c, int(unsafe.Offsetof(h.DiskEntryDirVTOCX)))
	require.Equal(t, 0x10, int(unsafe.Offsetof(h.OSPagingFileVTOCX)))
	require.Equal(t, 0x14, int(unsafe.Offsetof(h.SysbootVTOCX)))
	require.Equal(t, 0x18, int(unsafe.Offsetof(h.VTOCMapData)))
	require.Equal(t, 0x54, int(unsafe.Offsetof(h.CurBktBlock)))
	require.Equal(t, 0x58, int(unsafe.Offsetof(h.CurBktIdx)))
}

// Kernel reference: domain_os/vtoc/vtoc_internal.h.  Old-format block is a
// 4-byte chain long + 5 x 0xCC VTOCEs (0x400); the new-format block is an
// 8-byte header (next_block, entry_count, reserved) + 3 x 0x150 VTOCEs
// (0x3F8), VTOCE i at block+8+i*0x150 (0xE3A004..0xE3A040).
func TestVTOCBlockLayout(t *testing.T) {
	var sr9 VTOCBlockSR9
	requireDiskSize(t, 1024, sr9, unsafe.Sizeof(sr9))
	require.Equal(t, 0x00, int(unsafe.Offsetof(sr9.NextBlockDAddr)))
	require.Equal(t, 0x04, int(unsafe.Offsetof(sr9.Entries)))

	var sr10 VTOCBlock
	requireDiskSize(t, 0x3F8, sr10, unsafe.Sizeof(sr10))
	require.Equal(t, 0x00, int(unsafe.Offsetof(sr10.Padding)))
	require.Equal(t, 0x04, int(unsafe.Offsetof(sr10.NextBlockDAddr)))
	require.Equal(t, 0x08, int(unsafe.Offsetof(sr10.Entries)))
}

func TestVTOCEHeaderLayout(t *testing.T) {
	var sr9 VTOCEHeaderSR9
	requireDiskSize(t, 64, sr9, unsafe.Sizeof(sr9))
	require.Equal(t, 0x00, int(unsafe.Offsetof(sr9.Version)))
	require.Equal(t, 0x01, int(unsafe.Offsetof(sr9.SystemType)))
	require.Equal(t, 0x02, int(unsafe.Offsetof(sr9.Flags)))
	require.Equal(t, 0x04, int(unsafe.Offsetof(sr9.ObjectUID)))
	require.Equal(t, 0x0C, int(unsafe.Offsetof(sr9.ObjectTypeDefUID)))
	require.Equal(t, 0x14, int(unsafe.Offsetof(sr9.ObjectACLUID)))
	require.Equal(t, 0x1C, int(unsafe.Offsetof(sr9.CurrentLength)))
	require.Equal(t, 0x20, int(unsafe.Offsetof(sr9.BlocksUsed)))
	require.Equal(t, 0x24, int(unsafe.Offsetof(sr9.LastUsedTime)))
	require.Equal(t, 0x28, int(unsafe.Offsetof(sr9.LastModifiedTime)))
	require.Equal(t, 0x2C, int(unsafe.Offsetof(sr9.DirectoryUID)))
	require.Equal(t, 0x34, int(unsafe.Offsetof(sr9.MoreStuff)))
	require.Equal(t, 0x38, int(unsafe.Offsetof(sr9.ObjectLockKey)))

	var sr10 VTOCEHeader
	requireDiskSize(t, 0xC4, sr10, unsafe.Sizeof(sr10))
	require.Equal(t, 0x04, int(unsafe.Offsetof(sr10.ObjectUID)))
	require.Equal(t, 0x0C, int(unsafe.Offsetof(sr10.ObjectTypeDefUID)))
	require.Equal(t, 0x14, int(unsafe.Offsetof(sr10.CurrentLength)))
	require.Equal(t, 0x18, int(unsafe.Offsetof(sr10.BlocksUsed)))
	require.Equal(t, 0x1C, int(unsafe.Offsetof(sr10.DTM)))
	require.Equal(t, 0x3C, int(unsafe.Offsetof(sr10.DirectoryUID)))
	require.Equal(t, 0x48, int(unsafe.Offsetof(sr10.OwnerUID)))
	require.Equal(t, 0x60, int(unsafe.Offsetof(sr10.Rights)))
	require.Equal(t, 0x74, int(unsafe.Offsetof(sr10.RefCount)))
	require.Equal(t, 0x88, int(unsafe.Offsetof(sr10.ObjectACLUID)))
}

// Kernel: vtoce_$old_fm_t (direct 0x40, indirect 0xC0, 0xCC bytes) and
// vtoce_$new_fm_t (indirect roots 0xC4, direct 0xD0, 0x150 bytes).
func TestVTOCELayout(t *testing.T) {
	var sr9 VTOCESR9
	requireDiskSize(t, 0xCC, sr9, unsafe.Sizeof(sr9))
	require.Equal(t, 0x00, int(unsafe.Offsetof(sr9.Header)))
	require.Equal(t, 0x40, int(unsafe.Offsetof(sr9.FileMap0)))
	require.Equal(t, 0xC0, int(unsafe.Offsetof(sr9.FileMap1Ptr)))
	require.Equal(t, 0xC4, int(unsafe.Offsetof(sr9.FileMap2Ptr)))
	require.Equal(t, 0xC8, int(unsafe.Offsetof(sr9.FileMap3Ptr)))

	var sr10 VTOCE
	requireDiskSize(t, 0x150, sr10, unsafe.Sizeof(sr10))
	require.Equal(t, 0x00, int(unsafe.Offsetof(sr10.Header)))
	require.Equal(t, 0xC4, int(unsafe.Offsetof(sr10.FileMap1Ptr)))
	require.Equal(t, 0xC8, int(unsafe.Offsetof(sr10.FileMap2Ptr)))
	require.Equal(t, 0xCC, int(unsafe.Offsetof(sr10.FileMap3Ptr)))
	require.Equal(t, 0xD0, int(unsafe.Offsetof(sr10.FileMap0)))
}

// A VTOCE survives a big-endian encode/decode unchanged and lands its
// fields at the disk offsets.
func TestVTOCERoundTrip(t *testing.T) {
	var e VTOCE
	e.Header.Version = 1
	e.Header.ObjectUID.Hi = 0x11223344
	e.FileMap1Ptr = 0xAABBCCDD
	e.FileMap0[0] = 0x01020304
	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.BigEndian, &e))
	b := buf.Bytes()
	require.Len(t, b, 0x150)
	require.Equal(t, []byte{0x11, 0x22, 0x33, 0x44}, b[4:8])
	require.Equal(t, []byte{0xAA, 0xBB, 0xCC, 0xDD}, b[0xC4:0xC8])
	require.Equal(t, []byte{1, 2, 3, 4}, b[0xD0:0xD4])
	var back VTOCE
	require.NoError(t, binary.Read(bytes.NewReader(b), binary.BigEndian, &back))
	require.Equal(t, e, back)
}

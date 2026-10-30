package fs

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestBlockSizes(t *testing.T) {
	require.Equal(t, uintptr(BlockHeaderSize), unsafe.Sizeof(BlockHeader{}))

	require.Equal(t, uintptr(BlockSize), unsafe.Sizeof(Block{}))
}

func TestPVLabelLayout(t *testing.T) {
	var l PVLabel
	require.Equal(t, 0x32, int(unsafe.Offsetof(l.DriveType)))
	require.Equal(t, 0x34, int(unsafe.Offsetof(l.TotalBlocksInVolume)))
	require.Equal(t, 0x3C, int(unsafe.Offsetof(l.LVDAddr)))
	require.Equal(t, 0x64, int(unsafe.Offsetof(l.AltLVLabelDAddr)))
	require.Equal(t, 0x98, int(unsafe.Offsetof(l.PreCompCylinder)))
	require.Equal(t, 0xA0, int(unsafe.Offsetof(l.AddrEnd)))
	require.Equal(t, 0xA8, int(unsafe.Offsetof(l.NumParts)))
	require.Equal(t, 0xBC, int(unsafe.Offsetof(l.Interleave)))
	require.Equal(t, 0xBE, int(unsafe.Offsetof(l.SectorsPerBlock)))
	require.Equal(t, 0xC4, int(unsafe.Offsetof(l.Revision)))
	require.Equal(t, 0xF6, int(unsafe.Offsetof(l.BadspotLen)))
	require.Equal(t, 0xF8, int(unsafe.Offsetof(l.DiagLen)))
	requireDiskSize(t, 0xFC, l, unsafe.Sizeof(l))
}

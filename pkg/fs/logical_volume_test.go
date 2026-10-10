package fs

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// bat_$label_t (domain_os/bat/bat.h): the kernel copies the BAT header from
// +0x2C, the partition table from +0xFC; the VTOC header is +0x4C..+0xAF.
func TestLVLabelLayout(t *testing.T) {
	// TODO handle lv label version 0
	var l LVLabel
	requireDiskSize(t, 0x308, l, unsafe.Sizeof(l))
	require.Equal(t, 0x24, int(unsafe.Offsetof(l.UID)))
	require.Equal(t, 0x2C, int(unsafe.Offsetof(l.BATHeader)))
	require.Equal(t, 0x4C, int(unsafe.Offsetof(l.VTOCHeader)))
	require.Equal(t, 0xB0, int(unsafe.Offsetof(l.LabelWritten)))
	require.Equal(t, 0xB4, int(unsafe.Offsetof(l.LastMountedNode)))
	require.Equal(t, 0xB8, int(unsafe.Offsetof(l.BootTime)))
	require.Equal(t, 0xBC, int(unsafe.Offsetof(l.DismountedTime)))
	require.Equal(t, 0xC0, int(unsafe.Offsetof(l.CurrentTime)))
	require.Equal(t, 0xCE, int(unsafe.Offsetof(l.SalvageFlag)))
	require.Equal(t, 0xFC, int(unsafe.Offsetof(l.NumPartitions)))
	require.Equal(t, 0xFE, int(unsafe.Offsetof(l.PartitionStartOffset)))
	require.Equal(t, 0x100, int(unsafe.Offsetof(l.PartitionSize)))
	require.Equal(t, 0x108, int(unsafe.Offsetof(l.Partitions)))
}

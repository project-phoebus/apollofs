package fs

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestBATSizes(t *testing.T) {
	var h BATHeader
	requireDiskSize(t, 32, h, unsafe.Sizeof(h))
	require.Equal(t, 0x10, int(unsafe.Offsetof(h.VolumeTrouble)))
	require.Equal(t, 0x14, int(unsafe.Offsetof(h.BatStep)))
	var p BATPartition
	requireDiskSize(t, 8, p, unsafe.Sizeof(p))
}

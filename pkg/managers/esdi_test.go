package managers

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/project-phoebus/apollofs/pkg/drives"
	"github.com/project-phoebus/apollofs/pkg/fs"
)

// The Maxtor XT-4380E (0x504) as MAME's OMTI 8621 has it: 1223 x 15 x 18,
// the labels of two SR10.3 volumes on it.
func TestESDIFormat(t *testing.T) {
	dt, err := drives.GetDriveType(0x504)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "test.awd")
	v, err := Format(path, FormatOptions{DriveType: dt, PVName: "DN3500", PagingFilePages: 40,
		Now: time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.True(t, v.PV.Headers)
	requireClean(t, v)
	require.NoError(t, v.InstallSysboot(bytes.Repeat([]byte{0x4e, 0x71}, 5120), nil))
	require.NoError(t, v.Close())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(1223*15*18*fs.BlockSize), st.Size())

	img, err := os.ReadFile(path)
	require.NoError(t, err)
	block := func(d int) []byte { return img[d*fs.BlockSize : (d+1)*fs.BlockSize] }
	data := func(d int) []byte { return block(d)[fs.BlockHeaderSize:] }
	pv := data(0)
	be16 := func(o int) int { return int(binary.BigEndian.Uint16(pv[o:])) }
	be32 := func(o int) int { return int(binary.BigEndian.Uint32(pv[o:])) }
	require.Equal(t, 0x504, be16(0x32))      // drive type
	require.Equal(t, 1220*270, be32(0x34))   // total blocks: all but 3 cylinders
	require.Equal(t, 18, be16(0x38))         // blocks per track
	require.Equal(t, 15, be16(0x3A))         // heads
	require.Equal(t, 1220*270-1, be32(0x64)) // the alternate LV label in the volume's last block
	require.Equal(t, 1222*270, be32(0x8C))   // badspot cylinder: the last
	require.Equal(t, 1220*270, be32(0x90))   // diagnostic cylinders: right after the volume
	require.Equal(t, 1083, be16(0x96))       // sector size
	require.Equal(t, 0x7FF, be16(0x98))      // precompensation
	require.Equal(t, 1223*270, be32(0xA0))   // addr_end
	require.Equal(t, 0, be16(0xF6))          // no badspot or diagnostic lengths
	require.Equal(t, 10, be16(0xFA))         // cpboot's sysboot blocks
	require.Equal(t, byte(0), pv[0x27])      // name's last byte
	require.Equal(t, "DN3500 ", string(pv[0x08:0x0F]))

	// the diagnostic cylinders: the format fill, headers included
	for _, d := range []int{1220 * 270, 1221*270 + 269} {
		require.Equal(t, bytes.Repeat([]byte{0x6C}, fs.BlockSize), block(d), "block %d", d)
	}
	// the badspot cylinder: an empty list at the start of tracks 0-4, the
	// PV label (as cpboot left it) at the end of tracks 10-14
	bad := 1222 * 270
	for tr := 0; tr < 15; tr++ {
		first, last := bad+tr*18, bad+tr*18+17
		require.Equal(t, uint32(first), binary.BigEndian.Uint32(block(first)[28:]), "track %d", tr)
		if tr < 5 {
			want := make([]byte, fs.BlockDataSize)
			want[0] = 1
			copy(want[fs.BlockDataSize-4:], []byte{0xFF, 0xFF, 0xFF, 0x01})
			require.Equal(t, want, data(first), "track %d", tr)
		} else {
			require.Equal(t, make([]byte, fs.BlockDataSize), data(first), "track %d", tr)
		}
		if tr >= 10 {
			require.Equal(t, pv, data(last), "track %d", tr)
			require.Equal(t, uint32(last), binary.BigEndian.Uint32(block(last)[28:]), "track %d", tr)
		} else {
			require.Equal(t, make([]byte, fs.BlockDataSize), data(last), "track %d", tr)
		}
	}
	require.Equal(t, pv, data(1223*270-1)) // the disk's last block
}

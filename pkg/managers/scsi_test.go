package managers

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/domainos-archeology/apollofs/pkg/drives"
	"github.com/domainos-archeology/apollofs/pkg/fs"
)

// a small SCSI disk: 5120 sectors, 2559 blocks reported, 128 blocks per
// cylinder, so the volume is 2559 - (3*128 + 1) = 2174 blocks
var testSCSIDisk = drives.SCSIDisk{Cylinders: 40, Heads: 4, SectorsPerTrack: 32}

func newSCSIVolume(t *testing.T) (string, *Volume) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.hd")
	d := testSCSIDisk
	v, err := Format(path, FormatOptions{SCSI: &d, PVName: "TEST", PagingFilePages: 40,
		Now: time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return path, v
}

func TestSCSIFormat(t *testing.T) {
	path, v := newSCSIVolume(t)
	require.False(t, v.PV.Headers)
	requireClean(t, v)
	require.NoError(t, v.Close())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(5120*512), st.Size())

	img, err := os.ReadFile(path)
	require.NoError(t, err)
	// the PV label is LBA 0-1, no header in front of it
	require.Equal(t, "APOLLO", string(img[2:8]))
	be16 := func(o int) int { return int(binary.BigEndian.Uint16(img[o:])) }
	be32 := func(o int) int { return int(binary.BigEndian.Uint32(img[o:])) }
	require.Equal(t, 1, be16(0x00))                   // version
	require.Equal(t, 0x700, be16(0x32))               // 2.6 MB: 0 units of 10 MB
	require.Equal(t, 2174, be32(0x34))                // total blocks
	require.Equal(t, 32, be16(0x38))                  // sectors per track
	require.Equal(t, 4, be16(0x3A))                   // heads
	require.Equal(t, 1, be32(0x3C))                   // LV 0 at block 1
	require.Equal(t, 2173, be32(0x64))                // its alternate label in the last block
	require.Equal(t, 2559-129, be32(0x8C))            // badspot list
	require.Equal(t, 2174, be32(0x90))                // diagnostic area
	require.Equal(t, 0, be16(0x94))                   // sector start
	require.Equal(t, 512, be16(0x96))                 // sector size
	require.Equal(t, 0, be16(0x98))                   // precompensation
	require.Equal(t, 1, be16(0x9E))                   // sectors per block (again)
	require.Equal(t, 2559, be32(0xA0))                // addr_end
	require.Equal(t, 1, be16(0xA8))                   // num_parts
	require.Equal(t, 1, be16(0xBE))                   // sectors per block
	require.Equal(t, 0, be16(0xC0))                   // invol's 0xC0 word
	require.Equal(t, 128, be16(0xF6))                 // badspot area
	require.Equal(t, 256, be16(0xF8))                 // diagnostic area
	require.Equal(t, byte(0), img[0x27])              // name's last byte
	require.Equal(t, "TEST ", string(img[0x08:0x0D])) // blank padded
	// the LV label (version 1) at block 1 = byte 1024
	require.Equal(t, 1, int(binary.BigEndian.Uint16(img[1024:])))
	// and its alternate in the last block of the volume
	require.Equal(t, img[1024:2048], img[2173*1024:2174*1024])
}

func TestSCSIFilesRoundTrip(t *testing.T) {
	path, v := newSCSIVolume(t)
	require.NoError(t, v.InstallSysboot(bytes.Repeat([]byte{0x4e, 0x71}, 5120), nil))
	require.NoError(t, v.Names.MkdirAll("/a/b"))
	rng := rand.New(rand.NewSource(1))
	files := map[string][]byte{}
	for i, n := range []int{0, 1, 1024, 33 * 1024, 300 * 1024} {
		data := make([]byte, n)
		rng.Read(data)
		p := "/a/b/f" + string(rune('0'+i))
		_, err := v.Names.WriteFile(p, data, nil, false)
		require.NoError(t, err)
		files[p] = data
	}
	require.NoError(t, v.Names.CreateLink("/a/l", "`node_data/l", false))
	requireClean(t, v)
	v = reopen(t, path, v)
	require.False(t, v.PV.Headers)
	requireClean(t, v)
	for p, data := range files {
		e, err := v.Names.Stat(p)
		require.NoError(t, err)
		got, err := v.Files.ReadData(e)
		require.NoError(t, err)
		require.True(t, bytes.Equal(data, got), p)
	}
	// page 0 of a file is at byte (LV start + daddr) * 1024
	e, err := v.Names.Stat("/a/b/f3")
	require.NoError(t, err)
	d := e.FileMap0[0]
	require.NoError(t, v.Close())
	img, err := os.ReadFile(path)
	require.NoError(t, err)
	off := int(d+1) * 1024
	require.True(t, bytes.Equal(files["/a/b/f3"][:1024], img[off:off+1024]))
	// sysboot's pages are LV blocks 1..10, physical 2..11
	require.Equal(t, []byte{0x4e, 0x71}, img[2*1024:2*1024+2])
}

func TestHeadersDetected(t *testing.T) {
	hpath, hv := newTestVolume(t, 8000)
	require.True(t, hv.PV.Headers)
	require.NoError(t, hv.Close())
	pv, err := fs.Mount(hpath)
	require.NoError(t, err)
	require.True(t, pv.Headers)
	require.Equal(t, int64(1056), pv.BlockBytes())
	require.NoError(t, pv.Unmount())

	spath, sv := newSCSIVolume(t)
	require.NoError(t, sv.Close())
	pv, err = fs.Mount(spath)
	require.NoError(t, err)
	require.False(t, pv.Headers)
	require.Equal(t, int64(1024), pv.BlockBytes())
	require.NoError(t, pv.Unmount())

	junk := filepath.Join(t.TempDir(), "junk")
	require.NoError(t, os.WriteFile(junk, make([]byte, 4096), 0o644))
	_, err = fs.Mount(junk)
	require.Error(t, err)
}

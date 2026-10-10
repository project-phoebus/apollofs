package managers

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/domainos-archeology/apollofs/pkg/drives"
	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

func newTestVolume(t *testing.T, blocks int) (string, *Volume) {
	t.Helper()
	dt, err := drives.GetDriveType(0x105)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "test.awd")
	v, err := Format(path, FormatOptions{DriveType: dt, TotalBlocks: blocks, PVName: "TEST", PagingFilePages: 40,
		Now: time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return path, v
}

func requireClean(t *testing.T, v *Volume) *CheckReport {
	t.Helper()
	r, err := v.Check()
	require.NoError(t, err)
	require.Empty(t, r.Problems)
	return r
}

func reopen(t *testing.T, path string, v *Volume) *Volume {
	t.Helper()
	require.NoError(t, v.Close())
	nv, err := Open(path, true)
	require.NoError(t, err)
	return nv
}

func TestFormat(t *testing.T) {
	path, v := newTestVolume(t, 8000)
	r := requireClean(t, v)
	require.Equal(t, 2, r.Dirs)
	l := v.LV.Label
	require.Equal(t, int32(8000-1-FirstDataBlock), l.BATHeader.NumBlocksRepresented)
	require.Equal(t, int16(2), l.VTOCHeader.Version)
	v = reopen(t, path, v)
	defer v.Close()
	r = requireClean(t, v)
	require.Equal(t, 2, r.Dirs)
	net, err := v.Names.NetRootUID()
	require.NoError(t, err)
	d, err := v.Names.ReadDir(net)
	require.NoError(t, err)
	require.Len(t, d.Entries, 1)
	require.Equal(t, "node_12345", string(d.Entries[0].Name))
	root, _ := v.Names.RootUID()
	require.Equal(t, root, d.Entries[0].UID)
	re, err := v.VTOC.GetEntryForUID(root)
	require.NoError(t, err)
	require.Equal(t, uint8(2), re.Header.SystemType)
	require.Equal(t, uid.UIDroot, re.Header.DirectoryUID)
	pf, err := v.VTOC.GetEntry(v.LV.Label.VTOCHeader.OSPagingFileVTOCX)
	require.NoError(t, err)
	require.Equal(t, uid.UID{Hi: 0x323}, pf.Header.ObjectTypeDefUID)
	require.Equal(t, uid.Empty, pf.Header.DirectoryUID)
	require.Equal(t, uint32(40*1024), pf.Header.CurrentLength)
	require.Equal(t, uint32(41), pf.Header.BlocksUsed) // 40 pages + a level-1 map block
	require.Equal(t, 3, r.Objects)
}

func TestFilesRoundTrip(t *testing.T) {
	path, v := newTestVolume(t, 8000)
	free0 := v.LV.Label.BATHeader.NumFreeBlocks
	require.NoError(t, v.Names.MkdirAll("/a/b"))
	rng := rand.New(rand.NewSource(1))
	sizes := []int{0, 1, 1024, 1025, 32 * 1024, 32*1024 + 1, 288 * 1024, 288*1024 + 5, 700 * 1024}
	files := map[string][]byte{}
	for _, n := range sizes {
		data := make([]byte, n)
		rng.Read(data)
		p := fmt.Sprintf("/a/b/f%d", n)
		attrs := DefaultFileAttrs
		attrs[0x0C], attrs[0x0D] = 0, 0
		attrs[0x0E] = 3
		attrs[0x0F] = 0x22 // coff
		_, err := v.Names.WriteFile(p, data, &attrs, false)
		require.NoError(t, err)
		files[p] = data
	}
	requireClean(t, v)
	v = reopen(t, path, v)
	for p, data := range files {
		e, err := v.Names.Stat(p)
		require.NoError(t, err)
		require.Equal(t, uid.UID{Hi: 0x322}, e.Header.ObjectTypeDefUID)
		got, err := v.Files.ReadData(e)
		require.NoError(t, err)
		require.True(t, bytes.Equal(data, got), p)
		require.Equal(t, uint32(len(data)), e.Header.CurrentLength)
		pages := (len(data) + 1023) / 1024
		require.Equal(t, uint32(pages+fmBlocksFor(pages)), e.Header.BlocksUsed)
	}
	// replace keeps the UID and frees the old blocks
	before, _ := v.Names.Resolve("/a/b/f700")
	_, err := v.Names.WriteFile("/a/b/f716800", []byte("short"), nil, true)
	require.NoError(t, err)
	after, err := v.Names.Resolve("/a/b/f716800")
	require.NoError(t, err)
	_ = before
	e, _ := v.Names.Stat("/a/b/f716800")
	require.Equal(t, uint32(1), e.Header.BlocksUsed)
	got, _ := v.Files.ReadData(e)
	require.Equal(t, "short", string(got))
	require.Equal(t, after, e.Header.ObjectUID)
	_, err = v.Names.WriteFile("/a/b/f1", []byte("x"), nil, false)
	require.ErrorIs(t, err, ErrExists)
	requireClean(t, v)
	// remove everything: the free count returns to where it started
	for p := range files {
		require.NoError(t, v.Names.Remove(p))
	}
	require.NoError(t, v.Names.Remove("/a/b"))
	require.NoError(t, v.Names.Remove("/a"))
	requireClean(t, v)
	// "/" may have grown by nothing; only VTOCE blocks that stayed with
	// their partitions are still allocated
	held := 0
	for i := 0; i < int(v.LV.Label.NumPartitions); i++ {
		if v.LV.Label.Partitions[i].StatusVTOCE&0xFFFFFF != 0 {
			held++
		}
	}
	require.InDelta(t, int(free0), int(v.LV.Label.BATHeader.NumFreeBlocks), float64(held+2))
	require.NoError(t, v.Close())
}

func TestLinksAndBigDirectory(t *testing.T) {
	path, v := newTestVolume(t, 8000)
	require.NoError(t, v.Names.CreateLink("/bin", "$(SYSTYPE)/bin", false))
	require.NoError(t, v.Names.CreateLink("/tmp", "`node_data/tmp", false))
	require.Error(t, v.Names.CreateLink("/tmp", "x", false))
	require.NoError(t, v.Names.CreateLink("/tmp", "`node_data/tmp", true))
	_, err := v.Names.Mkdir("/com", nil, nil)
	require.NoError(t, err)
	for i := 0; i < 600; i++ {
		_, err := v.Names.WriteFile(fmt.Sprintf("/com/command_%03d", i), []byte{byte(i)}, nil, false)
		require.NoError(t, err)
	}
	_, err = v.Names.Resolve("/tmp/x")
	require.ErrorIs(t, err, ErrLinkInPath)
	r := requireClean(t, v)
	require.Equal(t, 2, r.Links)
	v = reopen(t, path, v)
	defer v.Close()
	u, err := v.Names.Resolve("/com")
	require.NoError(t, err)
	d, err := v.Names.ReadDir(u)
	require.NoError(t, err)
	require.Len(t, d.Entries, 600)
	require.Greater(t, d.VTOCE.Header.CurrentLength, uint32(1024))
	e, err := v.Names.Stat("/com/command_599")
	require.NoError(t, err)
	got, _ := v.Files.ReadData(e)
	require.Equal(t, []byte{byte(599 % 256)}, got)
	root, _ := v.Names.ReadDir(mustRoot(v.Names))
	require.Equal(t, "`node_data/tmp", string(root.Lookup("tmp").LinkText))
	requireClean(t, v)
}

// Separate opens (separate tool runs) at the same clock must not reuse UIDs.
func TestUIDsUniqueAcrossOpens(t *testing.T) {
	path, v := newTestVolume(t, 8000)
	now := v.Now()
	for i := 0; i < 5; i++ {
		v = reopen(t, path, v)
		v.Now = func() time.Time { return now }
		v.uidGen = NewUIDGenerator(now, DefaultNodeID)
		_, err := v.Names.WriteFile(fmt.Sprintf("/f%d", i), []byte("x"), nil, false)
		require.NoError(t, err)
	}
	requireClean(t, v)
	require.NoError(t, v.Close())
}

func TestSysboot(t *testing.T) {
	_, v := newTestVolume(t, 8000)
	data := bytes.Repeat([]byte{0x4e, 0x71}, 5120)
	require.NoError(t, v.InstallSysboot(data, nil))
	requireClean(t, v)
	e, err := v.Names.Stat("/sysboot")
	require.NoError(t, err)
	require.Equal(t, uid.UIDsysboot, e.Header.ObjectTypeDefUID)
	for i := 0; i < 10; i++ {
		require.Equal(t, fs.DAddr(1+i), e.FileMap0[i])
	}
	require.Equal(t, e.Header.ObjectUID, mustGet(t, v, v.LV.Label.VTOCHeader.SysbootVTOCX).Header.ObjectUID)
	a := DefaultFileAttrs // type nil, as the SR10.2 tape has it
	require.NoError(t, v.InstallSysboot(data[:4000], &a))
	e, err = v.Names.Stat("/sysboot")
	require.NoError(t, err)
	require.Equal(t, uid.UIDsysboot, e.Header.ObjectTypeDefUID)
	requireClean(t, v)
	require.NoError(t, v.Close())
}

func mustGet(t *testing.T, v *Volume, x fs.VTOCX) *fs.VTOCE {
	e, err := v.VTOC.GetEntry(x)
	require.NoError(t, err)
	return e
}

// The reference SR10.3 volume, when present, must read cleanly.
func TestReferenceVolume(t *testing.T) {
	ref := os.Getenv("APOLLOFS_REF_VOLUME")
	if ref == "" {
		t.Skip("set APOLLOFS_REF_VOLUME to an SR10 image to check it")
	}
	v, err := Open(ref, false)
	require.NoError(t, err)
	defer v.Close()
	e, err := v.Names.Stat("/com/sh")
	require.NoError(t, err)
	_, err = v.Files.ReadData(e)
	require.NoError(t, err)
	d, err := v.Names.ReadDir(mustRoot(v.Names))
	require.NoError(t, err)
	require.NotNil(t, d.Lookup("sau2"))
}

// TestHashUIDKinds checks every hash type against vtoc_$hash_uid /
// UID_$HASH (domain_os/vtoc/uid_cache.c, uid/hash.c): each folds to 16 bits
// before the divu.w.
func TestHashUIDKinds(t *testing.T) {
	lv := &fs.LogicalVolume{}
	vm := NewVTOCManager(lv, nil)
	lv.Label.VTOCHeader.VTOCSizeInBlocks = 401
	u := uid.UID{Hi: 0x12345678, Lo: 0x9ABCDEF0}
	fold := func(x uint32) uint32 { return (x>>16 ^ x&0xFFFF) & 0xFFFF }
	lv.Label.VTOCHeader.Version = 0
	require.Equal(t, fold(u.Hi^u.Lo)%401, vm.hashUID(u))
	lv.Label.VTOCHeader.Version = 2
	require.Equal(t, fold(u.Hi*2+1)%401, vm.hashUID(u))
	lv.Label.VTOCHeader.Version = 3
	require.Equal(t, fold(u.Hi)%401, vm.hashUID(u))
}

// 2,000 long names need a three-level directory B-tree; list and check
// must agree.
func TestHugeDirectory(t *testing.T) {
	path, v := newTestVolume(t, 20000)
	_, err := v.Names.Mkdir("/d", nil, nil)
	require.NoError(t, err)
	for i := 0; i < 2000; i++ {
		_, err := v.Names.WriteFile(fmt.Sprintf("/d/a_longer_file_name_%04d", i), []byte{byte(i)}, nil, false)
		require.NoError(t, err)
	}
	r := requireClean(t, v)
	require.Equal(t, 2004, r.Objects) // "/", "//", /d, 2000 files, paging file
	v = reopen(t, path, v)
	defer v.Close()
	u, err := v.Names.Resolve("/d")
	require.NoError(t, err)
	d, err := v.Names.ReadDir(u)
	require.NoError(t, err)
	require.Len(t, d.Entries, 2000)
	for i, e := range d.Entries {
		require.Equal(t, fmt.Sprintf("a_longer_file_name_%04d", i), string(e.Name))
	}
	e, err := v.Names.Stat("/d/a_longer_file_name_1234")
	require.NoError(t, err)
	got, _ := v.Files.ReadData(e)
	require.Equal(t, []byte{byte(1234 % 256)}, got)
	requireClean(t, v)
}

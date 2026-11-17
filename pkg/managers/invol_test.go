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
	"github.com/project-phoebus/apollofs/pkg/uid"
)

// A whole PRIAM 7050 (0x105), the DN300's disk: the labels the SR10.2 sau2
// invol writes (NOTES.md, "The SR10.2 sau2 invol").
func TestSau2InvolLabels(t *testing.T) {
	dt, err := drives.GetDriveType(0x105)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "dn300.awd")
	now := time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)
	v, err := Format(path, FormatOptions{DriveType: dt, PVName: "DN300", PagingFilePages: 40, Now: now})
	require.NoError(t, err)
	requireClean(t, v)
	nodeData, err := v.Names.Resolve("/sys/node_data")
	require.NoError(t, err)
	require.NoError(t, v.Close())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(62940*fs.BlockSize), st.Size()) // the whole disk

	img, err := os.ReadFile(path)
	require.NoError(t, err)
	data := func(d int) []byte { return img[d*fs.BlockSize+fs.BlockHeaderSize : (d+1)*fs.BlockSize] }
	pv := data(0)
	be16 := func(b []byte, o int) int { return int(binary.BigEndian.Uint16(b[o:])) }
	be32 := func(b []byte, o int) uint32 { return binary.BigEndian.Uint32(b[o:]) }

	require.Equal(t, 1, be16(pv, 0x00))
	require.Equal(t, "DN300 ", string(pv[0x08:0x0E]))
	require.Equal(t, byte(0), pv[0x27])                   // the name's last byte
	require.Equal(t, 0x105, be16(pv, 0x32))               // drive type
	require.Equal(t, uint32(62640), be32(pv, 0x34))       // total_blocks: all but 5 cylinders
	require.Equal(t, uint32(62639), be32(pv, 0x64))       // the alternate LV label: the volume's last block
	require.Equal(t, uint32(62880), be32(pv, 0x8C))       // badspot list: the last cylinder
	require.Equal(t, uint32(62820), be32(pv, 0x90))       // diagnostic area: 2 cylinders back
	require.Equal(t, 0, be16(pv, 0x94))                   // the ANSI driver's words
	require.Equal(t, 1120, be16(pv, 0x96))                //
	require.Equal(t, 0, be16(pv, 0x98))                   // precompensation
	require.Equal(t, 1, be16(pv, 0x9E))                   // sectors per block
	require.Equal(t, uint32(62940), be32(pv, 0xA0))       // addr_end: the disk
	require.Equal(t, uint32(2), be32(pv, 0xA4))           //
	require.Equal(t, 1, be16(pv, 0xA8))                   // num_parts
	require.Equal(t, 0, be16(pv, 0xAC))                   // part_dev[0]: controller 0, unit 0
	require.Equal(t, 0, be16(pv, 0xBC))                   // not the sector interleave
	require.Equal(t, 1, be16(pv, 0xBE))                   // sectors per block
	require.Equal(t, 0, be16(pv, 0xC0))                   //
	require.Equal(t, uint32(0xFEDCA986), be32(pv, 0x3F8)) //

	lv := data(1)
	require.Equal(t, 1, be16(lv, 0x00))
	require.Equal(t, uint32(62639-FirstDataBlock), be32(lv, 0x2C)) // total_blocks
	require.Equal(t, uint32(FirstDataBlock), be32(lv, 0x38))       // first_data_block
	require.Equal(t, 0xE0, int(lv[0x3C]))                          // volume_trouble
	require.Equal(t, uint32(2), be32(lv, 0x3E))                    // step_long: the interleave
	require.Equal(t, 2, be16(lv, 0x4C))                            // hash_type
	clockh := be32(lv, 0xB0)
	require.NotZero(t, clockh)
	for _, o := range []int{0xB4, 0xB8, 0xBC, 0xC0} { // BAT_$MOUNT's
		require.Zero(t, be32(lv, o), "LV label +0x%X", o)
	}
	require.Equal(t, "UTC ", string(lv[0xE2:0xE6]))
	require.Equal(t, clockh, be32(lv, 0xE6)) // last_valid_time
	require.Equal(t, nodeData, uid.UID{Hi: be32(lv, 0xF4), Lo: be32(lv, 0xF8)})
	// BAT partitions: 1043 cylinders in 7 zones of 149, from the first
	// cylinder boundary (LV block 59); the middle half may hold VTOCEs
	require.Equal(t, 7, be16(lv, 0xFC))
	require.Equal(t, 59, be16(lv, 0xFE))
	require.Equal(t, uint32(149*60), be32(lv, 0x100))
	for i, want := range []byte{1, 1, 2, 2, 2, 2, 1, 0} {
		require.Equal(t, want, lv[0x10C+8*i], "partition %d", i)
	}
	require.Equal(t, uint32(0xFEDCA987), be32(lv, 0x3F8))
	require.Equal(t, uint32(0xFFFFFFFF), be32(lv, 0x3FC))
	require.Equal(t, lv, data(62639)) // the alternate copy

	v, err = Open(path, false)
	require.NoError(t, err)
	defer v.Close()
	root, err := v.Names.ReadDir(mustRoot(v.Names))
	require.NoError(t, err)
	var names []string
	for _, e := range root.Entries {
		names = append(names, string(e.Name))
	}
	require.ElementsMatch(t, []string{"lost+found.list", "sys"}, names)
	for p, want := range map[string][]string{"/sys": {"node_data"}, "/sys/node_data": {"system_logs"}, "/sys/node_data/system_logs": nil} {
		u, err := v.Names.Resolve(p)
		require.NoError(t, err)
		d, err := v.Names.ReadDir(u)
		require.NoError(t, err)
		names = nil
		for _, e := range d.Entries {
			names = append(names, string(e.Name))
		}
		require.ElementsMatch(t, want, names, p)
	}
	e, err := v.Names.Stat("/lost+found.list")
	require.NoError(t, err)
	require.False(t, e.IsDirectory())
	require.Zero(t, e.Header.CurrentLength)
}

// The ESDI and SCSI volumes keep step 1.
func TestStepOneVolumes(t *testing.T) {
	dt, err := drives.GetDriveType(0x504)
	require.NoError(t, err)
	v, err := Format(filepath.Join(t.TempDir(), "esdi.awd"), FormatOptions{DriveType: dt, PagingFilePages: 40})
	require.NoError(t, err)
	require.Equal(t, 1, v.BAT.Step())
	require.Zero(t, v.LV.Label.BATHeader.StepBlocks)
	require.NoError(t, v.Close())
	d := drives.SCSIDisk{Cylinders: 100, Heads: 4, SectorsPerTrack: 32}
	v, err = Format(filepath.Join(t.TempDir(), "scsi.awd"), FormatOptions{SCSI: &d, PagingFilePages: 40})
	require.NoError(t, err)
	require.Equal(t, 1, v.BAT.Step())
	require.NoError(t, v.Close())
}

// BAT_$ALLOCATE's walk with step 2: the hint's own block is passed over
// (the stride starts at step - 1), every second block is taken, and a
// chunk whose free blocks were skipped is swept again from its start.
func TestAllocateStepped(t *testing.T) {
	lv := &fs.LogicalVolume{}
	l := &lv.Label
	l.Version = 1
	l.BATHeader = fs.BATHeader{NumBlocksRepresented: 64, BlockNumOfFirstBATBit: 11, BatStep: 2}
	l.NumPartitions, l.PartitionSize = 1, 1000
	bm := NewBATManager(lv)
	bm.InitEmpty()
	for d := fs.DAddr(11); d < 75; d++ {
		require.NoError(t, bm.Free(d))
	}
	require.Equal(t, 2, bm.Step())
	got, err := bm.AllocateRun(5, 20)
	require.NoError(t, err)
	require.Equal(t, []fs.DAddr{21, 23, 25, 27, 29}, got)
	// past the end of the (single) chunk at 75: back to its start, where
	// the stride goes on
	got, err = bm.AllocateRun(4, 70)
	require.NoError(t, err)
	require.Equal(t, []fs.DAddr{71, 73, 11, 13}, got)
	require.Equal(t, 64-9, bm.CountFree())
	require.Equal(t, int32(64-9), l.BATHeader.NumFreeBlocks)
	require.Equal(t, uint32(64-9), l.Partitions[0].FreeCount)
	// step 0 is BAT_$MOUNT's 3
	l.BATHeader.BatStep = 0
	require.Equal(t, 3, bm.Step())
}

// A file's pages on a step-2 volume are two blocks apart.
func TestFileLayoutFollowsStep(t *testing.T) {
	_, v := newTestVolume(t, 8000)
	defer v.Close()
	require.Equal(t, 2, v.BAT.Step())
	_, err := v.Names.WriteFile("/f", bytes.Repeat([]byte{1}, 20*1024), nil, false)
	require.NoError(t, err)
	e, err := v.Names.Stat("/f")
	require.NoError(t, err)
	twos := 0
	for i := 1; i < 20; i++ {
		diff := int(e.FileMap0[i]) - int(e.FileMap0[i-1])
		require.NotEqual(t, 1, diff, "pages %d and %d are adjacent", i-1, i)
		if diff == 2 {
			twos++
		}
	}
	require.GreaterOrEqual(t, twos, 17)
	requireClean(t, v)
}

// mkdir with attributes on a directory invol made gives it those
// attributes, as the install's tape records do; without them it is an
// error as before.
func TestMkdirRestoresInvolDirectory(t *testing.T) {
	_, v := newTestVolume(t, 8000)
	defer v.Close()
	before, err := v.Names.Resolve("/sys")
	require.NoError(t, err)
	_, err = v.Names.Mkdir("/sys", nil, nil)
	require.ErrorIs(t, err, ErrExists)
	a := DefaultDirAttrs
	a[0x2C] = 0x12 // some byte of the attribute image the tape would carry
	dp := [2]fs.DirDefProt{DefaultDefProt, rootDefProt}
	u, err := v.Names.Mkdir("/sys", &a, &dp)
	require.NoError(t, err)
	require.Equal(t, before, u)
	e, err := v.Names.Stat("/sys")
	require.NoError(t, err)
	got := e.Attrs()
	require.Equal(t, a[0x2C], got[0x2C])
	require.Equal(t, before, e.Header.ObjectUID)
	d, err := v.Names.ReadDir(u)
	require.NoError(t, err)
	require.Equal(t, rootDefProt, d.Root.FileDefProt)
	require.NotNil(t, d.Lookup("node_data"))
	_, err = v.Names.Mkdir("/lost+found.list", &a, nil)
	require.ErrorIs(t, err, ErrExists)
	requireClean(t, v)
}

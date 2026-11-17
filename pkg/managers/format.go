package managers

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/project-phoebus/apollofs/pkg/drives"
	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
	"github.com/project-phoebus/apollofs/pkg/util"
)

// FormatOptions describes a new volume.
type FormatOptions struct {
	DriveType drives.DriveType
	// SCSI, when set, makes a SCSI volume of that disk instead: no block
	// headers, and the PV label the SR10.2 invol writes for it (DriveType
	// and TotalBlocks are not used).
	SCSI *drives.SCSIDisk
	// TotalBlocks is the disk's size in blocks: 0 = the drive's capacity
	// (tests use small values).  The volume is smaller by the reserved
	// cylinders invol keeps at the end.
	TotalBlocks int
	PVName      string // PV label name (32 chars, blank padded)
	LVName      string
	NodeID      uint32 // node id in generated UIDs (DefaultNodeID if 0)
	NodeName    string // entry in "//" that names "/" ("node_<id>" if empty)
	Revision    string // invol revision text
	Now         time.Time
	// PagingFilePages preallocates the OS paging file (0 = none).
	PagingFilePages int
}

// DefaultPagingFilePages is the paging file invol makes by default: 1 MB.
// OS_$INIT wires the pageable part of the kernel into it page by page
// (AST_$PMAP_ASSOC), and a page with no disk block is "pmap bad assoc",
// which OS_$INIT reports as "The OS paging file is too small".  An SR10.3
// invol made 954 pages.
const DefaultPagingFilePages = 1024

// FirstDataBlock is the first LV block the BAT represents: LV block 0 is the
// label and 1..10 hold sysboot's ten pages (cpboot).
const FirstDataBlock = 11

const sysbootPages = 10

// The words invol stores at +0x3F8 of the PV label (process_disk) and of
// the LV label (create_lv), and the LV label offsets of the time zone name
// and the last valid time (bat_$label_t tz_name, last_valid_time).
const (
	pvLabelMagic   = 0xFEDCA986
	lvLabelMagic   = 0xFEDCA987
	lvTZNameOff    = 0xE2
	lvLastValidOff = 0xE6
)

// sau2Partitions is create_lv's BAT partition table (SR10): zones of whole
// cylinders, about 10240 blocks each (0x2800 / blocks per cylinder
// cylinders), 1 to 64 of them, the first ending on a cylinder boundary;
// zones the rounding leaves empty are dropped.  It returns the count, the
// partition_start_offset (the LV block of the first cylinder boundary, 0
// when the LV starts on one) and the partition size in blocks.
func sau2Partitions(lvStart fs.DAddr, lvSize, bpc int) (n, startOff, size int) {
	cyls := lvSize / bpc
	per := 0x2800 / bpc
	if per < 1 {
		per = 1
	}
	n = (cyls + per - 1) / per
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	startOff = bpc - int(lvStart)%bpc
	if startOff == bpc {
		startOff = 0
	}
	cylsPer := (cyls + n - 1) / n
	size = cylsPer * bpc
	n -= (cylsPer*n - cyls) / cylsPer
	return n, startOff, size
}

func padName(s string) [32]byte {
	var b [32]byte
	for i := range b {
		b[i] = ' '
	}
	copy(b[:], s)
	return b
}

// Format creates an empty SR10 volume: PV label, LV label (and its
// alternate copy in the last block), BAT, the VTOC bucket area, and the
// "/" (disk entry) and "//" (network root) directories.  The layout
// follows an SR10.3 invol's: the bucket area in the middle of the volume
// with the BAT right behind it and the root directories after that.
func Format(path string, opt FormatOptions) (*Volume, error) {
	if opt.Now.IsZero() {
		opt.Now = util.Now()
	}
	if opt.NodeID == 0 {
		opt.NodeID = DefaultNodeID
	}
	if opt.NodeName == "" {
		opt.NodeName = fmt.Sprintf("node_%x", opt.NodeID)
	}
	if opt.Revision == "" {
		opt.Revision = "revision 10.2"
	}
	dt := opt.DriveType
	total := opt.TotalBlocks
	if total == 0 {
		total = int(dt.TotalBlocks())
	}
	headers := true
	imageBytes := int64(total) * fs.BlockSize
	var scsiBlocks int
	if opt.SCSI != nil {
		// The SR10.2 invol on a SCSI disk (see NOTES.md): the volume ends
		// a badspot list and a diagnostic area before the size DINIT
		// reports, which is the label's addr_end.
		if err := opt.SCSI.Validate(); err != nil {
			return nil, err
		}
		headers = false
		dt = opt.SCSI.DriveType()
		scsiBlocks = opt.SCSI.Blocks()
		total = scsiBlocks - (3*opt.SCSI.BlocksPerCylinder() + 1)
		imageBytes = opt.SCSI.ImageBytes()
	}
	var esdi *drives.ESDIDisk
	if opt.SCSI == nil && opt.TotalBlocks == 0 && dt.IsESDI() {
		// An SR10 invol on an ESDI disk (see NOTES.md): the image is the
		// cylinders the controller addresses, and the volume ends three
		// cylinders before their end.
		esdi = &drives.ESDIDisk{DriveType: dt}
		total = esdi.VolumeBlocks()
		imageBytes = int64(esdi.AddrEnd()) * fs.BlockSize
	}
	bpc := int(dt.BlocksPerTrack) * int(dt.Heads)
	// The other Winchesters: the SR10.2 sau2 invol (see NOTES.md, "The
	// SR10.2 sau2 invol").  The disk is the driver's block count (the
	// drive table's, or TotalBlocks); set_disk_layout keeps the last
	// cylinders out of the volume for the badspot list and the
	// diagnostic area.
	sau2 := opt.SCSI == nil && esdi == nil
	var addrEnd int
	var layout drives.InvolLayout
	if sau2 && bpc != 0 {
		addrEnd = total
		layout = drives.WinchesterLayout(dt.DType, bpc)
		total = addrEnd - layout.Reserved
	}
	if total < 2000 || bpc == 0 {
		return nil, fmt.Errorf("volume too small (%d blocks)", total)
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(imageBytes); err != nil {
		f.Close()
		return nil, err
	}
	clockh := util.TimestampToApolloEpoch(opt.Now)
	gen := NewUIDGenerator(opt.Now, opt.NodeID)

	lvStart := fs.DAddr(1)
	lvSize := total - 1                    // LV blocks 0 .. total-2
	altLV := fs.DAddr(total - 1)           // physical
	altLVRel := fs.DAddr(lvSize - 1)       // LV-relative, marked in use
	represented := lvSize - FirstDataBlock // LV 11 .. lvSize-1
	nBAT := (represented + batBitsPerBlock - 1) / batBitsPerBlock

	// VTOC hash area: about one bucket block per 364 blocks (an SR10.3
	// invol gave a 329400-block disk 906), hash size 4n-1.
	nBkt := (lvSize + 363) / 364
	if nBkt < 8 {
		nBkt = 8
	}
	bktStart := fs.DAddr(lvSize/2 - nBkt/2)
	batStart := bktStart + fs.DAddr(nBkt)

	// BAT partitions: cylinder aligned, about 32 of them.
	cyls := total / bpc
	partSize := ((cyls + 31) / 32) * bpc
	partStart := bpc - 1 // LV block of physical cylinder 1
	nParts := (lvSize-1-partStart)/partSize + 1
	partStatus := func(i int) uint32 {
		if i >= nParts/4 && i < nParts-nParts/4 {
			return 2
		}
		return 1
	}
	if sau2 {
		nParts, partStart, partSize = sau2Partitions(lvStart, lvSize, bpc)
		lo, hi := (nParts+2)/4, (nParts+2)/4+(nParts+1)/2
		partStatus = func(i int) uint32 {
			if i >= lo && i < hi {
				return 2
			}
			return 1
		}
	}
	if nParts > 64 {
		return nil, fmt.Errorf("too many BAT partitions (%d)", nParts)
	}

	pv := fs.PVLabel{
		Version:             1,
		APOLLO:              [6]byte{'A', 'P', 'O', 'L', 'L', 'O'},
		Name:                padName(opt.PVName),
		UID:                 gen.Next(),
		DriveType:           dt.DType,
		TotalBlocksInVolume: int32(total),
		BlocksPerTrack:      dt.BlocksPerTrack,
		TracksPerCylinder:   dt.Heads,
		PhysBadspotDAddr:    fs.DAddr(total),
		PhysDiagDAddr:       fs.DAddr(total),
		SectorStart:         0,
		SectorSize:          fs.BlockSize,
		PreCompCylinder:     0x7FF,
		Reserved9A:          [6]byte{0, 0, 0, 0, 0, 1},
		SysbootDAddr:        2,
		NumParts:            1,
		PartNum:             1,
		SectorsPerBlock:     1,
		WordC0:              1,
		RevisionLen:         uint16(len(opt.Revision)),
	}
	if opt.SCSI != nil {
		// What the SR10.2 invol stores for a SCSI disk: WIN_SCSI_$DINIT's
		// sector length (format device page) and no start offset; the
		// precompensation cylinder and the word at 0xC0 stay as invol
		// set them before the assign (0); one sector per block, also at
		// 0x9E; the badspot list in the last cylinder + 1 blocks and the
		// diagnostic area in the two cylinders before it, both past the
		// volume, and addr_end the whole disk; the name's last byte NUL.
		bpcs := opt.SCSI.BlocksPerCylinder()
		pv.SectorSize = drives.SCSISectorBytes
		pv.PreCompCylinder = 0
		pv.WordC0 = 0
		pv.AddrEnd = uint32(scsiBlocks)
		pv.PhysBadspotDAddr = fs.DAddr(scsiBlocks - (bpcs + 1))
		pv.PhysDiagDAddr = fs.DAddr(total)
		pv.BadspotLen = uint16(bpcs)
		pv.DiagLen = uint16(2 * bpcs)
		pv.Name[len(pv.Name)-1] = 0
	}
	if esdi != nil {
		// What an SR10.3 invol stored for a 0x504 disk: the controller's
		// sector size, the diagnostic cylinders right after the volume and
		// the badspot cylinder after them, addr_end the whole disk, the
		// name's last byte NUL.
		pv.SectorSize = drives.ESDISectorSize
		pv.AddrEnd = uint32(esdi.AddrEnd())
		pv.PhysDiagDAddr = fs.DAddr(esdi.DiagDAddr())
		pv.PhysBadspotDAddr = fs.DAddr(esdi.BadspotDAddr())
		pv.Name[len(pv.Name)-1] = 0
	}
	if sau2 {
		// process_disk: total_blocks the usable blocks, addr_end the
		// disk, the badspot list and diagnostic area set_disk_layout
		// placed, the name's last byte NUL; the ANSI driver's sector
		// words, precompensation 0 and the word at 0xC0 0
		pv.AddrEnd = uint32(addrEnd)
		pv.PhysBadspotDAddr = fs.DAddr(addrEnd - layout.BadspotBack)
		pv.PhysDiagDAddr = fs.DAddr(addrEnd - layout.DiagBack)
		pv.Name[len(pv.Name)-1] = 0
		if start, size, ok := drives.ANSISectorWords(dt.DType); ok {
			pv.SectorStart, pv.SectorSize = start, size
			pv.PreCompCylinder = 0
			pv.WordC0 = 0
		}
	}
	pv.LVDAddr[0] = lvStart
	pv.AltLVLabelDAddr[0] = altLV
	copy(pv.Revision[:], opt.Revision)
	pvBlock := fs.NewBlock(fs.BlockHeader{ObjectUID: uid.UIDpvlabel, LastWritten: clockh}, pv)
	if sau2 {
		binary.BigEndian.PutUint32(pvBlock.Data[0x3F8:], pvLabelMagic)
	}
	if err := fs.WriteBlockTo(f, headers, 0, &pvBlock); err != nil {
		f.Close()
		return nil, err
	}
	lvBlock := fs.NewBlock(fs.BlockHeader{ObjectUID: uid.UIDlvlabel, LastWritten: clockh, PVBlockDAddr: lvStart}, fs.LVLabel{Version: 1})
	if sau2 {
		// create_lv: the SR10 word, and no alternate LV label word
		binary.BigEndian.PutUint32(lvBlock.Data[0x3F8:], lvLabelMagic)
		binary.BigEndian.PutUint32(lvBlock.Data[0x3FC:], 0xFFFFFFFF)
	}
	if err := fs.WriteBlockTo(f, headers, lvStart, &lvBlock); err != nil {
		f.Close()
		return nil, err
	}
	altBlock := lvBlock
	altBlock.Header.PVBlockDAddr = altLV
	if err := fs.WriteBlockTo(f, headers, altLV, &altBlock); err != nil {
		f.Close()
		return nil, err
	}
	if esdi != nil {
		if err := writeESDIReserved(f, *esdi, &pvBlock, &pv, clockh); err != nil {
			f.Close()
			return nil, err
		}
	}
	f.Close()

	pvol, err := fs.MountWritable(path)
	if err != nil {
		return nil, err
	}
	v := newVolume(pvol, true)
	v.Now = func() time.Time { return opt.Now }
	v.uidGen = gen
	l := &v.LV.Label
	l.Version = 1
	l.Name = padName(opt.LVName)
	l.UID = gen.Next()
	l.BATHeader = fs.BATHeader{
		NumBlocksRepresented:  int32(represented),
		NumFreeBlocks:         0,
		FirstBATBlockDAddr:    batStart,
		BlockNumOfFirstBATBit: FirstDataBlock,
		VolumeTrouble:         0xE000,
		BatStep:               1,
	}
	if sau2 {
		// create_lv stores the LV interleave as the longword step_long
		l.BATHeader.StepBlocks = 0
		l.BATHeader.BatStep = uint16(dt.DefaultInterleave())
	}
	l.VTOCHeader = fs.VTOCHeader{
		Version:          2,
		VTOCSizeInBlocks: int16(4*nBkt - 1),
		VTOCBlocksUsed:   int32(nBkt),
	}
	binary.BigEndian.PutUint16(l.VTOCHeader.VTOCMapData[0:], uint16(nBkt))
	binary.BigEndian.PutUint32(l.VTOCHeader.VTOCMapData[2:], uint32(bktStart))
	if sau2 {
		// create_lv writes only the mount time and the last valid time,
		// with the time zone name "UTC "; the rest is BAT_$MOUNT's
		l.LabelWritten = clockh
		copy(l.Reserved2[lvTZNameOff-0xD0:], "UTC ")
		binary.BigEndian.PutUint32(l.Reserved2[lvLastValidOff-0xD0:], clockh)
	} else {
		l.LabelWritten, l.BootTime, l.DismountedTime, l.CurrentTime = clockh, clockh, clockh, clockh
		l.LastMountedNode = opt.NodeID
	}
	l.NumPartitions = uint16(nParts)
	l.PartitionStartOffset = uint16(partStart)
	l.PartitionSize = uint32(partSize)
	for i := 0; i < nParts; i++ {
		l.Partitions[i].StatusVTOCE = partStatus(i) << 24
	}
	v.LV.VTOCMap = fs.VTOCMap{{NumBlocks: uint16(nBkt), FirstBlockDAddr: bktStart}}
	for i := 1; i < 10; i++ {
		v.LV.VTOCMap = append(v.LV.VTOCMap, fs.VTOCMapExtent{})
	}

	// everything free, then take the fixed areas
	v.BAT.InitEmpty()
	v.BAT.now = v.clockh
	for d := fs.DAddr(FirstDataBlock); d < fs.DAddr(FirstDataBlock+represented); d++ {
		if err := v.BAT.Free(d); err != nil {
			return nil, err
		}
	}
	reserve := func(from fs.DAddr, n int) error {
		for i := 0; i < n; i++ {
			if err := v.BAT.MarkUsed(from + fs.DAddr(i)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := reserve(altLVRel, 1); err != nil {
		return nil, err
	}
	if err := reserve(bktStart, nBkt); err != nil {
		return nil, err
	}
	if err := reserve(batStart, nBAT); err != nil {
		return nil, err
	}
	for i := 0; i < nBkt; i++ {
		if err := v.VTOC.InitBucketBlock(bktStart + fs.DAddr(i)); err != nil {
			return nil, err
		}
	}

	// "/" and "//"
	hint := batStart + fs.DAddr(nBAT)
	mkRoot := func(parent uid.UID) (*Dir, error) {
		e := &fs.VTOCE{}
		e.SetAttrs(rootDirAttrs)
		t := util.ClockBytes(opt.Now)
		e.Header.DTM, e.Header.DTU, e.Header.DTC, e.Header.DTA = t, t, t, t
		e.Header.ObjectUID = gen.Next()
		e.Header.DirectoryUID = parent
		e.Header.CurrentLength, e.Header.BlocksUsed = 0, 0
		x, err := v.VTOC.AllocateEntry(e, hint)
		if err != nil {
			return nil, err
		}
		d := &Dir{UID: e.Header.ObjectUID, X: x, VTOCE: e, Root: fs.NewDirRootArea(rootDefProt, rootDefProt)}
		return d, nil
	}
	root, err := mkRoot(uid.UIDroot)
	if err != nil {
		return nil, err
	}
	netRoot, err := mkRoot(uid.Empty)
	if err != nil {
		return nil, err
	}
	if err := v.Names.WriteDir(root); err != nil {
		return nil, err
	}
	netRoot.Entries = []fs.DirEnt{{Type: fs.DirEntryHardLink, Name: []byte(opt.NodeName), UID: root.UID}}
	if err := v.Names.WriteDir(netRoot); err != nil {
		return nil, err
	}
	l.VTOCHeader.DiskEntryDirVTOCX = root.X
	l.VTOCHeader.NetworkRootDirVTOCX = netRoot.X

	// create_lv's other root objects: /lost+found.list (an empty file),
	// /sys, /sys/node_data and /sys/node_data/system_logs; node_data's
	// UID goes in the label at +0xF4
	var nodeData uid.UID
	if sau2 {
		if _, err := v.Names.WriteFile("/lost+found.list", nil, nil, false); err != nil {
			return nil, err
		}
		for _, p := range []string{"/sys", "/sys/node_data", "/sys/node_data/system_logs"} {
			u, err := v.Names.Mkdir(p, nil, nil)
			if err != nil {
				return nil, err
			}
			if p == "/sys/node_data" {
				nodeData = u
			}
		}
	}

	// The OS paging file: an empty, uncatalogued os_pg_file object whose
	// VTOCX OS_$INIT reads from label+0x5C (VTOC header current_vtoce);
	// the kernel sets its length itself (FILE_$SET_LEN).  Without it the
	// boot says "Boot device has no OS paging file" and never pages.
	pf := &fs.VTOCE{}
	pf.SetAttrs(pagingFileAttrs)
	t := util.ClockBytes(opt.Now)
	pf.Header.DTM, pf.Header.DTU, pf.Header.DTC, pf.Header.DTA = t, t, t, t
	pf.Header.ObjectUID = gen.Next()
	pfx, err := v.VTOC.AllocateEntry(pf, root.X.BlockDAddr())
	if err != nil {
		return nil, err
	}
	if opt.PagingFilePages > 0 {
		if err := v.Files.WriteData(pf, make([]byte, opt.PagingFilePages*pageBytes), 0, pfx.BlockDAddr()); err != nil {
			return nil, err
		}
		if err := v.VTOC.PutEntry(pfx, pf); err != nil {
			return nil, err
		}
	}
	l.VTOCHeader.OSPagingFileVTOCX = pfx
	// an SR10.3 invol also leaves the paging file's UID at label+0xF4;
	// the SR10.2 sau2 invol, node_data's
	u := pf.Header.ObjectUID
	if sau2 {
		u = nodeData
	}
	binary.BigEndian.PutUint32(l.Reserved2[0x24:], u.Hi)
	binary.BigEndian.PutUint32(l.Reserved2[0x28:], u.Lo)
	if err := v.Flush(); err != nil {
		return nil, err
	}
	return v, nil
}

// InstallSysboot writes sysboot as the /sysboot object whose ten pages
// occupy LV blocks 1..10 (outside the BAT), records it in the VTOC header
// and catalogs it in "/" (cpboot).  An existing /sysboot is replaced.
func (v *Volume) InstallSysboot(data []byte, attrs *Attrs) error {
	if len(data) > sysbootPages*pageBytes {
		return fmt.Errorf("sysboot is %d bytes; at most %d fit", len(data), sysbootPages*pageBytes)
	}
	nm := v.Names
	root, err := nm.ReadDir(mustRoot(nm))
	if err != nil {
		return err
	}
	var e *fs.VTOCE
	var x fs.VTOCX
	if ent := root.Lookup("sysboot"); ent != nil {
		if ent.Type != fs.DirEntryObject {
			return fmt.Errorf("/sysboot is not a file")
		}
		if x, err = v.VTOC.GetIndexForUID(ent.UID); err != nil {
			return err
		}
		if e, err = v.VTOC.GetEntry(x); err != nil {
			return err
		}
		// pages below the BAT are not freed; anything else is
		m, fmb, err := v.VTOC.FileMap(e)
		if err != nil {
			return err
		}
		for _, d := range append(m, fmb...) {
			if d >= FirstDataBlock {
				if err := v.BAT.Free(d); err != nil {
					return err
				}
			}
		}
		e.FileMap0 = [32]fs.DAddr{}
	} else {
		a := DefaultFileAttrs
		if attrs != nil {
			a = *attrs
		}
		e = nm.newVTOCE(&a, false, root.UID)
		if attrs == nil {
			nm.stampTimes(e)
		}
		e.Header.ObjectTypeDefUID = uid.UIDsysboot
		if x, err = v.VTOC.AllocateEntry(e, root.X.BlockDAddr()); err != nil {
			return err
		}
		root.Entries = append(root.Entries, fs.DirEnt{Type: fs.DirEntryObject, Name: []byte("sysboot"), UID: e.Header.ObjectUID})
		if err := nm.WriteDir(root); err != nil {
			return err
		}
	}
	if attrs != nil {
		u, dirp := e.Header.ObjectUID, e.Header.DirectoryUID
		e.SetAttrs(*attrs)
		e.Header.ObjectUID, e.Header.DirectoryUID = u, dirp
		e.Header.Flags |= fs.VTOCEFlagInUse
	}
	// whatever the source file's type was (the SR10.2 tape records sysboot
	// as nil), the installed boot file is a sysboot object, as on an
	// SR10.3 volume
	e.Header.ObjectTypeDefUID = uid.UIDsysboot
	now := v.clockh()
	pages := (len(data) + pageBytes - 1) / pageBytes
	for i := 0; i < pages; i++ {
		d := fs.DAddr(1 + i)
		var b fs.Block
		b.Header = fs.BlockHeader{ObjectUID: e.Header.ObjectUID, PageWithinObject: int32(i), LastWritten: now,
			PVBlockDAddr: d + v.LV.StartDAddr()}
		copy(b.Data[:], data[i*pageBytes:])
		if err := v.LV.WriteBlock(d, &b); err != nil {
			return err
		}
		e.FileMap0[i] = d
	}
	e.Header.CurrentLength = uint32(len(data))
	e.Header.BlocksUsed = uint32(pages)
	if err := v.VTOC.PutEntry(x, e); err != nil {
		return err
	}
	v.LV.Label.VTOCHeader.SysbootVTOCX = x
	// the SR10.2 invol records the pages it installs in the PV label: the
	// PROM loads that many blocks from SysbootDAddr
	b, err := v.PV.ReadBlock(0)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(b.Data[0xFA:], uint16(pages))
	if err := v.PV.WriteBlock(0, b); err != nil {
		return fmt.Errorf("writing the PV label: %w", err)
	}
	v.PV.Label.SysbootBlocks = uint16(pages)
	if err := writePVLabelCopies(b, &v.PV.Label, v.PV.WriteBlock); err != nil {
		return fmt.Errorf("writing the PV label's copies: %w", err)
	}
	return nil
}

func mustRoot(nm *NamingManager) uid.UID {
	u, err := nm.RootUID()
	if err != nil {
		panic(err)
	}
	return u
}

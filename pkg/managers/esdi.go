package managers

import (
	"bytes"
	"os"

	"github.com/project-phoebus/apollofs/pkg/drives"
	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
)

// The cylinders past an ESDI volume, as an SR10.3 invol leaves them (two
// volumes on MAME's OMTI 8621; see NOTES.md, "ESDI volumes"):
//   - the two diagnostic cylinders formatted, every byte of every block the
//     controller's format fill;
//   - every block of the badspot cylinder with a header of the words below
//     (not understood; DISK_$READ_MFG_BADSPOTS takes a header error as ok)
//     and zero data, but for an empty badspot list in the first block of
//     tracks 0-4 and a copy of the PV label in the last block of the last
//     five tracks.
const (
	esdiDiagFill = 0x6C
	esdiCopies   = 5
)

var esdiBadspotHeader = fs.BlockHeader{
	ObjectUID:        uid.UID{Hi: 0x01FF8302, Lo: 0x0104BE9A},
	PageWithinObject: 0x01FFB16A,
	BlockSystemTypes: 0x01FF0000,
}

// esdiPVLabelCopies returns where an ESDI volume keeps copies of its PV
// label, nil for other volumes.
func esdiPVLabelCopies(l *fs.PVLabel) []fs.DAddr {
	if !(drives.DriveType{DType: l.DriveType}).IsESDI() || l.AddrEnd == 0 {
		return nil
	}
	bpt, heads := int(l.BlocksPerTrack), int(l.TracksPerCylinder)
	var ds []fs.DAddr
	for t := heads - esdiCopies; t < heads; t++ {
		ds = append(ds, l.PhysBadspotDAddr+fs.DAddr(t*bpt+bpt-1))
	}
	return ds
}

// writeESDIReserved writes the diagnostic and badspot cylinders of a new
// ESDI volume whose PV label block is pv.
func writeESDIReserved(f *os.File, d drives.ESDIDisk, pv *fs.Block, label *fs.PVLabel, clockh uint32) error {
	diag := bytes.Repeat([]byte{esdiDiagFill}, 2*d.BlocksPerCylinder()*fs.BlockSize)
	if _, err := f.WriteAt(diag, int64(d.DiagDAddr())*fs.BlockSize); err != nil {
		return err
	}
	bpt := int(d.BlocksPerTrack)
	for i := 0; i < d.BlocksPerCylinder(); i++ {
		b := fs.Block{Header: esdiBadspotHeader}
		b.Header.LastWritten = clockh
		b.Header.PVBlockDAddr = fs.DAddr(d.BadspotDAddr() + i)
		if i%bpt == 0 && i/bpt < esdiCopies {
			b.Data[0] = 1
			copy(b.Data[fs.BlockDataSize-4:], []byte{0xFF, 0xFF, 0xFF, 0x01})
		}
		if err := fs.WriteBlockTo(f, true, b.Header.PVBlockDAddr, &b); err != nil {
			return err
		}
	}
	return writePVLabelCopies(pv, label, func(d fs.DAddr, b *fs.Block) error { return fs.WriteBlockTo(f, true, d, b) })
}

// writePVLabelCopies writes the PV label block pv into the label's copies
// with write.
func writePVLabelCopies(pv *fs.Block, label *fs.PVLabel, write func(fs.DAddr, *fs.Block) error) error {
	for _, d := range esdiPVLabelCopies(label) {
		c := *pv
		c.Header.PVBlockDAddr = d
		if err := write(d, &c); err != nil {
			return err
		}
	}
	return nil
}

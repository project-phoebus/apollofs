package drives

import (
	"errors"
	"fmt"
)

// SCSIClass is the drive-type class of a SCSI disk (dtype 0x7nn).
const SCSIClass = 0x700

// SCSISectorBytes is the sector length the SCSI disks apollofs makes
// report in READ CAPACITY and the format device page.  WIN_SCSI moves an
// Apollo block as 1024 / sector length sectors from LBA daddr * that.
const SCSISectorBytes = 512

// SCSIDisk is a SCSI disk as WIN_SCSI_$DINIT and the SR10.2 invol see it:
// the cylinders and heads of its rigid disk geometry mode page (4), the
// sectors per track of its format device page (3), 512-byte sectors, and
// READ CAPACITY's last LBA, C*H*S - 1.  SCSI disks keep no Apollo block
// headers (WIN_SCSI_$CINIT's unit flags 0x8000): the image is a flat array
// of 1024-byte blocks, Apollo block N at byte N*1024.
type SCSIDisk struct {
	Cylinders       int
	Heads           int
	SectorsPerTrack int
}

// ParseSCSIDisk reads "cylinders/heads/sectors" (sectors per track, in
// 512-byte sectors).
func ParseSCSIDisk(s string) (SCSIDisk, error) {
	var d SCSIDisk
	var rest string
	n, _ := fmt.Sscanf(s, "%d/%d/%d%s", &d.Cylinders, &d.Heads, &d.SectorsPerTrack, &rest)
	if n != 3 {
		return d, fmt.Errorf("SCSI geometry %q: want cylinders/heads/sectors", s)
	}
	return d, d.Validate()
}

// Validate checks that the geometry fits the PV label's 16-bit fields and
// the kernel's blocks_per_cyl (a 16-bit divisor).
func (d SCSIDisk) Validate() error {
	if d.Cylinders < 1 || d.Heads < 1 || d.Heads > 255 || d.SectorsPerTrack < 1 || d.SectorsPerTrack > 0x7FFF {
		return fmt.Errorf("bad SCSI geometry %d/%d/%d", d.Cylinders, d.Heads, d.SectorsPerTrack)
	}
	if d.BlocksPerCylinder() > 0x7FFF {
		return errors.New("SCSI geometry: heads x sectors per track must be below 32768")
	}
	if d.Sectors() > 0xFFFFFFFF {
		return errors.New("SCSI disk too large")
	}
	return nil
}

// Sectors is the disk's size in 512-byte sectors.
func (d SCSIDisk) Sectors() int64 {
	return int64(d.Cylinders) * int64(d.Heads) * int64(d.SectorsPerTrack)
}

// ImageBytes is the size of the whole disk's image.
func (d SCSIDisk) ImageBytes() int64 { return d.Sectors() * SCSISectorBytes }

// Blocks is the size in 1024-byte blocks WIN_SCSI_$DINIT reports to an
// assign (invol): READ CAPACITY's last LBA times the sector length over
// 1024.  The kernel and the SR10.2 invol multiply the last LBA, not the
// sector count, so the disk's last block is not counted.
func (d SCSIDisk) Blocks() int {
	return int((d.Sectors() - 1) * SCSISectorBytes / 1024)
}

// DType is the drive type WIN_SCSI_$DINIT reports: the capacity (last LBA
// times the sector length) in units of 10 MB in the low byte, class 7 added
// to the high byte, 0x700 if that is above 0x7FF.
func (d SCSIDisk) DType() int16 {
	units := uint16((d.Sectors() - 1) * SCSISectorBytes / 10485760)
	v := uint16(byte(units>>8)+7)<<8 | units&0xFF
	if v > 0x7FF {
		v = SCSIClass
	}
	return int16(v)
}

// BlocksPerCylinder is the kernel's blocks_per_cyl for the volume: heads x
// sectors per track shifted down by log2 of the label's sectors per block,
// which the SR10.2 invol leaves at 1 for a SCSI disk.
func (d SCSIDisk) BlocksPerCylinder() int { return d.Heads * d.SectorsPerTrack }

// DriveType returns the geometry as the PV label records it: the dtype,
// sectors per track (as "blocks per track") and heads.
func (d SCSIDisk) DriveType() DriveType {
	return DriveType{DType: d.DType(), Name: "SCSI", Cylinders: int16(d.Cylinders), Heads: int16(d.Heads),
		BlocksPerTrack: int16(d.SectorsPerTrack)}
}

// MAMEGuess returns the geometry MAME's raw (.hd) hard disk loader guesses
// for an image of the given size (src/lib/util/harddisk.cpp): the first
// sector count from the image's that is sectors x heads x cylinders with
// sectors 63 down to 2 and heads 16 down to 2.  Its SCSI disk reports that
// geometry in READ CAPACITY and mode pages 3 and 4, so an image for MAME
// should have a geometry the guess reproduces.
func MAMEGuess(imageBytes int64) SCSIDisk {
	for total := imageBytes / 512; ; total++ {
		for s := int64(63); s > 1; s-- {
			if total%s != 0 {
				continue
			}
			th := total / s
			for h := int64(16); h > 1; h-- {
				if th%h == 0 {
					return SCSIDisk{Cylinders: int(th / h), Heads: int(h), SectorsPerTrack: int(s)}
				}
			}
		}
	}
}

// Name describes a drive type: the table's name, or the SCSI class.
func Name(dtype int16) string {
	if dt, err := GetDriveType(dtype); err == nil {
		return dt.Name
	}
	if uint16(dtype)&0xFF00 == SCSIClass {
		return fmt.Sprintf("SCSI, about %d MB", int(dtype&0xFF)*10)
	}
	return "unknown"
}

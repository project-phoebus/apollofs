package drives

// ESDIClass is the drive-type class of the 5 1/4" ESDI disks (dtype 0x5nn),
// the DN3500/DN4500/DN5500's disks behind the OMTI 8621.
const ESDIClass = 0x500

// ESDISectorSize is the sector size an SR10 invol records in the PV label of
// an ESDI disk: the 1056-byte block plus the controller's framing.
const ESDISectorSize = 1083

// IsESDI reports whether the drive type is in the ESDI class.
func (dt DriveType) IsESDI() bool { return uint16(dt.DType)&0xFF00 == ESDIClass }

// ESDIDisk is an ESDI disk as the controller presents it and an SR10 invol
// lays it out (see NOTES.md, "ESDI volumes"): the controller addresses one
// cylinder fewer than the drive table lists (the Maxtor XT-4380E, 0x504:
// 1223 of 1224, as MAME's OMTI 8621 has it and two SR10.3 volumes' labels
// record), and invol keeps the last three of those cylinders out of the
// volume: two for the diagnostic area, then one for the badspot list.
type ESDIDisk struct {
	DriveType
}

// Cylinders is the number of cylinders the controller addresses.
func (d ESDIDisk) Cylinders() int { return int(d.DriveType.Cylinders) - 1 }

// BlocksPerCylinder is heads x blocks per track.
func (d ESDIDisk) BlocksPerCylinder() int { return int(d.Heads) * int(d.BlocksPerTrack) }

// AddrEnd is the disk's size in blocks: the PV label's addr_end.
func (d ESDIDisk) AddrEnd() int { return d.Cylinders() * d.BlocksPerCylinder() }

// VolumeBlocks is the PV label's total_blocks: every cylinder but the last
// three.
func (d ESDIDisk) VolumeBlocks() int { return (d.Cylinders() - 3) * d.BlocksPerCylinder() }

// DiagDAddr is the first block of the two diagnostic cylinders, right after
// the volume.
func (d ESDIDisk) DiagDAddr() int { return d.VolumeBlocks() }

// BadspotDAddr is the first block of the badspot cylinder, the last one.
func (d ESDIDisk) BadspotDAddr() int { return (d.Cylinders() - 1) * d.BlocksPerCylinder() }

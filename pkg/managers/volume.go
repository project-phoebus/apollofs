package managers

import (
	"errors"
	"fmt"
	"time"

	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
	"github.com/project-phoebus/apollofs/pkg/util"
)

// Volume ties the managers for one SR10 logical volume together.
type Volume struct {
	PV       *fs.PhysicalVolume
	LV       *fs.LogicalVolume
	BAT      *BATManager
	VTOC     *VTOCManager
	Files    *FileManager
	Names    *NamingManager
	Writable bool

	// Now is the clock used for new objects and block headers.
	Now    func() time.Time
	uidGen *UIDGenerator
}

var errReadOnly = errors.New("volume opened read-only")

// Open mounts the first logical volume of an image.
func Open(path string, writable bool) (*Volume, error) {
	var pv *fs.PhysicalVolume
	var err error
	if writable {
		pv, err = fs.MountWritable(path)
	} else {
		pv, err = fs.Mount(path)
	}
	if err != nil {
		return nil, err
	}
	v := newVolume(pv, writable)
	if err := v.BAT.Mount(); err != nil {
		pv.Unmount()
		return nil, err
	}
	return v, nil
}

func newVolume(pv *fs.PhysicalVolume, writable bool) *Volume {
	v := &Volume{PV: pv, LV: pv.LV, Writable: writable, Now: util.Now}
	v.BAT = NewBATManager(v.LV)
	v.VTOC = NewVTOCManager(v.LV, v.BAT)
	v.Files = NewFileManager(v)
	v.Names = NewNamingManager(v)
	node := v.LV.Label.LastMountedNode & 0xFFFFF
	if node == 0 {
		node = DefaultNodeID
	}
	v.uidGen = NewUIDGenerator(v.Now(), node)
	v.VTOC.now = v.clockh
	v.BAT.now = v.clockh
	return v
}

// DefaultNodeID is the node id stamped into UIDs this tool generates when
// the volume has never been mounted (the low 20 bits of every UID).
const DefaultNodeID = 0x12345

func (v *Volume) clockh() uint32 { return util.TimestampToApolloEpoch(v.Now()) }

// NewUID returns a fresh object UID.  Successive runs of this tool start
// their generators at nearly the same clock value, so a candidate that is
// already indexed in the VTOC is skipped.
func (v *Volume) NewUID() uid.UID {
	for {
		u := v.uidGen.Next()
		if _, err := v.VTOC.GetIndexForUID(u); err != nil {
			return u
		}
	}
}

// Close flushes the BAT and labels (when writable) and closes the image.
func (v *Volume) Close() error {
	if v.PV == nil {
		return nil
	}
	var err error
	if v.Writable {
		err = v.Flush()
	}
	if cerr := v.PV.Unmount(); err == nil {
		err = cerr
	}
	v.PV = nil
	return err
}

// Flush writes the BAT and both copies of the LV label.
func (v *Volume) Flush() error {
	if !v.Writable {
		return errReadOnly
	}
	if err := v.BAT.Flush(); err != nil {
		return err
	}
	if err := v.LV.WriteLabel(); err != nil {
		return err
	}
	alt := v.PV.Label.AltLVLabelDAddr[0]
	if alt != 0 {
		primary, err := v.PV.ReadBlock(v.LV.StartDAddr())
		if err != nil {
			return err
		}
		altBlock, err := v.PV.ReadBlock(alt)
		if err != nil {
			return err
		}
		altBlock.Data = primary.Data
		altBlock.Header.ObjectUID = uid.UIDlvlabel
		altBlock.Header.PVBlockDAddr = alt
		if err := v.PV.WriteBlock(alt, altBlock); err != nil {
			return fmt.Errorf("writing alternate LV label: %w", err)
		}
	}
	return nil
}

// UIDGenerator hands out UIDs the way UID_$GEN lays them out: a 36-bit
// time (the top 36 bits of the 48-bit clock) above 8 spare bits and the
// 20-bit node id.  Each call advances the time field by one so UIDs are
// unique.
type UIDGenerator struct {
	next uint64
	node uint32
}

func NewUIDGenerator(now time.Time, node uint32) *UIDGenerator {
	return &UIDGenerator{next: util.Clock(now) >> 12, node: node & 0xFFFFF}
}

func (g *UIDGenerator) Next() uid.UID {
	u, err := uid.Generate(g.next&0xFFFFFFFFF, g.node)
	if err != nil {
		panic(err)
	}
	g.next++
	return u
}

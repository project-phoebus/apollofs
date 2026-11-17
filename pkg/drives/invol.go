package drives

// What the SR10.2 standalone invol for sau2 (install/ri.apollo.os.v.10.2/
// sau2/invol) decides from the drive type alone.  The routine names are the
// ones given to the program in Ghidra; see NOTES.md, "The SR10.2 sau2
// invol".

// Disk kinds, as invol's select_disk numbers them.
const (
	KindWinchester    = 0 // `w'
	KindFloppy        = 1 // `f'
	KindStorageModule = 4 // `s'
)

// Class is a Winchester drive type's class, its high byte.
func (dt DriveType) Class() int { return int(uint16(dt.DType) & 0xFF00) }

// DefaultInterleave is the interleave factor set_disk_layout chooses (disk
// params +0x18): process_disk takes it as the LV interleave, and create_lv
// stores it as the BAT step, a longword at LV label +0x3E (step_blocks 0,
// bat_step the factor).  BAT_$ALLOCATE then leaves that many blocks between
// the blocks it hands out.
//
//	Winchester  class 0x000, 0x100, 0x200, 0x300   2
//	Winchester  class 0x400 to 0x700               1
//	Winchester  any other class                    2
//	floppy                                         2
//	storage module  type 0                         9
//	storage module  any other type                 5
func DefaultInterleave(kind int, dtype int16) int {
	switch kind {
	case KindWinchester:
		switch uint16(dtype) & 0xFF00 {
		case 0x000, 0x100, 0x200, 0x300:
			return 2
		case 0x400, 0x500, 0x600, 0x700:
			return 1
		}
		return 2
	case KindStorageModule:
		if dtype == 0 {
			return 9
		}
		return 5
	}
	return 2
}

// DefaultInterleave is the Winchester rule for this drive type.
func (dt DriveType) DefaultInterleave() int { return DefaultInterleave(KindWinchester, dt.DType) }

// InvolLayout is the part of a Winchester disk set_disk_layout keeps out of
// the volume, in blocks from the disk's end (the driver's block count, the
// PV label's addr_end).
type InvolLayout struct {
	Reserved    int // the volume ends this many blocks before the disk does
	BadspotBack int // the badspot list starts this many blocks before the end
	DiagBack    int // the diagnostic area starts this many before the end
}

// WinchesterLayout is set_disk_layout's reserved area for a Winchester
// class, with bpc the blocks per cylinder (sectors per track x heads /
// sectors per block):
//
//	class 0x000, 0x100, 0x200        5 cylinders; badspot list in the
//	                                 last, diagnostic area 2 back
//	class 0x300, 0x400, 0x500, 0x600 3 cylinders; badspot list in the
//	  and any unknown class          last, diagnostic area 3 back
//	class 0x700                      3 bpc + 1: badspot list bpc + 1
//	                                 back, diagnostic area 2 bpc before it
//
// (In the class 0x300 arm a test for dtype 0x12F would reserve 12
// cylinders; 0x12F is class 0x100, so it never matches.)
func WinchesterLayout(dtype int16, bpc int) InvolLayout {
	switch uint16(dtype) & 0xFF00 {
	case 0x000, 0x100, 0x200:
		return InvolLayout{Reserved: 5 * bpc, BadspotBack: bpc, DiagBack: 2 * bpc}
	case 0x700:
		n := bpc + 1 + 2*bpc
		return InvolLayout{Reserved: n, BadspotBack: bpc + 1, DiagBack: n}
	}
	return InvolLayout{Reserved: 3 * bpc, BadspotBack: bpc, DiagBack: 3 * bpc}
}

// ANSISectorWords is what the ANSI (class 0x100) driver's DISK_INIT, as
// linked into invol, reports for a drive it knows: the two words invol
// copies to the PV label's sector_start (0x94) and sector_size (0x96).  The
// precompensation cylinder (0x98) and the word at 0xC0 stay as invol set
// them before the assign: 0.
func ANSISectorWords(dtype int16) (start, size uint16, ok bool) {
	switch dtype {
	case 0x104, 0x105:
		return 0, 1120, true
	case 0x103:
		return 0, 1181, true
	}
	return 0, 0, false
}

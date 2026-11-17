package drives

import "testing"

func TestDefaultInterleave(t *testing.T) {
	cases := []struct {
		kind  int
		dtype int16
		want  int
	}{
		{KindWinchester, 0x001, 2}, // class 0x000
		{KindWinchester, 0x103, 2}, // the DN300's Priam ANSI drives: class 0x100
		{KindWinchester, 0x104, 2},
		{KindWinchester, 0x105, 2},
		{KindWinchester, 0x201, 2},
		{KindWinchester, 0x306, 2},
		{KindWinchester, 0x401, 1},
		{KindWinchester, 0x504, 1}, // ESDI
		{KindWinchester, 0x601, 1},
		{KindWinchester, 0x7FF, 1}, // SCSI
		{KindWinchester, 0x801, 2}, // any other class
		{KindFloppy, 0, 2},
		{KindStorageModule, 0, 9},
		{KindStorageModule, 1, 5},
		{KindStorageModule, 2, 5},
	}
	for _, c := range cases {
		if got := DefaultInterleave(c.kind, c.dtype); got != c.want {
			t.Errorf("DefaultInterleave(%d, 0x%03X) = %d, want %d", c.kind, c.dtype, got, c.want)
		}
	}
	dt, _ := GetDriveType(0x105)
	if dt.DefaultInterleave() != 2 {
		t.Errorf("0x105: %d", dt.DefaultInterleave())
	}
}

func TestWinchesterLayout(t *testing.T) {
	cases := []struct {
		dtype int16
		want  InvolLayout
	}{
		{0x105, InvolLayout{Reserved: 300, BadspotBack: 60, DiagBack: 120}},
		{0x001, InvolLayout{Reserved: 300, BadspotBack: 60, DiagBack: 120}},
		{0x201, InvolLayout{Reserved: 300, BadspotBack: 60, DiagBack: 120}},
		{0x301, InvolLayout{Reserved: 180, BadspotBack: 60, DiagBack: 180}},
		{0x504, InvolLayout{Reserved: 180, BadspotBack: 60, DiagBack: 180}},
		{0x700, InvolLayout{Reserved: 181, BadspotBack: 61, DiagBack: 181}},
		{0x801, InvolLayout{Reserved: 180, BadspotBack: 60, DiagBack: 180}},
	}
	for _, c := range cases {
		if got := WinchesterLayout(c.dtype, 60); got != c.want {
			t.Errorf("WinchesterLayout(0x%03X, 60) = %+v, want %+v", c.dtype, got, c.want)
		}
	}
	// the ESDI disks' layout as NOTES.md has it from SR10.3 volumes: the
	// same three cylinders at the end
	d := ESDIDisk{DriveType: DriveType{DType: 0x504, Cylinders: 1224, Heads: 15, BlocksPerTrack: 18}}
	l := WinchesterLayout(0x504, d.BlocksPerCylinder())
	if d.AddrEnd()-l.Reserved != d.VolumeBlocks() || d.AddrEnd()-l.DiagBack != d.DiagDAddr() ||
		d.AddrEnd()-l.BadspotBack != d.BadspotDAddr() {
		t.Errorf("ESDI layout %+v disagrees with ESDIDisk", l)
	}
}

func TestANSISectorWords(t *testing.T) {
	for dtype, want := range map[int16]uint16{0x103: 1181, 0x104: 1120, 0x105: 1120} {
		start, size, ok := ANSISectorWords(dtype)
		if !ok || start != 0 || size != want {
			t.Errorf("0x%03X: %d %d %v", dtype, start, size, ok)
		}
	}
	if _, _, ok := ANSISectorWords(0x504); ok {
		t.Error("0x504 is not an ANSI drive")
	}
}

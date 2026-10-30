package managers

import (
	"encoding/binary"
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/fs"
)

// FileManager reads and writes object contents through the VTOCE file map:
// 32 direct daddrs, a level-1 block of 256 daddrs (pages 32..287), a
// level-2 block of 256 level-1 blocks (pages 288..65823) and a level-3
// root.  Data blocks carry the object UID and page number in their block
// header (on a volume with headers); file-map blocks carry the first page they map and their level in
// the top byte of the system-type word (an SR10 volume's level-1 roots are
// page 32 / 0x01000000, level-2 roots page 288 / 0x02000000).
type FileManager struct {
	v *Volume
}

const (
	fmDirect  = 32
	fmPerBlk  = 256
	fmLevel1  = fmDirect + fmPerBlk          // 288
	fmLevel2  = fmLevel1 + fmPerBlk*fmPerBlk // 65824
	pageBytes = fs.BlockDataSize
)

func NewFileManager(v *Volume) *FileManager { return &FileManager{v: v} }

// ReadData returns an object's contents (CurrentLength bytes).
func (fm *FileManager) ReadData(e *fs.VTOCE) ([]byte, error) {
	m, _, err := fm.v.VTOC.FileMap(e)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, e.Header.CurrentLength)
	remaining := int(e.Header.CurrentLength)
	for i, d := range m {
		n := pageBytes
		if remaining < n {
			n = remaining
		}
		if d == 0 {
			out = append(out, make([]byte, n)...)
		} else {
			b, err := fm.v.LV.ReadBlock(d)
			if err != nil {
				return nil, err
			}
			if fm.v.PV.Headers && (b.Header.ObjectUID != e.Header.ObjectUID || b.Header.PageWithinObject != int32(i)) {
				return nil, fmt.Errorf("block %d: header says %v page %d, expected %v page %d", d,
					b.Header.ObjectUID, b.Header.PageWithinObject, e.Header.ObjectUID, i)
			}
			out = append(out, b.Data[:n]...)
		}
		remaining -= n
		if remaining <= 0 {
			break
		}
	}
	return out, nil
}

// FreeData releases every data and file-map block of e and clears its map.
func (fm *FileManager) FreeData(e *fs.VTOCE) error {
	m, fmBlocks, err := fm.v.VTOC.FileMap(e)
	if err != nil {
		return err
	}
	for _, d := range append(m, fmBlocks...) {
		if d != 0 {
			if err := fm.v.BAT.Free(d); err != nil {
				return err
			}
		}
	}
	e.FileMap0 = [32]fs.DAddr{}
	e.FileMap1Ptr, e.FileMap2Ptr, e.FileMap3Ptr = 0, 0, 0
	e.Header.CurrentLength = 0
	e.Header.BlocksUsed = 0
	return nil
}

func fmBlocksFor(pages int) int {
	n := 0
	if pages > fmDirect {
		n++
	}
	if pages > fmLevel1 {
		n++ // level-2 root
		n += (pages - fmLevel1 + fmPerBlk - 1) / fmPerBlk
	}
	return n
}

// WriteData replaces e's contents with data, allocating blocks near hint.
// sysTypes goes into every data block header (sub type << 16 for
// directory pages, 0 for files).  The caller stores the VTOCE.
func (fm *FileManager) WriteData(e *fs.VTOCE, data []byte, sysTypes int32, hint fs.DAddr) error {
	if err := fm.FreeData(e); err != nil {
		return err
	}
	pages := (len(data) + pageBytes - 1) / pageBytes
	if pages > fmLevel2 {
		return fmt.Errorf("object too large (%d pages)", pages)
	}
	nfm := fmBlocksFor(pages)
	blocks, err := fm.v.BAT.AllocateRun(pages+nfm, hint)
	if err != nil {
		return err
	}
	dataBlocks, fmBlks := blocks[:pages], blocks[pages:]
	now := fm.v.clockh()
	u := e.Header.ObjectUID
	for i, d := range dataBlocks {
		var b fs.Block
		b.Header = fs.BlockHeader{ObjectUID: u, PageWithinObject: int32(i), LastWritten: now,
			BlockSystemTypes: sysTypes, PVBlockDAddr: d + fm.v.LV.StartDAddr()}
		copy(b.Data[:], data[i*pageBytes:])
		if err := fm.v.LV.WriteBlock(d, &b); err != nil {
			return err
		}
	}
	writeFM := func(d fs.DAddr, level int, firstPage int, entries []fs.DAddr) error {
		var b fs.Block
		b.Header = fs.BlockHeader{ObjectUID: u, PageWithinObject: int32(firstPage), LastWritten: now,
			BlockSystemTypes: int32(level) << 24, PVBlockDAddr: d + fm.v.LV.StartDAddr()}
		for i, c := range entries {
			binary.BigEndian.PutUint32(b.Data[i*4:], uint32(c))
		}
		return fm.v.LV.WriteBlock(d, &b)
	}
	for i := 0; i < pages && i < fmDirect; i++ {
		e.FileMap0[i] = dataBlocks[i]
	}
	if pages > fmDirect {
		end := pages
		if end > fmLevel1 {
			end = fmLevel1
		}
		e.FileMap1Ptr = fmBlks[0]
		if err := writeFM(fmBlks[0], 1, fmDirect, dataBlocks[fmDirect:end]); err != nil {
			return err
		}
	}
	if pages > fmLevel1 {
		root := fmBlks[1]
		children := fmBlks[2:]
		e.FileMap2Ptr = root
		if err := writeFM(root, 2, fmLevel1, children); err != nil {
			return err
		}
		for k, c := range children {
			first := fmLevel1 + k*fmPerBlk
			end := first + fmPerBlk
			if end > pages {
				end = pages
			}
			if err := writeFM(c, 1, first, dataBlocks[first:end]); err != nil {
				return err
			}
		}
	}
	e.Header.CurrentLength = uint32(len(data))
	e.Header.BlocksUsed = uint32(pages + nfm)
	return nil
}

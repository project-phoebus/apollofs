package ct

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type tapeBuilder struct {
	out bytes.Buffer
	blk []byte
}

func (b *tapeBuilder) label(s string) {
	blk := make([]byte, blockSize)
	for i := range blk {
		blk[i] = ' '
	}
	copy(blk, s)
	b.out.Write(blk)
}

func (b *tapeBuilder) mark() {
	blk := make([]byte, blockSize)
	copy(blk, tapeMark)
	b.out.Write(blk)
}

func (b *tapeBuilder) record(t1, t2 int16, c []byte) {
	if b.blk == nil || len(b.blk)+6+len(c)+1 > blockSize {
		b.flush()
		b.blk = make([]byte, 14)
	}
	h := make([]byte, 6)
	binary.BigEndian.PutUint16(h, uint16(t1))
	binary.BigEndian.PutUint16(h[2:], uint16(len(c)))
	binary.BigEndian.PutUint16(h[4:], uint16(t2))
	b.blk = append(b.blk, h...)
	b.blk = append(b.blk, c...)
	if len(c)&1 == 1 {
		b.blk = append(b.blk, 0)
	}
}

func (b *tapeBuilder) flush() {
	if b.blk != nil {
		blk := make([]byte, blockSize)
		copy(blk, b.blk)
		b.out.Write(blk)
		b.blk = nil
	}
}

func attrs(typeHi uint32, size int) []byte {
	a := make([]byte, AttrSize)
	a[0], a[2] = 1, 0x90
	binary.BigEndian.PutUint32(a[0x0C:], typeHi)
	binary.BigEndian.PutUint32(a[0x14:], uint32(size))
	return a
}

func TestReadSynthetic(t *testing.T) {
	var b tapeBuilder
	b.label("VOL1ST0241")
	b.label("HDR1/base_open_lib   BACKUP000100030001")
	b.label("HDR2F0051200512")
	b.mark()
	dir := append(attrs(0, 1024), make([]byte, 0x80)...)
	dir[1] = 1
	dir = append(dir, []byte("install/lib")...)
	b.record(3, 3, dir)
	b.record(2, 2, append(make([]byte, 12), []byte("install/lib/libc")...))
	data := bytes.Repeat([]byte("0123456789"), 70)
	b.record(0, 2, append(attrs(0x322, len(data)), make([]byte, 0x18)...))
	b.record(9, 2, make([]byte, 12))
	b.record(1, 1, data[:400])
	b.record(9, 2, make([]byte, 12))
	b.record(1, 1, data[400:])
	link := []byte{0, 0, 0, 0, 0, 7}
	link = append(link, []byte("lib/abc../x")...)
	b.record(5, 2, link)
	b.flush()
	b.mark()
	b.label("EOF1/base_open_lib   BACKUP000100030001")

	var got []*Entry
	err := ReadFrom(bytes.NewReader(b.out.Bytes()), true, func(e *Entry) error {
		got = append(got, e)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, KindDir, got[0].Kind)
	require.Equal(t, "install/lib", got[0].Path)
	require.Equal(t, 2, got[0].Volume)
	require.Equal(t, 3, got[0].FileSeq)
	require.Equal(t, "/base_open_lib", got[0].Label)
	require.Equal(t, KindFile, got[1].Kind)
	require.Equal(t, "install/lib/libc", got[1].Path)
	require.Equal(t, data, got[1].Data)
	hi, _ := got[1].Type()
	require.Equal(t, uint32(0x322), hi)
	require.Equal(t, KindLink, got[2].Kind)
	require.Equal(t, "lib/abc", got[2].Path)
	require.Equal(t, "../x", got[2].LinkText)
}

// The SR10.2 boot cartridge, when present.
func TestReadSR102BootTape(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), "src/domainos-archeology/sr10.2/017286-001*.ct"))
	if len(matches) == 0 {
		t.Skip("SR10.2 tapes not present")
	}
	n := map[Kind]int{}
	err := Read(matches[0], false, func(e *Entry) error {
		n[e.Kind]++
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, n[KindFile], 300)
	require.Greater(t, n[KindDir], 40)
}

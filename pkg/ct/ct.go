// Package ct reads Apollo rbak/wbak cartridge-tape images (.ct), the format
// of the SR10 release tapes: 512-byte blocks, ANSI VOL1/HDR1/EOF1 labels
// around each tape file, tape marks stored as blocks starting 0xDEAFFAED,
// and a stream of typed records inside the data blocks.  It is modelled on
// readct (github.com/domainos-archeology/readct) and additionally keeps
// what an installer needs: which labelled tape file (release
// subcomponent) each object came from and the raw VTOCE attribute image
// wbak recorded for it.
package ct

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const blockSize = 512

// AttrSize is the VTOCE attribute image wbak stores per object (VTOCE bytes
// 0x00..0x77: kind/sub type/flags, UID, type UID, length, pages, clocks,
// parent, owner/group/org, rights, ACL flags, link count).
const AttrSize = 0x78

// Kind of tape entry.
type Kind int

const (
	KindDir Kind = iota
	KindFile
	KindLink
)

func (k Kind) String() string { return [...]string{"dir", "file", "link"}[k] }

// Entry is one object on the tape.
type Entry struct {
	Volume   int    // tape volume number (from the VOL1 label, "ST0141" -> 1)
	Label    string // HDR1 file identifier (subcomponent name, 17 chars max)
	FileSeq  int    // HDR1 file sequence number on this volume
	Kind     Kind
	Path     string
	Attrs    [AttrSize]byte
	DirExtra []byte // directories: the 0x80 bytes after the attributes (default protections)
	Size     int    // files: bytes
	Data     []byte // files, when requested
	LinkText string
	ACL      []byte // an extended-ACL record that followed the object
}

// Type returns the type UID words from the attribute image.
func (e *Entry) Type() (uint32, uint32) {
	return binary.BigEndian.Uint32(e.Attrs[0x0C:]), binary.BigEndian.Uint32(e.Attrs[0x10:])
}

type recType struct{ t1, t2 int16 }

var (
	recFile = recType{0, 2}
	recData = recType{1, 1}
	recName = recType{2, 2}
	recDir  = recType{3, 3}
	recPopd = recType{4, 2}
	recLink = recType{5, 2}
	recOpt  = recType{6, 2}
	recEmpt = recType{8, 1}
	recSub  = recType{9, 2}
	recACL  = recType{10, 2}
	recPad  = recType{0, 0}
)

var tapeMark = []byte{0xde, 0xaf, 0xfa, 0xed}

// Read walks every object of a tape image in order.
func Read(path string, withData bool, fn func(*Entry) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return ReadFrom(f, withData, fn)
}

// ReadFrom walks a tape image.
func ReadFrom(r io.Reader, withData bool, fn func(*Entry) error) error {
	t := &tapeReader{withData: withData, fn: fn}
	buf := make([]byte, blockSize)
	for {
		_, err := io.ReadFull(r, buf)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
		if err := t.block(buf); err != nil {
			return err
		}
	}
	return t.flush()
}

type tapeReader struct {
	withData bool
	fn       func(*Entry) error

	volume  int
	label   string
	seq     int
	name    string
	pending *Entry // file waiting for data, or last object (for ACLs)
	need    int
	last    *Entry
}

func (t *tapeReader) emit(e *Entry) error {
	if err := t.flushLast(); err != nil {
		return err
	}
	t.last = e
	return nil
}

func (t *tapeReader) flushLast() error {
	if t.last == nil {
		return nil
	}
	e := t.last
	t.last = nil
	return t.fn(e)
}

func (t *tapeReader) flush() error {
	if t.pending != nil {
		return fmt.Errorf("%s: tape ended %d bytes short", t.pending.Path, t.need)
	}
	return t.flushLast()
}

func (t *tapeReader) block(b []byte) error {
	switch {
	case bytes.HasPrefix(b, []byte("VOL1")):
		id := strings.TrimSpace(string(b[4:10]))
		t.volume = 0
		if len(id) >= 4 && strings.HasPrefix(id, "ST") {
			if n, err := strconv.Atoi(id[2:4]); err == nil {
				t.volume = n
			}
		}
		return nil
	case bytes.HasPrefix(b, []byte("HDR1")):
		t.label = strings.TrimSpace(string(b[4:21]))
		if n, err := strconv.Atoi(string(b[31:35])); err == nil {
			t.seq = n
		}
		return nil
	case bytes.HasPrefix(b, []byte("HDR2")), bytes.HasPrefix(b, []byte("EOF1")),
		bytes.HasPrefix(b, []byte("EOF2")), bytes.HasPrefix(b, tapeMark):
		return nil
	}
	if t.label == "" && t.volume == 0 {
		// boot code (ctboot) before the first label
		return nil
	}
	p := 4 + 8 + 2
	for p+6 <= blockSize {
		rt := recType{int16(binary.BigEndian.Uint16(b[p:])), int16(binary.BigEndian.Uint16(b[p+4:]))}
		size := int(binary.BigEndian.Uint16(b[p+2:]))
		p += 6
		if p+size > blockSize {
			break
		}
		c := b[p : p+size]
		p += size + size&1
		known, err := t.record(rt, c)
		if err != nil {
			return err
		}
		if !known {
			break // the rest of the block is stale buffer contents
		}
	}
	return nil
}

func (t *tapeReader) newEntry(k Kind, path string) *Entry {
	return &Entry{Volume: t.volume, Label: t.label, FileSeq: t.seq, Kind: k, Path: path}
}

func (t *tapeReader) record(rt recType, c []byte) (bool, error) {
	switch rt {
	case recName:
		if len(c) < 12 {
			return false, nil // stale bytes, not a record
		}
		t.name = string(c[12:])
	case recDir:
		if len(c) < AttrSize+0x80 {
			return false, nil // stale bytes, not a record
		}
		e := t.newEntry(KindDir, string(c[AttrSize+0x80:]))
		copy(e.Attrs[:], c)
		e.DirExtra = append([]byte(nil), c[AttrSize:AttrSize+0x80]...)
		return true, t.emit(e)
	case recFile:
		if len(c) < AttrSize {
			return false, nil // stale bytes, not a record
		}
		if t.pending != nil {
			return false, fmt.Errorf("%s: new file before %d bytes of data", t.pending.Path, t.need)
		}
		e := t.newEntry(KindFile, t.name)
		copy(e.Attrs[:], c)
		e.Size = int(binary.BigEndian.Uint32(c[0x14:]))
		if e.Size == 0 {
			return true, t.emit(e)
		}
		t.pending, t.need = e, e.Size
	case recData:
		if t.pending == nil {
			return true, nil
		}
		n := len(c)
		if n > t.need {
			n = t.need
		}
		if t.withData {
			t.pending.Data = append(t.pending.Data, c[:n]...)
		}
		t.need -= n
		if t.need == 0 {
			e := t.pending
			t.pending = nil
			return true, t.emit(e)
		}
	case recLink:
		if len(c) < 6 {
			return false, nil // stale bytes, not a record
		}
		nl := int(binary.BigEndian.Uint32(c[2:]))
		if 6+nl > len(c) {
			return false, nil // stale bytes, not a record
		}
		e := t.newEntry(KindLink, string(c[6:6+nl]))
		e.LinkText = string(c[6+nl:])
		return true, t.emit(e)
	case recACL:
		if t.last != nil {
			t.last.ACL = append([]byte(nil), c...)
		}
	case recPopd, recOpt, recEmpt, recSub, recPad:
	default:
		return false, nil
	}
	return true, nil
}

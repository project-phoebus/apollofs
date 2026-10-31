package fs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/project-phoebus/apollofs/pkg/uid"
)

type DirSR9 struct {
	Version         uint16
	HashValue       uint16
	LastSize        uint16
	PoolSize        uint16
	EntriesPerBlock uint16
	HighBlock       uint16
	FreeChain       uint16
	ParentUID       uid.UID
	EntryCount      uint16
	MaxCount        uint16
	Linear          [18]DirEntry
	InfoBlock       DirInformationBlock
	HashThreads     [43]uint16
	EntryBlockPool  [429]uint16
}

type DirInformationBlock struct {
	MBZ          uint8
	Version      uint8
	TotalLength  uint16
	HeaderLength uint16
	MBZ2         uint16

	DefaultDirectoryACL uid.UID
	DefaultFileACL      uid.UID

	// rest is unused
	Padding [24]byte
}

type DirEntrySR9 struct {
	Name              [32]byte
	NetworkNumberHint uint16
	Unused            uint16
	Reserved          uint16
	EntryType         uint8
	Length            uint8
	Rest              [8]byte
}

func (de DirEntrySR9) UID() (uid.UID, error) {
	if de.EntryType != 2 {
		return uid.Empty, fmt.Errorf("not an entry with a UID")
	}

	var u uid.UID
	err := binary.Read(bytes.NewReader(de.Rest[:]), binary.BigEndian, &u)
	if err != nil {
		return uid.Empty, err
	}
	return u, nil
}

// XXX there will be a LinkInfo() here for the link case

type Dir struct {
	// dont-care-for-now [0x84]byte
	Entries []DirEntry
}

func (d *Dir) FromReader(r *bytes.Reader) error {
	var err error

	var dontCare [0x84]byte

	err = binary.Read(r, binary.BigEndian, &dontCare)
	if err != nil {
		return err
	}

	var indexes []int

	for {
		idx, err := readUint16(r)
		if err != nil {
			return err
		}
		if idx == 0x0000 {
			break
		}
		indexes = append(indexes, int(idx))
	}

	for _, idx := range indexes {
		_, err = r.Seek(int64(idx), io.SeekStart)
		if err != nil {
			return err
		}
		var de DirEntry
		err = de.FromReader(r)
		if err != nil {
			return err
		}
		d.Entries = append(d.Entries, de)
	}

	return nil
}

// looks like 10.4 filesystem has the following format per entry
//
// [0] uint8   .version
// [1] uint8   .name_length
// [2] uint8   .unknown (entry type?)
// [3] uint8   .unknown (entry length?)
// [4] uid_t   .uid (not sure what.  it's the same for all entries it seems)
// [5]
type DirEntry struct {
	EntryType uint8 // 0x*2 = file.  0x*4 = symlink
	// nameLength uint8
	LinkTextLength uint16
	// unknown2   uint32
	Name string

	UID      uid.UID // entry type == 0x*2
	LinkText string  // entry type == 0x*4
}

func (de *DirEntry) HasUID() bool {
	return (de.EntryType & 0xf) == 2
}

func (de *DirEntry) HasLinkText() bool {
	return (de.EntryType & 0xf) == 4
}

func (de *DirEntry) FromReader(r *bytes.Reader) error {
	var err error
	de.EntryType, err = readUint8(r)
	if err != nil {
		return err
	}

	nameLength, err := readUint8(r)
	if err != nil {
		return err
	}

	de.LinkTextLength, err = readUint16(r)
	if err != nil {
		return err
	}

	if de.HasUID() {
		de.UID, err = readUID(r)
		if err != nil {
			return err
		}
	} else if de.HasLinkText() {
		var padding [4]byte
		err = binary.Read(r, binary.BigEndian, &padding)
		if err != nil {
			return err
		}
	}

	/*unknown2*/
	_, err = readUint32(r)
	if err != nil {
		return err
	}

	de.Name, err = readStringLen(r, int(nameLength))
	if err != nil {
		return err
	}
	if de.HasLinkText() {
		de.LinkText, err = readStringLen(r, int(de.LinkTextLength))
		if err != nil {
			return err
		}
	}

	return nil
}

func readUint8(r *bytes.Reader) (uint8, error) {
	var v uint8
	err := binary.Read(r, binary.BigEndian, &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func readUint16(r *bytes.Reader) (uint16, error) {
	var v uint16
	err := binary.Read(r, binary.BigEndian, &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func readUint32(r *bytes.Reader) (uint32, error) {
	var v uint32
	err := binary.Read(r, binary.BigEndian, &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func readUID(r *bytes.Reader) (uid.UID, error) {
	var u uid.UID
	err := binary.Read(r, binary.BigEndian, &u)
	if err != nil {
		return uid.Empty, err
	}
	return u, nil
}

func readStringLen(r *bytes.Reader, length int) (string, error) {
	buf := make([]byte, length)
	err := binary.Read(r, binary.BigEndian, &buf)
	if err != nil {
		return "", err
	}
	return string(buf[:]), nil
}

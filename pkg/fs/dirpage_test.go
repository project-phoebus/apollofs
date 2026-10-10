package fs

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/domainos-archeology/apollofs/pkg/uid"
)

func pageBytes(p [DirPageSize]byte) []byte { return p[:] }

// Bytes taken from the "/" root page of an SR10.3 volume.
func TestDirEntryEncoding(t *testing.T) {
	sau2 := DirEnt{Type: DirEntryObject, Name: []byte("sau2"), UID: uid.UID{Hi: 0x6c972c45, Lo: 0xd0012345}}
	require.Equal(t, "020400006c972c45d00123450000000073617532", hex.EncodeToString(sau2.encode()))
	tmp := DirEnt{Type: DirEntrySoftLink, Name: []byte("tmp"), LinkText: []byte("`node_data/tmp")}
	require.Equal(t, "0403000effff000000000000746d70606e6f64655f646174612f746d70000000", hex.EncodeToString(tmp.encode()))
	require.Equal(t, "04010000ffff0000000000000000000"+"0", hex.EncodeToString(sentinelEntry()))
	node := DirEnt{Type: DirEntryHardLink, Name: []byte("node_1"), UID: uid.UID{Hi: 0x6c95ba45, Lo: 0x40012345}}
	require.Equal(t, "030600006c95ba454001234500000000000000006e6f64655f310000", hex.EncodeToString(node.encode()))
}

func TestDirSingleLeafRoot(t *testing.T) {
	ents := []DirEnt{
		{Type: DirEntryObject, Name: []byte("sau2"), UID: uid.UID{Hi: 1, Lo: 2}},
		{Type: DirEntrySoftLink, Name: []byte("bin"), LinkText: []byte("$(SYSTYPE)/bin")},
		{Type: DirEntryObject, Name: []byte("lib"), UID: uid.UID{Hi: 3, Lo: 4}},
	}
	root := NewDirRootArea(DirDefProt{}, DirDefProt{})
	pages, err := EncodeDirectory(ents, root, uid.UID{Hi: 9, Lo: 9})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	p := pageBytes(pages[0])
	// header: leaf version 5, no stamp, page 0, no next
	require.Equal(t, "000500000000000000000000ffff", hex.EncodeToString(p[:0x0e]))
	require.Equal(t, "0100007000680000", hex.EncodeToString(p[0x12:0x1a]))
	info, err := ParseDirPage(p)
	require.NoError(t, err)
	require.Equal(t, 0x82, info.IndexBase)
	require.Len(t, info.Offsets, 4)
	require.Equal(t, uint16(0x3f0), info.Offsets[0]) // sentinel first, at the top
	got, _, err := DecodeDirectory([][]byte{p})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "bin", string(got[0].Name))
	require.Equal(t, "$(SYSTYPE)/bin", string(got[0].LinkText))
	require.Equal(t, "lib", string(got[1].Name))
	require.Equal(t, "sau2", string(got[2].Name))
}

func TestDirMultiPage(t *testing.T) {
	var ents []DirEnt
	for i := 0; i < 400; i++ {
		ents = append(ents, DirEnt{Type: DirEntryObject, Name: []byte(fmt.Sprintf("file_%04d_with_a_long_name", i)), UID: uid.UID{Hi: uint32(i), Lo: 7}})
	}
	stamp := uid.UID{Hi: 0x11, Lo: 0x22}
	pages, err := EncodeDirectory(ents, NewDirRootArea(DirDefProt{}, DirDefProt{}), stamp)
	require.NoError(t, err)
	require.Greater(t, len(pages), 2)
	var raw [][]byte
	for i := range pages {
		raw = append(raw, pageBytes(pages[i]))
	}
	root, err := ParseDirPage(raw[0])
	require.NoError(t, err)
	require.Equal(t, DirKindInterior, root.Kind)
	require.Equal(t, uint16(0x4005), root.Flags)
	require.Equal(t, stamp, root.Stamp)
	require.Len(t, root.Offsets, len(pages)-1)
	first, _, err := ParseDirEntry(raw[0], int(root.Offsets[0]))
	require.NoError(t, err)
	require.Equal(t, []byte{0}, first.Name)
	require.Equal(t, uint32(1), first.Extra)
	// leaves chain 1 -> 2 -> ... -> 0xffff and each child key is its first name
	for i := 1; i < len(pages); i++ {
		li, err := ParseDirPage(raw[i])
		require.NoError(t, err)
		require.Equal(t, uint16(i), li.PageNo)
		if i == len(pages)-1 {
			require.Equal(t, uint16(0xffff), li.Next)
		} else {
			require.Equal(t, uint16(i+1), li.Next)
		}
		if i > 1 {
			key, _, _ := ParseDirEntry(raw[0], int(root.Offsets[i-1]))
			fe, _, _ := ParseDirEntry(raw[i], int(li.Offsets[0]))
			require.Equal(t, fe.Name, key.Name)
		}
		require.Equal(t, uint16(binary.BigEndian.Uint16(raw[i][0x10:])), li.HeapBase)
	}
	got, _, err := DecodeDirectory(raw)
	require.NoError(t, err)
	require.Len(t, got, 400)
	for i := range got {
		require.Equal(t, fmt.Sprintf("file_%04d_with_a_long_name", i), string(got[i].Name))
	}
}

func TestDirDuplicateName(t *testing.T) {
	ents := []DirEnt{{Type: DirEntryObject, Name: []byte("a")}, {Type: DirEntryObject, Name: []byte("a")}}
	_, err := EncodeDirectory(ents, DirRootArea{}, uid.Empty)
	require.Error(t, err)
}

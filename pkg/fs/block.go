package fs

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/uid"
	"github.com/domainos-archeology/apollofs/pkg/util"
)

const BlockDataSize = 1024
const BlockHeaderSize = 32
const BlockSize = BlockDataSize + BlockHeaderSize

type DAddr uint32
type BlockHeader struct {
	ObjectUID        uid.UID
	PageWithinObject int32
	LastWritten      uint32
	BlockSystemTypes int32
	Ignore1          int32
	Ignore2          int16
	DataChecksum     int16
	PVBlockDAddr     DAddr
}

func (bh *BlockHeader) BlockType() int {
	// XXX this is wrong
	return int(bh.BlockSystemTypes >> 28)
}

func (bh *BlockHeader) SystemType() int {
	// XXX this is wrong too
	return int(bh.BlockSystemTypes) >> 24 & 0xF
}

func (h *BlockHeader) Print() {
	fmt.Printf("BlockHeader:\n")
	fmt.Printf("  object uid: %s\n", h.ObjectUID)
	fmt.Printf("  Page within object: %d\n", h.PageWithinObject)
	fmt.Printf("  Block type: %d\n", h.BlockType())
	fmt.Printf("  System type: %d\n", h.SystemType())
	fmt.Printf("  Checksum: %d\n", h.DataChecksum)
	fmt.Printf("  Block PV DAddr: %d\n", h.PVBlockDAddr)
	fmt.Printf("  Last Writter: %s\n", util.FormatTimestamp(h.LastWritten))
	fmt.Printf("  Ignore1: %d\n", h.Ignore1)
	fmt.Printf("  Ignore2: %d\n", h.Ignore2)
}

type Block struct {
	Header BlockHeader
	Data   [1024]byte // must match BlockDataSize above
}

type BlockReadable interface {
	FromReader(*bytes.Reader) error
}

func (b *Block) ReadInto(data any) error {
	if v, ok := data.(BlockReadable); ok {
		return v.FromReader(bytes.NewReader(b.Data[:]))
	}
	return binary.Read(bytes.NewReader(b.Data[:]), binary.BigEndian, data)
}

func (b *Block) Print(includeContents bool, raw bool) {
	b.Header.Print()

	if includeContents {
		fmt.Printf("Block data:\n")

		if raw {
			fmt.Println(hex.Dump(b.Data[:]))
			return
		}

		if b.Header.ObjectUID == uid.UIDpvlabel {
			var pvlabel PVLabel
			err := b.ReadInto(&pvlabel)
			if err != nil {
				panic(err)
			}
			pvlabel.Print()
		} else if b.Header.ObjectUID == uid.UIDlvlabel {
			var lvlabel LVLabel
			err := b.ReadInto(&lvlabel)
			if err != nil {
				panic(err)
			}
			lvlabel.Print()
		} else if b.Header.ObjectUID == uid.UIDvtoc {
			var vtocBlock VTOCBlock
			err := b.ReadInto(&vtocBlock)
			if err != nil {
				panic(err)
			}
			vtocBlock.Print()
		} else if b.Header.ObjectUID == uid.UIDvtoc_bkt {
			var vtocBucketBlock VTOCBucketBlock
			err := b.ReadInto(&vtocBucketBlock)
			if err != nil {
				panic(err)
			}
			vtocBucketBlock.Print()
		} else {
			fmt.Println(hex.Dump(b.Data[:]))
		}
	}
}

func NewBlock(header BlockHeader, data any) Block {
	var buf bytes.Buffer
	err := binary.Write(&buf, binary.BigEndian, data)
	if err != nil {
		panic(err)
	}

	var blockData [1024]byte
	copy(blockData[:], buf.Bytes())

	return Block{
		Header: header,
		Data:   blockData,
	}
}

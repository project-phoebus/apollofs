package managers

import (
	"errors"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
	"golang.org/x/exp/slices"
)

var errFreeVTOCENotFound = errors.New("free VTOCE not found")

type VTOCManager struct {
	lvol *fs.LogicalVolume
	bat  *BATManager
}

func NewVTOCManager(lvol *fs.LogicalVolume, bat *BATManager) *VTOCManager {
	return &VTOCManager{
		lvol,
		bat,
	}
}

func (vm *VTOCManager) Mount() error {
	return nil
}

func (vm *VTOCManager) readVTOCBucketBlock(daddr fs.DAddr) (*fs.VTOCBucketBlock, error) {
	block, err := vm.lvol.ReadBlock(daddr)
	if err != nil {
		return nil, err
	}

	var vtocBucketBlock fs.VTOCBucketBlock
	err = block.ReadInto(&vtocBucketBlock)
	if err != nil {
		return nil, err
	}

	return &vtocBucketBlock, nil
}

func (vm *VTOCManager) findVTOCBucketBlockForUID(header fs.VTOCEHeader) (fs.VTOCX, error) {
	hashValue := vm.hashUID(header.ObjectUID)

	startingBlockNumber := hashValue >> 2
	idx := hashValue & 0x3

	blockDAddr := fs.DAddr(startingBlockNumber) + vm.lvol.VTOCMap[0].FirstBlockDAddr

	for blockDAddr != 0 {
		// fmt.Println("looking in block", blockDAddr)
		// fmt.Println(2)

		vtocBucketBlock, err := vm.readVTOCBucketBlock(blockDAddr)
		if err != nil {
			return 0, err
		}

		// fmt.Println(4)
		// check the entries in bucket `idx` for an unused bucket
		for _, entry := range vtocBucketBlock.Buckets[idx].Entries {
			if entry.UID == uid.Empty /* XXX not sure what the signal for unused slot is... */ {
				return entry.VTOCX, nil
			}
		}

		// fmt.Println(5)
		// if we didn't find it, follow the chain and keep looking
		blockDAddr = vtocBucketBlock.Buckets[idx].NextBlockDAddr
		idx = uint32(vtocBucketBlock.Buckets[idx].NextIndex)
	}

	return 0, errFreeVTOCENotFound
}

func (vm *VTOCManager) AllocateEntry(header fs.VTOCEHeader) (fs.VTOCX, error) {
	// hash the uid, locate the appropriate vtoc block, checking for a free vtoce there.

	vtocx, err := vm.findVTOCBucketBlockForUID(header)
	if err == errFreeVTOCENotFound {
	} else if err != nil {
		return 0, err
	}
	_ = vtocx // TODO: used once the fill-in below is implemented

	// if there isn't a free vtoce, call BATManager.AllocateBlock to create a
	// new vtoc extension block and add it to the chain.

	// now that we have a vtoc block with a vtoce there, fill it in and return a
	// vtocx referring to it.

	notImplemented()
	return 0, nil
}

func (vm *VTOCManager) LookupEntry(u uid.UID) (*fs.VTOCE, error) {
	notImplemented()
	return nil, nil
}

func (vm *VTOCManager) GetEntry(vtocx fs.VTOCX) (*fs.VTOCE, error) {
	vtoceAddr := vtocx.BlockDAddr()
	vtoceIndex := vtocx.Index()

	// read the block
	block, err := vm.lvol.ReadBlock(vtoceAddr)
	if err != nil {
		return nil, err
	}

	// parse the block
	var vtocBlock fs.VTOCBlock
	err = block.ReadInto(&vtocBlock)
	if err != nil {
		return nil, err
	}

	// return the entry
	return &vtocBlock.Entries[vtoceIndex], nil
}

func (vm *VTOCManager) GetEntryForUID(u uid.UID) (*fs.VTOCE, error) {
	vtocx, err := vm.GetIndexForUID(u)
	if err != nil {
		return nil, err
	}

	// fmt.Println("index = ", vtocx)
	return vm.GetEntry(vtocx)
}

func (vm *VTOCManager) GetIndexForUID(u uid.UID) (fs.VTOCX, error) {
	// fmt.Println(1)
	// hash the uid, locate the appropriate vtoc block, then follow the chain
	// until we find the block+index
	hashValue := vm.hashUID(u)

	startingBlockNumber := hashValue >> 2
	idx := hashValue & 0x3

	// XXX assume it's in the first extent for now (this seems likely for
	// filesystems involed in MAME, since there are no bad spots to break up the
	// vtoc)
	if vm.lvol.VTOCMap[1].NumBlocks > 0 || startingBlockNumber > uint32(vm.lvol.VTOCMap[0].NumBlocks) {
		panic("there are more blocks someplace else.  our assumption no longer holds!")
	}

	blockDAddr := fs.DAddr(startingBlockNumber) + vm.lvol.VTOCMap[0].FirstBlockDAddr

	// fmt.Println("starting search at block", blockDAddr, "and idx", idx)

	for blockDAddr != 0 {
		// fmt.Println("looking in block", blockDAddr)
		// fmt.Println(2)
		// read the vtoc bkt block
		block, err := vm.lvol.ReadBlock(blockDAddr)
		if err != nil {
			return 0, err
		}

		// fmt.Println(3)
		var vtocBucketBlock fs.VTOCBucketBlock
		err = block.ReadInto(&vtocBucketBlock)
		if err != nil {
			return 0, err
		}

		// fmt.Println(4)
		// check the entries in bucket `idx`
		for _, entry := range vtocBucketBlock.Buckets[idx].Entries {
			if entry.UID == u {
				// fmt.Println(4.1)
				return entry.VTOCX, nil
			}
		}

		// fmt.Println(5)
		// if we didn't find it, follow the chain and keep looking
		blockDAddr = vtocBucketBlock.Buckets[idx].NextBlockDAddr
		idx = uint32(vtocBucketBlock.Buckets[idx].NextIndex)
	}

	return 0, errNotFound
}

func (vm VTOCManager) GetFMBlocks(e *fs.VTOCE) ([]fs.DAddr, error) {
	var blocks []fs.DAddr

	for _, daddr := range e.FileMap0 {
		if daddr == 0 {
			break
		}
		blocks = append(blocks, daddr)
	}

	if e.FileMap1Ptr != 0 {
		fm1Blocks, err := vm.getFMBlocks(1, e.FileMap1Ptr)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, fm1Blocks...)
	}
	if e.FileMap2Ptr != 0 {
		fm2Blocks, err := vm.getFMBlocks(2, e.FileMap2Ptr)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, fm2Blocks...)
	}
	if e.FileMap3Ptr != 0 {
		fm3Blocks, err := vm.getFMBlocks(3, e.FileMap3Ptr)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, fm3Blocks...)
	}
	return blocks, nil
}

func (vm VTOCManager) getFMBlocks(level int, blockDAddr fs.DAddr) ([]fs.DAddr, error) {
	if level == 0 || level > 3 {
		panic("out of range file map level")
	}

	block, err := vm.lvol.ReadBlock(blockDAddr)
	if err != nil {
		return nil, err
	}

	var daddrs [256]fs.DAddr
	err = block.ReadInto(&daddrs)
	if err != nil {
		return nil, err
	}

	blocks := daddrs[:]
	// truncate at the first 0 daddr
	zeroIdx := slices.Index(blocks, fs.DAddr(0))
	if zeroIdx != -1 {
		blocks = blocks[:zeroIdx]
	}

	if level == 1 {
		// these daddrs are actual file blocks.  no further recursion
		return blocks, nil
	}

	// otherwise we get our list of daddrs and recurse here
	var rvBlocks []fs.DAddr
	for _, daddr := range blocks {
		fmBlocks, err := vm.getFMBlocks(level-1, daddr)
		if err != nil {
			return nil, err
		}
		rvBlocks = append(rvBlocks, fmBlocks...)
	}

	return rvBlocks, nil
}

func (vm *VTOCManager) hashUID(uid uid.UID) uint32 {
	vtocVersion := vm.lvol.Label.VTOCHeader.Version
	vtocSize := uint32(vm.lvol.Label.VTOCHeader.VTOCSizeInBlocks)

	switch {
	case vtocVersion < 2:
		// below translitered from domain_os decompiled UID_$HASH
		var tmp uint32

		tmp = uid.Hi ^ uid.Lo
		tmp = tmp ^ (tmp >> 16)

		return (tmp/vtocSize)<<16 | (tmp % vtocSize)

	case vtocVersion == 2:
		u := uid.Hi * 2
		if int32(uid.Lo) < 0 {
			u = u + 1
		}
		return (u>>0x10 ^ u&0xffff) % vtocSize

	case vtocVersion == 3:
		return uint32(uint16(uid.Hi>>0x10)^uint16(uid.Hi)) % vtocSize
	default:
		panic("unhandled vtoc version")
	}
}

func notImplemented() {
	panic("not implemented")
}

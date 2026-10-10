package managers

import (
	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

type FileManager struct {
	// Aegis Internals talks about the file manager using the containing
	// directory UID to locate the volume to create the file, but we've only got
	// one volume.
	lvol *fs.LogicalVolume
	vm   *VTOCManager
}

func NewFileManager(lvol *fs.LogicalVolume, vm *VTOCManager) *FileManager {
	return &FileManager{
		lvol,
		vm,
	}
}

func (fm *FileManager) CreateFile(containingDirectory uid.UID) (uid.UID, error) {
	// The file manager (file_$create) uses the directory UID to determine on
	// which volume to create the file, then creates the object in the following
	// steps:

	// 1. Calls the AST manager (ast_$get_info) to locate the volume that holds
	//    the directory UID.
	// NB(toshok): no need for this step for this cli.

	// 2. Calls the UID generation routine (uid_$gen) to create a UID for the
	//    object.
	u, err := uid.Generate(
		0, /*XXX fm.lvol.LastUsedTime*/
		fm.lvol.Label.LastMountedNode&0xFFFFF,
	)
	if err != nil {
		return uid.Empty, err
	}

	// 3. Fills the VTOCE header for the new file with default object attributes
	//    and calls the VTOC manager to allocate a VTOCE.
	vtoceHeader := fs.VTOCEHeader{
		ObjectUID: u,
		// TODO(toshok) fill in the rest?
	}

	// 4. The VTOCE manager (vtoc_$allocate) takes the hashed U1D, locates the
	//    appropriate VTOCE block, and checks for a free VTOCE. If it locates one,
	//    it sets the -in use- field in the VTOCE header and appends the header to
	//    the VTOCE.
	_ /*vtocx*/, err = fm.vm.AllocateEntry(vtoceHeader)
	if err != nil {
		return uid.Empty, err
	}

	// 5. If the VTOCEs are full, it calls the BAT manager (bat_$allocate) to
	//    allocate an extension block and chains it to the VTOCE block. Once it
	//    builds the VTOCE, the routine creates a VTOCX for the object and returns
	//    it to file_$create, who writes it to the active segment table entry for
	//    the object being created. In turn, file_$create returns the generated
	//    UID to its caller.

	// NB(toshok) we don't have an AST manager, so skip step 5

	return u, nil
}

func (fm *FileManager) Delete(u uid.UID) error {
	return nil
}

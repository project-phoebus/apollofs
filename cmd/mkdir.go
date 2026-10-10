package cmd

import (
	"fmt"
	"path"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
)

func Mkdir(diskImage string, p string) error {
	pvol, err := fs.Mount(diskImage)
	if err != nil {
		return err
	}
	defer pvol.Unmount()

	lvol := pvol.LV

	bat := managers.NewBATManager(lvol)
	vm := managers.NewVTOCManager(lvol, bat)
	file := managers.NewFileManager(lvol, vm)
	nm := managers.NewNamingManager(lvol, file, vm)

	if err = bat.Mount(); err != nil {
		return err
	}

	if err = vm.Mount(); err != nil {
		return err
	}

	dir, subdir := path.Split(p)
	dir = path.Clean(dir)

	dirUid, err := nm.Resolve(dir)
	if err != nil {
		return err
	}

	fmt.Println("not implemented: should create directory", subdir, "contained in uid", dirUid)
	return nil
}

func init() {
	rootCmd.AddCommand(mkdirCommand)
}

var mkdirCommand = &cobra.Command{
	Use:   "mkdir path",
	Short: "Create a directory in the disk image (similar to 'mkdir' in the host)",
	Long:  ``,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return Mkdir(diskImage, args[0])
	},
}

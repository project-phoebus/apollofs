package cmd

import (
	"fmt"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func List(diskImage string, paths []string) error {
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

	for _, path := range paths {
		uid, err := nm.Resolve(path)
		if err != nil {
			return err
		}

		logrus.Debugf("resolved path %s to uid %s", path, uid)

		vtoce, err := vm.GetEntryForUID(uid)
		if err != nil {
			return err
		}

		if vtoce.IsDirectory() {
			// read the Dir from the first block (will there be more?  I don't think so?)
			dirDAddr := vtoce.FileMap0[0]

			block, err := lvol.ReadBlock(dirDAddr)
			if err != nil {
				return err
			}

			var dir fs.Dir
			err = block.ReadInto(&dir)
			if err != nil {
				return err
			}

			// fmt.Println("there are ", len(dir.Entries), "entries")
			// only list the linear files for now
			for _, entry := range dir.Entries {
				switch {
				case entry.HasUID():
					fmt.Println(entry.Name)
				case entry.HasLinkText():
					fmt.Println(entry.Name + " -> " + entry.LinkText)
				default:
					fmt.Println(entry.EntryType)
				}
			}
		} else {
			fmt.Println(path)
		}
	}

	return pvol.Unmount()
}

func init() {
	rootCmd.AddCommand(listCommand)
}

var listCommand = &cobra.Command{
	Use:    "list path...",
	Short:  "List files/directories in the disk image (similar to 'ls' in the host)",
	Long:   ``,
	Args:   cobra.MinimumNArgs(1),
	PreRun: toggleDebug,
	RunE: func(cmd *cobra.Command, args []string) error {
		// fmt.Println("List: " + strings.Join(args, " "))
		return List(diskImage, args)
	},
}

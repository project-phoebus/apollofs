package cmd

import (
	"fmt"
	"math"
	"os"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
	"github.com/spf13/cobra"
)

func CopyIn(diskImage string, args []string) error {
	src := args[0]
	dest := args[1]

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

	_, err = nm.CreateFile(dest) // TODO(toshok) do we need to pass a uid here?
	if err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	numBlocksRequired := int(math.Ceil(float64(srcInfo.Size()) / 1024.0))

	fmt.Printf("Copying %d blocks (%d bytes) from %s to %s...\n", numBlocksRequired, srcInfo.Size(), src, dest)

	u, err := nm.Resolve(src)
	if err != nil {
		return err
	}

	vtoce, err := vm.GetEntryForUID(u)
	if err != nil {
		return err
	}

	if vtoce.IsDirectory() {
		return errNotAFile
	}

	fileBlockDAddrs, err := vm.GetFMBlocks(vtoce)
	if err != nil {
		return err
	}

	fmt.Printf("Copying %d blocks (%d bytes) from %s to %s...\n", len(fileBlockDAddrs), vtoce.Header.CurrentLength, src, dest)

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	remainingLength := int64(vtoce.Header.CurrentLength)

	for i, daddr := range fileBlockDAddrs {
		block, err := lvol.ReadBlock(daddr)
		if err != nil {
			return err
		}

		if block.Header.ObjectUID != vtoce.Header.ObjectUID {
			panic("block.ObjectUID != vtoce.ObjectUID")
		}

		if block.Header.PageWithinObject != int32(i) {
			panic(fmt.Sprintf("block.PageWithinObject %d != i %d", block.Header.PageWithinObject, i))
		}

		if remainingLength >= 1024 {
			// we can write the full length of the block data
			n, err := destFile.Write(block.Data[:])
			if err != nil {
				return err
			}
			if n != 1024 {
				panic("short write")
			}
			remainingLength -= int64(n)
		} else {
			// we need to truncate the write to remainingLength.  we're at the
			// end of the file.
			n, err := destFile.Write(block.Data[:remainingLength])
			if err != nil {
				return err
			}
			if n != int(remainingLength) {
				panic("short write")
			}

			// we're done.  make sure there are somehow not more blocks
			if i != len(fileBlockDAddrs)-1 {
				panic("more blocks after the last one written")
			}
			break
		}
	}

	fmt.Println("done.")

	return nil
}

func init() {
	rootCmd.AddCommand(copyInCommand)
}

var copyInCommand = &cobra.Command{
	Use:    "copyin [src] [dest]",
	Short:  "Copy a file from the host to the disk image",
	Long:   ``,
	PreRun: toggleDebug,
	RunE: func(cmd *cobra.Command, args []string) error {
		return CopyIn(diskImage, args)
	},
}

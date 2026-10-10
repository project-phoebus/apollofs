package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/managers"
)

var errNotAFile = errors.New("not a file")

func CopyOut(diskImage string, args []string) error {
	src, dest := args[0], args[1]
	v, err := managers.Open(diskImage, false)
	if err != nil {
		return err
	}
	defer v.Close()
	e, err := v.Names.Stat(src)
	if err != nil {
		return err
	}
	if e.IsDirectory() {
		return errNotAFile
	}
	data, err := v.Files.ReadData(e)
	if err != nil {
		return err
	}
	fmt.Printf("Copying %d blocks (%d bytes) from %s to %s...\n", (len(data)+1023)/1024, len(data), src, dest)
	if err := os.WriteFile(dest, data, 0644); err != nil {
		return err
	}
	fmt.Println("done.")
	return nil
}

func init() {
	rootCmd.AddCommand(copyOutCommand)
}

var copyOutCommand = &cobra.Command{
	Use:    "copyout [src] [dest]",
	Short:  "Copy a file from the disk image to the host",
	Args:   cobra.ExactArgs(2),
	PreRun: toggleDebug,
	RunE: func(cmd *cobra.Command, args []string) error {
		return CopyOut(diskImage, args)
	},
}

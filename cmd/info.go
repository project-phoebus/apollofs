package cmd

import (
	"fmt"
	"strconv"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
	"github.com/spf13/cobra"
)

var blockContents bool
var raw bool

func labels(diskImage string) error {
	pvol, err := fs.Mount(diskImage)
	if err != nil {
		return err
	}
	defer pvol.Unmount()

	fmt.Println("Disk image:", diskImage)
	pvol.PrintLabel()
	fmt.Println()
	pvol.LV.PrintLabel()

	return pvol.Unmount()
}

func vtoc(diskImage string) error {
	pvol, err := fs.Mount(diskImage)
	if err != nil {
		return err
	}
	defer pvol.Unmount()

	fmt.Println("Disk image:", diskImage)

	vtocHeader := pvol.LV.Label.VTOCHeader
	fmt.Println("VTOC Header:")
	vtocHeader.Print()

	fmt.Println("VTOC Map:")
	pvol.LV.VTOCMap.Print()

	fmt.Printf("DiskEntryDirVTOCX: daddr %d index %d\n", vtocHeader.DiskEntryDirVTOCX.BlockDAddr(), vtocHeader.DiskEntryDirVTOCX.Index())

	block, err := pvol.LV.ReadBlock(vtocHeader.DiskEntryDirVTOCX.BlockDAddr())
	if err != nil {
		return err
	}
	var vtocBlock fs.VTOCBlock
	err = block.ReadInto(&vtocBlock)
	if err != nil {
		return err
	}
	vtoce := vtocBlock.Entries[vtocHeader.DiskEntryDirVTOCX.Index()]
	vtoce.Print()

	return pvol.Unmount()
}

func block(diskImage string, physDAddr string) error {
	daddr, err := strconv.Atoi(physDAddr)
	if err != nil {
		return err
	}

	pvol, err := fs.Mount(diskImage)
	if err != nil {
		return err
	}
	defer pvol.Unmount()

	fmt.Println("Disk image:", diskImage)

	block, err := pvol.ReadBlock(fs.DAddr(daddr))
	if err != nil {
		return err
	}

	block.Print(blockContents, raw)

	return pvol.Unmount()
}

func vtoceInfo(diskImage, p string) error {
	v, err := managers.Open(diskImage, false)
	if err != nil {
		return err
	}
	defer v.Close()
	u, err := v.Names.Resolve(p)
	if err != nil {
		return err
	}
	x, err := v.VTOC.GetIndexForUID(u)
	if err != nil {
		return err
	}
	e, err := v.VTOC.GetEntry(x)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %v (type %s)\n", p, x, managers.TypeName(e.Header.ObjectTypeDefUID))
	e.Print()
	fmt.Printf("  attrs: %s\n", managers.Attrs(e.Attrs()))
	return nil
}

func init() {
	rootCmd.AddCommand(infoCommand)

	infoCommand.AddCommand(labelsCommand)
	infoCommand.AddCommand(vtoceCommand)
	infoCommand.AddCommand(vtocCommand)
	infoCommand.AddCommand(blockCommand)

	blockCommand.Flags().BoolVarP(&blockContents, "contents", "c", false, "dump block contents")
	blockCommand.Flags().BoolVarP(&raw, "raw", "r", false, "when dumping contents, force hex (raw) dump")
}

var infoCommand = &cobra.Command{
	Use:   "info [labels|vtoc|block]",
	Short: "Dump information about filesystem structures",
	Long:  "",
}

var vtoceCommand = &cobra.Command{
	Use:   "vtoce path",
	Short: "Dump the VTOCE of the object a path names (and its attribute image)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return vtoceInfo(diskImage, args[0])
	},
}

var labelsCommand = &cobra.Command{
	Use:   "labels",
	Short: "dump info about physical and logical volume labels",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		return labels(diskImage)
	},
}

var vtocCommand = &cobra.Command{
	Use:   "vtoc",
	Short: "dump info about logical volume VTOC",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		return vtoc(diskImage)
	},
}

var blockCommand = &cobra.Command{
	Use:   "block [physDAddr]",
	Short: "dump info about a disk block, given its physical address",
	Long:  "",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return block(diskImage, args[0])
	},
}

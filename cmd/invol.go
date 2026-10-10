package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/drives"
	"github.com/domainos-archeology/apollofs/pkg/managers"
)

var (
	sysbootPath    string
	dtype          string
	listDriveTypes bool
	involPVName    string
	involLVName    string
	involNodeID    string
	involNodeName  string
	involRevision  string
	involPaging    int
)

func invol(diskImage string) error {
	dtypeInt, err := strconv.ParseInt(dtype, 16, 64)
	if err != nil {
		return errors.New("driveType must be a hex string.  use --list-dtypes to see the list")
	}
	diskType, err := drives.GetDriveType(int16(dtypeInt))
	if err != nil {
		return fmt.Errorf("unknown driveType '%s'.  use --list-dtypes to see the list", dtype)
	}
	nodeID, err := strconv.ParseUint(involNodeID, 16, 32)
	if err != nil {
		return fmt.Errorf("bad node id %q", involNodeID)
	}
	v, err := managers.Format(diskImage, managers.FormatOptions{
		DriveType: diskType, PVName: involPVName, LVName: involLVName,
		NodeID: uint32(nodeID), NodeName: involNodeName, Revision: involRevision,
		PagingFilePages: involPaging,
	})
	if err != nil {
		return err
	}
	l := v.LV.Label
	fmt.Printf("%s: %s, %d blocks; BAT at %d (%d blocks free), %d VTOC hash buckets at %d, %d BAT partitions\n",
		diskImage, diskType.Name, diskType.TotalBlocks(), l.BATHeader.FirstBATBlockDAddr, l.BATHeader.NumFreeBlocks,
		l.VTOCHeader.VTOCSizeInBlocks, v.LV.VTOCMap[0].FirstBlockDAddr, l.NumPartitions)
	if err := v.Close(); err != nil {
		return err
	}
	if sysbootPath != "" {
		return CPboot(diskImage, sysbootPath)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(involCommand)
	f := involCommand.Flags()
	f.StringVarP(&sysbootPath, "cpboot", "b", "", "Path to sysboot file to copy to disk image")
	f.StringVarP(&dtype, "driveType", "t", "105", "drive type in hex (e.g. '105' for PRIAM 7050)")
	f.BoolVarP(&listDriveTypes, "list-dtypes", "l", false, "List available drive types")
	f.StringVar(&involPVName, "name", "APOLLO", "physical volume name")
	f.StringVar(&involLVName, "lvname", "", "logical volume name")
	f.StringVar(&involNodeID, "node-id", "12345", "node id (hex) for generated UIDs")
	f.StringVar(&involNodeName, "node-name", "", "name \"//\" catalogs \"/\" under (default node_<id>)")
	f.StringVar(&involRevision, "revision", "revision 10.2", "invol revision text in the PV label")
	f.IntVar(&involPaging, "paging-file", managers.DefaultPagingFilePages, "OS paging file size in pages (0 = none)")
}

var involCommand = &cobra.Command{
	Use:   "invol",
	Short: "Initialize a disk image (PV/LV labels, BAT, VTOC, / and //)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if listDriveTypes {
			for _, dt := range drives.GetDriveTypes() {
				fmt.Printf("0x%03X: %-20s %4d cyl %2d heads %2d blk/trk = %d blocks\n", dt.DType, dt.Name,
					dt.Cylinders, dt.Heads, dt.BlocksPerTrack, dt.TotalBlocks())
			}
			return nil
		}
		return invol(diskImage)
	},
}

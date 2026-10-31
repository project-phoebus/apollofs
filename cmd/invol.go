package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/project-phoebus/apollofs/pkg/drives"
	"github.com/project-phoebus/apollofs/pkg/managers"
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
	involSCSI      string
)

func invol(diskImage string) error {
	opt := managers.FormatOptions{PVName: involPVName, LVName: involLVName, NodeName: involNodeName,
		Revision: involRevision, PagingFilePages: involPaging}
	if involSCSI != "" {
		d, err := drives.ParseSCSIDisk(involSCSI)
		if err != nil {
			return err
		}
		if g := drives.MAMEGuess(d.ImageBytes()); g != d {
			fmt.Fprintf(os.Stderr, "note: MAME guesses %d/%d/%d for an image of this size, not %s\n",
				g.Cylinders, g.Heads, g.SectorsPerTrack, involSCSI)
		}
		opt.SCSI = &d
	} else {
		dtypeInt, err := strconv.ParseInt(dtype, 16, 64)
		if err != nil {
			return errors.New("driveType must be a hex string.  use --list-dtypes to see the list")
		}
		opt.DriveType, err = drives.GetDriveType(int16(dtypeInt))
		if err != nil {
			return fmt.Errorf("unknown driveType '%s'.  use --list-dtypes to see the list", dtype)
		}
	}
	nodeID, err := strconv.ParseUint(involNodeID, 16, 32)
	if err != nil {
		return fmt.Errorf("bad node id %q", involNodeID)
	}
	opt.NodeID = uint32(nodeID)
	v, err := managers.Format(diskImage, opt)
	if err != nil {
		return err
	}
	l := v.LV.Label
	name := opt.DriveType.Name
	if opt.SCSI != nil {
		name = fmt.Sprintf("SCSI %s (dtype 0x%03X, no block headers)", involSCSI, uint16(v.PV.Label.DriveType))
	}
	fmt.Printf("%s: %s, %d blocks; BAT at %d (%d blocks free), %d VTOC hash buckets at %d, %d BAT partitions\n",
		diskImage, name, v.PV.Label.TotalBlocksInVolume, l.BATHeader.FirstBATBlockDAddr, l.BATHeader.NumFreeBlocks,
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
	f.StringVar(&involSCSI, "scsi", "", "make a SCSI volume (no block headers) of a disk of cylinders/heads/sectors\n"+
		"(512-byte sectors per track; the image is the whole disk; -t is not used)")
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
			fmt.Println("0x7nn: SCSI (--scsi C/H/S): nn = the capacity in units of 10 MB")
			return nil
		}
		return invol(diskImage)
	},
}

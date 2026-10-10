package cmd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/drives"
	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/uid"
	"github.com/domainos-archeology/apollofs/pkg/util"
)

var (
	sysbootPath    string
	dtype          string
	listDriveTypes bool
)

func writeBlockAt(file *os.File, block fs.Block, blockDAddr fs.DAddr) error {
	_, err := file.Seek(int64(blockDAddr*fs.BlockSize), io.SeekStart)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.BigEndian, &block)
	if err != nil {
		return err
	}
	return nil
}

func createPVLabelBlock(diskType drives.DriveType, lvdaddr, altlvdaddr fs.DAddr) fs.Block {
	return fs.NewBlock(
		fs.BlockHeader{
			ObjectUID: uid.UIDpvlabel,
		},
		fs.PVLabel{
			Version:             1,
			APOLLO:              [6]byte{'A', 'P', 'O', 'L', 'L', 'O'},
			Name:                [32]byte{'A', 'P', 'O', 'L', 'L', 'O', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' '},
			UID:                 uid.UID{Hi: 0x776175af, Lo: 0x10012345}, // copied from my mame image
			DriveType:           1,                                       // is this DTYPE from EH87?
			TotalBlocksInVolume: diskType.TotalBlocks(),
			BlocksPerTrack:      diskType.BlocksPerTrack,
			TracksPerCylinder:   diskType.Heads,
			LVDAddr:             [10]fs.DAddr{lvdaddr, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			AltLVLabelDAddr:     [10]fs.DAddr{altlvdaddr, 0, 0, 0, 0, 0, 0, 0, 0},

			// next three copied from mame image.  no clue what they should be.
			SectorStart:     5,
			SectorSize:      2260,
			PreCompCylinder: 5,
		},
	)
}

func createLVLabelBlock(daddr fs.DAddr) fs.Block {
	return fs.NewBlock(
		fs.BlockHeader{
			ObjectUID:        uid.UIDlvlabel,
			PageWithinObject: int32(daddr) - 1,
			PVBlockDAddr:     daddr,
		},
		fs.LVLabel{
			Version:        1, // 1 == >= SR10
			Name:           [32]byte{' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' '},
			UID:            uid.UID{Hi: 0x776175d5, Lo: 0x20012345}, // copied from my mame image
			LabelWritten:   util.TimestampToApolloEpoch(time.Now()),
			BootTime:       util.TimestampToApolloEpoch(time.Now()),
			DismountedTime: util.TimestampToApolloEpoch(time.Now()),
		},
	)
}

func copySysboot(file *os.File, sysbootPath string) error {
	sysbootFile, err := os.Open(sysbootPath)
	if err != nil {
		return err
	}
	defer sysbootFile.Close()

	// copy the sysboot file a block at a time
	for i := 0; i < 10; i++ {
		var block fs.Block
		block.Header = fs.BlockHeader{
			ObjectUID:        uid.UID{Hi: 0x776175d5, Lo: 0x30012345},
			PageWithinObject: int32(i),
			PVBlockDAddr:     fs.DAddr(2 + i),
		}
		err = binary.Read(sysbootFile, binary.BigEndian, &block.Data)
		if err != nil {
			return err
		}
		err = writeBlockAt(file, block, fs.DAddr(2+i))
		if err != nil {
			return err
		}
	}

	return nil
}

func invol(diskImage string) error {
	// create a new disk image at that path
	dtypeInt, err := strconv.ParseInt(dtype, 16, 64)
	if err != nil {
		return errors.New("driveType must be a hex string.  use --list-dtypes to see the list")
	}

	diskType, err := drives.GetDriveType(int16(dtypeInt))
	if err != nil {
		return fmt.Errorf("unknown driveType '%s'.  use --list-dtypes to see the list", dtype)
	}

	totalBlocks := diskType.TotalBlocks()

	file, err := os.Create(diskImage)
	if err != nil {
		return err
	}
	defer file.Close()

	// initialize the image to be completely empty
	bytesToWrite := make([]byte, totalBlocks*fs.BlockSize)

	_, err = file.Write(bytesToWrite)
	if err != nil {
		return err
	}

	mainLVLabelDAddr := fs.DAddr(1)
	altLVLabelDAddr := fs.DAddr(totalBlocks / 2)

	// write our pv label
	writeBlockAt(file, createPVLabelBlock(diskType, mainLVLabelDAddr, altLVLabelDAddr), 0)

	// write our lv labels
	writeBlockAt(file, createLVLabelBlock(mainLVLabelDAddr), mainLVLabelDAddr) // primary
	writeBlockAt(file, createLVLabelBlock(altLVLabelDAddr), altLVLabelDAddr)   // alternate

	if sysbootPath != "" {
		err = copySysboot(file, sysbootPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(involCommand)

	involCommand.Flags().StringVarP(&sysbootPath, "cpboot", "b", "", "Path to sysboot file to copy to disk image")
	involCommand.Flags().StringVarP(&dtype, "driveType", "t", "", "string ID of drive type (e.g. '105' for PRIAM 7050).")
	involCommand.Flags().BoolVarP(&listDriveTypes, "list-dtypes", "l", false, "List available drive types")
}

var involCommand = &cobra.Command{
	Use:   "invol",
	Short: "Initialize a disk image",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		if listDriveTypes {
			for _, dt := range drives.GetDriveTypes() {
				fmt.Printf("0x%03X: %s\n", dt.DType, dt.Name)
			}
			return nil
		}

		err := invol(diskImage)
		if err != nil {
			return err
		}

		if sysbootPath != "" {
			// TODO(toshok) copy the sysboot file to the disk image
		}

		return nil
	},
}

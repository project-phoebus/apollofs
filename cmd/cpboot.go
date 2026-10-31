package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/project-phoebus/apollofs/pkg/managers"
)

var cpbootAttrs string

// CPboot installs sysboot as /sysboot with its pages in LV blocks 1..10.
func CPboot(diskImage, sysboot string) error {
	data, err := os.ReadFile(sysboot)
	if err != nil {
		return err
	}
	v, err := managers.Open(diskImage, true)
	if err != nil {
		return err
	}
	var a *managers.Attrs
	if cpbootAttrs != "" {
		p, err := managers.ParseAttrs(cpbootAttrs)
		if err != nil {
			v.Close()
			return err
		}
		a = &p
	}
	if err := v.InstallSysboot(data, a); err != nil {
		v.Close()
		return err
	}
	fmt.Printf("sysboot (%d bytes) installed in LV blocks 1..%d\n", len(data), (len(data)+1023)/1024)
	return v.Close()
}

func init() {
	rootCmd.AddCommand(cpbootCommand)
	cpbootCommand.Flags().StringVarP(&cpbootAttrs, "attrs", "a", "", "VTOCE attribute image for /sysboot (0x78 bytes hex)")
}

var cpbootCommand = &cobra.Command{
	Use:   "cpboot sysboot-file",
	Short: "Make the disk image bootable (copy sysboot to /sysboot, LV blocks 1-10)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return CPboot(diskImage, args[0])
	},
}

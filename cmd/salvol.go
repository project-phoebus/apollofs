package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/managers"
)

func Check(diskImage string) error {
	v, err := managers.Open(diskImage, false)
	if err != nil {
		return err
	}
	defer v.Close()
	r, err := v.Check()
	if err != nil {
		return err
	}
	for _, p := range r.Problems {
		fmt.Println(p)
	}
	l := v.LV.Label.BATHeader
	fmt.Printf("%d objects (%d directories), %d links, %d blocks in use, %d of %d free\n",
		r.Objects, r.Dirs, r.Links, r.BlocksInUse, l.NumFreeBlocks, l.NumBlocksRepresented)
	if len(r.Problems) > 0 {
		return fmt.Errorf("%d problems", len(r.Problems))
	}
	return nil
}

func init() {
	rootCmd.AddCommand(checkCommand)
}

var checkCommand = &cobra.Command{
	Use:     "check",
	Aliases: []string{"salvol"},
	Short:   "Check the volume's VTOC, file maps, directories and BAT for consistency (read-only)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return Check(diskImage)
	},
}

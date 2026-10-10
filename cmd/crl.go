package cmd

import (
	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/managers"
)

var crlReplace bool

func init() {
	rootCmd.AddCommand(crlCommand)
	rootCmd.AddCommand(rmCommand)
	crlCommand.Flags().BoolVarP(&crlReplace, "replace", "r", false, "replace an existing link")
}

var crlCommand = &cobra.Command{
	Use:   "crl path text",
	Short: "Create a soft link (like /com/crl): path names the link, text is what it resolves to",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := managers.Open(diskImage, true)
		if err != nil {
			return err
		}
		if err := v.Names.CreateLink(args[0], args[1], crlReplace); err != nil {
			v.Close()
			return err
		}
		return v.Close()
	},
}

var rmCommand = &cobra.Command{
	Use:   "rm path",
	Short: "Delete a file, a link or an empty directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := managers.Open(diskImage, true)
		if err != nil {
			return err
		}
		if err := v.Names.Remove(args[0]); err != nil {
			v.Close()
			return err
		}
		return v.Close()
	},
}

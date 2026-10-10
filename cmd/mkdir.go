package cmd

import (
	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
)

var (
	mkdirAttrs, mkdirDirProt, mkdirFileProt string
	mkdirParents                            bool
)

func Mkdir(diskImage string, p string) error {
	v, err := managers.Open(diskImage, true)
	if err != nil {
		return err
	}
	if mkdirParents && mkdirAttrs == "" && mkdirDirProt == "" {
		if err := v.Names.MkdirAll(p); err != nil {
			v.Close()
			return err
		}
		return v.Close()
	}
	var a *managers.Attrs
	if mkdirAttrs != "" {
		p, err := managers.ParseAttrs(mkdirAttrs)
		if err != nil {
			v.Close()
			return err
		}
		a = &p
	}
	var dp *[2]fs.DirDefProt
	if mkdirDirProt != "" || mkdirFileProt != "" {
		d := [2]fs.DirDefProt{managers.DefaultDefProt, managers.DefaultDefProt}
		for i, s := range []string{mkdirDirProt, mkdirFileProt} {
			if s == "" {
				continue
			}
			if d[i], err = managers.ParseDefProt(s); err != nil {
				v.Close()
				return err
			}
		}
		dp = &d
	}
	if _, err := v.Names.Mkdir(p, a, dp); err != nil {
		v.Close()
		return err
	}
	return v.Close()
}

func init() {
	rootCmd.AddCommand(mkdirCommand)
	f := mkdirCommand.Flags()
	f.StringVarP(&mkdirAttrs, "attrs", "a", "", "VTOCE attribute image (0x78 bytes hex)")
	f.StringVar(&mkdirDirProt, "dir-defprot", "", "default protection for new subdirectories (44 or 52 bytes hex)")
	f.StringVar(&mkdirFileProt, "file-defprot", "", "default protection for new files (44 or 52 bytes hex)")
	f.BoolVarP(&mkdirParents, "parents", "p", false, "create missing parents (default attributes)")
}

var mkdirCommand = &cobra.Command{
	Use:   "mkdir path",
	Short: "Create a directory in the disk image (similar to 'mkdir' in the host)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return Mkdir(diskImage, args[0])
	},
}

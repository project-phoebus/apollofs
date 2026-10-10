package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
)

var (
	copyinAttrs   string
	copyinType    string
	copyinLike    string
	copyinReplace bool
	copyinQuiet   bool
)

// fileAttrs builds the attribute image for a new or replaced object from
// --attrs, --like (an object already on the volume) or the default, then
// applies --type.
func fileAttrs(v *managers.Volume, attrsHex, like, typ string) (*managers.Attrs, error) {
	var a *managers.Attrs
	switch {
	case attrsHex != "":
		p, err := managers.ParseAttrs(attrsHex)
		if err != nil {
			return nil, err
		}
		a = &p
	case like != "":
		e, err := v.Names.Stat(like)
		if err != nil {
			return nil, fmt.Errorf("--like %s: %w", like, err)
		}
		p := managers.Attrs(e.Attrs())
		a = &p
	}
	if typ != "" {
		t, err := managers.ParseTypeUID(typ)
		if err != nil {
			return nil, err
		}
		if a == nil {
			p := managers.DefaultFileAttrs
			a = &p
		}
		e := fs.VTOCE{}
		e.SetAttrs(*a)
		e.Header.ObjectTypeDefUID = t
		p := managers.Attrs(e.Attrs())
		a = &p
	}
	return a, nil
}

func CopyIn(diskImage string, src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	v, err := managers.Open(diskImage, true)
	if err != nil {
		return err
	}
	a, err := fileAttrs(v, copyinAttrs, copyinLike, copyinType)
	if err != nil {
		v.Close()
		return err
	}
	if _, err := v.Names.WriteFile(dest, data, a, copyinReplace); err != nil {
		v.Close()
		return err
	}
	if !copyinQuiet {
		fmt.Printf("%s -> %s (%d bytes)\n", src, dest, len(data))
	}
	return v.Close()
}

func init() {
	rootCmd.AddCommand(copyInCommand)
	f := copyInCommand.Flags()
	f.StringVarP(&copyinAttrs, "attrs", "a", "", "VTOCE attribute image (0x78 bytes hex, as an rbak tape records it)")
	f.StringVarP(&copyinType, "type", "t", "", "type UID: a name (unstruct, uasc, coff, obj, rec, ...) or hhhhhhhh.llllllll")
	f.StringVar(&copyinLike, "like", "", "take attributes from this object on the volume")
	f.BoolVarP(&copyinReplace, "replace", "r", false, "overwrite an existing file (it keeps its UID)")
	f.BoolVarP(&copyinQuiet, "quiet", "q", false, "no progress line")
}

var copyInCommand = &cobra.Command{
	Use:    "copyin [src] [dest]",
	Short:  "Copy a file from the host to the disk image",
	Args:   cobra.ExactArgs(2),
	PreRun: toggleDebug,
	RunE: func(cmd *cobra.Command, args []string) error {
		return CopyIn(diskImage, args[0], args[1])
	},
}

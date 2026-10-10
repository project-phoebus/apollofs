package cmd

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/ct"
	"github.com/domainos-archeology/apollofs/pkg/managers"
	"github.com/domainos-archeology/apollofs/pkg/uid"
)

var ctVerifyRoot string

// CTIndex prints one tab-separated line per object on the tapes:
// volume, file sequence, HDR1 label, kind, path, type, size, attribute
// image (hex), and for directories their default-protection bytes, for
// links the link text.  With --verify DIR each file's tape contents are
// compared with DIR/<path>.
func CTIndex(tapes []string) error {
	bad := 0
	for _, tape := range tapes {
		err := ct.Read(tape, ctVerifyRoot != "", func(e *ct.Entry) error {
			hi, lo := e.Type()
			typ := managers.TypeName(uid.UID{Hi: hi, Lo: lo})
			extra := ""
			switch e.Kind {
			case ct.KindDir:
				extra = hex.EncodeToString(e.DirExtra)
			case ct.KindLink:
				extra = e.LinkText
				typ = "-"
			}
			acl := ""
			if e.ACL != nil {
				acl = "\tacl=" + hex.EncodeToString(e.ACL)
			}
			fmt.Printf("%d\t%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s%s\n", e.Volume, e.FileSeq, e.Label, e.Kind, e.Path, typ,
				e.Size, hex.EncodeToString(e.Attrs[:]), extra, acl)
			if ctVerifyRoot != "" && e.Kind == ct.KindFile {
				host, err := os.ReadFile(filepath.Join(ctVerifyRoot, e.Path))
				switch {
				case err != nil:
					fmt.Fprintf(os.Stderr, "verify: %s: %v\n", e.Path, err)
					bad++
				case !bytes.Equal(host, e.Data):
					fmt.Fprintf(os.Stderr, "verify: %s: host file (%d bytes) differs from tape (%d bytes)\n", e.Path, len(host), len(e.Data))
					bad++
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", tape, err)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d files differ from the tapes", bad)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(ctIndexCommand)
	ctIndexCommand.Flags().StringVar(&ctVerifyRoot, "verify", "", "compare file contents with this extracted tree")
}

var ctIndexCommand = &cobra.Command{
	Use:   "ctindex tape.ct...",
	Short: "List the objects on rbak cartridge-tape images with their tape file, type and VTOCE attributes",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return CTIndex(args)
	},
}

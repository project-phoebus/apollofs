package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domainos-archeology/apollofs/pkg/fs"
	"github.com/domainos-archeology/apollofs/pkg/managers"
	"github.com/domainos-archeology/apollofs/pkg/util"
)

var listLong, listRecursive bool

func listEntry(v *managers.Volume, path string, ent fs.DirEnt) {
	if !listLong {
		if ent.Type == fs.DirEntrySoftLink {
			fmt.Printf("%s -> %s\n", path, ent.LinkText)
		} else {
			fmt.Println(path)
		}
		return
	}
	if ent.Type == fs.DirEntrySoftLink {
		fmt.Printf("%-10s %-9s %10s  %-19s %s -> %s\n", "link", "", "", "", path, ent.LinkText)
		return
	}
	e, err := v.VTOC.GetEntryForUID(ent.UID)
	if err != nil {
		fmt.Printf("%-10s %s (%v)\n", "?", path, err)
		return
	}
	kind := managers.TypeName(e.Header.ObjectTypeDefUID)
	if e.IsDirectory() {
		kind = "dir"
	}
	h := e.Header
	clockh := uint32(h.DTM[0])<<24 | uint32(h.DTM[1])<<16 | uint32(h.DTM[2])<<8 | uint32(h.DTM[3])
	fmt.Printf("%-10s %d/%d %08x %10d  %s %s\n", kind, h.SystemType, h.Rights[0], h.ACLFlags, h.CurrentLength,
		util.ClockhToTime(clockh).Format("2006-01-02 15:04:05"), path)
}

func List(diskImage string, paths []string) error {
	v, err := managers.Open(diskImage, false)
	if err != nil {
		return err
	}
	defer v.Close()
	var walk func(p string, depth int) error
	walk = func(p string, depth int) error {
		u, err := v.Names.Resolve(p)
		if err != nil {
			return err
		}
		e, err := v.VTOC.GetEntryForUID(u)
		if err != nil {
			return err
		}
		if !e.IsDirectory() {
			parent, name, err := v.Names.LookupParent(p)
			if err != nil {
				return err
			}
			listEntry(v, p, *parent.Lookup(name))
			return nil
		}
		d, err := v.Names.ReadDir(u)
		if err != nil {
			return err
		}
		for _, ent := range d.Entries {
			sub := p + "/" + string(ent.Name)
			if p == "/" {
				sub = "/" + string(ent.Name)
			}
			show := string(ent.Name)
			if listRecursive {
				show = sub
			}
			listEntry(v, show, ent)
			if listRecursive && ent.Type == fs.DirEntryObject {
				if ce, err := v.VTOC.GetEntryForUID(ent.UID); err == nil && ce.IsDirectory() && depth < 64 {
					if err := walk(sub, depth+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, p := range paths {
		if err := walk(p, 0); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCommand)
	listCommand.Flags().BoolVarP(&listLong, "long", "l", false, "show type, sub type/owner rights, ACL flags, length and DTM")
	listCommand.Flags().BoolVarP(&listRecursive, "recursive", "R", false, "list subdirectories too, with full paths")
}

var listCommand = &cobra.Command{
	Use:    "list path...",
	Short:  "List files/directories in the disk image (similar to 'ls' in the host)",
	Args:   cobra.MinimumNArgs(1),
	PreRun: toggleDebug,
	RunE: func(cmd *cobra.Command, args []string) error {
		return List(diskImage, args)
	},
}

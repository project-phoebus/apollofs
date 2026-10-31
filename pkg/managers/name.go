package managers

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
	"github.com/project-phoebus/apollofs/pkg/util"
)

// NamingManager resolves pathnames through the "/" (disk entry) directory
// and edits directories by re-encoding them whole.
type NamingManager struct {
	v *Volume
}

var (
	ErrExists     = errors.New("name already exists")
	ErrNotDir     = errors.New("not a directory")
	ErrIsDir      = errors.New("is a directory")
	ErrLinkInPath = errors.New("path goes through a soft link")
)

func NewNamingManager(v *Volume) *NamingManager { return &NamingManager{v: v} }

// RootUID is the UID of "/" (the volume's disk entry directory).
func (nm *NamingManager) RootUID() (uid.UID, error) {
	e, err := nm.v.VTOC.GetEntry(nm.v.LV.Label.VTOCHeader.DiskEntryDirVTOCX)
	if err != nil {
		return uid.Empty, err
	}
	return e.Header.ObjectUID, nil
}

// NetRootUID is the UID of "//" (the network root directory).
func (nm *NamingManager) NetRootUID() (uid.UID, error) {
	e, err := nm.v.VTOC.GetEntry(nm.v.LV.Label.VTOCHeader.NetworkRootDirVTOCX)
	if err != nil {
		return uid.Empty, err
	}
	return e.Header.ObjectUID, nil
}

// Dir is a directory loaded for reading or editing.
type Dir struct {
	UID     uid.UID
	X       fs.VTOCX
	VTOCE   *fs.VTOCE
	Entries []fs.DirEnt
	Root    fs.DirRootArea
}

// ReadDir loads a directory.
func (nm *NamingManager) ReadDir(u uid.UID) (*Dir, error) {
	x, err := nm.v.VTOC.GetIndexForUID(u)
	if err != nil {
		return nil, fmt.Errorf("directory %v: %w", u, err)
	}
	e, err := nm.v.VTOC.GetEntry(x)
	if err != nil {
		return nil, err
	}
	if !e.IsDirectory() {
		return nil, ErrNotDir
	}
	data, err := nm.v.Files.ReadData(e)
	if err != nil {
		return nil, err
	}
	var pages [][]byte
	for o := 0; o+fs.DirPageSize <= len(data); o += fs.DirPageSize {
		pages = append(pages, data[o:o+fs.DirPageSize])
	}
	ents, root, err := fs.DecodeDirectory(pages)
	if err != nil {
		return nil, fmt.Errorf("directory %v: %w", u, err)
	}
	return &Dir{UID: u, X: x, VTOCE: e, Entries: ents, Root: root}, nil
}

// WriteDir re-encodes a directory and stores it.
func (nm *NamingManager) WriteDir(d *Dir) error {
	if !nm.v.Writable {
		return errReadOnly
	}
	pages, err := fs.EncodeDirectory(d.Entries, d.Root, nm.v.NewUID())
	if err != nil {
		return err
	}
	data := make([]byte, 0, len(pages)*fs.DirPageSize)
	for i := range pages {
		data = append(data, pages[i][:]...)
	}
	if err := nm.v.Files.WriteData(d.VTOCE, data, int32(d.VTOCE.Header.SystemType)<<16, d.X.BlockDAddr()); err != nil {
		return err
	}
	return nm.v.VTOC.PutEntry(d.X, d.VTOCE)
}

func (d *Dir) find(name string) int {
	for i, e := range d.Entries {
		if bytes.Equal(e.Name, []byte(name)) {
			return i
		}
	}
	return -1
}

// Lookup returns the entry for name, or nil.
func (d *Dir) Lookup(name string) *fs.DirEnt {
	if i := d.find(name); i >= 0 {
		return &d.Entries[i]
	}
	return nil
}

func splitPath(p string) ([]string, error) {
	if !path.IsAbs(p) {
		return nil, fmt.Errorf("path %q must be absolute", p)
	}
	return util.SplitPath(path.Clean(p)), nil
}

// Resolve returns the UID a path names.  Soft links are not followed.
func (nm *NamingManager) Resolve(p string) (uid.UID, error) {
	parts, err := splitPath(p)
	if err != nil {
		return uid.Empty, err
	}
	cur, err := nm.RootUID()
	if err != nil {
		return uid.Empty, err
	}
	for i, name := range parts {
		d, err := nm.ReadDir(cur)
		if err != nil {
			return uid.Empty, err
		}
		ent := d.Lookup(name)
		if ent == nil {
			return uid.Empty, fmt.Errorf("%s: %w", "/"+strings.Join(parts[:i+1], "/"), errNotFound)
		}
		if ent.Type == fs.DirEntrySoftLink {
			return uid.Empty, fmt.Errorf("%s -> %s: %w", "/"+strings.Join(parts[:i+1], "/"), ent.LinkText, ErrLinkInPath)
		}
		cur = ent.UID
	}
	return cur, nil
}

// LookupParent loads the directory holding p and returns the last name.
func (nm *NamingManager) LookupParent(p string) (*Dir, string, error) {
	parts, err := splitPath(p)
	if err != nil {
		return nil, "", err
	}
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("%q has no parent", p)
	}
	parent, err := nm.Resolve("/" + strings.Join(parts[:len(parts)-1], "/"))
	if err != nil {
		return nil, "", err
	}
	d, err := nm.ReadDir(parent)
	if err != nil {
		return nil, "", err
	}
	return d, parts[len(parts)-1], nil
}

func (nm *NamingManager) stampTimes(e *fs.VTOCE) {
	t := util.ClockBytes(nm.v.Now())
	e.Header.DTM, e.Header.DTU, e.Header.DTC, e.Header.DTA = t, t, t, t
}

// newVTOCE builds the VTOCE for a new object from attrs (or the default).
func (nm *NamingManager) newVTOCE(attrs *Attrs, dir bool, parent uid.UID) *fs.VTOCE {
	e := &fs.VTOCE{}
	switch {
	case attrs != nil:
		e.SetAttrs(*attrs)
	case dir:
		e.SetAttrs(DefaultDirAttrs)
		nm.stampTimes(e)
	default:
		e.SetAttrs(DefaultFileAttrs)
		nm.stampTimes(e)
	}
	e.Header.Version = 1
	e.Header.Flags |= fs.VTOCEFlagInUse
	e.Header.ObjectUID = nm.v.NewUID()
	e.Header.DirectoryUID = parent
	e.Header.CurrentLength, e.Header.BlocksUsed = 0, 0
	if dir {
		e.Header.SystemType = 1
		e.Header.ObjectTypeDefUID = uid.Empty
	}
	return e
}

// Mkdir creates a directory.  defProt (directory default, file default)
// fills the new root page; nil uses the SR10.2 default.
func (nm *NamingManager) Mkdir(p string, attrs *Attrs, defProt *[2]fs.DirDefProt) (uid.UID, error) {
	parent, name, err := nm.LookupParent(p)
	if err != nil {
		return uid.Empty, err
	}
	if parent.Lookup(name) != nil {
		return uid.Empty, fmt.Errorf("%s: %w", p, ErrExists)
	}
	e := nm.newVTOCE(attrs, true, parent.UID)
	x, err := nm.v.VTOC.AllocateEntry(e, parent.X.BlockDAddr())
	if err != nil {
		return uid.Empty, err
	}
	dp := [2]fs.DirDefProt{DefaultDefProt, DefaultDefProt}
	if defProt != nil {
		dp = *defProt
	}
	d := &Dir{UID: e.Header.ObjectUID, X: x, VTOCE: e, Root: fs.NewDirRootArea(dp[0], dp[1])}
	if err := nm.WriteDir(d); err != nil {
		return uid.Empty, err
	}
	parent.Entries = append(parent.Entries, fs.DirEnt{Type: fs.DirEntryObject, Name: []byte(name), UID: e.Header.ObjectUID})
	return e.Header.ObjectUID, nm.WriteDir(parent)
}

// MkdirAll creates p and any missing parents with default attributes.
func (nm *NamingManager) MkdirAll(p string) error {
	parts, err := splitPath(p)
	if err != nil {
		return err
	}
	for i := range parts {
		sub := "/" + strings.Join(parts[:i+1], "/")
		if _, err := nm.Resolve(sub); err == nil {
			continue
		}
		if _, err := nm.Mkdir(sub, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// WriteFile creates (or, with replace, overwrites) a file.  On replace the
// object keeps its UID; its attributes become attrs when given.
func (nm *NamingManager) WriteFile(p string, data []byte, attrs *Attrs, replace bool) (uid.UID, error) {
	parent, name, err := nm.LookupParent(p)
	if err != nil {
		return uid.Empty, err
	}
	if ent := parent.Lookup(name); ent != nil {
		if !replace {
			return uid.Empty, fmt.Errorf("%s: %w", p, ErrExists)
		}
		if ent.Type == fs.DirEntrySoftLink {
			parent.Entries = append(parent.Entries[:parent.find(name)], parent.Entries[parent.find(name)+1:]...)
			if err := nm.WriteDir(parent); err != nil {
				return uid.Empty, err
			}
			return nm.WriteFile(p, data, attrs, false)
		}
		x, err := nm.v.VTOC.GetIndexForUID(ent.UID)
		if err != nil {
			return uid.Empty, err
		}
		e, err := nm.v.VTOC.GetEntry(x)
		if err != nil {
			return uid.Empty, err
		}
		if e.IsDirectory() {
			return uid.Empty, fmt.Errorf("%s: %w", p, ErrIsDir)
		}
		if attrs != nil {
			u, dirp := e.Header.ObjectUID, e.Header.DirectoryUID
			fm := *e
			e.SetAttrs(*attrs)
			e.Header.ObjectUID, e.Header.DirectoryUID = u, dirp
			e.Header.Flags |= fs.VTOCEFlagInUse
			e.Header.CurrentLength, e.Header.BlocksUsed = fm.Header.CurrentLength, fm.Header.BlocksUsed
		}
		if err := nm.v.Files.WriteData(e, data, 0, x.BlockDAddr()); err != nil {
			return uid.Empty, err
		}
		return e.Header.ObjectUID, nm.v.VTOC.PutEntry(x, e)
	}
	e := nm.newVTOCE(attrs, false, parent.UID)
	if e.IsDirectory() {
		return uid.Empty, fmt.Errorf("%s: attributes describe a directory", p)
	}
	x, err := nm.v.VTOC.AllocateEntry(e, parent.X.BlockDAddr())
	if err != nil {
		return uid.Empty, err
	}
	if err := nm.v.Files.WriteData(e, data, 0, x.BlockDAddr()); err != nil {
		return uid.Empty, err
	}
	if err := nm.v.VTOC.PutEntry(x, e); err != nil {
		return uid.Empty, err
	}
	parent.Entries = append(parent.Entries, fs.DirEnt{Type: fs.DirEntryObject, Name: []byte(name), UID: e.Header.ObjectUID})
	return e.Header.ObjectUID, nm.WriteDir(parent)
}

// CreateLink makes a soft link (crl).  replace swaps an existing link.
func (nm *NamingManager) CreateLink(p, text string, replace bool) error {
	parent, name, err := nm.LookupParent(p)
	if err != nil {
		return err
	}
	if len(text) == 0 || len(text) > 1023 {
		return fmt.Errorf("bad link text %q", text)
	}
	if i := parent.find(name); i >= 0 {
		if !replace || parent.Entries[i].Type != fs.DirEntrySoftLink {
			return fmt.Errorf("%s: %w", p, ErrExists)
		}
		parent.Entries = append(parent.Entries[:i], parent.Entries[i+1:]...)
	}
	parent.Entries = append(parent.Entries, fs.DirEnt{Type: fs.DirEntrySoftLink, Name: []byte(name), LinkText: []byte(text)})
	return nm.WriteDir(parent)
}

// Remove deletes a link, a file or an empty directory.
func (nm *NamingManager) Remove(p string) error {
	parent, name, err := nm.LookupParent(p)
	if err != nil {
		return err
	}
	i := parent.find(name)
	if i < 0 {
		return fmt.Errorf("%s: %w", p, errNotFound)
	}
	ent := parent.Entries[i]
	if ent.Type == fs.DirEntryObject {
		x, err := nm.v.VTOC.GetIndexForUID(ent.UID)
		if err != nil {
			return err
		}
		e, err := nm.v.VTOC.GetEntry(x)
		if err != nil {
			return err
		}
		if e.IsDirectory() {
			d, err := nm.ReadDir(ent.UID)
			if err != nil {
				return err
			}
			if len(d.Entries) > 0 {
				return fmt.Errorf("%s: directory not empty", p)
			}
		}
		if err := nm.v.Files.FreeData(e); err != nil {
			return err
		}
		if err := nm.v.VTOC.FreeEntry(ent.UID); err != nil {
			return err
		}
	}
	parent.Entries = append(parent.Entries[:i], parent.Entries[i+1:]...)
	return nm.WriteDir(parent)
}

// Stat returns the VTOCE for p.
func (nm *NamingManager) Stat(p string) (*fs.VTOCE, error) {
	u, err := nm.Resolve(p)
	if err != nil {
		return nil, err
	}
	return nm.v.VTOC.GetEntryForUID(u)
}

package layer

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one filesystem item under the source directory.
type Entry struct {
	Rel  string      // slash-separated path relative to the source dir; "" is the dir itself (excluded)
	Abs  string      // absolute path on disk
	Mode fs.FileMode // file mode including type bits
	Size int64
	Link string // symlink target, if symlink
}

// IsDir reports whether the entry is a directory.
func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// IsSymlink reports whether the entry is a symlink.
func (e Entry) IsSymlink() bool { return e.Mode&fs.ModeSymlink != 0 }

// IsRegular reports whether the entry is a regular file.
func (e Entry) IsRegular() bool { return e.Mode.IsRegular() }

// Scan walks dir without following symlinks and returns entries sorted by Rel.
// It errors on whiteout names (.wh.*) and non-regular file types
// (sockets, devices, fifos). Hardlinked files appear as regular files.
func Scan(dir string) ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root dir itself is not an entry
		}
		name := d.Name()
		if strings.HasPrefix(name, ".wh.") {
			return fmt.Errorf("layer: whiteout-like name %q is not allowed", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := Entry{
			Rel:  filepath.ToSlash(rel),
			Abs:  path,
			Mode: info.Mode(),
			Size: info.Size(),
		}
		switch {
		case info.Mode().IsRegular(), info.Mode().IsDir():
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := readlinkAbs(path)
			if err != nil {
				return err
			}
			e.Link = target
			e.Size = 0
		default:
			return fmt.Errorf("layer: unsupported file type %s (%s)", rel, info.Mode().Type())
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })
	return entries, nil
}

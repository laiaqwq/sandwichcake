package layer

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// ChmodSpec overrides file/dir modes written into the tar.
type ChmodSpec struct {
	FileMode fs.FileMode // applied to regular files (0 = keep default)
	DirMode  fs.FileMode // applied to directories (0 = keep default)
}

// TarOptions controls WriteTar output.
type TarOptions struct {
	Root      string    // absolute container path the entries are rooted at, e.g. /opt/venv
	MTime     time.Time // ModTime for all entries
	UID, GID  int
	Chmod     *ChmodSpec // optional mode override
	NoParents bool       // do not write ancestor dirs above Root
}

// rootRel returns the slash path of Root without leading/trailing slashes,
// e.g. "/opt/venv" -> "opt/venv". "" or "/" -> "".
func rootRel(root string) string {
	return strings.Trim(path.Clean("/"+root), "/")
}

func dirMode(opts TarOptions) int64 {
	if opts.Chmod != nil && opts.Chmod.DirMode != 0 {
		return int64(opts.Chmod.DirMode.Perm())
	}
	return 0o755
}

func fileMode(e Entry, opts TarOptions) int64 {
	if opts.Chmod != nil && opts.Chmod.FileMode != 0 {
		return int64(opts.Chmod.FileMode.Perm())
	}
	if e.Mode.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// WriteTar writes a deterministic tar of the group's entries to w.
// See DESIGN 2.5: no leading "/", ancestor dirs 0755 0:0 unless NoParents,
// self-contained dir entries inside Root, fixed mtimes, no xattrs.
func WriteTar(w io.Writer, g Group, opts TarOptions) error {
	tw := tar.NewWriter(w)
	root := rootRel(opts.Root)
	mtime := opts.MTime.UTC()

	writeDir := func(name string, mode int64) error {
		return tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     name + "/",
			Mode:     mode,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		})
	}
	writeFile := func(name string, e Entry, mode int64) error {
		return tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     e.Size,
			Mode:     mode,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		})
	}
	writeLink := func(name string, e Entry) error {
		return tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeSymlink,
			Name:     name,
			Linkname: e.Link,
			Mode:     0o777,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		})
	}

	written := map[string]bool{}

	// Ancestors above Root (e.g. "opt" for "/opt/venv"), 0755 root:root.
	if !opts.NoParents && root != "" {
		for _, d := range ParentDirs(root) {
			if !written[d] {
				written[d] = true
				if err := writeDir(d, 0o755); err != nil {
					return err
				}
			}
		}
	}
	// The Root dir itself.
	if root != "" && !written[root] {
		written[root] = true
		if err := writeDir(root, dirMode(opts)); err != nil {
			return err
		}
	}

	// Ensure every ancestor dir within Root of every member is written once.
	needed := map[string]bool{}
	for _, e := range g.Entries {
		for _, d := range ParentDirs(e.Rel) {
			if !written[joinRel(root, d)] {
				needed[d] = true
			}
		}
	}
	var pending []string
	for d := range needed {
		pending = append(pending, d)
	}
	sort.Strings(pending)
	for _, d := range pending {
		name := joinRel(root, d)
		if written[name] {
			continue
		}
		written[name] = true
		if err := writeDir(name, dirMode(opts)); err != nil {
			return err
		}
	}

	for _, e := range g.Entries {
		name := joinRel(root, e.Rel)
		switch {
		case e.IsDir():
			if written[name] {
				continue
			}
			written[name] = true
			if err := writeDir(name, dirMode(opts)); err != nil {
				return err
			}
		case e.IsSymlink():
			if err := writeLink(name, e); err != nil {
				return err
			}
		case e.IsRegular():
			if err := writeFile(name, e, fileMode(e, opts)); err != nil {
				return err
			}
			f, err := os.Open(e.Abs)
			if err != nil {
				return err
			}
			_, cpErr := io.CopyN(tw, f, e.Size)
			clErr := f.Close()
			if cpErr != nil {
				return fmt.Errorf("layer: writing %s: %w", e.Rel, cpErr)
			}
			if clErr != nil {
				return clErr
			}
		default:
			return fmt.Errorf("layer: unsupported entry type %s", e.Rel)
		}
	}
	return tw.Close()
}

func joinRel(root, rel string) string {
	if root == "" {
		return rel
	}
	if rel == "" {
		return root
	}
	return root + "/" + rel
}

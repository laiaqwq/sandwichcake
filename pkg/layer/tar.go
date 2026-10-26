package layer

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
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

// tarItem is one header of a layer tar; Entry indexes g.Entries for
// regular files whose content follows the header (-1 otherwise).
type tarItem struct {
	Header *tar.Header
	Entry  int
}

// tarItems lists the deterministic headers of the group's tar in order.
// See DESIGN 2.5: no leading "/", ancestor dirs 0755 0:0 unless NoParents,
// self-contained dir entries inside Root, fixed mtimes, no xattrs.
func tarItems(g Group, opts TarOptions) ([]tarItem, error) {
	var items []tarItem
	root := rootRel(opts.Root)
	mtime := opts.MTime.UTC()

	writeDir := func(name string, mode int64) error {
		items = append(items, tarItem{Entry: -1, Header: &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     name + "/",
			Mode:     mode,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		}})
		return nil
	}
	writeFile := func(name string, i int, mode int64) error {
		items = append(items, tarItem{Entry: i, Header: &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     g.Entries[i].Size,
			Mode:     mode,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		}})
		return nil
	}
	writeLink := func(name string, e Entry) error {
		items = append(items, tarItem{Entry: -1, Header: &tar.Header{
			Typeflag: tar.TypeSymlink,
			Name:     name,
			Linkname: e.Link,
			Mode:     0o777,
			Uid:      opts.UID,
			Gid:      opts.GID,
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		}})
		return nil
	}

	written := map[string]bool{}

	// Ancestors above Root (e.g. "opt" for "/opt/venv"), 0755 root:root.
	if !opts.NoParents && root != "" {
		for _, d := range ParentDirs(root) {
			if !written[d] {
				written[d] = true
				if err := writeDir(d, 0o755); err != nil {
					return nil, err
				}
			}
		}
	}
	// The Root dir itself.
	if root != "" && !written[root] {
		written[root] = true
		if err := writeDir(root, dirMode(opts)); err != nil {
			return nil, err
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
			return nil, err
		}
	}

	for i, e := range g.Entries {
		name := joinRel(root, e.Rel)
		switch {
		case e.IsDir():
			if written[name] {
				continue
			}
			written[name] = true
			if err := writeDir(name, dirMode(opts)); err != nil {
				return nil, err
			}
		case e.IsSymlink():
			if err := writeLink(name, e); err != nil {
				return nil, err
			}
		case e.IsRegular():
			if err := writeFile(name, i, fileMode(e, opts)); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("layer: unsupported entry type %s", e.Rel)
		}
	}
	return items, nil
}

// WriteTar writes a deterministic tar of the group's entries to w.
func WriteTar(w io.Writer, g Group, opts TarOptions) error {
	items, err := tarItems(g, opts)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	for _, it := range items {
		if err := tw.WriteHeader(it.Header); err != nil {
			return err
		}
		if it.Entry < 0 {
			continue
		}
		e := g.Entries[it.Entry]
		f, err := e.OpenAt(0)
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
	}
	return tw.Close()
}

// Piece is one contiguous byte range of a layer tar: either literal bytes
// (headers, padding, trailer) or the content of g.Entries[Entry]. Every
// regular file has a content piece, even when empty (Len 0).
type Piece struct {
	Offset int64
	Len    int64
	Data   []byte // literal bytes when Entry < 0
	Entry  int
}

// Layout is the precomputed byte layout of a group's tar, identical to
// what WriteTar produces. It lets callers start the stream at any offset
// and know file boundaries before any content is read.
type Layout struct {
	Pieces []Piece
	Size   int64
}

// TarLayout computes the layout of WriteTar(g, opts) without reading content.
func TarLayout(g Group, opts TarOptions) (*Layout, error) {
	items, err := tarItems(g, opts)
	if err != nil {
		return nil, err
	}
	l := &Layout{}
	add := func(p Piece) {
		if p.Len == 0 && p.Entry < 0 {
			return
		}
		p.Offset = l.Size
		l.Pieces = append(l.Pieces, p)
		l.Size += p.Len
	}
	for _, it := range items {
		var buf bytes.Buffer
		// tar.Writer emits the header blocks (incl. PAX records) eagerly and
		// keeps no state across entries, so a fresh writer per header yields
		// the same bytes as one writer for the whole archive.
		if err := tar.NewWriter(&buf).WriteHeader(it.Header); err != nil {
			return nil, err
		}
		add(Piece{Data: buf.Bytes(), Len: int64(buf.Len()), Entry: -1})
		if it.Entry >= 0 {
			size := g.Entries[it.Entry].Size
			add(Piece{Entry: it.Entry, Len: size})
			if pad := (blockSize - size%blockSize) % blockSize; pad > 0 {
				add(Piece{Data: make([]byte, pad), Len: pad, Entry: -1})
			}
		}
	}
	add(Piece{Data: make([]byte, 2*blockSize), Len: 2 * blockSize, Entry: -1})
	return l, nil
}

const blockSize = 512

// PieceAt returns the index of the first piece that still has bytes at or
// after off, including an empty file piece starting exactly at off
// (len(Pieces) at EOF).
func (l *Layout) PieceAt(off int64) int {
	i := sort.Search(len(l.Pieces), func(i int) bool {
		return l.Pieces[i].Offset+l.Pieces[i].Len > off
	})
	for i > 0 && l.Pieces[i-1].Len == 0 && l.Pieces[i-1].Offset == off {
		i--
	}
	return i
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

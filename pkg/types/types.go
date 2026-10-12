// Package types defines the sandwichcake type system: the Type interface,
// the registry of built-in and user types, and shared data structures.
package types

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Option declares one type-specific flag (used by the CLI to generate
// `swcake pack <type>` flags).
type Option struct {
	Name    string
	Usage   string
	Default string
	Bool    bool
}

// Rewrite maps a path prefix to another (literal text replacement).
type Rewrite struct {
	From, To string
}

// Opts are the resolved type options for one pack invocation.
type Opts struct {
	Values        map[string]string // declared options; bools as "true"/"false"
	RewritePrefix []Rewrite
	Platform      string // "linux/amd64" or "any"
}

// Get returns the option value or def if unset.
func (o Opts) Get(name, def string) string {
	if v, ok := o.Values[name]; ok && v != "" {
		return v
	}
	return def
}

// Defaults are a type's default pack settings.
type Defaults struct {
	Compression layer.Compression
	Level       int
	Split       bool
	Large       int64
	Small       int64
}

// Source describes the directory being packed.
type Source struct {
	Dir     string        // working dir (may be a temp copy made by EnsureCopy)
	OrigDir string        // the user's original dir
	Root    string        // container root path, e.g. /opt/venv
	Entries []layer.Entry // result of Scan(Dir)
	Opts    Opts
	TmpDir  string         // scratch dir owned by the pack run
	State   map[string]any // scratch space shared between Prepare/Annotate/Metadata
}

// EnsureCopy copies OrigDir into a temp dir once (preserving symlinks and
// modes) so Prepare may modify files without touching the user's dir, then
// re-scans. Idempotent: if Dir already differs from OrigDir it is a no-op.
func (s *Source) EnsureCopy() error {
	if s.Dir != s.OrigDir && s.Dir != "" {
		return nil
	}
	dst, err := os.MkdirTemp(s.TmpDir, "src-*")
	if err != nil {
		return err
	}
	if err := copyTree(s.OrigDir, dst); err != nil {
		return err
	}
	s.Dir = dst
	entries, err := layer.Scan(dst)
	if err != nil {
		return err
	}
	s.Entries = entries
	return nil
}

// Rescan refreshes Entries after Prepare modified Dir.
func (s *Source) Rescan() error {
	entries, err := layer.Scan(s.Dir)
	if err != nil {
		return err
	}
	s.Entries = entries
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		default:
			return fmt.Errorf("types: cannot copy %s (%s)", rel, info.Mode().Type())
		}
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// MetadataFile is a file attached to the slice's metadata artifact.
type MetadataFile struct {
	Name      string // file name (annotation org.opencontainers.image.title)
	MediaType string // blob media type
	Data      []byte
}

// FindingLevel is the severity of a Check finding.
type FindingLevel string

const (
	Warn  FindingLevel = "warn"
	Error FindingLevel = "error"
)

// Finding is a Check result.
type Finding struct {
	Level FindingLevel
	Msg   string
}

// LayerInfo describes one layer of a resolved slice.
type LayerInfo struct {
	Digest, DiffID v1.Hash
	Size           int64
	MediaType      string
	Annotations    map[string]string
}

// Slice is a resolved sandwichcake slice (DESIGN 2.2).
type Slice struct {
	Ref         string // repo@digest
	Annotations map[string]string
	Layers      []LayerInfo
}

// ComposeCtx carries compose-time context to Check implementations.
type ComposeCtx struct {
	Platform      v1.Platform
	BaseRef       string // repo@<selected manifest digest>
	BaseRefDigest string // repo@<digest the base ref resolved to> (may differ on index)
	BaseEnv       []string
	BaseLibc      string // e.g. "glibc 2.35", "musl", "" unknown
}

// Type is the pack-stage plugin interface (DESIGN 2.7.1).
type Type interface {
	Name() string
	Schema() int
	Options() []Option
	Defaults() Defaults
	Prepare(ctx context.Context, src *Source, opts Opts) error
	Layout(src *Source) []layer.Rule
	Annotate(src *Source) map[string]string
	Metadata(src *Source) []MetadataFile
	Check(ctx context.Context, s *Slice, cctx *ComposeCtx) []Finding
}

// Namer is an optional interface for types that can auto-generate a target
// reference (e.g. weight's hf:// source).
type Namer interface {
	DefaultTarget(src *Source, prefix string) (string, error)
}

var (
	regMu sync.RWMutex
	reg   = map[string]Type{}
)

// Register registers t; panics on duplicate name.
func Register(t Type) {
	regMu.Lock()
	defer regMu.Unlock()
	name := t.Name()
	if _, ok := reg[name]; ok {
		panic("types: duplicate type " + name)
	}
	reg[name] = t
}

// Get returns the type with the given name, or nil.
func Get(name string) Type {
	regMu.RLock()
	defer regMu.RUnlock()
	return reg[name]
}

// All returns all registered types sorted by name.
func All() []Type {
	regMu.RLock()
	defer regMu.RUnlock()
	var out []Type
	for _, t := range reg {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

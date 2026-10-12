// Package venv implements the "venv" type: a Python virtual environment,
// self-contained with an embedded interpreter under .python/ by default.
package venv

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
	"github.com/baba/sandwichcake/pkg/types/internal/elfscan"
)

type venvType struct{}

func init() { types.Register(venvType{}) }

func (venvType) Name() string { return "venv" }
func (venvType) Schema() int  { return 1 }
func (venvType) Options() []types.Option {
	return []types.Option{
		{Name: "python", Usage: "embedded|system: whether the venv carries its own interpreter", Default: "embedded"},
		{Name: "lock", Usage: "path to a lock file (uv.lock, poetry.lock, requirements.txt) to attach as metadata"},
	}
}

func (venvType) Defaults() types.Defaults {
	return types.Defaults{
		Compression: layer.CompressionZstd,
		Split:       true,
		Large:       layer.DefaultLargeThreshold,
		Small:       layer.DefaultSmallThreshold,
	}
}

func key(k string) string { return layer.TypeKey("venv", k) }

var pyLibDir = regexp.MustCompile(`^lib/python3\.\d+/site-packages/`)

func hasEntry(src *types.Source, rel string, dir bool) bool {
	for _, e := range src.Entries {
		if e.Rel == rel {
			if dir {
				return e.IsDir()
			}
			return true
		}
		if dir && strings.HasPrefix(e.Rel, rel+"/") {
			return true
		}
	}
	return false
}

func anyEntryPrefix(src *types.Source, re *regexp.Regexp) bool {
	for _, e := range src.Entries {
		if re.MatchString(e.Rel + "/") {
			return true
		}
	}
	return false
}

func parsePyvenvCfg(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, sc.Err()
}

func isELFPath(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	return magic == [4]byte{0x7f, 'E', 'L', 'F'}
}

// rewriteFile performs literal From→To text replacement in path if the file
// is text (not ELF, no NUL bytes in the first 8 KiB).
func rewriteFile(path string, rw types.Rewrite) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if strings.IndexByte(string(probe), 0) >= 0 || isELFPath(path) {
		return false, nil
	}
	newData := strings.ReplaceAll(string(data), rw.From, rw.To)
	if newData == string(data) {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(newData), info.Mode().Perm())
}

func (venvType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error {
	mode := opts.Get("python", "embedded")
	if mode != "embedded" && mode != "system" {
		return fmt.Errorf("venv: --python must be embedded or system, got %q", mode)
	}
	if !hasEntry(src, "pyvenv.cfg", false) {
		return fmt.Errorf("venv: pyvenv.cfg not found under %s", src.Dir)
	}
	if !hasEntry(src, "bin/python", false) {
		return fmt.Errorf("venv: bin/python not found under %s", src.Dir)
	}
	if !anyEntryPrefix(src, pyLibDir) {
		return fmt.Errorf("venv: lib/python3.X/site-packages not found under %s", src.Dir)
	}
	cfg, err := parsePyvenvCfg(filepath.Join(src.Dir, "pyvenv.cfg"))
	if err != nil {
		return err
	}
	home := cfg["home"]
	if mode == "embedded" {
		if !hasEntry(src, ".python", true) {
			return fmt.Errorf("venv: embedded mode requires a .python/ interpreter tree")
		}
		want := filepath.Join(src.Root, ".python/bin")
		if filepath.Clean(home) != filepath.Clean(want) && len(opts.RewritePrefix) == 0 {
			return fmt.Errorf("venv: pyvenv.cfg home %q != %q; use --rewrite-prefix", home, want)
		}
	}
	if len(opts.RewritePrefix) > 0 {
		if err := src.EnsureCopy(); err != nil {
			return err
		}
		for _, rw := range opts.RewritePrefix {
			if err := applyRewrite(src, rw); err != nil {
				return err
			}
		}
		if err := src.Rescan(); err != nil {
			return err
		}
		// Re-validate home after rewrite.
		cfg, err = parsePyvenvCfg(filepath.Join(src.Dir, "pyvenv.cfg"))
		if err != nil {
			return err
		}
		if mode == "embedded" {
			want := filepath.Join(src.Root, ".python/bin")
			if filepath.Clean(cfg["home"]) != filepath.Clean(want) {
				return fmt.Errorf("venv: after rewrite, pyvenv.cfg home %q != %q", cfg["home"], want)
			}
		}
	}
	if src.State == nil {
		src.State = map[string]any{}
	}
	src.State["pyvenv.cfg"] = cfg
	src.State["python.mode"] = mode
	return nil
}

// applyRewrite replaces rw.From with rw.To in pyvenv.cfg, text files in bin/,
// *.pth in site-packages, _sysconfigdata_*.py and config-*/Makefile under
// .python/lib/python3.*/.
func applyRewrite(src *types.Source, rw types.Rewrite) error {
	pythonLib := regexp.MustCompile(`^\.python/lib/python3\.\d+/`)
	for _, e := range src.Entries {
		if !e.IsRegular() {
			continue
		}
		var do bool
		switch {
		case e.Rel == "pyvenv.cfg":
			do = true
		case strings.HasPrefix(e.Rel, "bin/"):
			do = true // shebang scripts; binaries skipped by rewriteFile
		case pyLibDir.MatchString(e.Rel) && strings.HasSuffix(e.Rel, ".pth"):
			do = true
		case pythonLib.MatchString(e.Rel) &&
			(strings.Contains(filepath.Base(e.Rel), "_sysconfigdata_") ||
				(strings.Contains(e.Rel, "/config-") && filepath.Base(e.Rel) == "Makefile")):
			do = true
		}
		if !do {
			continue
		}
		if _, err := rewriteFile(e.Abs, rw); err != nil {
			return fmt.Errorf("venv: rewriting %s: %w", e.Rel, err)
		}
	}
	return nil
}

func (venvType) Layout(src *types.Source) []layer.Rule {
	return []layer.Rule{{Prefix: ".python", Separate: true}}
}

func lockKind(name string) (kind, mediaType string) {
	switch filepath.Base(name) {
	case "uv.lock":
		return "uv", layer.MediaTypeUVLock
	case "poetry.lock":
		return "poetry", layer.MediaTypePoetryLock
	default:
		return "pip", layer.MediaTypePipReqs
	}
}

// scanELFs returns the max glibc requirement and musl flag over all regular
// files in the venv.
func scanELFs(src *types.Source) (glibc [2]int, musl bool, arch string, mixedArch bool) {
	for _, e := range src.Entries {
		if !e.IsRegular() {
			continue
		}
		info, ok, err := elfscan.ScanFile(e.Abs)
		if err != nil || !ok {
			continue
		}
		if info.Musl {
			musl = true
		}
		if info.GlibcMin[0] > glibc[0] || (info.GlibcMin[0] == glibc[0] && info.GlibcMin[1] > glibc[1]) {
			glibc = info.GlibcMin
		}
		if info.Arch != "" && info.Arch != "unknown" {
			if arch == "" {
				arch = info.Arch
			} else if arch != info.Arch {
				mixedArch = true
			}
		}
	}
	return
}

func (venvType) Annotate(src *types.Source) map[string]string {
	a := map[string]string{}
	cfg, _ := src.State["pyvenv.cfg"].(map[string]string)
	mode, _ := src.State["python.mode"].(string)
	a[key("python.mode")] = mode
	if v := cfg["version_info"]; v != "" {
		a[key("python")] = v
	} else if v := cfg["version"]; v != "" {
		a[key("python")] = v
	}
	if h := cfg["home"]; h != "" {
		a[key("python.home")] = h
	}
	if lock := src.Opts.Get("lock", ""); lock != "" {
		if data, err := os.ReadFile(lock); err == nil {
			kind, _ := lockKind(lock)
			sum := sha256.Sum256(data)
			a[key("lock.kind")] = kind
			a[key("lock.digest")] = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	glibc, musl, arch, mixed := scanELFs(src)
	if musl {
		a[key("libc")] = "musl"
	} else {
		a[key("libc")] = "glibc"
		if glibc != [2]int{} {
			a[key("libc")] = fmt.Sprintf("glibc %d.%d", glibc[0], glibc[1])
			a[layer.KeyLayerRequires] = fmt.Sprintf("glibc>=%d.%d", glibc[0], glibc[1])
		}
	}
	if arch != "" && !mixed {
		a[key("arch")] = arch
	}
	return a
}

func (venvType) Metadata(src *types.Source) []types.MetadataFile {
	var out []types.MetadataFile
	if lock := src.Opts.Get("lock", ""); lock != "" {
		if data, err := os.ReadFile(lock); err == nil {
			_, mt := lockKind(lock)
			out = append(out, types.MetadataFile{
				Name:      filepath.Base(lock),
				MediaType: mt,
				Data:      data,
			})
		}
	}
	if data, err := freeze(src); err == nil && len(data) > 0 {
		out = append(out, types.MetadataFile{
			Name:      "requirements.freeze.txt",
			MediaType: layer.MediaTypePipFreeze,
			Data:      data,
		})
	}
	return out
}

// freeze generates a pip-freeze equivalent by reading
// site-packages/*.dist-info/METADATA.
func freeze(src *types.Source) ([]byte, error) {
	var pkgs []string
	for _, e := range src.Entries {
		if !pyLibDir.MatchString(e.Rel + "/") {
			continue
		}
		if !strings.HasSuffix(e.Rel, ".dist-info/METADATA") || !e.IsRegular() {
			continue
		}
		name, version := parseMetadata(e.Abs)
		if name != "" {
			pkgs = append(pkgs, name+"=="+version)
		}
	}
	sort.Strings(pkgs)
	if len(pkgs) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(pkgs, "\n") + "\n"), nil
}

func parseMetadata(path string) (name, version string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(k) {
			case "Name":
				name = strings.TrimSpace(v)
			case "Version":
				version = strings.TrimSpace(v)
			}
		}
	}
	return name, version
}

func (venvType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
	var out []types.Finding
	mode := s.Annotations[key("python.mode")]
	if mode == "system" {
		out = append(out, types.Finding{
			Level: types.Warn,
			Msg:   "venv uses system python; cannot verify python.home exists in base",
		})
	}
	return out
}

// BuildFromLock creates a venv from a lock file using the local `uv` binary.
// Returns the generated directory, the rewrite to pass as RewritePrefix, and
// a cleanup func. pythonVer is e.g. "3.12"; root is e.g. "/opt/venv".
func BuildFromLock(ctx context.Context, lockPath, pythonVer, root string) (dir string, rewrite types.Rewrite, cleanup func(), err error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return "", rewrite, nil, fmt.Errorf("venv: --from-lock requires `uv` on PATH")
	}
	root = filepath.Clean(root)
	tmp, err := os.MkdirTemp("", "swcake-venv-*")
	if err != nil {
		return "", rewrite, nil, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	venvDir := filepath.Join(tmp, root) // <tmp><root>
	staging := filepath.Join(tmp, "py-staging")
	env := func(extra ...string) []string {
		return append(os.Environ(), extra...)
	}
	run := func(name string, envv []string, args ...string) error {
		cmd := exec.CommandContext(ctx, uv, args...)
		cmd.Env = envv
		cmd.Dir = filepath.Dir(lockPath)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	if err := run("uv-python-install", env("UV_PYTHON_INSTALL_DIR="+staging), "python", "install", pythonVer); err != nil {
		cleanup()
		return "", rewrite, nil, fmt.Errorf("venv: uv python install: %w", err)
	}
	ents, err := os.ReadDir(staging)
	if err != nil {
		cleanup()
		return "", rewrite, nil, err
	}
	var pyDir string
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "cpython-") {
			pyDir = filepath.Join(staging, e.Name())
			break
		}
	}
	if pyDir == "" {
		cleanup()
		return "", rewrite, nil, fmt.Errorf("venv: no cpython-* dir found in %s", staging)
	}
	if err := os.MkdirAll(filepath.Dir(venvDir), 0o755); err != nil {
		cleanup()
		return "", rewrite, nil, err
	}
	if err := os.MkdirAll(venvDir, 0o755); err != nil {
		cleanup()
		return "", rewrite, nil, err
	}
	if err := os.Rename(pyDir, filepath.Join(venvDir, ".python")); err != nil {
		cleanup()
		return "", rewrite, nil, fmt.Errorf("venv: %w", err)
	}
	if err := run("uv-venv", env(), "venv", "--allow-existing", "--python", filepath.Join(venvDir, ".python/bin/python3"), venvDir); err != nil {
		cleanup()
		return "", rewrite, nil, fmt.Errorf("venv: uv venv: %w", err)
	}
	if err := run("uv-sync", env("UV_PROJECT_ENVIRONMENT="+venvDir), "sync", "--locked", "--no-install-project"); err != nil {
		cleanup()
		return "", rewrite, nil, fmt.Errorf("venv: uv sync: %w", err)
	}
	return venvDir, types.Rewrite{From: filepath.Join(tmp, root), To: root}, cleanup, nil
}

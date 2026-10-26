package slice

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/laiaqwq/sandwichcake/internal/cache"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

// RemoteFile is one file of a remote source.
type RemoteFile struct {
	Path            string // slash path relative to the slice root
	Size            int64
	SHA256, GitSHA1 string // expected digests ("" = unknown)
	Open            func(ctx context.Context, off int64) (io.ReadCloser, error)
}

// Remote makes Pack stream files from a remote source straight into
// registry blobs instead of reading PackOptions.Dir.
type Remote struct {
	Name        string // replaces the dir base name in layer names and history
	Files       []RemoteFile
	KeepDir     string // also write the files here (verified, then renamed)
	Concurrency int    // layers streamed in parallel (default 2)
	// Files without an expected sha256 up to this size (and root
	// config.json / model_index.json) are downloaded first so files.json and
	// metadata are complete (default 64 MiB).
	Prefetch int64
	Log      func(string) // optional progress messages
}

func (r *Remote) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(format, args...))
	}
}

var metadataFiles = map[string]bool{"config.json": true, "model_index.json": true}

// remoteEntries turns remote files into layer entries (with synthesized
// parent dirs) and prefetches small unverified files into tmp.
func remoteEntries(ctx context.Context, r *Remote, tmp string) ([]layer.Entry, map[string]expected, error) {
	prefetch := r.Prefetch
	if prefetch <= 0 {
		prefetch = 64 << 20
	}
	expect := map[string]expected{}
	dirs := map[string]bool{}
	var entries []layer.Entry
	for _, f := range r.Files {
		if _, dup := expect[f.Path]; dup {
			return nil, nil, fmt.Errorf("slice: duplicate remote file %s", f.Path)
		}
		expect[f.Path] = expected{SHA256: f.SHA256, GitSHA1: f.GitSHA1}
		e := layer.Entry{Rel: f.Path, Mode: 0o644, Size: f.Size, SHA256: f.SHA256}
		if (f.SHA256 == "" && f.Size <= prefetch) || (metadataFiles[f.Path] && f.Size <= prefetch) {
			p, sum, err := prefetchFile(ctx, f, filepath.Join(tmp, "prefetch"))
			if err != nil {
				return nil, nil, err
			}
			e.Abs, e.SHA256 = p, sum
			expect[f.Path] = expected{SHA256: sum, GitSHA1: f.GitSHA1}
		} else {
			f := f
			e.Open = func(off int64) (io.ReadCloser, error) { return f.Open(ctx, off) }
		}
		for _, d := range layer.ParentDirs(f.Path) {
			dirs[d] = true
		}
		entries = append(entries, e)
	}
	for d := range dirs {
		if _, clash := expect[d]; clash {
			return nil, nil, fmt.Errorf("slice: remote path %s is both a file and a directory", d)
		}
	}
	return withParentDirs(entries), expect, nil
}

// withParentDirs adds dir entries for every ancestor of the files (like
// Scan reports them) and sorts by Rel.
func withParentDirs(files []layer.Entry) []layer.Entry {
	seen := map[string]bool{}
	for _, e := range files {
		seen[e.Rel] = true
	}
	out := append([]layer.Entry{}, files...)
	for _, e := range files {
		for _, d := range layer.ParentDirs(e.Rel) {
			if !seen[d] {
				seen[d] = true
				out = append(out, layer.Entry{Rel: d, Mode: fs.ModeDir | 0o755})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

func prefetchFile(ctx context.Context, f RemoteFile, dir string) (string, string, error) {
	p := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", "", err
	}
	rc, err := f.Open(ctx, 0)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()
	out, err := os.Create(p)
	if err != nil {
		return "", "", err
	}
	sh, gh := sha256.New(), sha1.New()
	fmt.Fprintf(gh, "blob %d\x00", f.Size)
	n, err := io.Copy(io.MultiWriter(out, sh, gh), rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", "", fmt.Errorf("slice: downloading %s: %w", f.Path, err)
	}
	if n != f.Size {
		return "", "", fmt.Errorf("slice: %s: got %d bytes, listing said %d", f.Path, n, f.Size)
	}
	sum := hex.EncodeToString(sh.Sum(nil))
	if f.SHA256 != "" && f.SHA256 != sum {
		return "", "", fmt.Errorf("slice: %s: sha256 %s, upstream says %s", f.Path, sum, f.SHA256)
	}
	if git := hex.EncodeToString(gh.Sum(nil)); f.GitSHA1 != "" && f.GitSHA1 != git {
		return "", "", fmt.Errorf("slice: %s: git blob %s, upstream says %s", f.Path, git, f.GitSHA1)
	}
	return p, sum, nil
}

// cachedLayer is the KindLayer cache value: a layer plan already uploaded.
type cachedLayer struct {
	Digest string            `json:"digest"`
	DiffID string            `json:"diffID"`
	Size   int64             `json:"size"`
	Hashes map[string]string `json:"hashes"`
}

// planKey identifies a layer by everything that determines its bytes. It
// is "" when some file has no expected digest (content not known upfront).
func planKey(g layer.Group, topts layer.TarOptions, comp layer.Compression, level int, expect map[string]expected) string {
	type file struct {
		Rel             string
		Mode            fs.FileMode
		Size            int64
		SHA256, GitSHA1 string
	}
	var files []file
	for _, e := range g.Entries {
		f := file{Rel: e.Rel, Mode: e.Mode, Size: e.Size}
		if e.IsRegular() {
			x := expect[e.Rel]
			if x.SHA256 == "" && x.GitSHA1 == "" {
				return ""
			}
			f.SHA256, f.GitSHA1 = x.SHA256, x.GitSHA1
		}
		files = append(files, f)
	}
	data, _ := json.Marshal(struct {
		V     int
		Files []file
		Tar   layer.TarOptions
		Comp  layer.Compression
		Level int
	}{1, files, topts, comp, level})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// streamLayers streams every group into repo and returns the uploaded
// layers plus the sha256 of every regular file.
func streamLayers(ctx context.Context, client *registry.Client, repo name.Repository, groups []layer.Group, topts layer.TarOptions, comp layer.Compression, level int, r *Remote, expect map[string]expected) ([]v1.Layer, map[string]string, error) {
	n := r.Concurrency
	if n <= 0 {
		n = 2
	}
	c := client.Cache()
	out := make([]v1.Layer, len(groups))
	hashes := map[string]string{}
	var mu sync.Mutex
	errs := make([]error, len(groups))
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g layer.Group) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			l, h, err := streamLayer(ctx, client, c, repo, i, len(groups), g, topts, comp, level, r, expect)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			out[i] = l
			mu.Lock()
			for k, v := range h {
				hashes[k] = v
			}
			mu.Unlock()
		}(i, g)
	}
	wg.Wait()
	// Report the root cause, not the cancellations it triggered.
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, nil, err
		}
	}
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	return out, hashes, nil
}

func streamLayer(ctx context.Context, client *registry.Client, c *cache.Cache, repo name.Repository, i, total int, g layer.Group, topts layer.TarOptions, comp layer.Compression, level int, r *Remote, expect map[string]expected) (v1.Layer, map[string]string, error) {
	mt := string(comp.MediaType())
	key := planKey(g, topts, comp, level, expect)
	if key != "" && c != nil && r.KeepDir == "" {
		var cl cachedLayer
		if c.Get(cache.KindLayer, key, &cl) {
			d, err1 := v1.NewHash(cl.Digest)
			diff, err2 := v1.NewHash(cl.DiffID)
			if err1 == nil && err2 == nil {
				if ok, err := client.BlobExists(ctx, repo, d); err == nil && ok {
					r.log("layer %d/%d: %s already in %s", i+1, total, d, repo)
					return &streamedLayer{digest: d, diffID: diff, size: cl.Size, mt: mt}, cl.Hashes, nil
				}
			}
		}
	}
	l, err := layer.TarLayout(g, topts)
	if err != nil {
		return nil, nil, err
	}
	name := g.Name
	if name == "" && len(g.Entries) == 1 {
		name = path.Base(g.Entries[0].Rel)
	}
	r.log("layer %d/%d: streaming %s (%d bytes uncompressed)", i+1, total, name, l.Size)
	ts := newTarStream(l, g.Entries, expect, r.KeepDir)
	defer ts.Close()
	var bs registry.BlobStream = ts
	cs := &compStream{tar: ts, comp: comp, level: level}
	if comp != layer.CompressionNone {
		bs = cs
		defer cs.stop()
	}
	sessionKey := ""
	if key != "" {
		sessionKey = repo.String() + "-" + key
	}
	digest, size, err := client.StreamBlob(ctx, repo, sessionKey, bs)
	if err != nil {
		return nil, nil, fmt.Errorf("slice: layer %d/%d: %w", i+1, total, err)
	}
	diffID := digest
	if comp != layer.CompressionNone {
		diffID = cs.diffID
	}
	if key != "" && c != nil {
		c.Set(cache.KindLayer, key, cachedLayer{Digest: digest.String(), DiffID: diffID.String(), Size: size, Hashes: ts.hashes})
	}
	r.log("layer %d/%d: pushed %s", i+1, total, digest)
	return &streamedLayer{digest: digest, diffID: diffID, size: size, mt: mt}, ts.hashes, nil
}

// planLayers reports the uncompressed layer sizes for a dry run.
func planLayers(groups []layer.Group, topts layer.TarOptions) ([]int64, error) {
	var sizes []int64
	for _, g := range groups {
		l, err := layer.TarLayout(g, topts)
		if err != nil {
			return nil, err
		}
		sizes = append(sizes, l.Size)
	}
	return sizes, nil
}

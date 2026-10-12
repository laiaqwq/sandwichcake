// Package resolve turns a cake.Ref into a concrete layer list (DESIGN 2.1,
// 4.1). All network access goes through pkg/registry.
package resolve

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/laiaqwq/sandwichcake/internal/cache"
	"github.com/laiaqwq/sandwichcake/pkg/cake"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

// Role distinguishes base from slice refs.
type Role int

const (
	Base  Role = iota // must be a plain image; all layers taken in order
	Slice             // slice (kind=slice) or plain image with layers selection
)

// Options controls Resolve.
type Options struct {
	Verify bool         // download layers and scan tar headers
	Cache  *cache.Cache // caches tar listings by diff_id (optional)
}

// Layer is one resolved layer.
type Layer struct {
	L          v1.Layer      // the remote layer object (kept *remote.MountableLayer)
	Desc       v1.Descriptor // manifest descriptor incl. annotations, original mediaType
	DiffID     v1.Hash
	History    *v1.History // aligned history entry, if any
	Index      int         // index within the source manifest
	External   bool        // from a plain image (2.1 external layer)
	PureFile   bool        // judged a pure file layer
	PureReason string
}

// Resolved is the result of resolving one Ref.
type Resolved struct {
	Input          cake.Ref
	Repo           string  // repository string
	RefDigest      v1.Hash // what ref points to (index or manifest)
	ManifestDigest v1.Hash // selected platform manifest
	Kind           string  // "base" | "slice" | "image"
	Any            bool    // platform-independent
	Config         *v1.ConfigFile
	Layers         []Layer
	Libc           string              // detected/declared base libc, "" unknown
	TarPaths       map[string][]string // diffID -> sorted paths (Verify only)
	// ManifestAnnotations are the selected manifest's annotations.
	ManifestAnnotations map[string]string
}

// SubjectRef returns repo@manifestdigest for annotations/locking.
func (r *Resolved) SubjectRef() string {
	return r.Repo + "@" + r.ManifestDigest.String()
}

var copyRe = regexp.MustCompile(`^\s*(COPY|ADD)\s`)
var nopCopyRe = regexp.MustCompile(`\(nop\)\s+(COPY|ADD)\s`)

// pureFile judges created_by per DESIGN 2.1.
func pureFile(createdBy string) (bool, string) {
	if copyRe.MatchString(createdBy) {
		return true, "COPY/ADD history"
	}
	if nopCopyRe.MatchString(createdBy) {
		return true, "legacy #(nop) COPY/ADD"
	}
	return false, ""
}

// Resolve resolves ref into a Resolved for the given platform.
func Resolve(ctx context.Context, client *registry.Client, ref cake.Ref, role Role, platform v1.Platform, opts Options) (*Resolved, error) {
	nref, err := client.ParseRef(ref.Ref)
	if err != nil {
		return nil, err
	}
	desc, err := client.Get(ctx, ref.Ref)
	if err != nil {
		return nil, err
	}
	r := &Resolved{
		Input:     ref,
		Repo:      nref.Context().String(),
		RefDigest: desc.Descriptor.Digest,
		TarPaths:  map[string][]string{},
	}

	var manifest *v1.Manifest
	var manifestDesc *v1.Descriptor
	repo := nref.Context()

	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, err
		}
		d, err := selectPlatform(im, platform, ref.Platform)
		if err != nil {
			return nil, err
		}
		manifestDesc = d
		mdesc, err := client.Get(ctx, repo.Digest(d.Digest.String()).String())
		if err != nil {
			return nil, err
		}
		manifest, err = v1.ParseManifest(bytes.NewReader(mdesc.Manifest))
		if err != nil {
			return nil, err
		}
	} else {
		manifestDesc = &desc.Descriptor
		manifest, err = v1.ParseManifest(bytes.NewReader(desc.Manifest))
		if err != nil {
			return nil, err
		}
	}
	r.ManifestDigest = manifestDesc.Digest
	r.ManifestAnnotations = manifest.Annotations

	img, err := client.Image(ctx, repo.Digest(r.ManifestDigest.String()).String(), nil)
	if err != nil {
		return nil, err
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	r.Config = cf

	// Kind.
	r.Kind = "image"
	if role == Base {
		r.Kind = "base"
	} else if manifest.Annotations[layer.KeyKind] == layer.KindSlice {
		r.Kind = "slice"
	}

	// Any.
	r.Any = manifest.Annotations[layer.KeyPlatform] == "any" ||
		(cf.OS == "unknown" && cf.Architecture == "unknown") ||
		ref.Platform == "any"
	if !r.Any && platform.OS != "" && platform.Architecture != "" {
		if cf.OS != platform.OS || cf.Architecture != platform.Architecture {
			return nil, fmt.Errorf("resolve: %s platform %s/%s does not match %s/%s",
				ref.Ref, cf.OS, cf.Architecture, platform.OS, platform.Architecture)
		}
	}

	vlayers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	// Non-empty history entries align with layers.
	var hist []*v1.History
	for i := range cf.History {
		if !cf.History[i].EmptyLayer {
			hist = append(hist, &cf.History[i])
		}
	}
	historyFor := func(idx int) *v1.History {
		if idx < len(hist) {
			return hist[idx]
		}
		return nil
	}

	// Layer selection.
	var sel []int
	switch {
	case role == Base || len(ref.Layers) == 0:
		if role != Base && r.Kind != "slice" && len(ref.Layers) == 0 {
			return nil, fmt.Errorf("resolve: %s is not a slice; select layers explicitly", ref.Ref)
		}
		for i := range vlayers {
			sel = append(sel, i)
		}
	default:
		for _, ls := range ref.Layers {
			if ls.Index != nil {
				if *ls.Index < 0 || *ls.Index >= len(vlayers) {
					return nil, fmt.Errorf("resolve: layer index %d out of range for %s", *ls.Index, ref.Ref)
				}
				sel = append(sel, *ls.Index)
				continue
			}
			found := -1
			for i, l := range vlayers {
				d, err := l.Digest()
				if err == nil && d.String() == ls.Digest {
					found = i
					break
				}
			}
			if found < 0 {
				return nil, fmt.Errorf("resolve: layer %s not found in %s", ls.Digest, ref.Ref)
			}
			sel = append(sel, found)
		}
	}

	for _, i := range sel {
		l := vlayers[i]
		var ldesc v1.Descriptor
		if i < len(manifest.Layers) {
			ldesc = manifest.Layers[i]
		} else {
			d, _ := l.Digest()
			mt, _ := l.MediaType()
			sz, _ := l.Size()
			ldesc = v1.Descriptor{MediaType: mt, Digest: d, Size: sz}
		}
		diffID, err := l.DiffID()
		if err != nil {
			return nil, err
		}
		rl := Layer{L: l, Desc: ldesc, DiffID: diffID, History: historyFor(i), Index: i}
		rl.External = r.Kind == "image"

		// Base checks.
		if role == Base {
			if ldesc.MediaType == gtypes.DockerForeignLayer {
				return nil, fmt.Errorf("resolve: base %s uses foreign layers (windows-only), unsupported", ref.Ref)
			}
			if cf.OS == "windows" {
				return nil, fmt.Errorf("resolve: windows base images unsupported")
			}
		}

		// Pure-file judgment for external layers.
		if rl.External {
			if ldesc.Annotations[layer.KeyLayerType] != "" {
				rl.PureFile = true
				rl.PureReason = "sandwichcake slice layer"
			} else if rl.History != nil {
				rl.PureFile, rl.PureReason = pureFile(rl.History.CreatedBy)
			}
			if !rl.PureFile && !ref.Force {
				return nil, fmt.Errorf("resolve: layer %d of %s is not a pure file layer (history: %q); use force: true or verify: true",
					i, ref.Ref, histLine(rl.History))
			}
			// source annotations (DESIGN 2.3).
			anns := map[string]string{}
			for k, v := range ldesc.Annotations {
				anns[k] = v
			}
			anns[layer.KeySourceImage] = r.SubjectRef()
			anns[layer.KeySourceIndex] = fmt.Sprint(i)
			rl.Desc = ldesc
			rl.Desc.Annotations = anns
		}

		r.Layers = append(r.Layers, rl)

		// Verify: scan tar headers.
		if opts.Verify || ref.Verify {
			paths, err := scanLayer(r, l, diffID, opts.Cache)
			if err != nil {
				return nil, err
			}
			r.TarPaths[diffID.Hex] = paths
		}
	}

	// Libc (base).
	if role == Base {
		r.Libc = detectLibc(ref, manifest, cf, r, opts)
	}
	return r, nil
}

func histLine(h *v1.History) string {
	if h == nil {
		return "<none>"
	}
	return h.CreatedBy
}

// selectPlatform picks a manifest descriptor from an index.
func selectPlatform(im *v1.IndexManifest, platform v1.Platform, override string) (*v1.Descriptor, error) {
	wantOS, wantArch := platform.OS, platform.Architecture
	if override != "" && override != "any" {
		parts := strings.SplitN(override, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("resolve: bad platform %q", override)
		}
		wantOS, wantArch = parts[0], parts[1]
	}
	if override == "any" {
		for _, m := range im.Manifests {
			if m.Platform != nil && m.Platform.OS == "unknown" && m.Platform.Architecture == "unknown" {
				m := m
				return &m, nil
			}
		}
		return nil, fmt.Errorf("resolve: no platform-independent manifest in index")
	}
	for _, m := range im.Manifests {
		if m.Platform != nil && m.Platform.OS == wantOS && m.Platform.Architecture == wantArch {
			m := m
			return &m, nil
		}
	}
	return nil, fmt.Errorf("resolve: no manifest for %s/%s in index", wantOS, wantArch)
}

// scanLayer downloads a layer, verifies no whiteouts, returns sorted paths.
func scanLayer(r *Resolved, l v1.Layer, diffID v1.Hash, c *cache.Cache) ([]string, error) {
	if c != nil {
		var paths []string
		if c.Get(cache.KindTarList, diffID.String(), &paths) {
			return paths, nil
		}
	}
	rc, err := l.Uncompressed()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var paths []string
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("resolve: reading layer %s: %w", diffID, err)
		}
		name := h.Name
		if base := name[strings.LastIndex(name, "/")+1:]; strings.HasPrefix(base, ".wh.") {
			return nil, fmt.Errorf("resolve: layer %s contains whiteout %s", diffID, name)
		}
		paths = append(paths, name)
	}
	sortStrings(paths)
	if c != nil {
		c.Set(cache.KindTarList, diffID.String(), paths)
	}
	return paths, nil
}

var muslLd = regexp.MustCompile(`(^|/)ld-musl-`)
var libcVerSo = regexp.MustCompile(`(^|/)libc-(\d+\.\d+)\.so$`)
var libcSo6 = regexp.MustCompile(`^(usr/)?lib(64)?/([^/]+-linux-gnu/)?libc\.so\.6$`)
var glibcVerStr = regexp.MustCompile(`GNU C Library[^\n]*version (\d+\.\d+)`)

// detectLibc resolves the base libc: ref.Libc > annotations/labels > verify scan.
func detectLibc(ref cake.Ref, manifest *v1.Manifest, cf *v1.ConfigFile, r *Resolved, opts Options) string {
	if ref.Libc != "" {
		return ref.Libc
	}
	libcKey := layer.Prefix + "libc"
	if v := manifest.Annotations[libcKey]; v != "" {
		return v
	}
	if cf != nil && cf.Config.Labels != nil {
		if v := cf.Config.Labels[libcKey]; v != "" {
			return v
		}
	}
	if !opts.Verify && !ref.Verify {
		return ""
	}
	// Scan tar headers (later layers win).
	result := ""
	for _, l := range r.Layers {
		paths := r.TarPaths[l.DiffID.Hex]
		// Cheap name checks first.
		for _, p := range paths {
			if muslLd.MatchString(p) {
				result = "musl"
			}
			if m := libcVerSo.FindStringSubmatch(p); m != nil {
				result = "glibc " + m[2]
			}
		}
	}
	if result != "" {
		return result
	}
	// Read libc.so.6 content.
	for i := len(r.Layers) - 1; i >= 0; i-- {
		l := r.Layers[i]
		var found bool
		for _, p := range r.TarPaths[l.DiffID.Hex] {
			if libcSo6.MatchString(p) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		rc, err := l.L.Uncompressed()
		if err != nil {
			continue
		}
		tr := tar.NewReader(rc)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			if libcSo6.MatchString(h.Name) && h.Typeflag == tar.TypeReg {
				data, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
				if m := glibcVerStr.FindSubmatch(data); m != nil {
					rc.Close()
					return "glibc " + string(m[1])
				}
			}
		}
		rc.Close()
	}
	return ""
}

func sortStrings(s []string) { sort.Strings(s) }

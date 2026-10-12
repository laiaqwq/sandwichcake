// Package compose merges a resolved base + slices into OCI images.
// It is pure: no network, no file I/O.
package compose

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/baba/sandwichcake/pkg/cake"
	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/resolve"
	stypes "github.com/baba/sandwichcake/pkg/types"
)

// Input is everything Compose needs.
type Input struct {
	Ctx         context.Context
	Platform    v1.Platform
	Base        *resolve.Resolved
	Slices      []*resolve.Resolved
	Patch       cake.ConfigPatch
	Annotations map[string]string
	Created     time.Time
	Compat      string // "docker20" or ""
}

// Result is the composed image plus non-fatal findings.
type Result struct {
	Image    v1.Image
	Findings []stypes.Finding
}

// PlatformImage pairs a platform with its composed image.
type PlatformImage struct {
	Platform v1.Platform
	Image    v1.Image
}

// normalizeMediaType rewrites Docker schema2 layer media types to OCI.
func normalizeMediaType(mt gtypes.MediaType) (gtypes.MediaType, error) {
	switch mt {
	case gtypes.DockerLayer:
		return gtypes.OCILayer, nil
	case gtypes.DockerUncompressedLayer:
		return gtypes.OCIUncompressedLayer, nil
	case gtypes.DockerForeignLayer:
		return "", fmt.Errorf("compose: foreign layers unsupported")
	default:
		return mt, nil
	}
}

func isPrefixedPath(path, root string) bool {
	root = strings.Trim(root, "/")
	path = strings.Trim(path, "/")
	return path == root || strings.HasPrefix(path, root+"/")
}

// Compose builds the image for one platform.
func Compose(in Input) (*Result, error) {
	var findings []stypes.Finding
	errf := func(format string, a ...any) {
		findings = append(findings, stypes.Finding{Level: stypes.Error, Msg: fmt.Sprintf(format, a...)})
	}
	warnf := func(format string, a ...any) {
		findings = append(findings, stypes.Finding{Level: stypes.Warn, Msg: fmt.Sprintf(format, a...)})
	}

	if in.Created.IsZero() {
		in.Created = time.Unix(0, 0).UTC()
	}

	// --- checks ---
	type rootRec struct {
		root    string
		slice   int
		overlay bool
	}
	var roots []rootRec
	requiresSet := map[string]bool{}

	ctx := in.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	baseRef := ""
	if in.Base != nil {
		baseRef = in.Base.SubjectRef()
	}
	cctx := &stypes.ComposeCtx{
		Platform: in.Platform,
		BaseRef:  baseRef,
		BaseLibc: "",
	}
	if in.Base != nil {
		cctx.BaseLibc = in.Base.Libc
		cctx.BaseEnv = in.Base.Config.Config.Env
		if in.Base.RefDigest.Hex != "" {
			cctx.BaseRefDigest = in.Base.Repo + "@" + in.Base.RefDigest.String()
		}
	}

	for si, s := range in.Slices {
		// per-slice type Check
		typeName := ""
		for _, l := range s.Layers {
			if tn := l.Desc.Annotations[layer.KeyLayerType]; tn != "" {
				typeName = tn
				break
			}
		}
		if typeName != "" {
			t := stypes.Get(typeName)
			if t == nil {
				warnf("unknown slice type %q; skipping type-specific checks", typeName)
			} else {
				sl := &stypes.Slice{Ref: s.SubjectRef(), Annotations: map[string]string{}}
				for k, v := range s.ManifestAnnotations {
					sl.Annotations[k] = v
				}
				mergedFirst := false
				for _, l := range s.Layers {
					sl.Layers = append(sl.Layers, stypes.LayerInfo{
						Digest: l.Desc.Digest, DiffID: l.DiffID, Size: l.Desc.Size,
						MediaType: string(l.Desc.MediaType), Annotations: l.Desc.Annotations,
					})
					if !mergedFirst && l.Desc.Annotations != nil {
						for k, v := range l.Desc.Annotations {
							sl.Annotations[k] = v
						}
						mergedFirst = true
					}
				}
				findings = append(findings, t.Check(ctx, sl, cctx)...)
			}
		}
		for _, l := range s.Layers {
			anns := l.Desc.Annotations
			root := anns[layer.KeyLayerRoot]
			overlay := anns[layer.KeyLayerOverlay] == "true"
			if root != "" && !overlay {
				roots = append(roots, rootRec{root: strings.Trim(root, "/"), slice: si})
			}
			if rq := anns[layer.KeyLayerRequires]; rq != "" {
				requiresSet[rq] = true
			}
		}
	}
	// root conflicts (different slices only)
	for i := 0; i < len(roots); i++ {
		for j := i + 1; j < len(roots); j++ {
			if roots[i].slice == roots[j].slice && roots[i].root == roots[j].root {
				continue
			}
			if isPrefixedPath(roots[i].root, roots[j].root) || isPrefixedPath(roots[j].root, roots[i].root) {
				errf("layer roots %q (slice %d) and %q (slice %d) conflict",
					roots[i].root, roots[i].slice, roots[j].root, roots[j].slice)
			}
		}
	}
	// TarPaths overlap (verified externals vs file roots and each other).
	var extPaths []struct {
		slice int
		paths []string
	}
	for si, s := range in.Slices {
		for _, l := range s.Layers {
			if !l.External || l.Desc.Annotations[layer.KeyLayerOverlay] == "true" {
				continue
			}
			if paths := s.TarPaths[l.DiffID.Hex]; paths != nil {
				extPaths = append(extPaths, struct {
					slice int
					paths []string
				}{si, paths})
			}
		}
	}
	for i, e := range extPaths {
		for _, p := range e.paths {
			for _, r := range roots {
				if r.slice == e.slice {
					continue
				}
				if isPrefixedPath(p, r.root) {
					errf("verified layer path %q conflicts with root %q", p, r.root)
				}
			}
			for j := i + 1; j < len(extPaths); j++ {
				for _, q := range extPaths[j].paths {
					if p == q {
						errf("verified layer path %q present in two external layers", p)
					}
				}
			}
		}
	}
	// requires
	for rq := range requiresSet {
		findings = append(findings, stypes.CheckRequires(rq, *cctx)...)
	}

	// --- layers ---
	var items []layerItem
	var baseNonEmpty int
	if in.Base != nil {
		for _, h := range in.Base.Config.History {
			if !h.EmptyLayer {
				baseNonEmpty++
			}
		}
		if baseNonEmpty != len(in.Base.Layers) {
			warnf("base history (%d entries) misaligned with %d layers", baseNonEmpty, len(in.Base.Layers))
		}
		for _, l := range in.Base.Layers {
			cb := ""
			if l.History != nil {
				cb = l.History.CreatedBy
			}
			items = append(items, layerItem{l, cb, true})
		}
	}
	for _, s := range in.Slices {
		for _, l := range s.Layers {
			cb := ""
			if l.History != nil {
				cb = l.History.CreatedBy
			} else {
				cb = fmt.Sprintf("sandwichcake compose: %s layer %d", s.SubjectRef(), l.Index)
			}
			items = append(items, layerItem{l, cb, false})
		}
	}
	// dedupe by diff_id (later duplicates dropped)
	seen := map[v1.Hash]bool{}
	var kept []layerItem
	for _, it := range items {
		if seen[it.l.DiffID] {
			continue
		}
		seen[it.l.DiffID] = true
		kept = append(kept, it)
	}
	// compat check
	if in.Compat == "docker20" {
		for _, it := range kept {
			if it.l.Desc.MediaType == gtypes.OCILayerZStd {
				errf("layer %s is zstd, incompatible with docker20", it.l.Desc.Digest)
			}
		}
	}

	// --- build image ---
	img := empty.Image
	img = mutate.MediaType(img, gtypes.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, gtypes.OCIConfigJSON)
	var adds []mutate.Addendum
	for _, it := range kept {
		mt, err := normalizeMediaType(it.l.Desc.MediaType)
		if err != nil {
			errf("%v", err)
			continue
		}
		anns := map[string]string{}
		for k, v := range it.l.Desc.Annotations {
			anns[k] = v
		}
		adds = append(adds, mutate.Addendum{
			Layer:       it.l.L,
			MediaType:   mt,
			Annotations: anns,
			History: v1.History{
				Created:   v1.Time{Time: in.Created},
				CreatedBy: it.created,
			},
		})
	}
	img, err := mutate.Append(img, adds...)
	if err != nil {
		return nil, err
	}

	// --- config ---
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cf = cf.DeepCopy()
	var baseCF *v1.ConfigFile
	if in.Base != nil {
		baseCF = in.Base.Config.DeepCopy()
	} else {
		baseCF = &v1.ConfigFile{}
	}
	cf.Config = *baseCF.Config.DeepCopy()
	cf.OS = in.Platform.OS
	cf.Architecture = in.Platform.Architecture
	cf.Variant = in.Platform.Variant
	cf.Created = v1.Time{Time: in.Created}
	// env
	envMap := map[string]string{}
	for _, kv := range cf.Config.Env {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}
	for _, k := range in.Patch.SortedEnvKeys() {
		ev := in.Patch.Env[k]
		switch {
		case ev.Value != nil:
			envMap[k] = *ev.Value
		case ev.Prepend != nil:
			if old, ok := envMap[k]; ok && old != "" {
				envMap[k] = *ev.Prepend + ":" + old
			} else {
				envMap[k] = *ev.Prepend
			}
		case ev.Append != nil:
			if old, ok := envMap[k]; ok && old != "" {
				envMap[k] = old + ":" + *ev.Append
			} else {
				envMap[k] = *ev.Append
			}
		}
	}
	var env []string
	var keys []string
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+envMap[k])
	}
	cf.Config.Env = env
	if in.Patch.Entrypoint != nil {
		cf.Config.Entrypoint = *in.Patch.Entrypoint
	}
	if in.Patch.Cmd != nil {
		cf.Config.Cmd = *in.Patch.Cmd
	}
	if in.Patch.Workdir != nil {
		cf.Config.WorkingDir = *in.Patch.Workdir
	}
	if in.Patch.User != nil {
		cf.Config.User = *in.Patch.User
	}
	if cf.Config.Labels == nil {
		cf.Config.Labels = map[string]string{}
	}
	for k, v := range in.Patch.Labels {
		cf.Config.Labels[k] = v
	}
	// history: base verbatim (incl. empty_layer), then per kept slice layer.
	cf.History = buildHistory(in, kept)
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		return nil, err
	}

	// --- manifest annotations ---
	anns := map[string]string{}
	for k, v := range in.Annotations {
		anns[k] = v
	}
	var refs []string
	for _, s := range in.Slices {
		refs = append(refs, s.SubjectRef())
	}
	if len(refs) > 0 {
		anns[layer.KeySlices] = strings.Join(refs, ",")
	}
	if in.Base != nil {
		baseName := in.Base.Input.Display
		if baseName == "" {
			baseName = in.Base.Input.Ref
		}
		anns["org.opencontainers.image.base.name"] = baseName
		anns["org.opencontainers.image.base.digest"] = in.Base.ManifestDigest.String()
	}
	img = mutate.Annotations(img, anns).(v1.Image)

	// Fatal findings?
	var errs []string
	var warns []stypes.Finding
	for _, f := range findings {
		if f.Level == stypes.Error {
			errs = append(errs, f.Msg)
		} else {
			warns = append(warns, f)
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("compose: %s", strings.Join(errs, "; "))
	}
	return &Result{Image: img, Findings: warns}, nil
}

// layerItem is a resolved layer plus its history text.
type layerItem struct {
	l       resolve.Layer
	created string
	base    bool
}

// buildHistory replays base history verbatim (empty_layer entries included,
// non-empty entries for kept base layers only) then appends one entry per
// kept slice layer.
func buildHistory(in Input, kept []layerItem) []v1.History {
	var hist []v1.History
	var sliceEntries []v1.History
	baseKept := map[v1.Hash]bool{}
	for _, it := range kept {
		if it.base {
			baseKept[it.l.DiffID] = true
		} else {
			sliceEntries = append(sliceEntries, v1.History{
				Created:   v1.Time{Time: in.Created},
				CreatedBy: it.created,
			})
		}
	}
	if in.Base != nil && baseHistoryAligned(in.Base) {
		li := 0
		for _, h := range in.Base.Config.History {
			if h.EmptyLayer {
				hist = append(hist, h)
				continue
			}
			if li < len(in.Base.Layers) && baseKept[in.Base.Layers[li].DiffID] {
				hist = append(hist, h)
			}
			li++
		}
	} else {
		for _, it := range kept {
			if it.base {
				hist = append(hist, v1.History{
					Created:   v1.Time{Time: in.Created},
					CreatedBy: it.created,
				})
			}
		}
	}
	return append(hist, sliceEntries...)
}

func baseHistoryAligned(b *resolve.Resolved) bool {
	n := 0
	for _, h := range b.Config.History {
		if !h.EmptyLayer {
			n++
		}
	}
	return n == len(b.Layers)
}

// ComposeIndex bundles per-platform images into an OCI index. The order of
// `images` is preserved (caller supplies cake's platform order).
func ComposeIndex(images []PlatformImage) (v1.ImageIndex, error) {
	var idx v1.ImageIndex = empty.Index
	idx = mutate.IndexMediaType(idx, gtypes.OCIImageIndex)
	var adds []mutate.IndexAddendum
	for _, pi := range images {
		p := pi.Platform
		adds = append(adds, mutate.IndexAddendum{
			Add:        pi.Image,
			Descriptor: v1.Descriptor{Platform: &p},
		})
	}
	return mutate.AppendManifests(idx, adds...), nil
}

// Package slice constructs and parses sandwichcake slices (DESIGN 2.2)
// and implements the pack flow.
package slice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types" // ggcr media types

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/registry"
	stypes "github.com/baba/sandwichcake/pkg/types"
	_ "github.com/baba/sandwichcake/pkg/types/builtin"
	"github.com/baba/sandwichcake/pkg/version"
)

// PackOptions controls Pack.
type PackOptions struct {
	Type            string
	Dir             string
	Root            string
	Target          string // repo:tag; or empty to use TargetPrefix + Namer
	TargetPrefix    string
	Platform        string // "linux/amd64" or "any"; "" = auto
	Compression     string // "" = type default
	Level           int    // 0 = default
	NoSplit         bool
	Large, Small    int64  // 0 = type default
	Owner           string // "uid:gid"
	Chmod           string // "fileMode:dirMode" octal, e.g. "644:755"
	NoParents       bool
	RewritePrefix   []stypes.Rewrite
	ArtifactCompat  bool
	Merge           bool // update an existing index at the target tag
	TypeOpts        map[string]string
	SourceDateEpoch int64 // -1 = env/default
	ToolVersion     string
	DryRun          bool // build locally, don't push
}

// PackResult reports what Pack produced.
type PackResult struct {
	Ref         string
	Digest      v1.Hash
	Size        int64
	Slice       stypes.Slice
	MetadataRef string // digest tag of the metadata artifact, if any
	DryRun      bool
}

// Epoch resolves the effective SOURCE_DATE_EPOCH.
func Epoch(opt int64) time.Time {
	v := opt
	if v < 0 {
		if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
			v, _ = strconv.ParseInt(s, 10, 64)
		} else {
			v = 0
		}
	}
	return time.Unix(v, 0).UTC()
}

func parseOwner(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	u, g, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("slice: --owner must be uid:gid")
	}
	uid, err := strconv.Atoi(u)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(g)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func parseChmod(s string) (*layer.ChmodSpec, error) {
	if s == "" {
		return nil, nil
	}
	f, d, ok := strings.Cut(s, ":")
	if !ok {
		return nil, fmt.Errorf("slice: --chmod must be fileMode:dirMode like 644:755")
	}
	fm, err := strconv.ParseUint(f, 8, 32)
	if err != nil {
		return nil, err
	}
	dm, err := strconv.ParseUint(d, 8, 32)
	if err != nil {
		return nil, err
	}
	return &layer.ChmodSpec{FileMode: os.FileMode(fm), DirMode: os.FileMode(dm)}, nil
}

// Pack builds a slice from opts.Dir and pushes it.
func Pack(ctx context.Context, client *registry.Client, opts PackOptions) (*PackResult, error) {
	if opts.ToolVersion == "" {
		opts.ToolVersion = version.Version
	}
	t := stypes.Get(opts.Type)
	if t == nil {
		return nil, fmt.Errorf("slice: unknown type %q", opts.Type)
	}
	def := t.Defaults()
	comp := def.Compression
	if opts.Compression != "" {
		c, err := layer.ParseCompression(opts.Compression)
		if err != nil {
			return nil, err
		}
		comp = c
	}
	level := opts.Level
	if level == 0 {
		level = def.Level
	}
	large, small := def.Large, def.Small
	if opts.Large > 0 {
		large = opts.Large
	}
	if opts.Small > 0 {
		small = opts.Small
	}
	split := def.Split
	if opts.NoSplit {
		split = false
	}

	// Source.
	entries, err := layer.Scan(opts.Dir)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "swcake-pack-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	src := &stypes.Source{
		Dir:     opts.Dir,
		OrigDir: opts.Dir,
		Root:    opts.Root,
		Entries: entries,
		Opts: stypes.Opts{
			Values:        opts.TypeOpts,
			RewritePrefix: opts.RewritePrefix,
			Platform:      opts.Platform,
		},
		TmpDir: tmp,
		State:  map[string]any{},
	}
	if err := t.Prepare(ctx, src, src.Opts); err != nil {
		return nil, err
	}

	// Groups.
	groups := layer.Split(src.Entries, layer.SplitOptions{
		Disabled: !split,
		Large:    large,
		Small:    small,
		Rules:    t.Layout(src),
	})
	if len(groups) == 0 {
		return nil, fmt.Errorf("slice: nothing to pack in %s", opts.Dir)
	}

	// Type annotations (validated: overlay only from patch).
	typeAnns := t.Annotate(src)
	if t.Name() != "patch" {
		if _, ok := typeAnns[layer.KeyLayerOverlay]; ok {
			return nil, fmt.Errorf("slice: type %q cannot set %s", t.Name(), layer.KeyLayerOverlay)
		}
	}

	// Platform resolution.
	platform := opts.Platform
	detected := typeAnns[layer.TypeKey(t.Name(), "arch")]
	if platform == "" {
		if detected != "" {
			platform = "linux/" + detected
		} else {
			platform = "any"
		}
	} else if detected != "" && platform != "any" && platform != "linux/"+detected {
		return nil, fmt.Errorf("slice: --platform %s conflicts with detected arch %s", platform, detected)
	}
	osStr, arch := "unknown", "unknown"
	if platform != "any" {
		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("slice: bad platform %q", platform)
		}
		osStr, arch = parts[0], parts[1]
	}

	// Target.
	target := opts.Target
	if target == "" {
		n, ok := t.(stypes.Namer)
		if !ok {
			return nil, fmt.Errorf("slice: --to required (type %q cannot name targets)", t.Name())
		}
		target, err = n.DefaultTarget(src, opts.TargetPrefix)
		if err != nil {
			return nil, err
		}
	}
	if _, err := client.ParseRef(target); err != nil {
		return nil, fmt.Errorf("slice: bad target %q: %w", target, err)
	}

	uid, gid, err := parseOwner(opts.Owner)
	if err != nil {
		return nil, err
	}
	chmod, err := parseChmod(opts.Chmod)
	if err != nil {
		return nil, err
	}

	// Build layers (parallel; order preserved).
	epoch := Epoch(opts.SourceDateEpoch)
	built := make([]*layer.Layer, len(groups))
	var wg sync.WaitGroup
	errs := make([]error, len(groups))
	sem := make(chan struct{}, 4)
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g layer.Group) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			l, err := layer.Build(g, layer.TarOptions{
				Root:      opts.Root,
				MTime:     epoch,
				UID:       uid,
				GID:       gid,
				Chmod:     chmod,
				NoParents: opts.NoParents,
			}, comp, level, tmp)
			built[i] = l
			errs[i] = err
		}(i, g)
	}
	wg.Wait()
	defer func() {
		for _, l := range built {
			if l != nil {
				l.Close()
			}
		}
	}()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	// Image.
	baseName := filepath.Base(strings.TrimSuffix(opts.Dir, "/"))
	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	img, err = mutate.CreatedAt(img, v1.Time{Time: epoch})
	if err != nil {
		return nil, err
	}
	var adds []mutate.Addendum
	var layerInfos []stypes.LayerInfo
	for i, g := range groups {
		name := g.Name
		if name == "" {
			name = baseName
		}
		createdBy := fmt.Sprintf("sandwichcake pack --type %s --root %s %s", t.Name(), opts.Root, baseName)
		if g.PartsTotal > 1 {
			createdBy += fmt.Sprintf(" (part %d/%d)", g.Part, g.PartsTotal)
		}
		anns := map[string]string{
			layer.KeyLayerType:   t.Name(),
			layer.KeyLayerSchema: strconv.Itoa(t.Schema()),
			layer.KeyLayerRoot:   opts.Root,
			layer.KeyLayerName:   name,
			layer.KeyLayerPart:   fmt.Sprintf("%d/%d", g.Part, g.PartsTotal),
			layer.KeyLayerFiles:  strconv.Itoa(len(g.Entries)),
			layer.KeyToolVersion: opts.ToolVersion,
		}
		for k, v := range typeAnns {
			anns[k] = v
		}
		digest, _ := built[i].Digest()
		diffID, _ := built[i].DiffID()
		size, _ := built[i].Size()
		mt, _ := built[i].MediaType()
		adds = append(adds, mutate.Addendum{
			Layer:       built[i],
			MediaType:   mt,
			Annotations: anns,
			History:     v1.History{Created: v1.Time{Time: epoch}, CreatedBy: createdBy},
		})
		layerInfos = append(layerInfos, stypes.LayerInfo{
			Digest: digest, DiffID: diffID, Size: size,
			MediaType: string(mt), Annotations: anns,
		})
	}
	img, err = mutate.Append(img, adds...)
	if err != nil {
		return nil, err
	}
	// OS/arch.
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cf = cf.DeepCopy()
	cf.OS = osStr
	cf.Architecture = arch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		return nil, err
	}
	manifestAnns := map[string]string{layer.KeyKind: layer.KindSlice}
	if platform == "any" {
		manifestAnns[layer.KeyPlatform] = "any"
	}
	img = mutate.Annotations(img, manifestAnns).(v1.Image)

	manifestDigest, err := img.Digest()
	if err != nil {
		return nil, err
	}
	rawManifest, err := img.RawManifest()
	if err != nil {
		return nil, err
	}
	res := &PackResult{
		Ref:    target,
		Digest: manifestDigest,
		Size:   int64(len(rawManifest)),
		DryRun: opts.DryRun,
	}
	res.Slice = stypes.Slice{Ref: target, Annotations: manifestAnns, Layers: layerInfos}

	if opts.DryRun {
		return res, nil
	}
	if opts.Merge {
		if err := mergePush(ctx, client, target, img, platform); err != nil {
			return nil, err
		}
	} else if err := client.WriteImage(ctx, target, img); err != nil {
		return nil, err
	}
	// Metadata artifact.
	mref, err := pushMetadata(ctx, client, target, img, t.Metadata(src), opts.ArtifactCompat)
	if err != nil {
		return nil, fmt.Errorf("slice: metadata artifact: %w", err)
	}
	res.MetadataRef = mref
	return res, nil
}

// mergePush pushes img to target, updating an existing index at the tag to
// include this platform's manifest (replacing the same-platform entry).
func mergePush(ctx context.Context, client *registry.Client, target string, img v1.Image, platform string) error {
	_, err := client.ParseRef(target)
	if err != nil {
		return err
	}
	var idx v1.ImageIndex = empty.Index
	idx = mutate.IndexMediaType(idx, types.OCIImageIndex)

	var plat *v1.Platform
	if platform != "any" {
		plat = &v1.Platform{OS: osStrOf(platform), Architecture: archOf(platform)}
	}
	samePlatform := func(p *v1.Platform) bool {
		if plat == nil || p == nil {
			return false
		}
		return p.OS == plat.OS && p.Architecture == plat.Architecture && p.Variant == plat.Variant
	}

	head, err := client.Head(ctx, target)
	if err == nil && head != nil {
		if head.MediaType.IsIndex() {
			existing, err := client.Index(ctx, target)
			if err != nil {
				return err
			}
			im, err := existing.IndexManifest()
			if err != nil {
				return err
			}
			var adds []mutate.IndexAddendum
			for _, m := range im.Manifests {
				if samePlatform(m.Platform) {
					continue // replaced by the new image
				}
				child, err := existing.Image(m.Digest)
				if err != nil {
					return err
				}
				adds = append(adds, mutate.IndexAddendum{Add: child, Descriptor: m})
			}
			idx = mutate.AppendManifests(idx, adds...)
		} else {
			// Existing single-platform manifest: keep it (unless same
			// platform, in which case the new image replaces it entirely).
			existing, err := client.Image(ctx, target, nil)
			if err != nil {
				return err
			}
			d := *head
			// Fill the descriptor's platform from the image config.
			if d.Platform == nil {
				if cf, err := existing.ConfigFile(); err == nil && cf != nil {
					p := &v1.Platform{OS: cf.OS, Architecture: cf.Architecture, Variant: cf.Variant}
					if p.OS == "unknown" && p.Architecture == "unknown" {
						p = nil // platform-independent
					}
					d.Platform = p
				}
			}
			if !samePlatform(d.Platform) {
				idx = mutate.AppendManifests(idx, mutate.IndexAddendum{Add: existing, Descriptor: d})
			}
		}
	}
	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add:        img,
		Descriptor: v1.Descriptor{Platform: plat},
	})
	return client.WriteIndex(ctx, target, idx)
}

func osStrOf(platform string) string {
	p := strings.SplitN(platform, "/", 2)
	return p[0]
}
func archOf(platform string) string {
	p := strings.SplitN(platform, "/", 2)
	if len(p) == 2 {
		return p[1]
	}
	return ""
}

// emptyConfig is the canonical empty OCI config blob.
var emptyConfig = []byte("{}")

// pushMetadata pushes the metadata artifact for the slice image.
// Returns the tag it was pushed under ("" if no metadata).
func pushMetadata(ctx context.Context, client *registry.Client, target string, img v1.Image, files []stypes.MetadataFile, compat bool) (string, error) {
	if len(files) == 0 {
		return "", nil
	}
	r, err := client.ParseRef(target)
	if err != nil {
		return "", err
	}
	repo := r.Context()
	digest, err := img.Digest()
	if err != nil {
		return "", err
	}
	raw, err := img.RawManifest()
	if err != nil {
		return "", err
	}
	mt, err := img.MediaType()
	if err != nil {
		return "", err
	}

	// Upload blobs.
	cfgSum := sha256.Sum256(emptyConfig)
	cfgDigest := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(cfgSum[:])}
	if err := client.UploadBlob(ctx, repo, cfgDigest, bytes.NewReader(emptyConfig), int64(len(emptyConfig))); err != nil {
		return "", err
	}
	var layerDescs []v1.Descriptor
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		d := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
		if err := client.UploadBlob(ctx, repo, d, bytes.NewReader(f.Data), int64(len(f.Data))); err != nil {
			return "", err
		}
		layerDescs = append(layerDescs, v1.Descriptor{
			MediaType: types.MediaType(f.MediaType),
			Digest:    d,
			Size:      int64(len(f.Data)),
			Annotations: map[string]string{
				"org.opencontainers.image.title": f.Name,
			},
		})
	}

	manifest := &v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIEmptyJSON,
			Digest:    cfgDigest,
			Size:      int64(len(emptyConfig)),
		},
		Layers: layerDescs,
	}
	if compat {
		manifest.Config.MediaType = types.OCIConfigJSON
	} else {
		manifest.ArtifactType = layer.MediaTypeMetadata
		manifest.Subject = &v1.Descriptor{
			MediaType: mt,
			Digest:    digest,
			Size:      int64(len(raw)),
		}
	}
	rawArt, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	artSum := sha256.Sum256(rawArt)
	artDigest := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(artSum[:])}
	// PUT the artifact by its own digest (OCI 1.1).
	hdr, err := client.PutManifest(ctx, repo.Digest(artDigest.String()), rawArt, string(types.OCIManifestSchema1))
	if err != nil {
		return "", err
	}
	subjectDigest := repo.Digest(digest.String())
	artDesc := v1.Descriptor{
		MediaType:    types.OCIManifestSchema1,
		Digest:       artDigest,
		Size:         int64(len(rawArt)),
		ArtifactType: layer.MediaTypeMetadata,
	}
	if compat || hdr.Get("OCI-Subject") == "" {
		// No referrers API: record it in the fallback index.
		if err := client.AddReferrer(ctx, subjectDigest, artDesc); err != nil {
			return "", err
		}
	}
	return repo.String() + "@" + artDigest.String(), nil
}

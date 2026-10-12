package compose

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/baba/sandwichcake/internal/testutil"
	"github.com/baba/sandwichcake/pkg/cake"
	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/resolve"
	stypes "github.com/baba/sandwichcake/pkg/types"
	_ "github.com/baba/sandwichcake/pkg/types/builtin"
)

func mkLayer(t *testing.T, files map[string]string, mt gtypes.MediaType, anns map[string]string) resolve.Layer {
	l := testutil.DockerLayer(t, files)
	d, _ := l.Digest()
	id, _ := l.DiffID()
	sz, _ := l.Size()
	return resolve.Layer{
		L: l, DiffID: id, Index: 0,
		Desc: v1.Descriptor{MediaType: mt, Digest: d, Size: sz, Annotations: anns},
	}
}

func baseResolved(t *testing.T) *resolve.Resolved {
	l := mkLayer(t, map[string]string{"bin/sh": "x"}, gtypes.DockerLayer, nil)
	l.History = &v1.History{CreatedBy: "/bin/sh -c #(nop) ADD file:abc in /"}
	h := v1.Hash{Algorithm: "sha256", Hex: "aa"}
	return &resolve.Resolved{
		Input:          cake.Ref{Ref: "r/base:1"},
		Repo:           "r/base",
		ManifestDigest: h,
		RefDigest:      v1.Hash{Algorithm: "sha256", Hex: "dd"},
		Kind:           "base",
		Libc:           "glibc 2.35",
		Config: &v1.ConfigFile{
			OS: "linux", Architecture: "amd64",
			Config: v1.Config{
				Env:        []string{"PATH=/usr/bin", "FOO=old"},
				Entrypoint: []string{"/init"},
				Labels:     map[string]string{"a": "1"},
			},
			History: []v1.History{
				{CreatedBy: "ENV FOO=old", EmptyLayer: true},
				{CreatedBy: "/bin/sh -c #(nop) ADD file:abc in /"},
			},
		},
		Layers: []resolve.Layer{l},
	}
}

func sliceResolved(t *testing.T, typ, root string, files map[string]string, extra map[string]string) *resolve.Resolved {
	anns := map[string]string{
		layer.KeyLayerType: typ,
		layer.KeyLayerRoot: root,
	}
	for k, v := range extra {
		anns[k] = v
	}
	l := mkLayer(t, files, gtypes.DockerLayer, anns)
	h := v1.Hash{Algorithm: "sha256", Hex: "bb"}
	return &resolve.Resolved{
		Input:          cake.Ref{Ref: "r/s:1"},
		Repo:           "r/s",
		ManifestDigest: h,
		Kind:           "slice",
		Config:         &v1.ConfigFile{},
		Layers:         []resolve.Layer{l},
	}
}

func input(slices ...*resolve.Resolved) Input {
	return Input{
		Ctx:      context.Background(),
		Platform: v1.Platform{OS: "linux", Architecture: "amd64"},
		Base:     baseResolved(&testing.T{}),
		Slices:   slices,
		Created:  time.Unix(0, 0),
	}
}

func TestComposeBasic(t *testing.T) {
	in := input(
		sliceResolved(t, "other", "/etc/app", map[string]string{"etc/app/c.conf": "x"}, nil),
	)
	in.Patch = cake.ConfigPatch{
		Env: map[string]cake.EnvValue{
			"PATH": {Prepend: strp("/opt/bin")},
			"FOO":  {Value: strp("new")},
		},
		Cmd:    &[]string{"run"},
		Labels: map[string]string{"b": "2"},
	}
	res, err := Compose(in)
	if err != nil {
		t.Fatal(err)
	}
	img := res.Image
	m, _ := img.Manifest()
	if m.MediaType != gtypes.OCIManifestSchema1 {
		t.Fatalf("mediaType %s", m.MediaType)
	}
	for _, l := range m.Layers {
		if l.MediaType != gtypes.OCILayer {
			t.Fatalf("layer not normalized: %s", l.MediaType)
		}
	}
	cf, _ := img.ConfigFile()
	if cf.OS != "linux" || cf.Architecture != "amd64" {
		t.Fatalf("platform %s/%s", cf.OS, cf.Architecture)
	}
	envs := map[string]string{}
	for _, kv := range cf.Config.Env {
		k, v, _ := cutKV(kv)
		envs[k] = v
	}
	if envs["PATH"] != "/opt/bin:/usr/bin" {
		t.Fatalf("PATH prepend: %v", envs["PATH"])
	}
	if envs["FOO"] != "new" {
		t.Fatalf("FOO: %v", envs)
	}
	if len(cf.Config.Cmd) != 1 || cf.Config.Cmd[0] != "run" {
		t.Fatalf("cmd: %v", cf.Config.Cmd)
	}
	if cf.Config.Labels["a"] != "1" || cf.Config.Labels["b"] != "2" {
		t.Fatalf("labels: %v", cf.Config.Labels)
	}
	// history: base 2 entries (empty+ADD) + 1 slice layer
	var nonEmpty int
	for _, h := range cf.History {
		if !h.EmptyLayer {
			nonEmpty++
		}
	}
	if len(cf.History) != 3 || nonEmpty != 2 {
		t.Fatalf("history: %+v", cf.History)
	}
	if len(cf.RootFS.DiffIDs) != 2 {
		t.Fatalf("diffids: %v", cf.RootFS.DiffIDs)
	}
	if cf.History[2].CreatedBy == "" {
		t.Fatal("slice history missing")
	}
	// annotations
	if m.Annotations[layer.KeySlices] == "" {
		t.Fatal("slices annotation missing")
	}
	if m.Annotations["org.opencontainers.image.base.name"] != "r/base:1" {
		t.Fatalf("base.name: %v", m.Annotations)
	}
}

func TestComposeRootConflict(t *testing.T) {
	in := input(
		sliceResolved(t, "other", "/etc", map[string]string{"etc/a": "1"}, nil),
		sliceResolved(t, "other", "/etc/app", map[string]string{"etc/app/b": "2"}, nil),
	)
	if _, err := Compose(in); err == nil {
		t.Fatal("expected root conflict")
	}
}

func TestComposeOverlaySkipsConflict(t *testing.T) {
	in := input(
		sliceResolved(t, "other", "/etc", map[string]string{"etc/a": "1"}, nil),
		sliceResolved(t, "patch", "/etc/app", map[string]string{"etc/app/b": "2"},
			map[string]string{layer.KeyLayerOverlay: "true"}),
	)
	if _, err := Compose(in); err != nil {
		t.Fatal(err)
	}
}

func TestComposeDedupe(t *testing.T) {
	l1 := mkLayer(t, map[string]string{"a": "1"}, gtypes.OCILayer, nil)
	l2 := l1 // same diff_id
	s1 := &resolve.Resolved{Input: cake.Ref{Ref: "r/s:1"}, Repo: "r/s",
		ManifestDigest: v1.Hash{Algorithm: "sha256", Hex: "b1"}, Config: &v1.ConfigFile{},
		Kind: "slice", Layers: []resolve.Layer{l1}}
	s2 := &resolve.Resolved{Input: cake.Ref{Ref: "r/s2:1"}, Repo: "r/s2",
		ManifestDigest: v1.Hash{Algorithm: "sha256", Hex: "b2"}, Config: &v1.ConfigFile{},
		Kind: "slice", Layers: []resolve.Layer{l2}}
	res, err := Compose(input(s1, s2))
	if err != nil {
		t.Fatal(err)
	}
	cf, _ := res.Image.ConfigFile()
	if len(cf.RootFS.DiffIDs) != 2 { // base + 1 unique
		t.Fatalf("dedupe failed: %v", cf.RootFS.DiffIDs)
	}
}

func TestComposeUnknownTypeWarns(t *testing.T) {
	in := input(sliceResolved(t, "weirdtype", "/x", map[string]string{"x/f": "1"}, nil))
	res, err := Compose(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Level != stypes.Warn {
		t.Fatalf("findings: %v", res.Findings)
	}
}

func TestComposeRequiresFails(t *testing.T) {
	in := input(sliceResolved(t, "binary", "/usr/bin", map[string]string{"usr/bin/x": "1"},
		map[string]string{layer.KeyLayerRequires: "glibc>=2.99"}))
	if _, err := Compose(in); err == nil {
		t.Fatal("expected glibc error")
	}
}

func TestComposeDocker20Zstd(t *testing.T) {
	in := input(sliceResolved(t, "other", "/x", map[string]string{"x/f": "1"}, nil))
	in.Slices[0].Layers[0].Desc.MediaType = gtypes.OCILayerZStd
	in.Compat = "docker20"
	if _, err := Compose(in); err == nil {
		t.Fatal("expected zstd error")
	}
}

func TestComposeDeterministic(t *testing.T) {
	mk := func() v1.Hash {
		res, err := Compose(input(sliceResolved(t, "other", "/x", map[string]string{"x/f": "1"}, nil)))
		if err != nil {
			t.Fatal(err)
		}
		d, _ := res.Image.Digest()
		return d
	}
	if mk() != mk() {
		t.Fatal("nondeterministic digest")
	}
}

func TestMountableLayerPreserved(t *testing.T) {
	ref, _ := name.ParseReference("r/src:tag")
	l := testutil.DockerLayer(t, map[string]string{"a": "1"})
	ml := &remote.MountableLayer{Layer: l, Reference: ref}
	d, _ := ml.Digest()
	id, _ := ml.DiffID()
	s := &resolve.Resolved{Input: cake.Ref{Ref: "r/s:1"}, Repo: "r/s",
		ManifestDigest: v1.Hash{Algorithm: "sha256", Hex: "cc"}, Config: &v1.ConfigFile{},
		Kind: "slice",
		Layers: []resolve.Layer{{
			L: ml, DiffID: id,
			Desc: v1.Descriptor{MediaType: gtypes.DockerLayer, Digest: d},
		}}}
	res, err := Compose(input(s))
	if err != nil {
		t.Fatal(err)
	}
	layers, _ := res.Image.Layers()
	if _, ok := layers[1].(*remote.MountableLayer); !ok {
		t.Fatalf("layer wrapped: %T", layers[1])
	}
}

// A second slice of the same type must also run Check: here binary arch.
func TestCheckRunsPerSlice(t *testing.T) {
	ok := sliceResolved(t, "binary", "/a", map[string]string{"a/x": "1"},
		map[string]string{layer.TypeKey("binary", "arch"): "amd64"})
	bad := sliceResolved(t, "binary", "/b", map[string]string{"b/y": "1"},
		map[string]string{layer.TypeKey("binary", "arch"): "arm64"})
	if _, err := Compose(input(ok, bad)); err == nil {
		t.Fatal("expected arch mismatch error from second binary slice")
	}
	// Both matching => no error.
	good := sliceResolved(t, "binary", "/b", map[string]string{"b/y": "1"},
		map[string]string{layer.TypeKey("binary", "arch"): "amd64"})
	if _, err := Compose(input(ok, good)); err != nil {
		t.Fatal(err)
	}
}

// patch.base may name the base by selected manifest digest or by the digest
// the base ref resolved to (index digest); a different digest is an error.
func TestPatchBaseForms(t *testing.T) {
	mk := func(baseAnn string) *resolve.Resolved {
		return sliceResolved(t, "patch", "/p", map[string]string{"p/f": "1"},
			map[string]string{
				layer.KeyLayerOverlay:          "true",
				layer.TypeKey("patch", "base"): baseAnn,
			})
	}
	// manifest digest form
	if _, err := Compose(input(mk("r/base@sha256:aa"))); err != nil {
		t.Fatal(err)
	}
	// index/ref digest form
	if _, err := Compose(input(mk("r/base@sha256:dd"))); err != nil {
		t.Fatal(err)
	}
	// mismatch
	if _, err := Compose(input(mk("r/base@sha256:ff"))); err == nil {
		t.Fatal("expected patch base mismatch error")
	}
}

func strp(s string) *string { return &s }

func cutKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}

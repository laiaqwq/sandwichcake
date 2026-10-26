package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"archive/tar"
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"io"
	"net/http"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/cake"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/plan"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
	"github.com/laiaqwq/sandwichcake/pkg/slice"
)

func pushBase(t *testing.T, c *registry.Client, ref, os_, arch string) {
	t.Helper()
	l := testutil.DockerLayer(t, map[string]string{"bin/sh": "#!/bin/sh", "etc/os-release": "ID=test"})
	img := empty.Image
	img = mutate.MediaType(img, gtypes.DockerManifestSchema2)
	var err error
	img, err = mutate.Append(img, mutate.Addendum{
		Layer:   l,
		History: v1.History{CreatedBy: "/bin/sh -c #(nop) ADD file:abc in /"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cf, _ := img.ConfigFile()
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = os_, arch
	cf.Config.Env = []string{"PATH=/usr/bin"}
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
}

func packSlice(t *testing.T, c *registry.Client, typ, root, target string, files map[string]string) *slice.PackResult {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	res, err := slice.Pack(context.Background(), c, slice.PackOptions{
		Type: typ, Dir: dir, Root: root, Target: target, SourceDateEpoch: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestComposeEndToEnd(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	host := strings.TrimPrefix(s.URL, "http://")

	pushBase(t, c, host+"/base:1", "linux", "amd64")
	packSlice(t, c, "other", "/etc/app", host+"/s/conf:1", map[string]string{"app.conf": "x=1"})
	packSlice(t, c, "weight", "/models/m", host+"/s/w:1", map[string]string{"model.safetensors": "w"})

	ck := &cake.Cake{
		APIVersion: cake.APIVersion, Kind: cake.KindCake,
		Target: cake.Target{Ref: host + "/app:v1", Platforms: []string{"linux/amd64"}},
		Base:   cake.Ref{Ref: host + "/base:1", Libc: "glibc 2.35"},
		Slices: []cake.Ref{
			{Ref: host + "/s/conf:1"},
			{Ref: host + "/s/w:1"},
		},
		Config: cake.ConfigPatch{
			Env: map[string]cake.EnvValue{"PATH": {Prepend: strp("/models/m/bin")}},
		},
	}
	out, err := Compose(ctx, c, ck, nil, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Pushed || out.Digest.Hex == "" {
		t.Fatal("not pushed")
	}
	// Pull result.
	img, err := c.Image(ctx, host+"/app:v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.MediaType != gtypes.OCIManifestSchema1 {
		t.Fatalf("mediaType %s", m.MediaType)
	}
	slicesAnn := m.Annotations[layer.KeySlices]
	if !strings.Contains(slicesAnn, "/s/conf@sha256:") || !strings.Contains(slicesAnn, "/s/w@sha256:") {
		t.Fatalf("slices annotation: %q", slicesAnn)
	}
	if len(m.Layers) != 3 {
		t.Fatalf("layers: %d", len(m.Layers))
	}
	// Extract and check files present under roots.
	rc := mutate.Extract(img)
	defer rc.Close()
	seen := map[string]bool{}
	{
		trr := tar.NewReader(rc)
		for {
			h, err := trr.Next()
			if err != nil {
				break
			}
			seen[h.Name] = true
		}
	}
	for _, want := range []string{"bin/sh", "etc/app/app.conf", "models/m/model.safetensors"} {
		if !seen[want] {
			t.Fatalf("missing %s in composed image; have %v", want, keysOf(seen))
		}
	}
	cf, _ := img.ConfigFile()
	var pathEnv string
	for _, kv := range cf.Config.Env {
		if strings.HasPrefix(kv, "PATH=") {
			pathEnv = kv
		}
	}
	if pathEnv != "PATH=/models/m/bin:/usr/bin" {
		t.Fatalf("PATH: %q", pathEnv)
	}

	// Plan to a second repo on the same registry: mount. The fake registry
	// shares blobs globally, so pretend app2 is empty via a transport shim.
	cM := registry.New(registry.Options{
		Keychain: authn.NewMultiKeychain(), Insecure: true,
		Transport: head404{http.DefaultTransport, "/v2/app2/blobs/"},
	})
	pl, err := plan.Compute(ctx, cM, host+"/app2:v1", img)
	if err != nil {
		t.Fatal(err)
	}
	if pl.MountBytes == 0 {
		t.Fatalf("expected mount: %+v", pl)
	}
	// Only the config blob (not mountable) may be copy.
	var copies int
	for _, it := range pl.Items {
		if it.Strategy == registry.StrategyCopy {
			copies++
			if it.MediaType != "config" {
				t.Fatalf("unexpected copy item: %+v", it)
			}
		}
	}
	if copies != 1 {
		t.Fatalf("expected 1 copy (config): %+v", pl.Items)
	}

	// Plan to a different registry: copy.
	s2, c2 := testutil.Registry(t)
	host2 := strings.TrimPrefix(s2.URL, "http://")
	pl2, err := plan.Compute(ctx, c2, host2+"/app:v1", img)
	if err != nil {
		t.Fatal(err)
	}
	if pl2.CopyBytes == 0 || pl2.MountBytes != 0 {
		t.Fatalf("expected all copy: %+v", pl2)
	}

	// Copy without Yes/Confirm => error.
	ck2 := *ck
	ck2.Target.Ref = host2 + "/app:v1"
	if _, err := Compose(ctx, c, &ck2, nil, Options{}); err == nil {
		t.Fatal("expected copy confirmation error")
	}

	// Locked rerun: same digest.
	out2, err := Compose(ctx, c, ck, out.Lock, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Digest != out.Digest {
		t.Fatalf("locked rerun digest %s != %s", out2.Digest, out.Digest)
	}
}

func TestMultiArchIndex(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	host := strings.TrimPrefix(s.URL, "http://")

	// Multi-arch base index.
	var adds []mutate.IndexAddendum
	for _, arch := range []string{"arm64", "amd64"} {
		l := testutil.DockerLayer(t, map[string]string{"bin/sh-" + arch: "x"})
		img := empty.Image
		img = mutate.MediaType(img, gtypes.DockerManifestSchema2)
		var err error
		img, err = mutate.AppendLayers(img, l)
		if err != nil {
			t.Fatal(err)
		}
		cf, _ := img.ConfigFile()
		cf = cf.DeepCopy()
		cf.OS, cf.Architecture = "linux", arch
		img, _ = mutate.ConfigFile(img, cf)
		adds = append(adds, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: arch}},
		})
	}
	idx := mutate.IndexMediaType(empty.Index, gtypes.OCIImageIndex)
	idx = mutate.AppendManifests(idx, adds...)
	if err := c.WriteIndex(ctx, host+"/base:multi", idx); err != nil {
		t.Fatal(err)
	}
	packSlice(t, c, "weight", "/models/m", host+"/s/w:1", map[string]string{"w.bin": "x"})

	ck := &cake.Cake{
		APIVersion: cake.APIVersion, Kind: cake.KindCake,
		Target: cake.Target{Ref: host + "/app:multi"},
		Base:   cake.Ref{Ref: host + "/base:multi"},
		Slices: []cake.Ref{{Ref: host + "/s/w:1"}},
	}
	out, err := Compose(ctx, c, ck, nil, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	resIdx, err := c.Index(ctx, host+"/app:multi")
	if err != nil {
		t.Fatal(err)
	}
	im, _ := resIdx.IndexManifest()
	if len(im.Manifests) != 2 {
		t.Fatalf("index manifests: %d", len(im.Manifests))
	}
	// Both manifests must reference the same weight layer digest.
	var weightDigests []string
	for _, d := range im.Manifests {
		img, err := resIdx.Image(d.Digest)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := img.Manifest()
		for _, l := range m.Layers {
			if l.Annotations[layer.KeyLayerType] == "weight" {
				weightDigests = append(weightDigests, l.Digest.String())
			}
		}
	}
	if len(weightDigests) != 2 || weightDigests[0] != weightDigests[1] {
		t.Fatalf("weight layers: %v", weightDigests)
	}

	// Locked rerun reproduces the identical index digest; the lock pins the
	// base to the index digest, not a platform manifest.
	idxDesc, _ := c.Get(ctx, host+"/base:multi")
	wantRef := host + "/base@" + idxDesc.Digest.String()
	if out.Lock.Base.Ref != wantRef {
		t.Fatalf("lock ref %q, want %q", out.Lock.Base.Ref, wantRef)
	}
	out2, err := Compose(ctx, c, ck, out.Lock, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Digest != out.Digest {
		t.Fatalf("locked rerun digest %s != %s", out2.Digest, out.Digest)
	}
}

// pushPlainImage pushes a non-slice image whose layers all have pure
// COPY history so they are selectable as external layers.
func pushPlainImage(t *testing.T, c *registry.Client, ref string, n int) {
	t.Helper()
	img := empty.Image
	img = mutate.MediaType(img, gtypes.DockerManifestSchema2)
	var adds []mutate.Addendum
	for i := 0; i < n; i++ {
		l := testutil.DockerLayer(t, map[string]string{fmt.Sprintf("f%d.txt", i): "x"})
		adds = append(adds, mutate.Addendum{
			Layer:   l,
			History: v1.History{CreatedBy: fmt.Sprintf("COPY f%d /", i)},
		})
	}
	var err error
	img, err = mutate.Append(img, adds...)
	if err != nil {
		t.Fatal(err)
	}
	cf, _ := img.ConfigFile()
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = "linux", "amd64"
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
}

// Selection fields (layers, libc) survive the lock round trip: a --locked
// rerun reproduces the identical digest.
func TestLockedSelectionSemantics(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	host := strings.TrimPrefix(s.URL, "http://")

	pushBase(t, c, host+"/base:1", "linux", "amd64")
	pushPlainImage(t, c, host+"/ext:1", 3)

	one := 1
	ck := &cake.Cake{
		APIVersion: cake.APIVersion, Kind: cake.KindCake,
		Target: cake.Target{Ref: host + "/app:sel", Platforms: []string{"linux/amd64"}},
		Base:   cake.Ref{Ref: host + "/base:1", Libc: "glibc 2.35"},
		Slices: []cake.Ref{
			{Ref: host + "/ext:1", Layers: []cake.LayerSel{{Index: &one}}},
		},
	}
	out, err := Compose(ctx, c, ck, nil, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	// Only layer 1 selected => 2 layers total.
	img, _ := c.Image(ctx, host+"/app:sel", nil)
	m, _ := img.Manifest()
	if len(m.Layers) != 2 {
		t.Fatalf("layers: %d", len(m.Layers))
	}
	// Lock carries the selection.
	if len(out.Lock.Slices) != 1 || len(out.Lock.Slices[0].Layers) != 1 ||
		out.Lock.Base.Libc != "glibc 2.35" {
		t.Fatalf("lock lost selection: %+v", out.Lock)
	}
	out2, err := Compose(ctx, c, ck, out.Lock, Options{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Digest != out.Digest {
		t.Fatalf("locked rerun digest %s != %s", out2.Digest, out.Digest)
	}
}

// Dry-run never prompts/fails on copy bytes.
func TestDryRunNoConfirm(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	host := strings.TrimPrefix(s.URL, "http://")
	s2, _ := testutil.Registry(t)
	host2 := strings.TrimPrefix(s2.URL, "http://")

	pushBase(t, c, host+"/base:1", "linux", "amd64")
	packSlice(t, c, "other", "/x", host+"/s/o:1", map[string]string{"f": "x"})

	ck := &cake.Cake{
		APIVersion: cake.APIVersion, Kind: cake.KindCake,
		Target: cake.Target{Ref: host2 + "/app:dry", Platforms: []string{"linux/amd64"}},
		Base:   cake.Ref{Ref: host + "/base:1"},
		Slices: []cake.Ref{{Ref: host + "/s/o:1"}},
	}
	out, err := Compose(ctx, c, ck, nil, Options{DryRun: true}) // no Yes, no Confirm
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil || out.Plan.CopyBytes == 0 {
		t.Fatalf("expected copy plan, got %+v", out.Plan)
	}
	if out.Pushed {
		t.Fatal("dry-run pushed")
	}
}

func strp(s string) *string { return &s }

// head404 returns 404 for HEAD requests under a path prefix, emulating
// per-repo blob storage that the fake registry lacks.
type head404 struct {
	base   http.RoundTripper
	prefix string
}

func (h head404) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, h.prefix) {
		return &http.Response{
			StatusCode: 404, Status: "404 Not Found",
			Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r,
		}, nil
	}
	return h.base.RoundTrip(r)
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

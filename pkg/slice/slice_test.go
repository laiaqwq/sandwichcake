package slice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/registry"
)

func testEnv(t *testing.T) (*registry.Client, string) {
	t.Helper()
	s := httptest.NewServer(ggcrregistry.New(ggcrregistry.WithReferrersSupport(true)))
	t.Cleanup(s.Close)
	c := registry.New(registry.Options{
		Keychain: authn.NewMultiKeychain(), Insecure: true, Transport: http.DefaultTransport,
	})
	return c, strings.TrimPrefix(s.URL, "http://")
}

func weightDir(t *testing.T) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("w"), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644)
	return dir
}

func TestPackWeightEndToEnd(t *testing.T) {
	client, host := testEnv(t)
	dir := weightDir(t)
	target := host + "/u/weights/m:x"
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Dir: dir, Root: "/models/m", Target: target,
		TypeOpts: map[string]string{"source": "hf://org/m@rev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Read back the manifest.
	desc, err := client.Get(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	m, err := v1.ParseManifest(strings.NewReader(string(desc.Manifest)))
	if err != nil {
		t.Fatal(err)
	}
	if !IsSlice(m) {
		t.Fatalf("not a slice: %v", m.Annotations)
	}
	if m.Annotations[layer.KeyPlatform] != "any" {
		t.Fatalf("platform annotation: %v", m.Annotations)
	}
	if len(m.Layers) != len(res.Slice.Layers) {
		t.Fatalf("layers: %d vs %d", len(m.Layers), len(res.Slice.Layers))
	}
	for i, l := range m.Layers {
		a := l.Annotations
		if a[layer.KeyLayerType] != "weight" {
			t.Fatalf("layer %d type: %v", i, a)
		}
		if a[layer.KeyLayerRoot] != "/models/m" {
			t.Fatalf("layer %d root: %v", i, a)
		}
		if l.Digest != res.Slice.Layers[i].Digest {
			t.Fatalf("layer %d digest mismatch", i)
		}
	}
	// Config: unknown/unknown, history aligns with diff_ids.
	img, err := client.Image(context.Background(), target, nil)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cf.OS != "unknown" || cf.Architecture != "unknown" {
		t.Fatalf("config platform: %s/%s", cf.OS, cf.Architecture)
	}
	if len(cf.RootFS.DiffIDs) != len(res.Slice.Layers) {
		t.Fatalf("diff_ids: %v", cf.RootFS.DiffIDs)
	}
	var nonEmpty int
	for _, h := range cf.History {
		if !h.EmptyLayer {
			nonEmpty++
			if !strings.HasPrefix(h.CreatedBy, "sandwichcake pack --type weight") {
				t.Fatalf("history: %q", h.CreatedBy)
			}
		}
	}
	if nonEmpty != len(res.Slice.Layers) {
		t.Fatalf("history %d != layers %d", nonEmpty, len(res.Slice.Layers))
	}
	for i, li := range res.Slice.Layers {
		if cf.RootFS.DiffIDs[i] != li.DiffID {
			t.Fatalf("diff_id %d mismatch", i)
		}
	}
	// Metadata artifact discoverable via referrers.
	if res.MetadataRef == "" {
		t.Fatal("no metadata artifact")
	}
	dref := host + "/u/weights/m@" + res.Digest.String()
	refs, err := client.Referrers(context.Background(), dref)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range refs {
		if d.ArtifactType == layer.MediaTypeMetadata ||
			d.Annotations["org.opencontainers.image.title"] != "" ||
			d.Digest.String() != "" {
			found = true // at least one referrer exists
		}
	}
	if len(refs) == 0 || !found {
		t.Fatalf("no referrers: %v", refs)
	}

	// Re-pack: identical manifest digest.
	res2, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Dir: dir, Root: "/models/m", Target: target,
		TypeOpts: map[string]string{"source": "hf://org/m@rev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Digest != res.Digest {
		t.Fatalf("repack digest %s != %s", res2.Digest, res.Digest)
	}
}

func TestPackDryRun(t *testing.T) {
	client, host := testEnv(t)
	dir := weightDir(t)
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Dir: dir, Root: "/m", Target: host + "/r/w:t",
		DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Digest.Hex == "" {
		t.Fatal("dry run result")
	}
	if _, err := client.Head(context.Background(), host+"/r/w:t"); err == nil {
		t.Fatal("dry run pushed")
	}
}

func TestPatchOverlayOnlyPatch(t *testing.T) {
	// patch type produces overlay; slice layer annotations carry it.
	client, host := testEnv(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte("x"), 0o644)
	target := host + "/r/patch:1"
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "patch", Dir: dir, Root: "/etc/nginx", Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Slice.Layers[0].Annotations[layer.KeyLayerOverlay] != "true" {
		t.Fatal("patch overlay missing")
	}
}

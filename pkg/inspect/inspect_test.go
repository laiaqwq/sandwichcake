package inspect_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/inspect"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
	"github.com/laiaqwq/sandwichcake/pkg/slice"
	_ "github.com/laiaqwq/sandwichcake/pkg/types/builtin"
)

func pushImage(t *testing.T, c *registry.Client, ref string, img v1.Image,
	os_, arch string) {
	t.Helper()
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = os_, arch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
}

func ociImage(t *testing.T, anns map[string]string,
	adds ...mutate.Addendum) v1.Image {
	t.Helper()
	img := mutate.MediaType(empty.Image, gtypes.OCIManifestSchema1)
	var err error
	img, err = mutate.Append(img, adds...)
	if err != nil {
		t.Fatal(err)
	}
	if anns != nil {
		img = mutate.Annotations(img, anns).(v1.Image)
	}
	return img
}

func TestInspectPlainImage(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.GzipLayer(t, map[string]string{"etc/app.conf": "x"})
	ref := testutil.RepoURL(s, "app") + ":1"
	pushImage(t, c, ref,
		ociImage(t, nil, mutate.Addendum{Layer: l}), "linux", "amd64")

	rep, err := inspect.Inspect(context.Background(), c, ref)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "image" {
		t.Fatalf("kind = %q, want image", rep.Kind)
	}
	if rep.Platform != "linux/amd64" {
		t.Fatalf("platform = %q", rep.Platform)
	}
	if rep.Ref != ref || rep.Digest == "" || len(rep.Layers) != 1 {
		t.Fatalf("report: %+v", rep)
	}
	row := rep.Layers[0]
	if row.Index != 0 || row.DiffID == "" || row.Size <= 0 ||
		row.MediaType == "" || row.Digest == "" {
		t.Fatalf("layer row incomplete: %+v", row)
	}
	if len(rep.Children) != 0 {
		t.Fatal("plain image must not have children")
	}
}

func TestInspectSliceAndJSON(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.GzipLayer(t, map[string]string{"models/m/w": "x"})
	ref := testutil.RepoURL(s, "s/w") + ":1"
	img := ociImage(t,
		map[string]string{layer.KeyKind: layer.KindSlice, layer.KeyPlatform: "any"},
		mutate.Addendum{Layer: l, Annotations: map[string]string{
			layer.KeyLayerType: "weight",
			layer.KeyLayerRoot: "/models/m",
			layer.KeyLayerName: "m",
			layer.KeyLayerPart: "1/1",
		}})
	pushImage(t, c, ref, img, "", "")

	rep, err := inspect.Inspect(context.Background(), c, ref)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "slice" || rep.Platform != "any" {
		t.Fatalf("kind=%q platform=%q", rep.Kind, rep.Platform)
	}
	if rep.Annotations[layer.KeyKind] != layer.KindSlice {
		t.Fatalf("annotations: %v", rep.Annotations)
	}
	row := rep.Layers[0]
	if row.Type != "weight" || row.Root != "/models/m" ||
		row.Name != "m" || row.Part != "1/1" {
		t.Fatalf("layer row: %+v", row)
	}

	// JSON keys are capitalized Go field names; LayerRow has no
	// Annotations field.
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Ref", "Kind", "Digest", "Annotations", "Layers", "Metadata", "Children"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("JSON missing key %q", k)
		}
	}
	layers, _ := decoded["Layers"].([]any)
	rowMap, _ := layers[0].(map[string]any)
	for _, k := range []string{"Index", "Digest", "Size", "Type", "Root", "Name"} {
		if _, ok := rowMap[k]; !ok {
			t.Errorf("layer JSON missing key %q", k)
		}
	}
	if _, ok := rowMap["Annotations"]; ok {
		t.Error("LayerRow unexpectedly exposes Annotations")
	}
}

func TestRenderOutput(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.GzipLayer(t, map[string]string{"a": "x"})
	ref := testutil.RepoURL(s, "s/r") + ":1"
	img := ociImage(t,
		map[string]string{
			layer.KeyKind:     layer.KindSlice,
			layer.KeyPlatform: "any",
			layer.Prefix + "zzz": "last",
		},
		mutate.Addendum{Layer: l, Annotations: map[string]string{
			layer.KeyLayerType: "other", layer.KeyLayerRoot: "/etc/x",
		}})
	pushImage(t, c, ref, img, "", "")

	rep, err := inspect.Inspect(context.Background(), c, ref)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rep.Render(&buf)
	out := buf.String()

	if !strings.Contains(out, "slice  "+ref) {
		t.Fatalf("header line missing:\n%s", out)
	}
	// Annotations render sorted.
	iki := strings.Index(out, layer.KeyKind)
	izi := strings.Index(out, layer.Prefix+"zzz")
	if iki < 0 || izi < 0 || iki > izi {
		t.Fatalf("annotations not sorted:\n%s", out)
	}
	// Layer line shows integer (compressed) byte size and decoded fields.
	if !strings.Contains(out, "type=other") || !strings.Contains(out, "root=/etc/x") {
		t.Fatalf("layer extras missing:\n%s", out)
	}
	size := rep.Layers[0].Size
	if !strings.Contains(out, " "+strconv.FormatInt(size, 10)+" ") {
		t.Fatalf("integer size %d not in output:\n%s", size, out)
	}
}

func TestMetadataFiles(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()

	dir := t.TempDir()
	cfgContent := []byte(`{"arch":"test"}`)
	for name, data := range map[string]string{
		"m.safetensors": "weights",
		"config.json":   string(cfgContent),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ref := testutil.RepoURL(s, "s/wm") + ":1"
	res, err := slice.Pack(ctx, c, slice.PackOptions{
		Type: "weight", Dir: dir, Root: "/models/m", Target: ref,
		SourceDateEpoch: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MetadataRef == "" {
		t.Fatal("weight pack produced no metadata artifact")
	}

	rep, err := inspect.Inspect(ctx, c, ref)
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, mf := range rep.Metadata {
		titles[mf.Title] = true
	}
	if !titles["files.json"] || !titles["config.json"] {
		t.Fatalf("metadata titles: %v", titles)
	}

	data, err := rep.ReadMetadataFile(ctx, c, "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, cfgContent) {
		t.Fatalf("config.json = %q, want %q", data, cfgContent)
	}
	data, err = rep.ReadMetadataFile(ctx, c, "files.json")
	if err != nil {
		t.Fatal(err)
	}
	var files []map[string]any
	if err := json.Unmarshal(data, &files); err != nil || len(files) == 0 {
		t.Fatalf("files.json: %v %s", err, data)
	}
	if _, err := rep.ReadMetadataFile(ctx, c, "nope.txt"); err == nil {
		t.Fatal("missing metadata title did not error")
	}
}

func TestInspectIndexChildren(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	var adds []mutate.IndexAddendum
	for _, arch := range []string{"amd64", "arm64"} {
		l := testutil.GzipLayer(t, map[string]string{"bin/" + arch: "x"})
		img := ociImage(t, nil, mutate.Addendum{Layer: l})
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
	ref := testutil.RepoURL(s, "multi") + ":1"
	if err := c.WriteIndex(ctx, ref, idx); err != nil {
		t.Fatal(err)
	}

	rep, err := inspect.Inspect(ctx, c, ref)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "index" || len(rep.Children) != 2 {
		t.Fatalf("index report: kind=%q children=%d", rep.Kind, len(rep.Children))
	}
	pls := map[string]bool{}
	for _, ch := range rep.Children {
		pls[ch.Platform] = true
		if len(ch.Layers) != 1 {
			t.Fatalf("child %s has %d layers", ch.Platform, len(ch.Layers))
		}
	}
	if !pls["linux/amd64"] || !pls["linux/arm64"] {
		t.Fatalf("child platforms: %v", pls)
	}
}

func TestDiffSharedAndChanged(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()

	// gzipped layer vs the SAME uncompressed tar bytes share a diff_id.
	gz := testutil.GzipLayer(t, map[string]string{"a": "x"})
	rc, err := gz.Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	plain := static.NewLayer(raw, gtypes.OCIUncompressedLayer)

	pushImage(t, c, testutil.RepoURL(s, "d/a")+":1",
		ociImage(t, nil, mutate.Addendum{Layer: gz}), "linux", "amd64")
	pushImage(t, c, testutil.RepoURL(s, "d/b")+":1",
		ociImage(t, nil, mutate.Addendum{Layer: plain}), "linux", "amd64")

	out, err := inspect.Diff(ctx, c,
		testutil.RepoURL(s, "d/a")+":1", testutil.RepoURL(s, "d/b")+":1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "shared") || strings.Contains(out, "only in") {
		t.Fatalf("expected all-shared diff:\n%s", out)
	}

	// A different tar yields only-in markers on both sides.
	other := testutil.GzipLayer(t, map[string]string{"b": "y"})
	pushImage(t, c, testutil.RepoURL(s, "d/c")+":1",
		ociImage(t, nil, mutate.Addendum{Layer: other}), "linux", "amd64")
	out, err = inspect.Diff(ctx, c,
		testutil.RepoURL(s, "d/a")+":1", testutil.RepoURL(s, "d/c")+":1")
	if err != nil {
		t.Fatal(err)
	}
	aRef := testutil.RepoURL(s, "d/a") + ":1"
	cRef := testutil.RepoURL(s, "d/c") + ":1"
	if strings.Contains(out, "shared") {
		t.Fatalf("unexpected shared layer:\n%s", out)
	}
	if !strings.Contains(out, "only in "+aRef) || !strings.Contains(out, "only in "+cRef) {
		t.Fatalf("expected +/- markers for both refs:\n%s", out)
	}
}

package resolve

import (
	"context"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/cake"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
)

// pushImage builds an image with the given layers+history and pushes it.
func pushImage(t *testing.T, c interface {
	WriteImage(context.Context, string, v1.Image) error
}, ref string, mt gtypes.MediaType, layers []v1.Layer, history []v1.History, os_, arch string) {
	t.Helper()
	img := empty.Image
	img = mutate.MediaType(img, mt)
	var adds []mutate.Addendum
	for i, l := range layers {
		var h v1.History
		if i < len(history) {
			h = history[i]
		}
		adds = append(adds, mutate.Addendum{Layer: l, History: h})
	}
	var err error
	img, err = mutate.Append(img, adds...)
	if err != nil {
		t.Fatal(err)
	}
	cf, _ := img.ConfigFile()
	cf = cf.DeepCopy()
	cf.OS = os_
	cf.Architecture = arch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
}

func TestResolveBase(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.DockerLayer(t, map[string]string{"bin/sh": "x"})
	ref := testutil.RepoURL(s, "base") + ":1"
	pushImage(t, c, ref, gtypes.DockerManifestSchema2,
		[]v1.Layer{l},
		[]v1.History{{CreatedBy: "/bin/sh -c #(nop) ADD file:ab in /"}},
		"linux", "amd64")
	r, err := Resolve(context.Background(), c, cake.Ref{Ref: ref}, Base,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "base" || len(r.Layers) != 1 {
		t.Fatalf("resolved: %+v", r)
	}
	if _, ok := r.Layers[0].L.(*remote.MountableLayer); !ok {
		t.Fatalf("layer not mountable: %T", r.Layers[0].L)
	}
	if r.Layers[0].Desc.MediaType != gtypes.DockerLayer {
		t.Fatalf("mediaType: %s", r.Layers[0].Desc.MediaType)
	}
	if r.Layers[0].History == nil || r.Layers[0].History.CreatedBy == "" {
		t.Fatal("history not aligned")
	}
}

func TestResolveSlice(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.GzipLayer(t, map[string]string{"models/m/w": "x"})
	ref := testutil.RepoURL(s, "s/w") + ":1"
	img := empty.Image
	img = mutate.MediaType(img, gtypes.OCIManifestSchema1)
	var err error
	img, err = mutate.Append(img, mutate.Addendum{
		Layer: l, Annotations: map[string]string{layer.KeyLayerType: "weight"},
	})
	if err != nil {
		t.Fatal(err)
	}
	img = mutate.Annotations(img, map[string]string{
		layer.KeyKind: "slice", layer.KeyPlatform: "any",
	}).(v1.Image)
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(context.Background(), c, cake.Ref{Ref: ref}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "slice" || !r.Any {
		t.Fatalf("resolved: %+v", r)
	}
}

func TestResolveExternalPureJudgment(t *testing.T) {
	s, c := testutil.Registry(t)
	l1 := testutil.DockerLayer(t, map[string]string{"app/x": "1"})
	l2 := testutil.DockerLayer(t, map[string]string{"app/y": "2"})
	ref := testutil.RepoURL(s, "img") + ":1"
	pushImage(t, c, ref, gtypes.DockerManifestSchema2,
		[]v1.Layer{l1, l2},
		[]v1.History{
			{CreatedBy: "COPY /app/x /app/x # buildkit"},
			{CreatedBy: "RUN make thing"},
		}, "linux", "amd64")
	// Selecting the RUN layer must fail without force.
	_, err := Resolve(context.Background(), c,
		cake.Ref{Ref: ref, Layers: selIdx(1)}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err == nil {
		t.Fatal("expected pure-file rejection")
	}
	// COPY layer is fine.
	r, err := Resolve(context.Background(), c,
		cake.Ref{Ref: ref, Layers: selIdx(0)}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Layers[0].PureFile || !r.Layers[0].External {
		t.Fatalf("layer: %+v", r.Layers[0])
	}
	if r.Layers[0].Desc.Annotations[layer.KeySourceImage] == "" ||
		r.Layers[0].Desc.Annotations[layer.KeySourceIndex] != "0" {
		t.Fatalf("source annotations: %v", r.Layers[0].Desc.Annotations)
	}
	// force passes the RUN layer.
	if _, err := Resolve(context.Background(), c,
		cake.Ref{Ref: ref, Layers: selIdx(1), Force: true}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{}); err != nil {
		t.Fatal(err)
	}
	// Whole image as slice requires layers selection.
	if _, err := Resolve(context.Background(), c, cake.Ref{Ref: ref}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{}); err == nil {
		t.Fatal("expected layers-required error")
	}
}

func TestResolveVerifyWhiteout(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.DockerLayer(t, map[string]string{"etc/.wh.passwd": ""})
	ref := testutil.RepoURL(s, "img") + ":1"
	pushImage(t, c, ref, gtypes.DockerManifestSchema2, []v1.Layer{l},
		[]v1.History{{CreatedBy: "COPY a b"}}, "linux", "amd64")
	_, err := Resolve(context.Background(), c,
		cake.Ref{Ref: ref, Layers: selIdx(0)}, Slice,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{Verify: true})
	if err == nil {
		t.Fatal("expected whiteout rejection")
	}
}

func TestResolveForeignRejected(t *testing.T) {
	s, c := testutil.Registry(t)
	l := testutil.DockerLayer(t, map[string]string{"a": "1"})
	// craft a foreign-layer manifest manually
	img := empty.Image
	img = mutate.MediaType(img, gtypes.DockerManifestSchema2)
	var err error
	img, err = mutate.Append(img, mutate.Addendum{Layer: l, MediaType: gtypes.DockerForeignLayer})
	if err != nil {
		t.Fatal(err)
	}
	cf, _ := img.ConfigFile()
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = "linux", "amd64"
	img, _ = mutate.ConfigFile(img, cf)
	ref := testutil.RepoURL(s, "base") + ":1"
	if err := c.WriteImage(context.Background(), ref, img); err != nil {
		t.Fatal(err)
	}
	_, err = Resolve(context.Background(), c, cake.Ref{Ref: ref}, Base,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err == nil {
		t.Fatal("expected foreign layer rejection")
	}
}

func TestResolveIndexPlatform(t *testing.T) {
	s, c := testutil.Registry(t)
	ctx := context.Background()
	var adds []mutate.IndexAddendum
	for _, arch := range []string{"amd64", "arm64"} {
		l := testutil.DockerLayer(t, map[string]string{"bin/" + arch: "x"})
		ref := testutil.RepoURL(s, "base") + ":tmp-" + arch
		img := empty.Image
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
		_ = ref
	}
	idx := mutate.IndexMediaType(empty.Index, gtypes.OCIImageIndex)
	idx = mutate.AppendManifests(idx, adds...)
	ref := testutil.RepoURL(s, "base") + ":multi"
	if err := c.WriteIndex(ctx, ref, idx); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(ctx, c, cake.Ref{Ref: ref}, Base,
		v1.Platform{OS: "linux", Architecture: "arm64"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.Architecture != "arm64" {
		t.Fatalf("picked wrong platform: %s", r.Config.Architecture)
	}
}

func TestResolveLibc(t *testing.T) {
	s, c := testutil.Registry(t)
	libc := "GNU C Library (Ubuntu GLIBC 2.35-0ubuntu3) stable release version 2.35.\n"
	l := testutil.DockerLayer(t, map[string]string{
		"lib/x86_64-linux-gnu/libc.so.6": libc,
	})
	ref := testutil.RepoURL(s, "base") + ":1"
	pushImage(t, c, ref, gtypes.DockerManifestSchema2, []v1.Layer{l},
		[]v1.History{{CreatedBy: "ADD x /"}}, "linux", "amd64")
	r, err := Resolve(context.Background(), c, cake.Ref{Ref: ref}, Base,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Libc != "glibc 2.35" {
		t.Fatalf("libc %q", r.Libc)
	}
	// Declared libc wins.
	r2, err := Resolve(context.Background(), c, cake.Ref{Ref: ref, Libc: "musl"}, Base,
		v1.Platform{OS: "linux", Architecture: "amd64"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Libc != "musl" {
		t.Fatalf("libc %q", r2.Libc)
	}
}

func selIdx(i int) []cake.LayerSel {
	return []cake.LayerSel{{Index: &i}}
}

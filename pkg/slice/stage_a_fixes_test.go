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
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

// server without referrers API support: falls back to the sha256-<hex> tag index.
func noReferrersEnv(t *testing.T) (*registry.Client, string) {
	t.Helper()
	s := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(s.Close)
	c := registry.New(registry.Options{
		Keychain: authn.NewMultiKeychain(), Insecure: true, Transport: http.DefaultTransport,
	})
	return c, strings.TrimPrefix(s.URL, "http://")
}

func TestReferrersTagFallback(t *testing.T) {
	client, host := noReferrersEnv(t)
	dir := weightDir(t)
	target := host + "/u/w/m:x"
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Dir: dir, Root: "/m", Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MetadataRef == "" {
		t.Fatal("no metadata")
	}
	refs, err := client.Referrers(context.Background(), host+"/u/w/m@"+res.Digest.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Fatal("no referrers found via tag fallback")
	}
}

func TestArtifactCompatReferrers(t *testing.T) {
	client, host := noReferrersEnv(t)
	dir := weightDir(t)
	target := host + "/u/w/m:x"
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Dir: dir, Root: "/m", Target: target, ArtifactCompat: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := client.Referrers(context.Background(), host+"/u/w/m@"+res.Digest.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Fatal("no referrers in compat mode")
	}
}

func TestMergePlatforms(t *testing.T) {
	client, host := testEnv(t)
	target := host + "/u/bin/tool:x"

	packArch := func(goarch string) {
		dir := t.TempDir()
		elf := testutil.BuildLinuxELFArch(t, goarch)
		data, _ := os.ReadFile(elf)
		os.WriteFile(filepath.Join(dir, "tool"), data, 0o755)
		_, err := Pack(context.Background(), client, PackOptions{
			Type: "binary", Dir: dir, Root: "/usr/local/bin", Target: target, Merge: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	packArch("amd64")
	packArch("arm64")

	idx, err := client.Index(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	im, _ := idx.IndexManifest()
	if len(im.Manifests) != 2 {
		t.Fatalf("manifests: %d", len(im.Manifests))
	}
	var platforms []string
	for _, m := range im.Manifests {
		platforms = append(platforms, m.Platform.OS+"/"+m.Platform.Architecture)
	}
	if !(contains(platforms, "linux/amd64") && contains(platforms, "linux/arm64")) {
		t.Fatalf("platforms: %v", platforms)
	}
	// Re-pack amd64: still 2 manifests, no duplicate.
	packArch("amd64")
	idx, err = client.Index(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	im, _ = idx.IndexManifest()
	if len(im.Manifests) != 2 {
		t.Fatalf("after re-pack: %d manifests", len(im.Manifests))
	}
	// The arm64 descriptor must be untouched; amd64 replaced.
	var amd64Descs int
	for _, m := range im.Manifests {
		if m.Platform.Architecture == "amd64" {
			amd64Descs++
		}
	}
	if amd64Descs != 1 {
		t.Fatalf("amd64 manifests: %d", amd64Descs)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

var _ = gtypes.OCIManifestSchema1
var _ = layer.KeyKind

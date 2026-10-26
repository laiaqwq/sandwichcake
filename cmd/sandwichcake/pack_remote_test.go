package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
)

func TestPackWeightRemote(t *testing.T) {
	isolateEnv(t)
	t.Setenv("SANDWICHCAKE_CACHE_DIR", t.TempDir())
	hub := testutil.NewHub(t, "org/Model-7B", []testutil.HubFile{
		{Path: "config.json", Data: []byte(`{}`)},
		{Path: "model.safetensors", Data: []byte("weights"), LFS: true},
		{Path: "original/consolidated.pth", Data: []byte("dup"), LFS: true},
		{Path: "README.md", Data: []byte("readme")},
	})
	t.Setenv("HF_ENDPOINT", hub.API.URL)
	t.Setenv("HF_TOKEN", "hf_cli_token")
	srv, c := testutil.Registry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	insecure := "--insecure-registry=" + host

	project := t.TempDir()
	run(t, "init", project)
	cfg := "apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: " + host + "/one\n  repos: {weight: weights}\n  repoFormat: \"{prefix}/{repo}/{name}\"\n  tagFormat: {weight: \"{rev}\"}\n" +
		"pack:\n  weight: {exclude: [\"original/**\"]}\nmodels:\n  paths: {hf: \"/models/{org}/{name}\"}\n"
	if err := os.WriteFile(filepath.Join(project, "cake-config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	src := hub.API.URL + "/org/Model-7B/tree/main"

	out := run(t, insecure, "--project", project, "pack", "weight", src, "--dry-run")
	if !strings.Contains(out, "dry-run: "+host+"/one/weights/org--model-7b:main (") || strings.Contains(out, "original") {
		t.Fatalf("dry-run: %s", out)
	}
	if hub.CDNRequests != 0 {
		t.Fatalf("dry-run downloaded %d files", hub.CDNRequests)
	}

	keep := filepath.Join(t.TempDir(), "Model-7B")
	out = run(t, insecure, "--project", project, "--registry-prefix", host+"/two", "pack", "weight", src,
		"--include", "*.safetensors", "--include", "*.json", "--keep-dir", keep)
	ref := host + "/two/weights/org--model-7b:main"
	if !strings.Contains(out, "pushed "+ref) {
		t.Fatalf("push: %s", out)
	}
	img, err := c.Image(context.Background(), ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := img.Manifest()
	a := m.Layers[0].Annotations
	if a[layer.KeyLayerRoot] != "/models/org/Model-7B" || a[layer.TypeKey("weight", "source")] != "hf://org/Model-7B@main" || a[layer.TypeKey("weight", "source.commit")] != hub.Commit {
		t.Fatalf("annotations: %v", a)
	}
	for _, p := range []string{"config.json", "model.safetensors"} {
		if _, err := os.Stat(filepath.Join(keep, p)); err != nil {
			t.Fatalf("keep-dir: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(keep, "README.md")); !os.IsNotExist(err) {
		t.Fatal("--include did not filter README.md")
	}
	for _, auth := range hub.CDNAuth {
		if auth != "" {
			t.Fatal("token sent to CDN")
		}
	}

	// Packing the kept dir with the same settings reproduces the digest.
	streamed, _ := img.Digest()
	out = run(t, insecure, "--project", project, "--registry-prefix", host+"/two", "pack", "weight", keep,
		"--source", "hf://org/Model-7B@main", "--source-commit", hub.Commit, "--to", host+"/two/weights/local:1")
	if !strings.Contains(out, streamed.String()) {
		t.Fatalf("local pack of keep-dir differs from streamed %s:\n%s", streamed, out)
	}

	if _, err := runErr(t, "--project", project, "pack", "weight", keep, "--include", "*.json", "--dry-run"); err == nil || !strings.Contains(err.Error(), "remote source") {
		t.Fatalf("--include on a dir: %v", err)
	}
	if _, err := runErr(t, "--project", project, "pack", "weight", src, "--source", "hf://x/y", "--dry-run"); err == nil {
		t.Fatal("--source with remote arg must fail")
	}
}

func TestPackDefaultsRejectIncludeOutsideWeight(t *testing.T) {
	isolateEnv(t)
	project := t.TempDir()
	run(t, "init", project)
	if err := os.WriteFile(filepath.Join(project, "cake-config.yaml"), []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\npack:\n  venv: {include: [\"*\"]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "--project", project, "config", "show"); err == nil || !strings.Contains(err.Error(), "only supported for pack.weight") {
		t.Fatalf("err %v", err)
	}
}

func TestDownloadConfigPrecedence(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HF_ENDPOINT", "")
	t.Setenv("MODELSCOPE_DOMAIN", "")
	run(t, "config", "set", "hf-endpoint", "https://global.example")
	run(t, "config", "set", "download-concurrency", "3")
	project := t.TempDir()
	run(t, "init", project)
	if err := os.WriteFile(filepath.Join(project, "cake-config.local.yaml"), []byte("download: {hf: {endpoint: https://local.example}, segmentSize: 8Mi}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := run(t, "--project", project, "config", "show", "--origin")
	if !strings.Contains(out, "endpoint: https://local.example") || !strings.Contains(out, "# download.concurrency: ") || !strings.Contains(out, "# download.hf.endpoint: "+filepath.Join(project, "cake-config.local.yaml")) {
		t.Fatalf("show: %s", out)
	}
	t.Setenv("HF_ENDPOINT", "https://env.example")
	t.Setenv("MODELSCOPE_DOMAIN", "ms.example")
	out = run(t, "--project", project, "config", "show", "--origin")
	if !strings.Contains(out, "endpoint: https://env.example") || !strings.Contains(out, "endpoint: https://ms.example") || !strings.Contains(out, "# download.hf.endpoint: HF_ENDPOINT") {
		t.Fatalf("env: %s", out)
	}
	if _, err := runErr(t, "config", "set", "download-segment-size", "huge"); err == nil {
		t.Fatal("bad size accepted")
	}
}

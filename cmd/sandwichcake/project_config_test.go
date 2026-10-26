package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
)

func TestProjectDiscoveryAndRefs(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	run(t, "init", dir)
	nested := filepath.Join(dir, "cake", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := findProject(nested); err != nil || got != dir {
		t.Fatalf("project: %q %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cake-config.yaml"), []byte(`apiVersion: sandwichcake.io/v1
kind: CakeConfig
registry:
  prefix: example.com/team
  repos: {venv: venv, weight: weights, image: infer}
  repoFormat: "{prefix}/{repo}"
  tagFormat: {weight: "{type}-{name}-{rev}"}
pack:
  defaults: {compression: zstd, level: 3}
  weight: {root: /models, split: {enabled: false, large: 3Gi}}
models:
  paths: {hf: "/models/{org}/{name}"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir = dir
	defer func() { projectDir = "" }()
	p, _, err := loadProject()
	if err != nil {
		t.Fatal(err)
	}
	if p.Pack.forType("weight").Root != "/models" || *p.Pack.forType("weight").Level != 3 {
		t.Fatalf("defaults not merged: %+v", p.Pack.forType("weight"))
	}
	for input, want := range map[string]string{
		"venv:torch":          "example.com/team/venv:torch",
		"weight:qwen":         "example.com/team/weights:qwen",
		"image:serve":         "example.com/team/infer:serve",
		"example.com/other:1": "example.com/other:1",
		"alpine:3.20":         "alpine:3.20",
	} {
		got, err := p.Registry.expand(input)
		if err != nil || got != want {
			t.Fatalf("expand %s: %s %v", input, got, err)
		}
	}
	if got := lockPath(filepath.Join(dir, "cake", "serve.yaml")); got != filepath.Join(dir, "cake", "serve.lock") {
		t.Fatalf("lock path: %s", got)
	}
	if out := run(t, "--project", dir, "config", "show", "--origin"); !strings.Contains(out, "weights") || !strings.Contains(out, "# registry.*") {
		t.Fatalf("project show: %s", out)
	}
	root, err := modelRoot(p.Models.Paths, "hf://Qwen/Qwen3-8B@main")
	if err != nil || root != "/models/Qwen/Qwen3-8B" {
		t.Fatalf("model root: %q %v", root, err)
	}
}

func TestTagPolicy(t *testing.T) {
	isolateEnv(t)
	srv, _ := testutil.Registry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := host + "/team/other:existing"
	run(t, "--insecure-registry="+host, "pack", "other", dir, "--root", "/data", "--to", ref)
	project := t.TempDir()
	run(t, "init", project)
	cfg := filepath.Join(project, "cake-config.yaml")
	if err := os.WriteFile(cfg, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  onTagExists: fail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "--insecure-registry="+host, "--project", project, "pack", "other", dir, "--root", "/data", "--to", ref); err == nil {
		t.Fatal("fail policy allowed existing tag")
	}
	if err := os.WriteFile(cfg, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  onTagExists: skip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run(t, "--insecure-registry="+host, "--project", project, "pack", "other", dir, "--root", "/data", "--to", ref); !strings.Contains(out, "skipped") {
		t.Fatalf("skip policy: %s", out)
	}
}

func TestProjectConfigIsolation(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	run(t, "init", dir)
	projectDir = dir
	defer func() { projectDir = "" }()
	if err := os.WriteFile(filepath.Join(dir, "cake-config.local.yaml"), []byte("cache:\n  dir: tmp/cache\nupload:\n  concurrency: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, origins, err := effectiveConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.Dir != filepath.Join(dir, "tmp", "cache") || cfg.Upload.Concurrency != 7 || origins["upload.concurrency"] == "" {
		t.Fatalf("effective: %+v %+v", cfg, origins)
	}
	if err := os.WriteFile(filepath.Join(dir, "cake-config.local.yaml"), []byte("pack:\n  weight: {root: /models}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := effectiveConfig(); err == nil {
		t.Fatal("local output settings accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "cake-config.yaml"), []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\npack:\n  weight: {typo: yes}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadProject(); err == nil {
		t.Fatal("unknown pack field accepted")
	}
}

func TestInitMigrateLegacy(t *testing.T) {
	isolateEnv(t)
	path := mustConfigPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("toPrefix: example.com/a\ntagFormat: \"{type}-{name}\"\nroots:\n  weight: /models\ncacheDir: /tmp/old-cache\nrepos: [example.com/a/weight]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("legacy global output settings accepted")
	}
	dir := t.TempDir()
	run(t, "init", dir, "--migrate")
	p, err := os.ReadFile(filepath.Join(dir, "cake-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p), "example.com/a") || !strings.Contains(string(p), "/models") {
		t.Fatalf("migration: %s", p)
	}
	projectDir = dir
	defer func() { projectDir = "" }()
	if _, _, err := loadProject(); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil || cfg.Cache.Dir != "/tmp/old-cache" || len(cfg.Repos) != 1 {
		t.Fatalf("global migration: %+v %v", cfg, err)
	}
}

func TestNamedCakeShortRefsAndLocked(t *testing.T) {
	isolateEnv(t)
	srv, _ := testutil.Registry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	project := t.TempDir()
	run(t, "init", project)
	config := filepath.Join(project, "cake-config.yaml")
	prefix := host + "/team"
	if err := os.WriteFile(config, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: "+prefix+"\n  repos: {image: infer, other: other}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"base", "slice"} {
		dir := filepath.Join(t.TempDir(), tag)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(tag), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, "--insecure-registry="+host, "pack", "other", dir, "--root", "/"+tag, "--to", prefix+"/other:"+tag)
	}
	cakeFile := filepath.Join(project, "cake", "serve.yaml")
	cakeYAML := "apiVersion: sandwichcake.io/v1\nkind: Cake\ntarget: {ref: \"image:serve\"}\nbase: {ref: \"other:base\"}\nslices:\n  - {ref: \"other:slice\"}\n"
	if err := os.WriteFile(cakeFile, []byte(cakeYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	out := run(t, "--insecure-registry="+host, "--project", project, "compose", "serve", "--yes")
	if !strings.Contains(out, prefix+"/infer:serve") {
		t.Fatalf("target: %s", out)
	}
	lock, err := os.ReadFile(filepath.Join(project, "cake", "serve.lock"))
	if err != nil || !strings.Contains(string(lock), "input: "+prefix+"/other:base") {
		t.Fatalf("lock: %s %v", lock, err)
	}
	// A changed prefix cannot reinterpret a locked short reference.
	if err := os.WriteFile(config, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: "+host+"/new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = run(t, "--insecure-registry="+host, "--project", project, "compose", "serve", "--locked", "--yes")
	if !strings.Contains(out, prefix+"/infer:serve") {
		t.Fatalf("locked target changed: %s", out)
	}
}

func TestOCISync(t *testing.T) {
	isolateEnv(t)
	srv, _ := testutil.Registry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	source := "oci://" + host + "/projects/team:latest"
	src := t.TempDir()
	run(t, "init", src)
	data := "apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nfrom: " + source + "\n"
	if err := os.WriteFile(filepath.Join(src, "cake-config.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cake", "app.yaml"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"secret.env", "cake-config.local.yaml"} {
		content := "secret"
		if file == "cake-config.local.yaml" {
			content = "cache: {dir: cache}\n"
		}
		if err := os.WriteFile(filepath.Join(src, file), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(src, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cache", "private.json"), []byte("not synced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".cakeignore"), []byte("credentials[1-9].json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "credentials3.json"), []byte("not synced"), 0o600); err != nil {
		t.Fatal(err)
	}
	remote := "--insecure-registry=" + host
	run(t, remote, "--project", src, "config", "push")
	initialized := t.TempDir()
	run(t, "init", initialized)
	run(t, remote, "--project", initialized, "config", "pull", source)
	collision := filepath.Join(t.TempDir(), "collision")
	if err := os.MkdirAll(filepath.Join(collision, "cake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collision, "cake", "app.yaml"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, remote, "--project", collision, "config", "pull", source); err == nil || !strings.Contains(err.Error(), "untracked local file cake/app.yaml") {
		t.Fatalf("adopted matching local file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(collision, ".cake", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("failed pull wrote state: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "fetched")
	run(t, remote, "--project", dst, "config", "pull", source)
	if _, err := os.Stat(filepath.Join(dst, "cake", "app.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"secret.env", "cake-config.local.yaml"} {
		if _, err := os.Stat(filepath.Join(dst, file)); !os.IsNotExist(err) {
			t.Fatalf("%s synced unexpectedly: %v", file, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "cache", "private.json")); !os.IsNotExist(err) {
		t.Fatalf("local cache synced unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "credentials3.json")); !os.IsNotExist(err) {
		t.Fatalf("bracket-class .cakeignore failed to exclude credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "cake", "app.yaml"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, ".cakeignore"), []byte("cake/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, remote, "--project", dst, "config", "pull"); err == nil {
		t.Fatal("overwrote a locally modified but ignored file")
	}
	run(t, remote, "--project", dst, "config", "pull", "--force")
	if err := os.WriteFile(filepath.Join(src, "cake", "app.yaml"), []byte("new remote"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, remote, "--project", src, "config", "push")
	if _, err := runErr(t, remote, "--project", dst, "config", "push"); err == nil {
		t.Fatal("overwrote changed remote tag")
	}
}

func TestSyncPathSafety(t *testing.T) {
	for _, p := range []string{"../x", "/etc/passwd", "x/../y", "x/.git/config", "x/.cake/state.json", "secret.env", "cake-config.local.yaml"} {
		if validSyncPath(p) {
			t.Fatalf("unsafe path allowed: %s", p)
		}
	}
	if _, err := parseGitSource("git+https://example.com/team.git@main#../x"); err == nil {
		t.Fatal("unsafe git subdir")
	}
	if s, err := parseGitSource("git+ssh://git@example.com/team.git@main#projects/demo"); err != nil || s.Subdir != "projects/demo" {
		t.Fatalf("git source: %+v %v", s, err)
	}
	if !ignoredSyncPath("cake/tmp/test.yaml", []string{"cake/**"}) ||
		ignoredSyncPath("cake/live.yaml", []string{"cake/**", "!cake/live.yaml"}) ||
		!ignoredSyncPath("cake/live.yaml", []string{"cake/**", "!cake/live.yaml", "cake/live.yaml"}) {
		t.Fatal("ignore negation/ordering broken")
	}
	for _, tc := range []struct {
		path, pattern string
		match         bool
	}{
		{"credentials3.json", "credentials[1-9].json", true},
		{"credentialsA.json", "credentials[1-9].json", false},
		{"credentialsA.json", "credentials[!1-9].json", true},
		{"credentials3.json", "credentials[[:digit:]].json", true},
		{"模型3.json", "模型[1-9].json", true},
	} {
		if got := ignoreMatch(tc.path, tc.pattern); got != tc.match {
			t.Errorf("ignoreMatch(%q, %q) = %v; want %v", tc.path, tc.pattern, got, tc.match)
		}
	}
}

func TestGitSyncBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateEnv(t)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	base := t.TempDir()
	bare := filepath.Join(base, "team.git")
	ctx := context.Background()
	if _, err := gitExec(ctx, "", "init", "--bare", "--initial-branch=main", bare); err != nil {
		t.Fatal(err)
	}
	url := "ssh://git@example.test/team.git"
	source := "git+" + url + "@main#projects/demo"
	global := filepath.Join(base, "gitconfig")
	if _, err := gitExec(ctx, "", "config", "--file", global, "url.file://"+bare+".insteadOf", url); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	seed := filepath.Join(base, "seed")
	if _, err := gitExec(ctx, "", "clone", "--quiet", bare, seed); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, "projects", "demo", "cake"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nfrom: " + source + "\n")
	if err := os.WriteFile(filepath.Join(seed, "projects", "demo", "cake-config.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "projects", "demo", "cake", "base.yaml"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "commit", "-qm", "initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "push", "-q", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}

	local := filepath.Join(base, "local")
	run(t, "--project", local, "config", "pull", source)
	ref, err := parseGitSource(source)
	if err != nil {
		t.Fatal(err)
	}
	clone, _, err := gitClone(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	ghost := filepath.Join(clone, "projects", "demo", "cake", "ghost.yaml")
	if err := os.MkdirAll(filepath.Dir(ghost), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghost, []byte("untracked cache content"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--project", local, "config", "pull")
	if _, err := os.Stat(filepath.Join(local, "cake", "ghost.yaml")); !os.IsNotExist(err) {
		t.Fatalf("cached clone's untracked file synced: %v", err)
	}
	if err := os.WriteFile(filepath.Join(local, "cake", "serve.yaml"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--project", local, "config", "push", "--new-branch", "review")
	if branch, err := gitExec(ctx, seed, "ls-remote", "--heads", "origin", "refs/heads/review"); err != nil || !strings.Contains(branch, "refs/heads/review") {
		t.Fatalf("new branch: %s %v", branch, err)
	}
	run(t, "--project", local, "config", "push")
	if _, err := gitExec(ctx, seed, "pull", "--ff-only", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(seed, "projects", "demo", "cake", "ghost.yaml")); !os.IsNotExist(err) {
		t.Fatalf("cached clone's untracked file pushed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seed, "projects", "demo", "cake", "else.yaml"), []byte("upstream"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "commit", "-qm", "upstream"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, seed, "push", "-q", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "cake", "serve.yaml"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "--project", local, "config", "push"); err == nil {
		t.Fatal("overwrote changed git head")
	}
	if _, err := gitExec(ctx, clone, "add", "-f", "--", "projects/demo/cake/ghost.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "--project", local, "config", "push", "--new-branch", "staged-leak"); err == nil || !strings.Contains(err.Error(), "staged changes") {
		t.Fatalf("staged unrelated content was not rejected: %v", err)
	}
}

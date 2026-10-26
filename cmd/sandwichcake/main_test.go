package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
)

func run(t *testing.T, args ...string) string {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestConfigDefaults(t *testing.T) {
	s, _ := testutil.Registry(t)
	host := strings.TrimPrefix(s.URL, "http://")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SANDWICHCAKE_CACHE_DIR", "")
	insecure := "--insecure-registry=" + host
	configFile, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	run(t, "config", "init", "--global")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatal(err)
	}
	root := newRoot()
	root.SetArgs([]string{"config", "init", "--global"})
	if err := root.Execute(); !os.IsExist(err) {
		t.Fatalf("init overwrote config: %v", err)
	}
	cachePath := filepath.Join(t.TempDir(), "metadata")
	run(t, "config", "set", "cache-dir", cachePath)
	run(t, "config", "set", "repos", host+"/s/other")
	project := t.TempDir()
	run(t, "init", project)
	t.Setenv("SANDWICHCAKE_TEST_PROJECT", project)
	config := filepath.Join(project, "cake-config.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: "+host+"/s\npack:\n  other: {root: /etc/app}\n  weight: {root: /models}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := run(t, insecure, "--project", project, "pack", "other", dir)
	ref := host + "/s/other:" + filepath.Base(dir)
	if !strings.Contains(out, ref) {
		t.Fatalf("auto target: %s", out)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("saved cache directory was not used: %v", err)
	}
	out = run(t, insecure, "ls")
	if !strings.Contains(out, ref) {
		t.Fatalf("configured repos: %s", out)
	}
	if err := os.WriteFile(config, []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: "+host+"/s\n  repos: {weight: weights}\n  tagFormat: {weight: \"{type}-{name}-{rev}\"}\npack:\n  other: {root: /etc/app}\n  weight: {root: /models}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = run(t, insecure, "--project", project, "pack", "weight", dir, "--source", "hf://team/model@v2")
	if !strings.Contains(out, host+"/s/weights:weight-team--model-v2") {
		t.Fatalf("weight target: %s", out)
	}
	out = run(t, insecure, "pack", "other", dir, "--to", host+"/explicit:latest", "--root", "/override")
	if !strings.Contains(out, host+"/explicit:latest") {
		t.Fatalf("explicit target: %s", out)
	}
	run(t, "config", "set", "repos", host+"/s/other,"+host+"/s/team--model")
	cfg, err := loadConfig()
	if err != nil || len(cfg.Repos) != 2 {
		t.Fatalf("config after unset: %+v, %v", cfg, err)
	}
}

func TestConfigValidationAndNaming(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := cliConfig{}
	if err := cfg.set("tag-format", "{missing}"); err == nil {
		t.Fatal("unknown tag placeholder accepted")
	}
	if err := cfg.set("root.venv", "relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	if err := cfg.set("unknown", "value"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := os.MkdirAll(filepath.Dir(mustConfigPath(t)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustConfigPath(t), []byte("invalid: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown YAML key accepted")
	}
}

func mustConfigPath(t *testing.T) string {
	t.Helper()
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLISmoke(t *testing.T) {
	s, _ := testutil.Registry(t)
	host := strings.TrimPrefix(s.URL, "http://")
	t.Setenv("SANDWICHCAKE_CACHE_DIR", t.TempDir())
	insecure := "--insecure-registry=" + host

	// pack other
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "c.conf"), []byte("x"), 0o644)
	out := run(t, insecure, "pack", "other", dir, "--root", "/etc/app", "--to", host+"/s/conf:1")
	if !strings.Contains(out, "pushed") {
		t.Fatalf("pack output: %s", out)
	}

	// types
	out = run(t, insecure, "types")
	for _, typ := range []string{"venv", "weight", "binary", "patch", "other"} {
		if !strings.Contains(out, typ) {
			t.Fatalf("types missing %s: %s", typ, out)
		}
	}

	// inspect --json
	out = run(t, insecure, "inspect", host+"/s/conf:1", "--json")
	if !strings.Contains(out, "\"kind\": \"slice\"") && !strings.Contains(out, "slice") {
		t.Fatalf("inspect: %s", out)
	}

	// inspect --file would fail for 'other' (no metadata) — skip.

	// base image via pack (as stand-in base) then compose
	baseDir := t.TempDir()
	os.WriteFile(filepath.Join(baseDir, "sh"), []byte("#!/bin/sh"), 0o755)
	run(t, insecure, "pack", "other", baseDir, "--root", "/bin", "--to", host+"/base:1")
	// cake.yaml
	cakeYAML := fmt.Sprintf(`
apiVersion: sandwichcake.io/v1
kind: Cake
target:
  ref: %s/app:v1
base:
  ref: %s/base:1
slices:
  - ref: %s/s/conf:1
`, host, host, host)
	workdir := t.TempDir()
	os.WriteFile(filepath.Join(workdir, "cake.yaml"), []byte(cakeYAML), 0o644)
	out = run(t, insecure, "compose", "-f", filepath.Join(workdir, "cake.yaml"), "--yes")
	if !strings.Contains(out, "pushed") {
		t.Fatalf("compose: %s", out)
	}
	if _, err := os.Stat(filepath.Join(workdir, "cake.lock")); err != nil {
		t.Fatal("cake.lock not written")
	}
	// inspect composed
	out = run(t, insecure, "inspect", host+"/app:v1")
	if !strings.Contains(out, "/s/conf") {
		t.Fatalf("inspect app: %s", out)
	}
	// rm
	out = run(t, insecure, "rm", host+"/s/conf:1")
	if !strings.Contains(out, "deleted") {
		t.Fatalf("rm: %s", out)
	}
}

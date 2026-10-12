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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// isolateEnv points HOME, XDG dirs and the cache-dir env at fresh temp
// dirs so tests never touch the real user config or cache. No t.Parallel
// — these tests manipulate globals/env.
func isolateEnv(t *testing.T) string {
	t.Helper()
	old := projectDir
	projectDir = ""
	t.Cleanup(func() { projectDir = old })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("SANDWICHCAKE_CACHE_DIR", "")
	return home
}

func runErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestConfigPath(t *testing.T) {
	home := isolateEnv(t)

	p, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "sandwichcake", "config.yaml"); p != want {
		t.Fatalf("HOME fallback: got %q want %q", p, want)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	p, err = configPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "sandwichcake", "config.yaml"); p != want {
		t.Fatalf("XDG_CONFIG_HOME: got %q want %q", p, want)
	}
}

func TestLoadConfig(t *testing.T) {
	isolateEnv(t)
	path := mustConfigPath(t)

	// Missing file -> empty config, no error.
	cfg, err := loadConfig()
	if err != nil || !reflect.DeepEqual(cfg, cliConfig{}) {
		t.Fatalf("missing file: cfg=%+v err=%v", cfg, err)
	}

	// Empty / whitespace-only file -> empty config, no error.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil || !reflect.DeepEqual(cfg, cliConfig{}) {
		t.Fatalf("empty file: cfg=%+v err=%v", cfg, err)
	}

	// Malformed YAML -> error.
	if err := os.WriteFile(path, []byte("repos: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("malformed YAML accepted")
	}

	// Unknown keys rejected (KnownFields).
	if err := os.WriteFile(path, []byte("bogusKey: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown key accepted")
	}

	// Valid file round-trips.
	if err := os.WriteFile(path, []byte("cache:\n  dir: /tmp/cache\nrepos:\n  - a/b\nupload:\n  concurrency: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.Dir != "/tmp/cache" || len(cfg.Repos) != 1 || cfg.Repos[0] != "a/b" || cfg.Upload.Concurrency != 2 {
		t.Fatalf("loaded cfg: %+v", cfg)
	}
}

func TestSaveConfigCreateOnly(t *testing.T) {
	isolateEnv(t)
	path := mustConfigPath(t)

	cfg := cliConfig{Cache: cacheConfig{Dir: "reg/x"}}
	if err := saveConfig(cfg, true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Second create-only save must refuse and leave content unchanged.
	err = saveConfig(cliConfig{Cache: cacheConfig{Dir: "reg/other"}}, true)
	if !os.IsExist(err) {
		t.Fatalf("createOnly overwrite: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("createOnly modified existing file")
	}

	// Non-createOnly save overwrites atomically.
	if err := saveConfig(cliConfig{Cache: cacheConfig{Dir: "reg/new"}}, false); err != nil {
		t.Fatal(err)
	}
	cfg2, err := loadConfig()
	if err != nil || cfg2.Cache.Dir != "reg/new" {
		t.Fatalf("after overwrite: %+v %v", cfg2, err)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("config mode %o, want 600", fi.Mode().Perm())
		}
		di, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if di.Mode().Perm() != 0o700 {
			t.Fatalf("config dir mode %o, want 700", di.Mode().Perm())
		}
	}
}

func TestConfigSetGetUnset(t *testing.T) {
	var cfg cliConfig

	// repos splits on commas and trims whitespace/empties.
	if err := cfg.set("repos", " a/b , c/d ,, "); err != nil {
		t.Fatal(err)
	}
	if v, _ := cfg.get("repos"); v != "a/b,c/d" {
		t.Fatalf("repos: %q", v)
	}

	for _, key := range []string{"to-prefix", "tag-format", "root.weight"} {
		if err := cfg.set(key, "/models"); err == nil {
			t.Fatalf("output-affecting global key %q accepted", key)
		}
	}

	// cache-dir is a free-form string.
	if err := cfg.set("cache-dir", "/tmp/c"); err != nil {
		t.Fatal(err)
	}
	if v, _ := cfg.get("cache-dir"); v != "/tmp/c" {
		t.Fatalf("cache-dir: %q", v)
	}

	// Unknown keys rejected.
	for _, key := range []string{"nope", "root.", "root"} {
		if err := cfg.set(key, "x"); err == nil {
			t.Fatalf("key %q accepted", key)
		}
	}
}

func TestConfigCLI(t *testing.T) {
	isolateEnv(t)
	path := mustConfigPath(t)

	out := run(t, "config", "init", "--global")
	if !strings.Contains(out, "created") {
		t.Fatalf("init output: %s", out)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "config", "init", "--global"); !os.IsExist(err) {
		t.Fatalf("second init: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("second init modified file: %v", err)
	}

	// set/get/unset round trip via the CLI.
	run(t, "config", "set", "cache-dir", "/tmp/sc-cache")
	if got := run(t, "config", "get", "cache-dir"); got != "/tmp/sc-cache\n" {
		t.Fatalf("get cache-dir: %q", got)
	}
	run(t, "config", "set", "repos", "a/b, c/d")
	if got := run(t, "config", "get", "repos"); got != "a/b,c/d\n" {
		t.Fatalf("get repos: %q", got)
	}
	run(t, "config", "set", "upload-concurrency", "8")
	run(t, "config", "unset", "upload-concurrency")
	if got := run(t, "config", "get", "upload-concurrency"); got != "\n" {
		t.Fatalf("unset upload-concurrency: %q", got)
	}
	if out := run(t, "config", "show", "--origin"); !strings.Contains(out, "dir: /tmp/sc-cache") || !strings.Contains(out, "# cache.dir:") {
		t.Fatalf("show: %s", out)
	}
	if _, err := runErr(t, "config", "set", "bogus", "x"); err == nil {
		t.Fatal("unknown key accepted via CLI")
	}
}

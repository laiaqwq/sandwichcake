package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	stypes "github.com/laiaqwq/sandwichcake/pkg/types"
	"github.com/laiaqwq/sandwichcake/pkg/version"
)

func TestDefaultPackTarget(t *testing.T) {
	other := stypes.Get("other")
	weight := stypes.Get("weight")
	venv := stypes.Get("venv")
	if other == nil || weight == nil || venv == nil {
		t.Fatal("builtin types not registered")
	}

	cases := []struct {
		name              string
		typ               stypes.Type
		dir, lock, prefix string
		tagFormat         string
		typeOpts          map[string]string
		want              string
		wantErr           bool
	}{
		{
			name: "generic other uses dir basename",
			typ:  other, dir: "/tmp/myconf", prefix: "reg.example.com/ai",
			want: "reg.example.com/ai/other:myconf",
		},
		{
			name: "prefix trailing slash trimmed",
			typ:  other, dir: "/tmp/myconf", prefix: "reg.example.com/ai/",
			want: "reg.example.com/ai/other:myconf",
		},
		{
			name: "hf weight default rev",
			typ:  weight, dir: "/tmp/qwen", prefix: "reg.example.com/ai",
			typeOpts: map[string]string{"source": "hf://Qwen/Qwen3-8B"},
			want:     "reg.example.com/ai/qwen--qwen3-8b:latest",
		},
		{
			name: "hf weight explicit rev",
			typ:  weight, dir: "/tmp/qwen", prefix: "reg.example.com/ai",
			typeOpts: map[string]string{"source": "hf://Qwen/Qwen3-8B@v2"},
			want:     "reg.example.com/ai/qwen--qwen3-8b:v2",
		},
		{
			name: "custom tag format",
			typ:  weight, dir: "/tmp/qwen", prefix: "reg.example.com/ai",
			tagFormat: "{type}-{name}-{rev}",
			typeOpts:  map[string]string{"source": "hf://Qwen/Qwen3-8B@v2"},
			want:      "reg.example.com/ai/qwen--qwen3-8b:weight-Qwen3-8B-v2",
		},
		{
			name: "venv lock basename",
			typ:  venv, dir: "/tmp/whatever", lock: "/work/uv.lock",
			prefix: "reg.example.com/ai",
			want:   "reg.example.com/ai/venv:uv",
		},
		{
			name: "malformed hf source",
			typ:  weight, dir: "/tmp/q", prefix: "reg/x",
			typeOpts: map[string]string{"source": "hf://"},
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := defaultPackTarget(tc.typ, tc.dir, tc.lock, tc.prefix, tc.tagFormat, tc.typeOpts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackRootRequired(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No --root and nothing saved: error.
	if _, err := runErr(t, "pack", "other", dir); err == nil ||
		!strings.Contains(err.Error(), "--root required") {
		t.Fatalf("want --root required error, got %v", err)
	}

	project := t.TempDir()
	run(t, "init", project)
	if err := os.WriteFile(filepath.Join(project, "cake-config.yaml"), []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\npack:\n  other: {root: /cfg/root}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runErr(t, "--project", project, "pack", "other", dir, "--dry-run", "--to-prefix", "example.com/a"); err != nil {
		t.Fatalf("project root unused: %v", err)
	}

	// ... but an explicitly empty --root must NOT fall back to config.
	if _, err := runErr(t, "--project", project, "pack", "other", dir, "--root="); err == nil ||
		!strings.Contains(err.Error(), "--root required") {
		t.Fatalf("explicit empty --root fell back: %v", err)
	}
}

func TestPackFlagPrecedence(t *testing.T) {
	s, _ := testutil.Registry(t)
	host := strings.TrimPrefix(s.URL, "http://")
	isolateEnv(t)
	t.Setenv("SANDWICHCAKE_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Configured defaults that flags must beat.
	project := t.TempDir()
	run(t, "init", project)
	if err := os.WriteFile(filepath.Join(project, "cake-config.yaml"), []byte("apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: "+host+"/saved\npack:\n  other: {root: /saved/root}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --to wins over saved to-prefix.
	out := run(t, "--project", project, "pack", "other", dir, "--root", "/o",
		"--to", host+"/explicit:1", "--dry-run")
	if !strings.Contains(out, "dry-run: "+host+"/explicit:1") {
		t.Fatalf("--to did not win: %s", out)
	}
	if strings.Contains(out, "saved") {
		t.Fatalf("config to-prefix leaked: %s", out)
	}

	// --to-prefix wins over saved to-prefix (auto-named).
	out = run(t, "--project", project, "pack", "other", dir, "--root", "/o",
		"--to-prefix", host+"/flagprefix", "--dry-run")
	want := host + "/flagprefix/other:" + filepath.Base(dir)
	if !strings.Contains(out, "dry-run: "+want) {
		t.Fatalf("--to-prefix did not win: want %q in %s", want, out)
	}

	// Saved to-prefix is used when no flag is given.
	out = run(t, "--project", project, "pack", "other", dir, "--root", "/o", "--dry-run")
	want = host + "/saved/other:" + filepath.Base(dir)
	if !strings.Contains(out, "dry-run: "+want) {
		t.Fatalf("saved to-prefix unused: want %q in %s", want, out)
	}
}

func TestCacheDirPrecedence(t *testing.T) {
	base := t.TempDir()
	mk := func(name string) string { return filepath.Join(base, name) }

	setGlobals := func(flagDir string) {
		old := cacheDir
		cacheDir = flagDir
		t.Cleanup(func() { cacheDir = old })
	}
	cfgCache := mk("cfgcache")
	writeCfg := func() {
		if err := saveConfig(cliConfig{Cache: cacheConfig{Dir: cfgCache}}, false); err != nil {
			t.Fatal(err)
		}
	}

	// flag > env > config.
	t.Run("flag wins", func(t *testing.T) {
		isolateEnv(t)
		writeCfg()
		setGlobals(mk("flagcache"))
		t.Setenv("SANDWICHCAKE_CACHE_DIR", mk("envcache"))
		if _, err := client(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(mk("flagcache")); err != nil {
			t.Fatalf("flag dir not created: %v", err)
		}
		for _, d := range []string{mk("envcache"), mk("cfgcache")} {
			if _, err := os.Stat(d); !os.IsNotExist(err) {
				t.Fatalf("%s should not exist", d)
			}
		}
	})

	// env > config (config not even read).
	t.Run("env wins", func(t *testing.T) {
		isolateEnv(t)
		writeCfg()
		setGlobals("")
		t.Setenv("SANDWICHCAKE_CACHE_DIR", mk("envcache"))
		if _, err := client(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(mk("envcache")); err != nil {
			t.Fatalf("env dir not created: %v", err)
		}
		if _, err := os.Stat(mk("cfgcache")); !os.IsNotExist(err) {
			t.Fatal("config cache dir should not exist")
		}
	})

	// config when flag/env unset.
	t.Run("config wins over xdg", func(t *testing.T) {
		isolateEnv(t)
		writeCfg()
		setGlobals("")
		t.Setenv("XDG_CACHE_HOME", mk("xdg"))
		if _, err := client(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(mk("cfgcache")); err != nil {
			t.Fatalf("config cache dir not created: %v", err)
		}
		if _, err := os.Stat(mk("xdg")); !os.IsNotExist(err) {
			t.Fatal("xdg cache should not exist")
		}
	})

	// XDG fallback, then HOME fallback.
	t.Run("xdg fallback", func(t *testing.T) {
		isolateEnv(t)
		setGlobals("")
		t.Setenv("XDG_CACHE_HOME", mk("xdg"))
		if _, err := client(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(mk("xdg"), "sandwichcake")); err != nil {
			t.Fatalf("xdg cache not created: %v", err)
		}
	})
	t.Run("home fallback", func(t *testing.T) {
		home := isolateEnv(t)
		setGlobals("")
		if _, err := client(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(home, ".cache", "sandwichcake")); err != nil {
			t.Fatalf("home cache not created: %v", err)
		}
	})
}

func TestVersionCommand(t *testing.T) {
	isolateEnv(t)
	// A malformed config must not affect version.
	if err := os.MkdirAll(filepath.Dir(mustConfigPath(t)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustConfigPath(t), []byte("bogusKey: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run(t, "version"); out != version.Version+"\n" {
		t.Fatalf("version output %q, want %q", out, version.Version)
	}
}

func TestTypesAndCompletionSmoke(t *testing.T) {
	isolateEnv(t)
	if err := os.MkdirAll(filepath.Dir(mustConfigPath(t)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustConfigPath(t), []byte("bogusKey: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run(t, "types"); !strings.Contains(out, "venv") {
		t.Fatalf("types: %s", out)
	}
	if out := run(t, "completion", "bash"); !strings.Contains(out, "bash") {
		t.Fatalf("completion: %s", out)
	}
}

func TestLsReposOverride(t *testing.T) {
	s, _ := testutil.Registry(t)
	host := strings.TrimPrefix(s.URL, "http://")
	isolateEnv(t)
	t.Setenv("SANDWICHCAKE_CACHE_DIR", t.TempDir())
	insecure := "--insecure-registry=" + host

	// No args and no configured repos -> error.
	if _, err := runErr(t, insecure, "ls"); err == nil ||
		!strings.Contains(err.Error(), "no repos") {
		t.Fatalf("want no-repos error, got %v", err)
	}

	// Push a slice into two repos via pack.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	run(t, insecure, "pack", "other", dir, "--root", "/etc/x",
		"--to", host+"/cfgrepo:s1")
	run(t, insecure, "pack", "other", dir, "--root", "/etc/x",
		"--to", host+"/argrepo:s2")

	// Configured repos are used with no args.
	run(t, "config", "set", "repos", host+"/cfgrepo")
	out := run(t, insecure, "ls")
	if !strings.Contains(out, host+"/cfgrepo:s1") {
		t.Fatalf("configured repo not listed: %s", out)
	}

	// Explicit repos override configured repos.
	out = run(t, insecure, "ls", host+"/argrepo")
	if !strings.Contains(out, host+"/argrepo:s2") {
		t.Fatalf("arg repo not listed: %s", out)
	}
	if strings.Contains(out, "cfgrepo") {
		t.Fatalf("configured repo leaked into explicit ls: %s", out)
	}
}

func TestPackAnnotatesToolVersion(t *testing.T) {
	s, c := testutil.Registry(t)
	host := strings.TrimPrefix(s.URL, "http://")
	isolateEnv(t)
	t.Setenv("SANDWICHCAKE_CACHE_DIR", t.TempDir())
	insecure := "--insecure-registry=" + host

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.conf"), []byte("x"), 0o644)
	ref := host + "/ann:1"
	run(t, insecure, "pack", "other", dir, "--root", "/etc/myapp",
		"--to", ref)

	d, err := c.Get(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	img, err := d.Image()
	if err != nil {
		t.Fatal(err)
	}
	m, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) == 0 {
		t.Fatal("no layers in pushed slice")
	}
	for i, l := range m.Layers {
		if got := l.Annotations[layer.KeyToolVersion]; got != version.Version {
			t.Fatalf("layer %d tool.version = %q, want %q", i, got, version.Version)
		}
		if got := l.Annotations[layer.KeyLayerRoot]; got != "/etc/myapp" {
			t.Fatalf("layer %d root = %q, want /etc/myapp", i, got)
		}
	}
}

package venv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Requires SANDWICHCAKE_TEST_UV=1 and network access to PyPI.
func TestBuildFromLock(t *testing.T) {
	if os.Getenv("SANDWICHCAKE_TEST_UV") != "1" {
		t.Skip("set SANDWICHCAKE_TEST_UV=1 to run")
	}
	dir := t.TempDir()
	// Minimal uv project + lock.
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(
		"[project]\nname = \"t\"\nversion = \"0.1.0\"\nrequires-python = \">=3.11\"\ndependencies = []\n"), 0o644)
	cmd := exec.Command("uv", "lock")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("uv lock: %v\n%s", err, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	venvDir, rw, cleanup, err := BuildFromLock(ctx, filepath.Join(dir, "uv.lock"), "3.12", "/opt/venv")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if rw.To != "/opt/venv" || rw.From != venvDir {
		t.Fatalf("rewrite: %+v venvDir=%s", rw, venvDir)
	}
	for _, rel := range []string{"pyvenv.cfg", "bin/python", ".python/bin/python3"} {
		if _, err := os.Lstat(filepath.Join(venvDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
}

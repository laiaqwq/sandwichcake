package cake

import (
	"testing"
)

const doc = `
apiVersion: sandwichcake.io/v1
kind: Cake
target:
  ref: reg.example.com/app:v1
  platforms: [linux/amd64]
base:
  ref: nvidia/cuda:12.4
slices:
  - ref: reg.example.com/venv:1
    layers: [3, "sha256:abc"]
    platform: any
    force: true
config:
  env:
    VIRTUAL_ENV: /opt/venv
    PATH: { prepend: /opt/venv/bin }
  entrypoint: ["python", "-m", "srv"]
  cmd: []
  labels: { team: ml }
`

func TestParse(t *testing.T) {
	c, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if c.Target.Ref != "reg.example.com/app:v1" {
		t.Fatalf("target %q", c.Target.Ref)
	}
	s := c.Slices[0]
	if s.Layers[0].Index == nil || *s.Layers[0].Index != 3 {
		t.Fatalf("layer sel index: %+v", s.Layers[0])
	}
	if s.Layers[1].Digest != "sha256:abc" {
		t.Fatalf("layer sel digest: %+v", s.Layers[1])
	}
	if !s.Force || s.Platform != "any" {
		t.Fatalf("slice flags: %+v", s)
	}
	if v := c.Config.Env["VIRTUAL_ENV"]; v.Value == nil || *v.Value != "/opt/venv" {
		t.Fatalf("env scalar: %+v", v)
	}
	if v := c.Config.Env["PATH"]; v.Prepend == nil || *v.Prepend != "/opt/venv/bin" {
		t.Fatalf("env prepend: %+v", v)
	}
	if c.Config.Cmd == nil || len(*c.Config.Cmd) != 0 {
		t.Fatalf("cmd should be explicit empty: %+v", c.Config.Cmd)
	}
	if c.Config.Workdir != nil {
		t.Fatalf("workdir should be absent: %+v", c.Config.Workdir)
	}
	keys := c.Config.SortedEnvKeys()
	if keys[0] != "PATH" {
		t.Fatalf("sorted keys: %v", keys)
	}
}

func TestValidate(t *testing.T) {
	bad := `
apiVersion: sandwichcake.io/v1
kind: Cake
target: { ref: r/t:1 }
base: { ref: b:1, layers: [0] }
`
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected base.layers rejection")
	}
	bad2 := `
apiVersion: sandwichcake.io/v1
kind: Cake
target: { ref: r/t:1 }
base: { ref: b:1 }
slices: [{ ref: s:1, libc: musl }]
`
	if _, err := Parse([]byte(bad2)); err == nil {
		t.Fatal("expected libc rejection")
	}
}

func TestLockRoundTrip(t *testing.T) {
	l := &Lock{
		APIVersion: APIVersion, Kind: KindLock,
		Target: LockTarget{Ref: "r/t:1", Digest: "sha256:dd"},
		Base: LockRef{
			Ref: "r/base@sha256:bb",
			Resolved: map[string]ResolvedItem{
				"linux/amd64": {Manifest: "sha256:mm", Layers: []string{"sha256:l1", "sha256:l2"}},
			},
		},
	}
	if _, err := l.Marshal(); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/cake.lock"
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Base.Ref != l.Base.Ref || back.Base.Resolved["linux/amd64"].Layers[1] != "sha256:l2" {
		t.Fatalf("round trip: %+v", back)
	}
	if err := back.Base.CheckLayers("linux/amd64", []string{"sha256:l1", "sha256:l2"}); err != nil {
		t.Fatal(err)
	}
	if err := back.Base.CheckLayers("linux/amd64", []string{"sha256:xx", "sha256:l2"}); err == nil {
		t.Fatal("expected layer mismatch error")
	}
}

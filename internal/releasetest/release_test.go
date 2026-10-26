package releasetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const workflowPath = "../../.github/workflows/release.yml"

type wfStep struct {
	Name string         `yaml:"name"`
	ID   string         `yaml:"id"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

type wfJob struct {
	Needs       any               `yaml:"needs"`
	Outputs     map[string]string `yaml:"outputs"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []wfStep          `yaml:"steps"`
}

type workflow struct {
	On struct {
		Push struct {
			Tags []string `yaml:"tags"`
		} `yaml:"push"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]wfJob  `yaml:"jobs"`
}

func loadWorkflow(t *testing.T) workflow {
	t.Helper()
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("workflow YAML does not parse: %v", err)
	}
	return wf
}

func stepWithID(steps []wfStep, id string) *wfStep {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
	}
	return nil
}

func stepsUsing(steps []wfStep, substr string) []wfStep {
	var out []wfStep
	for _, s := range steps {
		if strings.Contains(s.Uses, substr) {
			out = append(out, s)
		}
	}
	return out
}

// runTagValidation executes the workflow's real Validate tag script for
// tag and reports (exit status, contents of GITHUB_OUTPUT).
func runTagValidation(t *testing.T, script, tag string) (bool, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "github-output")
	if err := os.WriteFile(out, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"GITHUB_REF_NAME="+tag,
		"GITHUB_OUTPUT="+out,
	)
	err := cmd.Run()
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return err == nil, string(data)
}

func TestValidateTagAccepts(t *testing.T) {
	wf := loadWorkflow(t)
	step := stepWithID(wf.Jobs["build"].Steps, "tag")
	if step == nil || step.Run == "" {
		t.Fatal("no Validate tag step with id: tag")
	}
	cases := map[string]string{
		"v0.0.1":                       "false",
		"v1.2.3":                       "false",
		"v1.2.3-rc.1":                  "true",
		"v1.2.3-alpha-01":              "true",
		"v1.2.3+001":                   "false",
		"v1.2.3+build-with-hyphen":     "false",
		"v1.2.3-rc.1+build-with-hyphen": "true",
	}
	for tag, want := range cases {
		ok, output := runTagValidation(t, step.Run, tag)
		if !ok {
			t.Errorf("%s: validation failed", tag)
			continue
		}
		if got := strings.TrimSpace(output); got != "prerelease="+want {
			t.Errorf("%s: GITHUB_OUTPUT = %q, want prerelease=%s", tag, got, want)
		}
	}
}

func TestValidateTagRejects(t *testing.T) {
	wf := loadWorkflow(t)
	step := stepWithID(wf.Jobs["build"].Steps, "tag")
	if step == nil || step.Run == "" {
		t.Fatal("no Validate tag step with id: tag")
	}
	bad := []string{
		"vfoo", "v1.2", "1.2.3",
		"v01.2.3", "v1.02.3", "v1.2.03",
		"v1.2.3-", "v1.2.3-01", "v1.2.3-rc..1",
		"v1.2.3+", "v1.2.3+build..1",
		"v1.2.3/path", "v1.2.3 rc", "v1.2.3\nnext", "v1.2.3;rm -rf",
	}
	for _, tag := range bad {
		ok, output := runTagValidation(t, step.Run, tag)
		if ok {
			t.Errorf("%q: validation succeeded", tag)
		}
		if strings.Contains(output, "prerelease") {
			t.Errorf("%q: GITHUB_OUTPUT polluted on failure: %q", tag, output)
		}
	}
}

func TestWorkflowContract(t *testing.T) {
	wf := loadWorkflow(t)

	// Trigger: tag pushes only, v* pattern.
	if len(wf.On.Push.Tags) != 1 || wf.On.Push.Tags[0] != "v*" {
		t.Errorf("push tags = %v, want [v*]", wf.On.Push.Tags)
	}

	// Least privilege: read at top level, write only on release.
	if wf.Permissions["contents"] != "read" {
		t.Errorf("top-level contents permission = %q, want read", wf.Permissions["contents"])
	}
	build, ok := wf.Jobs["build"]
	if !ok {
		t.Fatal("no build job")
	}
	if build.Permissions["contents"] == "write" {
		t.Error("build job must not hold contents: write")
	}
	release, ok := wf.Jobs["release"]
	if !ok {
		t.Fatal("no release job")
	}
	if release.Permissions["contents"] != "write" {
		t.Errorf("release job contents permission = %q, want write", release.Permissions["contents"])
	}
	needs, _ := release.Needs.(string)
	if needs != "build" {
		t.Errorf("release needs = %v, want build", release.Needs)
	}

	// setup-go pins go.mod and caches modules.
	foundSetup := false
	for _, s := range stepsUsing(build.Steps, "actions/setup-go@") {
		foundSetup = true
		if s.With["go-version-file"] != "go.mod" {
			t.Errorf("setup-go go-version-file = %v", s.With["go-version-file"])
		}
	}
	if !foundSetup {
		t.Error("build job does not use actions/setup-go")
	}

	// Artifact names agree between upload and download.
	var upload, download string
	for _, s := range stepsUsing(build.Steps, "actions/upload-artifact@") {
		upload, _ = s.With["name"].(string)
	}
	for _, s := range stepsUsing(release.Steps, "actions/download-artifact@") {
		download, _ = s.With["name"].(string)
	}
	if upload == "" || upload != download {
		t.Errorf("artifact names differ: upload %q vs download %q", upload, download)
	}

	// Prerelease wiring: build output -> release action input.
	if !strings.Contains(build.Outputs["prerelease"], "steps.tag.outputs.prerelease") {
		t.Errorf("build.outputs.prerelease = %q", build.Outputs["prerelease"])
	}
	foundRel := false
	for _, s := range stepsUsing(release.Steps, "action-gh-release@") {
		foundRel = true
		if s.With["tag_name"] == "" || s.With["files"] == "" {
			t.Error("release step missing tag_name/files")
		}
		pre, _ := s.With["prerelease"].(string)
		if !strings.Contains(pre, "needs.build.outputs.prerelease") {
			t.Errorf("release prerelease = %q, want needs.build wiring", pre)
		}
	}
	if !foundRel {
		t.Error("release job does not use action-gh-release")
	}

	// Build targets and test gate present.
	var buildScript, testScript string
	for _, s := range build.Steps {
		if strings.Contains(s.Name, "Build release") {
			buildScript = s.Run
		}
		if s.Name == "Test" {
			testScript = s.Run
		}
	}
	for _, target := range []string{"darwin/arm64", "linux/arm64", "linux/amd64"} {
		if !strings.Contains(buildScript, target) {
			t.Errorf("build script missing target %s", target)
		}
	}
	if !strings.Contains(testScript, "go vet ./...") ||
		!strings.Contains(testScript, "go test -count=1 ./...") {
		t.Errorf("test gate script unexpected:\n%s", testScript)
	}

	// Every run block must be valid bash.
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if s.Run == "" {
				continue
			}
			f := filepath.Join(t.TempDir(), "step.sh")
			if err := os.WriteFile(f, []byte(s.Run), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("bash", "-n", f).CombinedOutput(); err != nil {
				t.Errorf("run block of %q fails bash -n: %v\n%s", s.Name, err, out)
			}
		}
	}
}

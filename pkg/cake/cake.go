// Package cake defines the cake.yaml / cake.lock schemas (DESIGN 4.1).
// It performs validation and file read/write only; resolution lives in
// pkg/resolve.
package cake

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "sandwichcake.io/v1"
	KindCake   = "Cake"
	KindLock   = "CakeLock"
)

// Cake is a parsed cake.yaml.
type Cake struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Target     Target      `yaml:"target"`
	Base       Ref         `yaml:"base"`
	Slices     []Ref       `yaml:"slices,omitempty"`
	Config     ConfigPatch `yaml:"config,omitempty"`
}

// Target is where the composed image goes.
type Target struct {
	Ref         string            `yaml:"ref"`
	Platforms   []string          `yaml:"platforms,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// Ref references a slice or image.
type Ref struct {
	Ref      string     `yaml:"ref"`
	Layers   []LayerSel `yaml:"layers,omitempty"`
	Platform string     `yaml:"platform,omitempty"` // "linux/amd64" or "any"
	Force    bool       `yaml:"force,omitempty"`
	Verify   bool       `yaml:"verify,omitempty"`
	Name     string     `yaml:"name,omitempty"`
	Libc     string     `yaml:"libc,omitempty"` // base only: "glibc 2.35" | "musl"
	// Display is the human ref used for annotations (e.g. base.name); set
	// when Ref is a locked digest but the original tag should be kept.
	Display string `yaml:"-"`
}

// LayerSel selects one layer by index or digest.
// YAML form: `3` or `sha256:...`.
type LayerSel struct {
	Index  *int   `yaml:"-"`
	Digest string `yaml:"-"`
}

// UnmarshalYAML accepts a scalar int or "sha256:...".
func (l *LayerSel) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("cake: layers entries must be int or digest string")
	}
	if n, err := strconv.Atoi(node.Value); err == nil {
		l.Index = &n
		return nil
	}
	l.Digest = node.Value
	return nil
}

// MarshalYAML writes index or digest form.
func (l LayerSel) MarshalYAML() (any, error) {
	if l.Index != nil {
		return *l.Index, nil
	}
	return l.Digest, nil
}

// EnvValue is a scalar value or {prepend|append: ...}.
type EnvValue struct {
	Value   *string `yaml:"-"`
	Prepend *string `yaml:"-"`
	Append  *string `yaml:"-"`
}

// UnmarshalYAML accepts `KEY: value` or `KEY: {prepend: v}` / `{append: v}`.
func (e *EnvValue) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		v := node.Value
		e.Value = &v
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			k, v := node.Content[i].Value, node.Content[i+1].Value
			switch k {
			case "prepend":
				e.Prepend = &v
			case "append":
				e.Append = &v
			default:
				return fmt.Errorf("cake: unknown env modifier %q", k)
			}
		}
		return nil
	}
	return fmt.Errorf("cake: bad env value")
}

// MarshalYAML emits scalar form for Value, mapping for prepend/append.
func (e EnvValue) MarshalYAML() (any, error) {
	if e.Value != nil {
		return *e.Value, nil
	}
	m := map[string]string{}
	if e.Prepend != nil {
		m["prepend"] = *e.Prepend
	}
	if e.Append != nil {
		m["append"] = *e.Append
	}
	return m, nil
}

// ConfigPatch overrides parts of the OCI config (DESIGN 4.2).
type ConfigPatch struct {
	Env        map[string]EnvValue `yaml:"env,omitempty"`
	Entrypoint *[]string           `yaml:"entrypoint,omitempty"`
	Cmd        *[]string           `yaml:"cmd,omitempty"`
	Workdir    *string             `yaml:"workdir,omitempty"`
	User       *string             `yaml:"user,omitempty"`
	Labels     map[string]string   `yaml:"labels,omitempty"`
}

// SortedEnvKeys returns env keys in deterministic (sorted) order.
func (c ConfigPatch) SortedEnvKeys() []string {
	var keys []string
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Validate checks the Cake structure.
func (c *Cake) Validate() error {
	if c.APIVersion != APIVersion {
		return fmt.Errorf("cake: apiVersion must be %q, got %q", APIVersion, c.APIVersion)
	}
	if c.Kind != KindCake {
		return fmt.Errorf("cake: kind must be %q, got %q", KindCake, c.Kind)
	}
	if c.Target.Ref == "" {
		return fmt.Errorf("cake: target.ref is required")
	}
	if c.Base.Ref == "" {
		return fmt.Errorf("cake: base.ref is required")
	}
	if len(c.Base.Layers) > 0 || c.Base.Force {
		return fmt.Errorf("cake: base does not allow layers/force")
	}
	for i, s := range c.Slices {
		if s.Ref == "" {
			return fmt.Errorf("cake: slices[%d].ref is required", i)
		}
		if s.Libc != "" {
			return fmt.Errorf("cake: libc only allowed on base")
		}
	}
	return nil
}

// Parse reads and validates a cake.yaml.
func Parse(data []byte) (*Cake, error) {
	var c Cake
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Load reads a cake.yaml file.
func Load(path string) (*Cake, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

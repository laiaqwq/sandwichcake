package cake

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Lock is a cake.lock: all refs pinned to digests plus resolved layer lists.
type Lock struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Target     LockTarget  `yaml:"target"`
	Base       LockRef     `yaml:"base"`
	Slices     []LockRef   `yaml:"slices,omitempty"`
	Config     ConfigPatch `yaml:"config,omitempty"`
}

// LockTarget pins the final output digest.
type LockTarget struct {
	Ref       string   `yaml:"ref"`
	Digest    string   `yaml:"digest"` // final index/manifest digest
	Platforms []string `yaml:"platforms,omitempty"`
}

// LockRef is a Ref with digest resolution results.
type LockRef struct {
	Ref      string                  `yaml:"ref"`             // repo@digest the input ref resolved to (index or manifest)
	Input    string                  `yaml:"input,omitempty"` // original tag ref
	Platform string                  `yaml:"platform,omitempty"`
	Name     string                  `yaml:"name,omitempty"`
	Layers   []LayerSel              `yaml:"layers,omitempty"` // original selection
	Force    bool                    `yaml:"force,omitempty"`
	Verify   bool                    `yaml:"verify,omitempty"`
	Libc     string                  `yaml:"libc,omitempty"` // base only
	Resolved map[string]ResolvedItem `yaml:"resolved"`       // key: "linux/amd64" or "any"
}

// ResolvedItem records the selected manifest and its layer digests.
type ResolvedItem struct {
	Manifest string   `yaml:"manifest"`
	Layers   []string `yaml:"layers"`
}

// Write serializes the lock deterministically.
func (l *Lock) Marshal() ([]byte, error) {
	return yaml.Marshal(l)
}

// Save writes cake.lock.
func (l *Lock) Save(path string) error {
	data, err := l.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadLock reads a cake.lock.
func LoadLock(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Lock
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return nil, err
	}
	if l.Kind != KindLock {
		return nil, fmt.Errorf("cake: kind must be %q, got %q", KindLock, l.Kind)
	}
	return &l, nil
}

// CheckLayers verifies a freshly resolved layer list matches the lock.
func (r LockRef) CheckLayers(key string, layers []string) error {
	item, ok := r.Resolved[key]
	if !ok {
		return fmt.Errorf("cake: lock missing resolved entry %q for %s", key, r.Ref)
	}
	if len(item.Layers) != len(layers) {
		return fmt.Errorf("cake: locked layer count differs for %s", r.Ref)
	}
	for i, d := range item.Layers {
		if layers[i] != d {
			return fmt.Errorf("cake: locked layer %d differs for %s", i, r.Ref)
		}
	}
	return nil
}

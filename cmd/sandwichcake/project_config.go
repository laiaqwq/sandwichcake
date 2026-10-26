package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type projectConfig struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	From       string         `yaml:"from,omitempty"`
	Registry   registryConfig `yaml:"registry"`
	Pack       packConfig     `yaml:"pack,omitempty"`
	Models     modelConfig    `yaml:"models,omitempty"`
}

type registryConfig struct {
	Prefix      string            `yaml:"prefix"`
	Repos       map[string]string `yaml:"repos,omitempty"`
	RepoFormat  string            `yaml:"repoFormat,omitempty"`
	TagFormat   map[string]string `yaml:"tagFormat,omitempty"`
	OnTagExists string            `yaml:"onTagExists,omitempty"`
}

type packConfig struct {
	Defaults packDefault            `yaml:"defaults,omitempty"`
	Types    map[string]packDefault `yaml:"-"`
}

type packDefault struct {
	Root        string       `yaml:"root,omitempty"`
	Compression string       `yaml:"compression,omitempty"`
	Level       *int         `yaml:"level,omitempty"`
	Split       *splitConfig `yaml:"split,omitempty"`
	Owner       string       `yaml:"owner,omitempty"`
	Chmod       string       `yaml:"chmod,omitempty"`
}

type splitConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty"`
	Large   string `yaml:"large,omitempty"`
	Small   string `yaml:"small,omitempty"`
}

type modelConfig struct {
	Paths map[string]string `yaml:"paths,omitempty"`
}

// Type-specific pack settings use the same field schema as pack.defaults.
func (p *packConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("pack must be a mapping")
	}
	p.Types = make(map[string]packDefault)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		var value packDefault
		raw, err := yaml.Marshal(node.Content[i+1])
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("pack.%s: %w", key, err)
		}
		if key == "defaults" {
			p.Defaults = value
		} else if validPackType(key) {
			p.Types[key] = value
		} else {
			return fmt.Errorf("unknown pack type %q", key)
		}
	}
	return nil
}

func (p packConfig) MarshalYAML() (any, error) {
	m := map[string]packDefault{}
	m["defaults"] = p.Defaults
	for key, value := range p.Types {
		m[key] = value
	}
	return m, nil
}

func validPackType(s string) bool {
	switch s {
	case "venv", "weight", "binary", "patch", "other":
		return true
	}
	return false
}

func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func findProject(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "cake-config.yaml")); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func currentProject() (string, error) {
	if projectDir != "" {
		dir, err := filepath.Abs(projectDir)
		if err != nil {
			return "", err
		}
		return dir, nil
	}
	return findProject(".")
}

func loadProject() (projectConfig, string, error) {
	var cfg projectConfig
	dir, err := currentProject()
	if err != nil || dir == "" {
		return cfg, dir, err
	}
	path := filepath.Join(dir, "cake-config.yaml")
	if err := readYAML(path, &cfg); err != nil {
		return cfg, dir, err
	}
	if cfg.APIVersion != "sandwichcake.io/v1" || cfg.Kind != "CakeConfig" {
		return cfg, dir, fmt.Errorf("%s: expected apiVersion sandwichcake.io/v1 and kind CakeConfig", path)
	}
	if cfg.Registry.Prefix != "" {
		cfg.Registry.Prefix = strings.TrimSuffix(cfg.Registry.Prefix, "/")
	}
	for kind, repo := range cfg.Registry.Repos {
		if !validPackType(kind) && kind != "image" {
			return cfg, dir, fmt.Errorf("%s: unknown registry.repos key %q", path, kind)
		}
		if repo == "" || strings.ContainsAny(repo, ":@") || strings.HasPrefix(repo, "/") || strings.Contains(repo, "..") {
			return cfg, dir, fmt.Errorf("%s: invalid registry.repos.%s", path, kind)
		}
	}
	if cfg.Registry.OnTagExists != "" && cfg.Registry.OnTagExists != "fail" && cfg.Registry.OnTagExists != "overwrite" && cfg.Registry.OnTagExists != "skip" {
		return cfg, dir, fmt.Errorf("%s: invalid registry.onTagExists %q", path, cfg.Registry.OnTagExists)
	}
	if err := validateTemplate(cfg.Registry.RepoFormat, "prefix", "repo", "name", "type"); err != nil {
		return cfg, dir, fmt.Errorf("%s: registry.repoFormat: %w", path, err)
	}
	for kind, tag := range cfg.Registry.TagFormat {
		if kind != "default" && kind != "image" && !validPackType(kind) {
			return cfg, dir, fmt.Errorf("%s: unknown registry.tagFormat key %q", path, kind)
		}
		if err := validateTemplate(tag, "name", "type", "rev"); err != nil {
			return cfg, dir, fmt.Errorf("%s: registry.tagFormat.%s: %w", path, kind, err)
		}
	}
	for kind, opts := range cfg.Pack.Types {
		if err := validatePack(opts); err != nil {
			return cfg, dir, fmt.Errorf("%s: pack.%s: %w", path, kind, err)
		}
	}
	if err := validatePack(cfg.Pack.Defaults); err != nil {
		return cfg, dir, fmt.Errorf("%s: pack.defaults: %w", path, err)
	}
	for provider, path := range cfg.Models.Paths {
		if provider != "hf" && provider != "modelscope" {
			return cfg, dir, fmt.Errorf("unknown models.paths provider %q", provider)
		}
		if err := validateTemplate(path, "org", "name", "rev"); err != nil {
			return cfg, dir, err
		}
		if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return cfg, dir, fmt.Errorf("%s: models.paths.%s must be an absolute container path", dir, provider)
		}
	}
	return cfg, dir, nil
}

func validatePack(p packDefault) error {
	if p.Root != "" && !strings.HasPrefix(p.Root, "/") {
		return fmt.Errorf("root must be an absolute container path")
	}
	if p.Compression != "" && p.Compression != "gzip" && p.Compression != "zstd" && p.Compression != "none" {
		return fmt.Errorf("unknown compression %q", p.Compression)
	}
	return nil
}

var templateVariable = regexp.MustCompile(`\{[^{}]*\}`)

func validateTemplate(s string, keys ...string) error {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	rest := templateVariable.ReplaceAllStringFunc(s, func(match string) string {
		if !allowed[strings.Trim(match, "{}")] {
			return match
		}
		return ""
	})
	if strings.ContainsAny(rest, "{}") {
		return fmt.Errorf("unsupported template variable in %q", s)
	}
	return nil
}

func (r registryConfig) repo(kind, name string) (string, error) {
	if r.Prefix == "" {
		return "", fmt.Errorf("registry.prefix required for short refs and auto targets")
	}
	repo := r.Repos[kind]
	if repo == "" {
		repo = kind
	}
	format := r.RepoFormat
	if format == "" {
		format = "{prefix}/{repo}"
	}
	return strings.NewReplacer("{prefix}", r.Prefix, "{repo}", repo, "{name}", name, "{type}", kind).Replace(format), nil
}

func (r registryConfig) expand(ref string) (string, error) {
	kind, tag, ok := strings.Cut(ref, ":")
	if !ok || strings.Contains(kind, "/") || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`).MatchString(kind) {
		return ref, nil
	}
	// An ordinary Docker Hub ref such as alpine:3.20 must keep working.
	if !validPackType(kind) && kind != "image" {
		return ref, nil
	}
	if tag == "" {
		return "", fmt.Errorf("empty short ref tag %q", ref)
	}
	repo, err := r.repo(kind, tag)
	return repo + ":" + tag, err
}

func (p packConfig) forType(kind string) packDefault {
	a := p.Defaults
	b := p.Types[kind]
	if b.Root != "" {
		a.Root = b.Root
	}
	if b.Compression != "" {
		a.Compression = b.Compression
	}
	if b.Level != nil {
		a.Level = b.Level
	}
	if b.Owner != "" {
		a.Owner = b.Owner
	}
	if b.Chmod != "" {
		a.Chmod = b.Chmod
	}
	if b.Split != nil {
		if a.Split == nil {
			a.Split = &splitConfig{}
		}
		if b.Split.Enabled != nil {
			a.Split.Enabled = b.Split.Enabled
		}
		if b.Split.Large != "" {
			a.Split.Large = b.Split.Large
		}
		if b.Split.Small != "" {
			a.Split.Small = b.Split.Small
		}
	}
	return a
}

func projectPackTarget(kind, dir, lock string, cfg registryConfig, opts map[string]string) (string, error) {
	name := filepath.Base(filepath.Clean(dir))
	if lock != "" {
		name = strings.TrimSuffix(filepath.Base(lock), filepath.Ext(lock))
	}
	rev := "latest"
	source, ok := strings.CutPrefix(opts["source"], "hf://")
	if !ok {
		source, ok = strings.CutPrefix(opts["source"], "modelscope://")
	}
	if ok && kind == "weight" {
		id, sourceRev, _ := strings.Cut(source, "@")
		if id == "" {
			return "", fmt.Errorf("malformed hf source %q", opts["source"])
		}
		name = strings.ReplaceAll(id, "/", "--")
		if sourceRev != "" {
			rev = sourceRev
		}
	}
	repo, err := cfg.repo(kind, name)
	if err != nil {
		return "", err
	}
	format := cfg.TagFormat[kind]
	if format == "" {
		format = cfg.TagFormat["default"]
	}
	if format == "" {
		format = "{name}"
	}
	tag := strings.NewReplacer("{name}", name, "{type}", kind, "{rev}", rev).Replace(format)
	return repo + ":" + tag, nil
}

func modelRoot(paths map[string]string, source string) (string, error) {
	provider := ""
	for _, key := range []string{"hf", "modelscope"} {
		if rest, ok := strings.CutPrefix(source, key+"://"); ok {
			source, provider = rest, key
			break
		}
	}
	template := paths[provider]
	if provider == "" || template == "" {
		return "", nil
	}
	id, rev, _ := strings.Cut(source, "@")
	org, name, ok := strings.Cut(id, "/")
	if !ok || org == "" || name == "" || strings.Contains(name, "/") || name == "." || name == ".." || org == "." || org == ".." {
		return "", fmt.Errorf("invalid %s model source %q", provider, source)
	}
	if rev == "" {
		rev = "latest"
	}
	root := strings.NewReplacer("{org}", org, "{name}", name, "{rev}", rev).Replace(template)
	if !strings.HasPrefix(root, "/") || strings.Contains(root, "..") {
		return "", fmt.Errorf("unsafe model path %q", root)
	}
	return root, nil
}

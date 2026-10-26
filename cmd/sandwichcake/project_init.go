package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const projectScaffold = "apiVersion: sandwichcake.io/v1\nkind: CakeConfig\nregistry:\n  prefix: \"\"\n  repos: {venv: venv, weight: weights, binary: bin, image: infer}\n"

func initProject(cmd *cobra.Command, dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	config := filepath.Join(dir, "cake-config.yaml")
	if _, err := os.Lstat(config); err == nil {
		return fmt.Errorf("%s: %w", config, os.ErrExist)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "cake"), 0o755); err != nil {
		return err
	}
	// Existing .gitignore content must be preserved.
	ignore := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(ignore)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	add := ""
	for _, entry := range []string{"cake-config.local.yaml", ".cake/"} {
		if !strings.Contains("\n"+string(data)+add, "\n"+entry+"\n") {
			add += entry + "\n"
		}
	}
	if add != "" {
		f, err := os.OpenFile(ignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			add = "\n" + add
		}
		_, err = f.WriteString(add)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	// Never overwrite an existing project config, including a dangling symlink.
	f, err := os.OpenFile(config, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(f, projectScaffold)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", config)
	return nil
}

func migrateProject(cmd *cobra.Command, dir string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	var legacy struct {
		cliConfig `yaml:",inline"`
		CacheDir  string            `yaml:"cacheDir"`
		ToPrefix  string            `yaml:"toPrefix"`
		TagFormat string            `yaml:"tagFormat"`
		Roots     map[string]string `yaml:"roots"`
	}
	if err := readYAML(path, &legacy); err != nil && !os.IsNotExist(err) {
		return err
	}
	for typ, root := range legacy.Roots {
		if !validPackType(typ) || !filepath.IsAbs(root) {
			return fmt.Errorf("invalid legacy root.%s %q", typ, root)
		}
	}
	if err := validateTemplate(legacy.TagFormat, "name", "type", "rev"); err != nil {
		return err
	}
	if err := initProject(cmd, dir); err != nil {
		return err
	}
	if legacy.ToPrefix == "" && legacy.TagFormat == "" && len(legacy.Roots) == 0 && legacy.CacheDir == "" {
		return nil
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	project := projectConfig{APIVersion: "sandwichcake.io/v1", Kind: "CakeConfig",
		Registry: registryConfig{Prefix: strings.TrimSuffix(legacy.ToPrefix, "/"),
			Repos: map[string]string{"venv": "venv", "weight": "weights", "binary": "bin", "image": "infer"}}}
	if legacy.TagFormat != "" {
		project.Registry.TagFormat = map[string]string{"default": legacy.TagFormat}
	}
	project.Pack.Types = map[string]packDefault{}
	for typ, root := range legacy.Roots {
		project.Pack.Types[typ] = packDefault{Root: root}
	}
	data, err := yaml.Marshal(project)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "cake-config.yaml"), data, 0o644); err != nil {
		return err
	}
	if legacy.CacheDir != "" && legacy.Cache.Dir == "" {
		legacy.Cache.Dir = legacy.CacheDir
	}
	if err := saveConfig(legacy.cliConfig, false); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "migrated output settings from %s\n", path)
	return nil
}

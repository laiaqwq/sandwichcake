package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type cliConfig struct {
	Repos  []string     `yaml:"repos,omitempty"`
	Cache  cacheConfig  `yaml:"cache,omitempty"`
	Upload uploadConfig `yaml:"upload,omitempty"`
}

type cacheConfig struct {
	Dir string `yaml:"dir,omitempty"`
}

type uploadConfig struct {
	Concurrency int `yaml:"concurrency,omitempty"`
	Retries     int `yaml:"retries,omitempty"`
}

func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sandwichcake", "config.yaml"), nil
}

func loadConfig() (cliConfig, error) {
	var cfg cliConfig
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	err = readYAML(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}

func effectiveConfig() (cliConfig, map[string]string, error) {
	cfg, err := loadConfig()
	origins := map[string]string{}
	if err != nil {
		return cfg, nil, err
	}
	path, _ := configPath()
	if cfg.Cache.Dir != "" {
		cfg.Cache.Dir = resolveConfigPath(cfg.Cache.Dir, filepath.Dir(path))
		origins["cache.dir"] = path
	}
	if cfg.Upload.Concurrency != 0 {
		origins["upload.concurrency"] = path
	}
	if cfg.Upload.Retries != 0 {
		origins["upload.retries"] = path
	}
	if cfg.Repos != nil {
		origins["repos"] = path
	}
	project, err := currentProject()
	if err != nil {
		return cfg, nil, err
	}
	if project != "" {
		local := filepath.Join(project, "cake-config.local.yaml")
		var override cliConfig
		if err := readYAML(local, &override); err != nil && !errors.Is(err, os.ErrNotExist) {
			return cfg, nil, err
		}
		if override.Cache.Dir != "" {
			cfg.Cache.Dir = resolveConfigPath(override.Cache.Dir, project)
			origins["cache.dir"] = local
		}
		if override.Upload.Concurrency != 0 {
			cfg.Upload.Concurrency = override.Upload.Concurrency
			origins["upload.concurrency"] = local
		}
		if override.Upload.Retries != 0 {
			cfg.Upload.Retries = override.Upload.Retries
			origins["upload.retries"] = local
		}
		if override.Repos != nil {
			cfg.Repos = override.Repos
			origins["repos"] = local
		}
	}
	if value := os.Getenv("SANDWICHCAKE_CACHE_DIR"); value != "" {
		cfg.Cache.Dir = value
		origins["cache.dir"] = "SANDWICHCAKE_CACHE_DIR"
	}
	for _, item := range []struct {
		key, env string
		dst      *int
	}{
		{"upload.concurrency", "SANDWICHCAKE_UPLOAD_CONCURRENCY", &cfg.Upload.Concurrency},
		{"upload.retries", "SANDWICHCAKE_UPLOAD_RETRIES", &cfg.Upload.Retries},
	} {
		if value := os.Getenv(item.env); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return cfg, nil, fmt.Errorf("%s must be a positive integer", item.env)
			}
			*item.dst = n
			origins[item.key] = item.env
		}
	}
	if cfg.Upload.Concurrency < 0 || cfg.Upload.Retries < 0 {
		return cfg, nil, fmt.Errorf("upload settings must be positive")
	}
	return cfg, origins, nil
}

func resolveConfigPath(p, base string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

func saveConfig(cfg cliConfig, createOnly bool) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if createOnly {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func initCmd() *cobra.Command {
	var migrate bool
	cmd := &cobra.Command{
		Use: "init [dir]", Short: "Create a project without overwriting files",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) != 0 {
				dir = args[0]
			}
			if migrate {
				return migrateProject(cmd, dir)
			}
			if _, err := loadConfig(); err != nil {
				return fmt.Errorf("global config: %w (for legacy output settings, use init --migrate)", err)
			}
			return initProject(cmd, dir)
		},
	}
	cmd.Flags().BoolVar(&migrate, "migrate", false, "move legacy global output settings into this project")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Show or change user defaults and sync projects"}
	var global bool
	var origin bool
	show := &cobra.Command{
		Use: "show", Short: "Show current configuration", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, sources, err := effectiveConfig()
			if err != nil {
				return err
			}
			var view any = cfg
			project, _, err := loadProject()
			if err != nil {
				return err
			}
			if project.Kind != "" {
				view = struct {
					cliConfig     `yaml:",inline"`
					projectConfig `yaml:",inline"`
				}{cfg, project}
			}
			data, err := yaml.Marshal(view)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			if err == nil && origin {
				for _, key := range []string{"cache.dir", "upload.concurrency", "upload.retries", "repos"} {
					if sources[key] != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "# %s: %s\n", key, sources[key])
					}
				}
				if project.Kind != "" {
					dir, _ := currentProject()
					fmt.Fprintf(cmd.OutOrStdout(), "# registry.*, pack.*, models.*: %s\n", filepath.Join(dir, "cake-config.yaml"))
				}
			}
			return err
		},
	}
	show.Flags().BoolVar(&origin, "origin", false, "print value sources")
	cmd.AddCommand(show)
	initGlobal := &cobra.Command{
		Use: "init --global", Short: "Create user config without overwriting it", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !global {
				return fmt.Errorf("config init requires --global")
			}
			if err := saveConfig(cliConfig{}, true); err != nil {
				return err
			}
			path, _ := configPath()
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
			return nil
		},
	}
	initGlobal.Flags().BoolVar(&global, "global", false, "create global defaults")
	cmd.AddCommand(initGlobal)
	cmd.AddCommand(&cobra.Command{
		Use: "set <key> <value>", Short: "Set cache-dir, upload-concurrency, upload-retries or repos", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := cfg.set(args[0], args[1]); err != nil {
				return err
			}
			return saveConfig(cfg, false)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "get <key>", Short: "Print an effective environment value", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := effectiveConfig()
			if err != nil {
				return err
			}
			v, err := cfg.get(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "unset <key>", Short: "Remove a configured default", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := cfg.set(args[0], ""); err != nil {
				return err
			}
			return saveConfig(cfg, false)
		},
	})
	cmd.AddCommand(configPullCmd(), configPushCmd())
	return cmd
}

func (cfg *cliConfig) set(key, value string) error {
	switch key {
	case "cache-dir":
		cfg.Cache.Dir = value
	case "upload-concurrency", "upload-retries":
		n := 0
		if value != "" {
			var err error
			n, err = strconv.Atoi(value)
			if err != nil || n < 1 {
				return fmt.Errorf("%s must be a positive integer", key)
			}
		}
		if key == "upload-concurrency" {
			cfg.Upload.Concurrency = n
		} else {
			cfg.Upload.Retries = n
		}
	case "repos":
		cfg.Repos = nil
		for _, repo := range strings.Split(value, ",") {
			if repo = strings.TrimSpace(repo); repo != "" {
				cfg.Repos = append(cfg.Repos, repo)
			}
		}
	default:
		return fmt.Errorf("unknown or project-only config key %q (use cake-config.yaml for output settings)", key)
	}
	return nil
}

func (cfg cliConfig) get(key string) (string, error) {
	switch key {
	case "cache-dir":
		return cfg.Cache.Dir, nil
	case "upload-concurrency":
		if cfg.Upload.Concurrency == 0 {
			return "", nil
		}
		return strconv.Itoa(cfg.Upload.Concurrency), nil
	case "upload-retries":
		if cfg.Upload.Retries == 0 {
			return "", nil
		}
		return strconv.Itoa(cfg.Upload.Retries), nil
	case "repos":
		return strings.Join(cfg.Repos, ","), nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

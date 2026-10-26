package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/laiaqwq/sandwichcake/internal/cache"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
	_ "github.com/laiaqwq/sandwichcake/pkg/types/builtin"
	"github.com/laiaqwq/sandwichcake/pkg/version"
)

var (
	insecureHosts  []string
	cacheDir       string
	verbose        bool
	projectDir     string
	registryPrefix string
)

func main() {
	root := newRoot()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	name := filepath.Base(os.Args[0])
	root := &cobra.Command{
		Use:   name,
		Short: "sandwichcake: OCI image composition tool",
	}
	root.PersistentFlags().StringArrayVar(&insecureHosts, "insecure-registry", nil, "registry host to access over plain HTTP (repeatable)")
	root.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "metadata cache dir (default ~/.cache/sandwichcake)")
	root.PersistentFlags().StringVar(&projectDir, "project", "", "project directory (default: discover from cwd)")
	root.PersistentFlags().StringVar(&registryPrefix, "registry-prefix", "", "override registry.prefix from cake-config.yaml")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	root.AddCommand(
		packCmd(),
		composeCmd(),
		planCmd(),
		inspectCmd(),
		lsCmd(),
		discoverCmd(),
		tagCmd(),
		diffCmd(),
		rmCmd(),
		typesCmd(),
		versionCmd(),
		initCmd(),
		configCmd(),
	)
	return root
}

func client() (*registry.Client, error) {
	dir := cacheDir
	cfg, _, err := effectiveConfig()
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = cfg.Cache.Dir
	}
	c, err := cache.New(dir)
	if err != nil {
		return nil, err
	}
	opts := registry.Options{Cache: c, Concurrency: cfg.Upload.Concurrency, Retries: cfg.Upload.Retries}
	// If every repo we touch is insecure, enable insecure globally.
	// (Per-host insecure is not supported by ggcr name parsing; the flag
	// marks all registries insecure when set.)
	if len(insecureHosts) > 0 {
		opts.Insecure = true
	}
	return registry.New(opts), nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.Version)
			return nil
		},
	}
}

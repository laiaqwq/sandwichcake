package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/baba/sandwichcake/pkg/inspect"
	"github.com/baba/sandwichcake/pkg/layer"
	stypes "github.com/baba/sandwichcake/pkg/types"
)

func inspectCmd() *cobra.Command {
	var jsonOut bool
	var fileTitle string
	cmd := &cobra.Command{
		Use:   "inspect <ref>",
		Short: "Inspect an image or slice",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			rep, err := inspect.Inspect(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			if fileTitle != "" {
				data, err := rep.ReadMetadataFile(cmd.Context(), c, fileTitle)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			rep.Render(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	cmd.Flags().StringVar(&fileTitle, "file", "", "print a metadata file's content by title")
	return cmd
}

type cliConfig struct {
	Repos []string `yaml:"repos"`
}

func loadConfig() cliConfig {
	var cfg cliConfig
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	data, err := os.ReadFile(filepath.Join(dir, "sandwichcake", "config.yaml"))
	if err != nil {
		return cfg
	}
	yaml.Unmarshal(data, &cfg)
	return cfg
}

func lsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls [repo...]",
		Short: "List slices in repos (default: config repos)",
		RunE: func(cmd *cobra.Command, args []string) error {
			repos := args
			if len(repos) == 0 {
				repos = loadConfig().Repos
			}
			if len(repos) == 0 {
				return fmt.Errorf("no repos given and none configured in ~/.config/sandwichcake/config.yaml")
			}
			c, err := client()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			for _, repo := range repos {
				tags, err := c.ListTags(cmd.Context(), repo)
				if err != nil {
					return fmt.Errorf("%s: %w", repo, err)
				}
				for _, tag := range tags {
					ref := repo + ":" + tag
					d, err := c.Get(cmd.Context(), ref)
					if err != nil {
						continue
					}
					img, err := d.Image()
					if err != nil {
						continue // index or non-image manifest
					}
					m, err := img.Manifest()
					if err != nil {
						continue
					}
					if m.Annotations[layer.KeyKind] == layer.KindSlice {
						fmt.Fprintln(w, ref)
					}
				}
			}
			return nil
		},
	}
}

func discoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover <prefix>",
		Short: "List repos under a prefix (requires catalog/GAR support)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			prefix := args[0]
			// registry = first path segment
			reg := strings.SplitN(prefix, "/", 2)[0]
			repos, err := c.Catalog(cmd.Context(), reg)
			if err == nil {
				var hits []string
				for _, r := range repos {
					if strings.HasPrefix(r, prefix) || strings.HasPrefix(reg+"/"+r, prefix) {
						hits = append(hits, r)
					}
				}
				sort.Strings(hits)
				for _, h := range hits {
					fmt.Fprintln(cmd.OutOrStdout(), h)
				}
				return nil
			}
			children, err2 := c.GARChildren(cmd.Context(), prefix)
			if err2 == nil {
				for _, ch := range children {
					fmt.Fprintln(cmd.OutOrStdout(), ch)
				}
				return nil
			}
			return fmt.Errorf("discover: registry does not support catalog (%v) or GAR children (%v)", err, err2)
		},
	}
}

func tagCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag <ref> <new-ref>",
		Short: "Copy a manifest to a new tag (cross-repo mounts layers)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			src, dst := args[0], args[1]
			srcRef, err := c.ParseRef(src)
			if err != nil {
				return err
			}
			dstRef, err := c.ParseRef(dst)
			if err != nil {
				return err
			}
			if srcRef.Context().String() == dstRef.Context().String() {
				if err := c.Tag(cmd.Context(), src, dst); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "tagged %s -> %s\n", src, dst)
				return nil
			}
			// Different repo: re-push (blobs mount/reuse as possible).
			d, err := c.Get(cmd.Context(), src)
			if err != nil {
				return err
			}
			if d.MediaType.IsIndex() {
				idx, err := d.ImageIndex()
				if err != nil {
					return err
				}
				err = c.WriteIndex(cmd.Context(), dst, idx)
				if err != nil {
					return err
				}
			} else {
				img, err := c.Image(cmd.Context(), src, nil)
				if err != nil {
					return err
				}
				err = c.WriteImage(cmd.Context(), dst, img)
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tagged %s -> %s\n", src, dst)
			return nil
		},
	}
}

func diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <ref-a> <ref-b>",
		Short: "Compare two images' layers by diff_id",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			out, err := inspect.Diff(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <ref>",
		Short: "Delete a manifest/tag and its metadata referrers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			w := cmd.OutOrStdout()
			head, err := c.Head(ctx, args[0])
			if err != nil {
				return err
			}
			srcRef, err := c.ParseRef(args[0])
			if err != nil {
				return err
			}
			digestRef := srcRef.Context().Digest(head.Digest.String())
			// Delete referrers (metadata artifacts).
			refs, err := c.Referrers(ctx, digestRef.String())
			if err == nil {
				for _, rd := range refs {
					rm := srcRef.Context().Digest(rd.Digest.String())
					if err := c.Delete(ctx, rm.String()); err == nil {
						fmt.Fprintf(w, "deleted referrer %s\n", rd.Digest)
					}
					c.RemoveReferrer(ctx, digestRef, rd.Digest)
				}
			}
			desc, err := c.Get(ctx, args[0])
			var layerDigests []string
			if err == nil && !desc.MediaType.IsIndex() {
				{
					m := desc.Manifest
					var parsed struct {
						Layers []struct {
							Digest string `json:"digest"`
						} `json:"layers"`
					}
					if json.Unmarshal(m, &parsed) == nil {
						for _, l := range parsed.Layers {
							layerDigests = append(layerDigests, l.Digest)
						}
					}
				}
			}
			if err := c.Delete(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(w, "deleted %s\n", args[0])
			if len(layerDigests) > 0 {
				fmt.Fprintln(w, "possibly unreferenced layers (GC is registry-side):")
				for _, d := range layerDigests {
					fmt.Fprintf(w, "  %s\n", d)
				}
			}
			return nil
		},
	}
}

func typesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types",
		Short: "List registered pack types and their options",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			for _, t := range stypes.All() {
				fmt.Fprintf(w, "%s (schema %d, default compression %s)\n", t.Name(), t.Schema(), t.Defaults().Compression)
				for _, o := range t.Options() {
					fmt.Fprintf(w, "  --%-12s %s (default %q)\n", o.Name, o.Usage, o.Default)
				}
			}
			return nil
		},
	}
}

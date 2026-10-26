package main

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/spf13/cobra"

	"github.com/laiaqwq/sandwichcake/pkg/fetch"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/slice"
)

// remoteFlags are the pack weight flags for remote sources.
type remoteFlags struct {
	include, exclude []string
	keepDir          string
}

func addRemoteFlags(cmd *cobra.Command, f *remoteFlags) {
	fl := cmd.Flags()
	fl.StringArrayVar(&f.include, "include", nil, "only remote files matching this glob (repeatable; replaces pack.weight.include)")
	fl.StringArrayVar(&f.exclude, "exclude", nil, "skip remote files matching this glob (repeatable; replaces pack.weight.exclude)")
	fl.StringVar(&f.keepDir, "keep-dir", "", "also write the streamed files into this directory")
}

func fetchClient(env cliConfig) (*fetch.Client, error) {
	seg, err := parseSizeOr(env.Download.SegmentSize, 0)
	if err != nil {
		return nil, err
	}
	return fetch.New(fetch.Options{
		Endpoints:       endpoints(env),
		Retries:         env.Download.Retries,
		Connections:     env.Download.Connections,
		SegmentSize:     seg,
		HFToken:         fetch.HFToken(),
		ModelScopeToken: fetch.ModelScopeToken(),
	}), nil
}

func endpoints(env cliConfig) fetch.Endpoints {
	return fetch.Endpoints{HF: env.Download.HF.Endpoint, ModelScope: env.Download.ModelScope.Endpoint}
}

// selectFiles applies include/exclude globs (gitignore syntax, matched on
// repo paths); no include patterns means every file.
func selectFiles(files []fetch.File, include, exclude []string) []fetch.File {
	var out []fetch.File
	for _, f := range files {
		keep := len(include) == 0
		for _, p := range include {
			if ignoreMatch(f.Path, p) {
				keep = true
				break
			}
		}
		for _, p := range exclude {
			if ignoreMatch(f.Path, p) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, f)
		}
	}
	return out
}

// listRemote resolves spec into a slice.Remote; it returns the resolved
// upstream commit ("" if the provider has none).
func listRemote(cmd *cobra.Command, spec fetch.Spec, f *remoteFlags, defaults packDefault) (*slice.Remote, string, error) {
	env, _, err := effectiveConfig()
	if err != nil {
		return nil, "", err
	}
	fc, err := fetchClient(env)
	if err != nil {
		return nil, "", err
	}
	include, exclude := defaults.Include, defaults.Exclude
	if cmd.Flags().Changed("include") {
		include = f.include
	}
	if cmd.Flags().Changed("exclude") {
		exclude = f.exclude
	}
	listing, err := fc.List(cmd.Context(), spec)
	if err != nil {
		return nil, "", err
	}
	files := selectFiles(listing.Files, include, exclude)
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no files of %s match --include/--exclude", spec.Canonical())
	}
	r := &slice.Remote{
		Name:        remoteName(spec),
		KeepDir:     f.keepDir,
		Concurrency: env.Download.Concurrency,
		Log:         func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) },
	}
	for _, file := range files {
		file := file
		r.Files = append(r.Files, slice.RemoteFile{
			Path: file.Path, Size: file.Size, SHA256: file.SHA256, GitSHA1: file.GitSHA1,
			Open: func(ctx context.Context, off int64) (io.ReadCloser, error) { return fc.Open(ctx, file, off) },
		})
	}
	return r, listing.Commit, nil
}

// remoteName is the slice's logical name (what a local dir's base name
// would be): the model name, or the file name of a plain URL. Packing a
// --keep-dir of that name reproduces the streamed digests.
func remoteName(s fetch.Spec) string {
	if s.Provider == fetch.HTTP {
		return s.Name()
	}
	return path.Base(s.Repo)
}

func printRemotePlan(cmd *cobra.Command, res *slice.PackResult) {
	out := cmd.OutOrStdout()
	files := 0
	for _, l := range res.Slice.Layers {
		var n int
		fmt.Sscan(l.Annotations[layer.KeyLayerFiles], &n)
		files += n
	}
	fmt.Fprintf(out, "dry-run: %s (%d layers, %d bytes uncompressed, %d entries)\n", res.Ref, len(res.Slice.Layers), res.Size, files)
	for i, l := range res.Slice.Layers {
		name := l.Annotations[layer.KeyLayerName]
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(out, "  layer %d: %s %d bytes, %s entries\n", i+1, name, l.Size, l.Annotations[layer.KeyLayerFiles])
	}
}

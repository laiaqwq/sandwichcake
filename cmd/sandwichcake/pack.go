package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/slice"
	stypes "github.com/laiaqwq/sandwichcake/pkg/types"
	"github.com/laiaqwq/sandwichcake/pkg/types/venv"
)

type packFlags struct {
	to, toPrefix, root string
	compression        string
	level              int
	noSplit            bool
	large, small       string
	platform           string
	rewritePrefix      []string
	owner, chmod       string
	noParents          bool
	artifactCompat     bool
	merge              bool
	dryRun             bool
	fromLock           string
	typeVals           map[string]*string
	typeBools          map[string]*bool
}

func packCmd() *cobra.Command {
	parent := &cobra.Command{Use: "pack", Short: "Pack a directory into a slice"}
	for _, t := range stypes.All() {
		t := t
		f := &packFlags{typeVals: map[string]*string{}, typeBools: map[string]*bool{}}
		cmd := &cobra.Command{
			Use:   t.Name() + " <dir>",
			Short: fmt.Sprintf("pack a %s directory", t.Name()),
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runPack(cmd, t, f, args)
			},
		}
		fl := cmd.Flags()
		fl.StringVar(&f.to, "to", "", "target repo:tag")
		fl.StringVar(&f.toPrefix, "to-prefix", "", "registry/repo prefix for auto-naming (type Namer)")
		fl.StringVar(&f.root, "root", "", "container root path (e.g. /opt/venv)")
		fl.StringVar(&f.compression, "compression", "", "gzip|zstd|none (type default)")
		fl.IntVar(&f.level, "level", 0, "compression level")
		fl.BoolVar(&f.noSplit, "no-split", false, "disable automatic layer splitting")
		fl.StringVar(&f.large, "large-threshold", "", "large file threshold (default 5Gi)")
		fl.StringVar(&f.small, "small-threshold", "", "small file threshold (default 64Mi)")
		fl.StringVar(&f.platform, "platform", "", "linux/amd64 | any")
		fl.StringArrayVar(&f.rewritePrefix, "rewrite-prefix", nil, "src=dst path prefix rewrite (repeatable)")
		fl.StringVar(&f.owner, "owner", "", "uid:gid for layer files")
		fl.StringVar(&f.chmod, "chmod", "", "fileMode:dirMode octal e.g. 644:755")
		fl.BoolVar(&f.noParents, "no-parents", false, "do not write ancestor dir entries")
		fl.BoolVar(&f.artifactCompat, "artifact-compat", false, "metadata artifact compatibility mode")
		fl.BoolVar(&f.merge, "merge", false, "merge into an existing index at the target tag")
		fl.BoolVar(&f.dryRun, "dry-run", false, "build locally, do not push")
		if t.Name() == "venv" {
			fl.StringVar(&f.fromLock, "from-lock", "", "build the venv from a lock file using local uv")
		}
		for _, o := range t.Options() {
			if o.Bool {
				var b bool
				f.typeBools[o.Name] = &b
				fl.BoolVar(&b, o.Name, o.Default == "true", o.Usage)
			} else {
				var s string
				f.typeVals[o.Name] = &s
				fl.StringVar(&s, o.Name, o.Default, o.Usage)
			}
		}
		cmd.MarkFlagRequired("root")
		parent.AddCommand(cmd)
	}
	return parent
}

func runPack(cmd *cobra.Command, t stypes.Type, f *packFlags, args []string) error {
	ctx := cmd.Context()
	var dir string
	var extraRewrites []stypes.Rewrite
	if f.fromLock != "" {
		if t.Name() != "venv" {
			return fmt.Errorf("--from-lock only supported for venv")
		}
		pyVer := ""
		if p, ok := f.typeVals["python"]; ok {
			pyVer = *p
		}
		if pyVer == "" || pyVer == "embedded" || pyVer == "system" {
			return fmt.Errorf("--from-lock requires --python <version>, e.g. --python 3.12")
		}
		venvDir, rw, cleanup, err := venv.BuildFromLock(ctx, f.fromLock, pyVer, f.root)
		if err != nil {
			return err
		}
		defer cleanup()
		dir = venvDir
		extraRewrites = append(extraRewrites, rw)
		// from-lock always produces an embedded python venv
		*f.typeVals["python"] = "embedded"
	} else {
		if len(args) == 0 {
			return fmt.Errorf("dir argument required")
		}
		dir = args[0]
	}
	large, err := parseSizeOr(f.large, 0)
	if err != nil {
		return err
	}
	small, err := parseSizeOr(f.small, 0)
	if err != nil {
		return err
	}
	var rewrites []stypes.Rewrite
	for _, rp := range f.rewritePrefix {
		from, to, ok := strings.Cut(rp, "=")
		if !ok {
			return fmt.Errorf("--rewrite-prefix must be src=dst, got %q", rp)
		}
		rewrites = append(rewrites, stypes.Rewrite{From: from, To: to})
	}
	rewrites = append(rewrites, extraRewrites...)
	typeOpts := map[string]string{}
	for name, p := range f.typeVals {
		if *p != "" {
			typeOpts[name] = *p
		}
	}
	for name, p := range f.typeBools {
		if *p {
			typeOpts[name] = "true"
		}
	}
	c, err := client()
	if err != nil {
		return err
	}
	res, err := slice.Pack(ctx, c, slice.PackOptions{
		Type: t.Name(), Dir: dir, Root: f.root,
		Target: f.to, TargetPrefix: f.toPrefix,
		Platform: f.platform, Compression: f.compression, Level: f.level,
		NoSplit: f.noSplit, Large: large, Small: small,
		Owner: f.owner, Chmod: f.chmod, NoParents: f.noParents,
		RewritePrefix: rewrites, ArtifactCompat: f.artifactCompat,
		Merge: f.merge, TypeOpts: typeOpts, DryRun: f.dryRun,
		SourceDateEpoch: -1,
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if f.dryRun {
		fmt.Fprintf(out, "dry-run: %s -> %s (%d layers)\n", res.Ref, res.Digest, len(res.Slice.Layers))
	} else {
		fmt.Fprintf(out, "pushed %s\n  digest %s\n", res.Ref, res.Digest)
		if res.MetadataRef != "" {
			fmt.Fprintf(out, "  metadata %s\n", res.MetadataRef)
		}
	}
	return nil
}

func parseSizeOr(s string, def int64) (int64, error) {
	if s == "" {
		return def, nil
	}
	return layer.ParseSize(s)
}

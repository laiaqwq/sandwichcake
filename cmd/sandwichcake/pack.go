package main

import (
	"fmt"
	"path/filepath"
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
		parent.AddCommand(cmd)
	}
	return parent
}

func runPack(cmd *cobra.Command, t stypes.Type, f *packFlags, args []string) error {
	project, _, err := loadProject()
	if err != nil {
		return err
	}
	defaults := project.Pack.forType(t.Name())
	if !cmd.Flags().Changed("root") {
		f.root = defaults.Root
		if f.root == "" && t.Name() == "weight" {
			f.root, err = modelRoot(project.Models.Paths, *f.typeVals["source"])
			if err != nil {
				return err
			}
		}
	}
	if f.root == "" {
		return fmt.Errorf("--root required (or set pack.%s.root in cake-config.yaml)", t.Name())
	}
	if !cmd.Flags().Changed("to-prefix") {
		f.toPrefix = project.Registry.Prefix
	}
	if !cmd.Flags().Changed("compression") {
		f.compression = defaults.Compression
	}
	if !cmd.Flags().Changed("level") && defaults.Level != nil {
		f.level = *defaults.Level
	}
	if !cmd.Flags().Changed("owner") {
		f.owner = defaults.Owner
	}
	if !cmd.Flags().Changed("chmod") {
		f.chmod = defaults.Chmod
	}
	if defaults.Split != nil {
		if !cmd.Flags().Changed("no-split") && defaults.Split.Enabled != nil {
			f.noSplit = !*defaults.Split.Enabled
		}
		if !cmd.Flags().Changed("large-threshold") {
			f.large = defaults.Split.Large
		}
		if !cmd.Flags().Changed("small-threshold") {
			f.small = defaults.Split.Small
		}
	}
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
	if f.to == "" && f.toPrefix != "" {
		if project.Kind != "" && !cmd.Flags().Changed("to-prefix") {
			f.to, err = projectPackTarget(t.Name(), dir, f.fromLock, project.Registry, typeOpts)
		} else {
			f.to, err = defaultPackTarget(t, dir, f.fromLock, f.toPrefix, "", typeOpts)
		}
		if err != nil {
			return err
		}
	}
	if project.Kind != "" && !f.dryRun && f.to != "" && !f.merge {
		skip, err := applyTagPolicy(cmd, project.Registry.OnTagExists, f.to)
		if err != nil {
			return err
		}
		if skip {
			return nil
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

func defaultPackTarget(t stypes.Type, dir, lock, prefix, tagFormat string, typeOpts map[string]string) (string, error) {
	name := filepath.Base(filepath.Clean(dir))
	if lock != "" {
		name = strings.TrimSuffix(filepath.Base(lock), filepath.Ext(lock))
	}
	rev := "latest"
	repo := strings.TrimSuffix(prefix, "/") + "/" + t.Name()
	if source, ok := strings.CutPrefix(typeOpts["source"], "hf://"); ok && t.Name() == "weight" {
		id, sourceRev, _ := strings.Cut(source, "@")
		if id == "" {
			return "", fmt.Errorf("weight: malformed hf source %q", typeOpts["source"])
		}
		name = filepath.Base(id)
		if sourceRev != "" {
			rev = sourceRev
		}
	}
	if n, ok := t.(stypes.Namer); ok && (t.Name() != "weight" || strings.HasPrefix(typeOpts["source"], "hf://")) {
		target, err := n.DefaultTarget(&stypes.Source{Opts: stypes.Opts{Values: typeOpts}}, prefix)
		if err != nil {
			return "", err
		}
		if tagFormat == "" {
			return target, nil
		}
		i := strings.LastIndex(target, ":")
		if i < strings.LastIndex(target, "/") {
			return "", fmt.Errorf("cannot apply tag-format to target %q", target)
		}
		repo = target[:i]
	}
	if tagFormat == "" {
		tagFormat = "{name}"
	}
	tag := strings.NewReplacer("{name}", name, "{type}", t.Name(), "{rev}", rev).Replace(tagFormat)
	return repo + ":" + tag, nil
}

func parseSizeOr(s string, def int64) (int64, error) {
	if s == "" {
		return def, nil
	}
	return layer.ParseSize(s)
}

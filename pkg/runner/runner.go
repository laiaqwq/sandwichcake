// Package runner orchestrates compose: resolve → compose → plan → push.
// The CLI is a thin wrapper around it.
package runner

import (
	"context"
	"fmt"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/laiaqwq/sandwichcake/internal/cache"
	"github.com/laiaqwq/sandwichcake/pkg/cake"
	"github.com/laiaqwq/sandwichcake/pkg/compose"
	"github.com/laiaqwq/sandwichcake/pkg/plan"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
	"github.com/laiaqwq/sandwichcake/pkg/resolve"
	"github.com/laiaqwq/sandwichcake/pkg/slice"
	stypes "github.com/laiaqwq/sandwichcake/pkg/types"
)

// Options controls Compose.
type Options struct {
	DryRun  bool
	Yes     bool // skip copy confirmation
	Verify  bool
	Compat  string                                   // "docker20"
	Confirm func(totalCopyBytes int64) (bool, error) // nil => error on copy
	Cache   *cache.Cache
}

// Outcome reports the compose result.
type Outcome struct {
	Plan     *plan.Plan
	Digest   v1.Hash
	Lock     *cake.Lock
	Findings []stypes.Finding
	Pushed   bool
}

// Compose resolves, composes, plans and (unless DryRun) pushes.
// If lock is non-nil, refs come from the lock and resolved layers are
// verified against it (no tag resolution).
func Compose(ctx context.Context, client *registry.Client, c *cake.Cake, lock *cake.Lock, opts Options) (*Outcome, error) {
	ropts := resolve.Options{Verify: opts.Verify, Cache: opts.Cache}
	out := &Outcome{}

	// Determine platforms.
	platforms, err := targetPlatforms(ctx, client, c, lock)
	if err != nil {
		return nil, err
	}

	// Refs: from lock when present.
	baseRef := c.Base
	var sliceRefs []cake.Ref
	sliceRefs = c.Slices
	if lock != nil {
		baseRef = cake.Ref{Ref: lock.Base.Ref, Display: lock.Base.Input,
			Platform: lock.Base.Platform, Name: lock.Base.Name,
			Verify: lock.Base.Verify, Libc: lock.Base.Libc}
		sliceRefs = nil
		for _, s := range lock.Slices {
			sliceRefs = append(sliceRefs, cake.Ref{Ref: s.Ref, Display: s.Input,
				Platform: s.Platform, Name: s.Name, Layers: s.Layers,
				Force: s.Force, Verify: s.Verify})
		}
	}

	var images []compose.PlatformImage
	lockOut := &cake.Lock{
		APIVersion: cake.APIVersion,
		Kind:       cake.KindLock,
		Target:     cake.LockTarget{Ref: c.Target.Ref},
		Base: cake.LockRef{Platform: baseRef.Platform, Name: baseRef.Name,
			Verify: baseRef.Verify, Libc: baseRef.Libc,
			Resolved: map[string]cake.ResolvedItem{}},
		Config: c.Config,
	}
	for _, p := range platforms {
		lockOut.Target.Platforms = append(lockOut.Target.Platforms, p.os+"/"+p.arch)
	}
	for _, s := range sliceRefs {
		lockOut.Slices = append(lockOut.Slices, cake.LockRef{
			Platform: s.Platform, Name: s.Name, Layers: s.Layers,
			Force: s.Force, Verify: s.Verify,
			Resolved: map[string]cake.ResolvedItem{},
		})
	}

	for _, p := range platforms {
		plat := v1.Platform{OS: p.os, Architecture: p.arch}
		base, err := resolve.Resolve(ctx, client, baseRef, resolve.Base, plat, ropts)
		if err != nil {
			return nil, fmt.Errorf("base: %w", err)
		}
		recordResolved(&lockOut.Base, base)
		if lock != nil {
			if err := checkLock(lock.Base, base); err != nil {
				return nil, err
			}
		}
		var slices []*resolve.Resolved
		for si, sr := range sliceRefs {
			s, err := resolve.Resolve(ctx, client, sr, resolve.Slice, plat, ropts)
			if err != nil {
				return nil, fmt.Errorf("slice %d (%s): %w", si, sr.Ref, err)
			}
			recordResolved(&lockOut.Slices[si], s)
			if lock != nil {
				if err := checkLock(lock.Slices[si], s); err != nil {
					return nil, err
				}
			}
			slices = append(slices, s)
		}
		res, err := compose.Compose(compose.Input{
			Ctx:         ctx,
			Platform:    plat,
			Base:        base,
			Slices:      slices,
			Patch:       c.Config,
			Annotations: c.Target.Annotations,
			Created:     slice.Epoch(-1),
			Compat:      opts.Compat,
		})
		if err != nil {
			return nil, err
		}
		out.Findings = append(out.Findings, res.Findings...)
		images = append(images, compose.PlatformImage{Platform: plat, Image: res.Image})
	}

	// Plan.
	var v1imgs []v1.Image
	for _, pi := range images {
		v1imgs = append(v1imgs, pi.Image)
	}
	pl, err := plan.Compute(ctx, client, c.Target.Ref, v1imgs...)
	if err != nil {
		return nil, err
	}
	out.Plan = pl
	if opts.DryRun {
		out.Lock = lockOut
		return out, nil
	}
	if pl.CopyBytes > 0 && !opts.Yes {
		if opts.Confirm == nil {
			return nil, fmt.Errorf("compose: %d bytes must be copied to the target registry; pass --yes or run interactively", pl.CopyBytes)
		}
		ok, err := opts.Confirm(pl.CopyBytes)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("compose: aborted")
		}
	}

	// Push.
	if len(images) == 1 {
		img := images[0].Image
		if err := client.WriteImage(ctx, c.Target.Ref, img); err != nil {
			return nil, err
		}
		d, err := img.Digest()
		if err != nil {
			return nil, err
		}
		out.Digest = d
	} else {
		var pis []compose.PlatformImage
		pis = images
		idx, err := compose.ComposeIndex(pis)
		if err != nil {
			return nil, err
		}
		if err := client.WriteIndex(ctx, c.Target.Ref, idx); err != nil {
			return nil, err
		}
		d, err := idx.Digest()
		if err != nil {
			return nil, err
		}
		out.Digest = d
	}
	out.Pushed = true
	lockOut.Target.Digest = out.Digest.String()
	out.Lock = lockOut
	return out, nil
}

type platStr struct{ os, arch string }

// targetPlatforms determines the platform list.
func targetPlatforms(ctx context.Context, client *registry.Client, c *cake.Cake, lock *cake.Lock) ([]platStr, error) {
	if lock != nil && len(lock.Target.Platforms) > 0 {
		return parsePlatforms(lock.Target.Platforms)
	}
	if len(c.Target.Platforms) > 0 {
		return parsePlatforms(c.Target.Platforms)
	}
	// Follow the base.
	baseRef := c.Base.Ref
	if lock != nil {
		baseRef = lock.Base.Ref
	}
	desc, err := client.Get(ctx, baseRef)
	if err != nil {
		return nil, err
	}
	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, err
		}
		var ps []platStr
		for _, m := range im.Manifests {
			if m.Platform == nil || m.Platform.OS == "unknown" {
				continue // attestations etc.
			}
			ps = append(ps, platStr{m.Platform.OS, m.Platform.Architecture})
		}
		if len(ps) == 0 {
			return nil, fmt.Errorf("runner: base index has no platforms")
		}
		return ps, nil
	}
	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	return []platStr{{cf.OS, cf.Architecture}}, nil
}

func parsePlatform(s string) (platStr, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return platStr{}, fmt.Errorf("runner: bad platform %q", s)
	}
	return platStr{parts[0], parts[1]}, nil
}

func parsePlatforms(ss []string) ([]platStr, error) {
	var out []platStr
	for _, s := range ss {
		p, err := parsePlatform(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// lockKey is the resolved-map key for a resolved ref: explicit platform,
// "any" for platform-independent slices, else config os/arch.
func lockKey(r *resolve.Resolved) string {
	if r.Input.Platform != "" {
		return r.Input.Platform
	}
	if r.Any {
		return "any"
	}
	if r.Config != nil {
		return r.Config.OS + "/" + r.Config.Architecture
	}
	return ""
}

func recordResolved(lr *cake.LockRef, r *resolve.Resolved) {
	lr.Ref = r.Repo + "@" + r.RefDigest.String()
	if lr.Input == "" {
		lr.Input = r.Input.Ref
	}
	var layers []string
	for _, l := range r.Layers {
		layers = append(layers, l.Desc.Digest.String())
	}
	lr.Resolved[lockKey(r)] = cake.ResolvedItem{Manifest: r.ManifestDigest.String(), Layers: layers}
}

func checkLock(lr cake.LockRef, r *resolve.Resolved) error {
	key := lockKey(r)
	item, ok := lr.Resolved[key]
	if !ok {
		return fmt.Errorf("cake: lock missing resolved entry %q for %s", key, lr.Ref)
	}
	if item.Manifest != r.ManifestDigest.String() {
		return fmt.Errorf("cake: locked manifest %s differs from resolved %s for %s",
			item.Manifest, r.ManifestDigest, lr.Ref)
	}
	var layers []string
	for _, l := range r.Layers {
		layers = append(layers, l.Desc.Digest.String())
	}
	return lr.CheckLayers(key, layers)
}

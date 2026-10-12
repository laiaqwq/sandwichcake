// Package plan classifies each blob of an image as reuse/mount/copy for a
// target repo (DESIGN 3), terraform-plan style.
package plan

import (
	"context"
	"fmt"
	"io"
	"sort"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/registry"
)

// Item is one blob's transfer classification.
type Item struct {
	Digest    v1.Hash
	Size      int64
	MediaType string
	Name      string // layer.name annotation, or "config"
	Type      string // layer.type annotation
	Strategy  registry.Strategy
}

// Plan is the transfer plan for a set of images.
type Plan struct {
	Items      []Item
	ReuseBytes int64
	MountBytes int64
	CopyBytes  int64
}

// CopyTotal returns total bytes needing upload.
func (p *Plan) CopyTotal() int64 { return p.CopyBytes }

// Compute classifies every unique blob (layers + config) of images for
// targetRef's repo.
func Compute(ctx context.Context, client *registry.Client, targetRef string, images ...v1.Image) (*Plan, error) {
	r, err := client.ParseRef(targetRef)
	if err != nil {
		return nil, err
	}
	repo := r.Context()
	seen := map[v1.Hash]bool{}
	p := &Plan{}
	add := func(l v1.Layer, name, typ string) error {
		d, err := l.Digest()
		if err != nil {
			return err
		}
		if seen[d] {
			return nil
		}
		seen[d] = true
		strat, err := client.Classify(ctx, repo, l)
		if err != nil {
			return err
		}
		size, _ := l.Size()
		mt, _ := l.MediaType()
		it := Item{Digest: d, Size: size, MediaType: string(mt), Name: name, Type: typ, Strategy: strat}
		switch strat {
		case registry.StrategyReuse:
			p.ReuseBytes += size
		case registry.StrategyMount:
			p.MountBytes += size
		default:
			p.CopyBytes += size
		}
		p.Items = append(p.Items, it)
		return nil
	}
	for _, img := range images {
		layers, err := img.Layers()
		if err != nil {
			return nil, err
		}
		// manifest descriptors carry the annotations
		anns := map[v1.Hash]map[string]string{}
		if m, err := img.Manifest(); err == nil {
			for _, d := range m.Layers {
				anns[d.Digest] = d.Annotations
			}
		}
		for _, l := range layers {
			d, _ := l.Digest()
			a := anns[d]
			if err := add(l, a[layer.KeyLayerName], a[layer.KeyLayerType]); err != nil {
				return nil, err
			}
		}
		// config blob
		cl, err := img.ConfigName()
		if err != nil {
			return nil, err
		}
		if !seen[cl] {
			cfg, err := img.RawConfigFile()
			if err != nil {
				return nil, err
			}
			seen[cl] = true
			it := Item{Digest: cl, Size: int64(len(cfg)), MediaType: "config", Name: "config", Strategy: registry.StrategyCopy}
			// config exists check
			exists, err := client.BlobExists(ctx, repo, cl)
			if err != nil {
				return nil, err
			}
			if exists {
				it.Strategy = registry.StrategyReuse
				p.ReuseBytes += it.Size
			} else {
				p.CopyBytes += it.Size
			}
			p.Items = append(p.Items, it)
		}
	}
	return p, nil
}

var marker = map[registry.Strategy]string{
	registry.StrategyReuse: "=",
	registry.StrategyMount: "~",
	registry.StrategyCopy:  "+",
}

// Render writes a terraform-plan-style summary.
func (p *Plan) Render(w io.Writer) {
	sort.SliceStable(p.Items, func(i, j int) bool { return p.Items[i].Digest.String() < p.Items[j].Digest.String() })
	for _, it := range p.Items {
		name := it.Name
		if name == "" {
			name = it.Digest.String()[:19]
		}
		if it.Type != "" {
			name += " (" + it.Type + ")"
		}
		fmt.Fprintf(w, "  %s %-40s %10s  %s\n", marker[it.Strategy], name, humanBytes(it.Size), it.Strategy)
	}
	fmt.Fprintf(w, "\nPlan: %s reuse (%s), %s mount (%s), %s copy (%s)\n",
		humanBytes(p.ReuseBytes), count(p.Items, registry.StrategyReuse),
		humanBytes(p.MountBytes), count(p.Items, registry.StrategyMount),
		humanBytes(p.CopyBytes), count(p.Items, registry.StrategyCopy))
}

func count(items []Item, s registry.Strategy) string {
	n := 0
	for _, it := range items {
		if it.Strategy == s {
			n++
		}
	}
	return fmt.Sprintf("%d layers", n)
}

func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

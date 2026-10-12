// Package inspect reads and renders image/slice details (DESIGN: inspect).
package inspect

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

// LayerRow describes one layer for display.
type LayerRow struct {
	Index     int
	Digest    string
	DiffID    string
	Size      int64
	MediaType string
	Type      string
	Root      string
	Name      string
	Part      string
	Files     string
	Requires  string
	Source    string // source.image + index
	CreatedBy string
}

// MetadataFile describes one blob of a metadata artifact.
type MetadataFile struct {
	Title     string
	MediaType string
	Size      int64
	Artifact  string // artifact digest ref
}

// Report is the inspection result for one manifest (or index member).
type Report struct {
	Ref         string
	Kind        string // "slice" | "image" | "index"
	Digest      string
	Annotations map[string]string
	Platform    string // "linux/amd64", "any", "" for index parent
	Layers      []LayerRow
	Metadata    []MetadataFile // referrers files
	Children    []*Report      // index members
}

// Inspect reads ref and builds a report, following referrers.
func Inspect(ctx context.Context, client *registry.Client, ref string) (*Report, error) {
	r, err := client.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	desc, err := client.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	rep := &Report{Ref: ref, Digest: desc.Descriptor.Digest.String()}
	if desc.MediaType.IsIndex() {
		rep.Kind = "index"
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, err
		}
		repo := r.Context()
		for _, m := range im.Manifests {
			child, err := inspectManifest(ctx, client, repo.Digest(m.Digest.String()).String())
			if err != nil {
				return nil, err
			}
			if m.Platform != nil {
				child.Platform = m.Platform.OS + "/" + m.Platform.Architecture
			}
			rep.Children = append(rep.Children, child)
		}
		return rep, nil
	}
	return inspectDesc(ctx, client, ref, desc)
}

func inspectManifest(ctx context.Context, client *registry.Client, ref string) (*Report, error) {
	desc, err := client.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return inspectDesc(ctx, client, ref, desc)
}

func inspectDesc(ctx context.Context, client *registry.Client, ref string, desc *remote.Descriptor) (*Report, error) {
	m, err := v1.ParseManifest(strings.NewReader(string(desc.Manifest)))
	if err != nil {
		return nil, err
	}
	rep := &Report{
		Ref:         ref,
		Kind:        "image",
		Digest:      desc.Descriptor.Digest.String(),
		Annotations: m.Annotations,
	}
	if m.Annotations[layer.KeyKind] == layer.KindSlice {
		rep.Kind = "slice"
	}
	if m.Annotations[layer.KeyPlatform] == "any" {
		rep.Platform = "any"
	}

	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	cf, _ := img.ConfigFile()
	if rep.Platform == "" && cf != nil {
		rep.Platform = cf.OS + "/" + cf.Architecture
	}
	var hist []string
	if cf != nil {
		for _, h := range cf.History {
			if !h.EmptyLayer {
				hist = append(hist, h.CreatedBy)
			}
		}
	}
	diffIDs := map[int]string{}
	if cf != nil {
		for i, d := range cf.RootFS.DiffIDs {
			diffIDs[i] = d.String()
		}
	}
	for i, l := range m.Layers {
		row := LayerRow{
			Index: i, Digest: l.Digest.String(), Size: l.Size,
			MediaType: string(l.MediaType), DiffID: diffIDs[i],
		}
		a := l.Annotations
		row.Type = a[layer.KeyLayerType]
		row.Root = a[layer.KeyLayerRoot]
		row.Name = a[layer.KeyLayerName]
		row.Part = a[layer.KeyLayerPart]
		row.Files = a[layer.KeyLayerFiles]
		row.Requires = a[layer.KeyLayerRequires]
		if s := a[layer.KeySourceImage]; s != "" {
			row.Source = s + " #" + a[layer.KeySourceIndex]
		}
		if i < len(hist) {
			row.CreatedBy = hist[i]
		}
		rep.Layers = append(rep.Layers, row)
	}
	// Referrers.
	dref := manifestRepoRef(ref, desc.Descriptor.Digest.String())
	refs, err := client.Referrers(ctx, dref)
	if err == nil {
		for _, rd := range refs {
			art := dref
			art = strings.Split(art, "@")[0] + "@" + rd.Digest.String()
			adesc, err := client.Get(ctx, art)
			if err != nil {
				continue
			}
			am, err := v1.ParseManifest(strings.NewReader(string(adesc.Manifest)))
			if err != nil {
				continue
			}
			for _, blob := range am.Layers {
				rep.Metadata = append(rep.Metadata, MetadataFile{
					Title:     blob.Annotations["org.opencontainers.image.title"],
					MediaType: string(blob.MediaType),
					Size:      blob.Size,
					Artifact:  art,
				})
			}
		}
	}
	return rep, nil
}

func manifestRepoRef(ref, digest string) string {
	i := strings.Index(ref, "@")
	if i >= 0 {
		return ref[:i] + "@" + digest
	}
	i = strings.LastIndex(ref, ":")
	if i >= 0 && !strings.Contains(ref[i:], "/") {
		return ref[:i] + "@" + digest
	}
	return ref + "@" + digest
}

// ReadMetadataFile fetches a metadata blob's content by title.
func (rep *Report) ReadMetadataFile(ctx context.Context, client *registry.Client, title string) ([]byte, error) {
	for _, mf := range rep.allMetadata() {
		if mf.Title != title {
			continue
		}
		desc, err := client.Get(ctx, mf.Artifact)
		if err != nil {
			return nil, err
		}
		am, err := v1.ParseManifest(strings.NewReader(string(desc.Manifest)))
		if err != nil {
			return nil, err
		}
		for _, blob := range am.Layers {
			if blob.Annotations["org.opencontainers.image.title"] != title {
				continue
			}
			repo := strings.Split(mf.Artifact, "@")[0]
			return client.Blob(ctx, repo, blob.Digest)
		}
	}
	return nil, fmt.Errorf("inspect: metadata file %q not found", title)
}

func (rep *Report) allMetadata() []MetadataFile {
	out := rep.Metadata
	for _, c := range rep.Children {
		out = append(out, c.allMetadata()...)
	}
	return out
}

// Render writes a human-readable report.
func (rep *Report) Render(w io.Writer) {
	renderRep(w, rep, "")
}

func renderRep(w io.Writer, rep *Report, indent string) {
	fmt.Fprintf(w, "%s%s  %s  %s", indent, rep.Kind, rep.Ref, rep.Digest)
	if rep.Platform != "" {
		fmt.Fprintf(w, "  [%s]", rep.Platform)
	}
	fmt.Fprintln(w)
	if len(rep.Annotations) > 0 {
		var keys []string
		for k := range rep.Annotations {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s  %s = %s\n", indent, k, rep.Annotations[k])
		}
	}
	for _, l := range rep.Layers {
		fmt.Fprintf(w, "%s  [%d] %.19s %10d  %s", indent, l.Index, l.Digest, l.Size, l.MediaType)
		var extra []string
		if l.Type != "" {
			extra = append(extra, "type="+l.Type)
		}
		if l.Name != "" {
			extra = append(extra, "name="+l.Name)
		}
		if l.Root != "" {
			extra = append(extra, "root="+l.Root)
		}
		if l.Part != "" {
			extra = append(extra, "part="+l.Part)
		}
		if l.Requires != "" {
			extra = append(extra, "requires="+l.Requires)
		}
		if l.Source != "" {
			extra = append(extra, "src="+l.Source)
		}
		if len(extra) > 0 {
			fmt.Fprintf(w, "  %s", strings.Join(extra, " "))
		}
		fmt.Fprintln(w)
		if l.CreatedBy != "" {
			fmt.Fprintf(w, "%s       %s\n", indent, l.CreatedBy)
		}
	}
	for _, m := range rep.Metadata {
		fmt.Fprintf(w, "%s  meta: %s (%s, %d B)\n", indent, m.Title, m.MediaType, m.Size)
	}
	for _, c := range rep.Children {
		renderRep(w, c, indent+"  ")
	}
}

// Diff compares two manifests' layers by diff_id.
func Diff(ctx context.Context, client *registry.Client, a, b string) (string, error) {
	ra, err := Inspect(ctx, client, a)
	if err != nil {
		return "", err
	}
	rb, err := Inspect(ctx, client, b)
	if err != nil {
		return "", err
	}
	ida := map[string]LayerRow{}
	for _, l := range ra.flatLayers() {
		ida[l.DiffID] = l
	}
	idb := map[string]LayerRow{}
	for _, l := range rb.flatLayers() {
		idb[l.DiffID] = l
	}
	var sb strings.Builder
	for id, l := range ida {
		if _, ok := idb[id]; ok {
			fmt.Fprintf(&sb, "= %.19s %10d shared\n", l.Digest, l.Size)
		} else {
			fmt.Fprintf(&sb, "- %.19s %10d only in %s\n", l.Digest, l.Size, a)
		}
	}
	for id, l := range idb {
		if _, ok := ida[id]; !ok {
			fmt.Fprintf(&sb, "+ %.19s %10d only in %s\n", l.Digest, l.Size, b)
		}
	}
	return sb.String(), nil
}

func (rep *Report) flatLayers() []LayerRow {
	out := rep.Layers
	for _, c := range rep.Children {
		out = append(out, c.flatLayers()...)
	}
	return out
}

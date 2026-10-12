// Package weight implements the "weight" type: model weight directories.
package weight

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/types"
)

type weightType struct{}

func init() { types.Register(weightType{}) }

func (weightType) Name() string { return "weight" }
func (weightType) Schema() int  { return 1 }
func (weightType) Options() []types.Option {
	return []types.Option{
		{Name: "source", Usage: "provenance, e.g. hf://org/name@rev, https://, s3://"},
	}
}

func (weightType) Defaults() types.Defaults {
	return types.Defaults{
		Compression: layer.CompressionNone,
		Split:       true,
		Large:       layer.DefaultLargeThreshold,
		Small:       layer.DefaultSmallThreshold,
	}
}

func key(k string) string { return layer.TypeKey("weight", k) }

var weightExts = map[string]bool{
	".safetensors": true,
	".gguf":        true,
	".bin":         true,
	".pt":          true,
}

func (weightType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error {
	if src.State == nil {
		src.State = map[string]any{}
	}
	manifest, err := filesManifest(src)
	if err != nil {
		return err
	}
	src.State["files.json"] = manifest
	return nil
}

type fileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// filesManifest hashes every regular file and returns sorted JSON bytes.
func filesManifest(src *types.Source) ([]byte, error) {
	var files []fileEntry
	for _, e := range src.Entries {
		if !e.IsRegular() {
			continue
		}
		h := sha256.New()
		f, err := os.Open(e.Abs)
		if err != nil {
			return nil, err
		}
		_, cpErr := io.Copy(h, f)
		f.Close()
		if cpErr != nil {
			return nil, cpErr
		}
		files = append(files, fileEntry{
			Path:   e.Rel,
			Size:   e.Size,
			SHA256: hex.EncodeToString(h.Sum(nil)),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return json.MarshalIndent(files, "", "  ")
}

func (weightType) Layout(src *types.Source) []layer.Rule { return nil }

func (weightType) Annotate(src *types.Source) map[string]string {
	a := map[string]string{}
	if s := src.Opts.Get("source", ""); s != "" {
		a[key("source")] = s
	}
	// Dominant weight format by count.
	counts := map[string]int{}
	for _, e := range src.Entries {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(e.Rel)), ".")
		if weightExts["."+ext] {
			counts[ext]++
		}
	}
	best, bestN := "", 0
	for ext, n := range counts {
		if n > bestN || (n == bestN && ext < best) {
			best, bestN = ext, n
		}
	}
	if best != "" {
		a[key("format")] = best
	}
	if data, ok := src.State["files.json"].([]byte); ok {
		sum := sha256.Sum256(data)
		a[key("files.digest")] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return a
}

func (weightType) Metadata(src *types.Source) []types.MetadataFile {
	var out []types.MetadataFile
	if data, ok := src.State["files.json"].([]byte); ok {
		out = append(out, types.MetadataFile{
			Name:      "files.json",
			MediaType: layer.MediaTypeFilesJSON,
			Data:      data,
		})
	}
	for _, name := range []string{"config.json", "model_index.json"} {
		for _, e := range src.Entries {
			if e.Rel == name && e.IsRegular() {
				if data, err := os.ReadFile(e.Abs); err == nil {
					out = append(out, types.MetadataFile{
						Name:      name,
						MediaType: "application/json",
						Data:      data,
					})
				}
			}
		}
	}
	return out
}

func (weightType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
	return nil
}

// DefaultTarget implements types.Namer: hf://org/name@rev + prefix
// → <prefix>/org--name:<rev>.
func (weightType) DefaultTarget(src *types.Source, prefix string) (string, error) {
	srcURL := src.Opts.Get("source", "")
	rest, ok := strings.CutPrefix(srcURL, "hf://")
	if !ok {
		return "", fmt.Errorf("weight: cannot derive target from source %q (only hf:// supported)", srcURL)
	}
	id, rev, _ := strings.Cut(rest, "@")
	if id == "" {
		return "", fmt.Errorf("weight: malformed hf source %q", srcURL)
	}
	if rev == "" {
		rev = "latest"
	}
	repo := strings.ReplaceAll(id, "/", "--")
	if prefix == "" {
		return "", fmt.Errorf("weight: target prefix required")
	}
	return strings.TrimSuffix(prefix, "/") + "/" + repo + ":" + rev, nil
}

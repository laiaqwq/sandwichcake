package slice

import (
	"github.com/baba/sandwichcake/pkg/layer"
	stypes "github.com/baba/sandwichcake/pkg/types"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// IsSlice reports whether manifest is a sandwichcake slice.
func IsSlice(m *v1.Manifest) bool {
	return m != nil && m.Annotations[layer.KeyKind] == layer.KindSlice
}

// Parse converts a slice manifest (+ ref) into a types.Slice.
// The ref should be a repo@digest for stability.
func Parse(ref string, m *v1.Manifest) (*stypes.Slice, error) {
	s := &stypes.Slice{Ref: ref, Annotations: m.Annotations}
	for _, l := range m.Layers {
		s.Layers = append(s.Layers, stypes.LayerInfo{
			Digest:      l.Digest,
			Size:        l.Size,
			MediaType:   string(l.MediaType),
			Annotations: l.Annotations,
		})
	}
	return s, nil
}

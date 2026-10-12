// Package layer implements deterministic tar construction, splitting
// strategy, and compression for sandwichcake layers.
package layer

// Prefix is the annotation namespace for all sandwichcake annotations.
const Prefix = "com.github.baba.sandwichcake."

// Generic (framework-level) annotation keys.
const (
	KeyLayerType     = Prefix + "layer.type"
	KeyLayerSchema   = Prefix + "layer.schema"
	KeyLayerRoot     = Prefix + "layer.root"
	KeyLayerName     = Prefix + "layer.name"
	KeyLayerPart     = Prefix + "layer.part"
	KeyLayerFiles    = Prefix + "layer.files"
	KeyLayerOverlay  = Prefix + "layer.overlay"
	KeyLayerRequires = Prefix + "layer.requires"
	KeySourceImage   = Prefix + "source.image"
	KeySourceIndex   = Prefix + "source.index"
	KeyToolVersion   = Prefix + "tool.version"
	KeyKind          = Prefix + "kind"
	KeyPlatform      = Prefix + "platform"
	KeySlices        = Prefix + "slices"
)

// Kind values for KeyKind.
const (
	KindSlice = "slice"
	KindBase  = "base"
)

// Media types produced by sandwichcake.
const (
	MediaTypeMetadata   = "application/vnd.sandwichcake.metadata.v1"
	MediaTypeUVLock     = "application/vnd.sandwichcake.uv.lock"
	MediaTypePoetryLock = "application/vnd.sandwichcake.poetry.lock"
	MediaTypePipReqs    = "application/vnd.sandwichcake.pip.requirements"
	MediaTypePipFreeze  = "application/vnd.sandwichcake.pip.freeze"
	MediaTypeFilesJSON  = "application/vnd.sandwichcake.files+json"
)

// TypeKey returns the annotation key for a type-specific annotation:
// Prefix + <typeName> + "." + <key>.
func TypeKey(typeName, key string) string {
	return Prefix + typeName + "." + key
}

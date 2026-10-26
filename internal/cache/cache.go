// Package cache is a JSON-file metadata cache under
// $XDG_CACHE_HOME/sandwichcake or ~/.cache/sandwichcake
// (overridable via SANDWICHCAKE_CACHE_DIR or New(dir)).
// It caches metadata only — never blobs.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Cache is a directory of JSON files.
type Cache struct {
	dir string
	mu  sync.Mutex
}

// Dir returns the default cache directory.
func Dir() string {
	if d := os.Getenv("SANDWICHCAKE_CACHE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "sandwichcake")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sandwichcake-cache")
	}
	return filepath.Join(home, ".cache", "sandwichcake")
}

// New opens (creating if needed) the cache at dir; "" means the default.
func New(dir string) (*Cache, error) {
	if dir == "" {
		dir = Dir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) path(kind, key string) string {
	safe := filepath.Base(filepath.FromSlash(key))
	// keep digest keys readable but unique
	if key != safe {
		safe = key
	}
	return filepath.Join(c.dir, kind+"-"+sanitize(safe)+".json")
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '/' || ch == ':' || ch == '@' || ch == '\\' {
			out = append(out, '_')
		} else {
			out = append(out, ch)
		}
	}
	return string(out)
}

// Get reads a cached JSON value; ok is false if absent or unreadable.
func (c *Cache) Get(kind, key string, v any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.path(kind, key))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// Set writes v atomically (write + rename).
func (c *Cache) Set(kind, key string, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p := c.path(kind, key)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Delete removes a cache entry.
func (c *Cache) Delete(kind, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	os.Remove(c.path(kind, key))
}

// Kinds.
const (
	KindManifest = "manifest" // manifests keyed by digest
	KindDiffID   = "diffid"   // digest <-> diff_id map
	KindTarList  = "tarlist"  // tar header listings keyed by diff_id
	KindUpload   = "upload"   // resumable upload sessions keyed by repo+digest
	KindStream   = "stream"   // resumable streamed uploads keyed by repo+layer plan
	KindLayer    = "layer"    // remote layer plan -> uploaded blob
)

// StreamSession records a streamed upload whose digest is computed on the
// fly; State is the producer's opaque snapshot (hash states) at Offset.
type StreamSession struct {
	Location string `json:"location"`
	Offset   int64  `json:"offset"`
	State    []byte `json:"state"`
}

// UploadSession records a resumable blob upload.
type UploadSession struct {
	Location string `json:"location"`
	Offset   int64  `json:"offset"`
}

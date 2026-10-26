package layer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/klauspost/pgzip"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Compression selects the layer compression algorithm.
type Compression string

const (
	CompressionGzip Compression = "gzip"
	CompressionZstd Compression = "zstd"
	CompressionNone Compression = "none"
)

// ParseCompression parses "gzip"|"zstd"|"none".
func ParseCompression(s string) (Compression, error) {
	switch Compression(s) {
	case CompressionGzip, CompressionZstd, CompressionNone:
		return Compression(s), nil
	}
	return "", fmt.Errorf("layer: unknown compression %q", s)
}

// MediaType returns the OCI media type for this compression.
func (c Compression) MediaType() types.MediaType {
	switch c {
	case CompressionGzip:
		return types.OCILayer
	case CompressionZstd:
		return types.OCILayerZStd
	default:
		return types.OCIUncompressedLayer
	}
}

// Layer is a built layer backed by temp files. Implements v1.Layer.
// Call Close to remove the temp files.
type Layer struct {
	tarPath, blobPath string
	diffID, digest    v1.Hash
	size              int64
	mediaType         types.MediaType
	comp              Compression

	mu     sync.Mutex
	closed bool
}

// Build writes the group's tar to a temp file (hashing for diff_id), then
// compresses to a second temp file (hashing for digest and size).
// tmpDir must exist.
func Build(g Group, topts TarOptions, comp Compression, level int, tmpDir string) (*Layer, error) {
	tf, err := os.CreateTemp(tmpDir, "swcake-tar-*")
	if err != nil {
		return nil, err
	}
	tarPath := tf.Name()
	th := sha256.New()
	mw := io.MultiWriter(tf, th)
	if err := WriteTar(mw, g, topts); err != nil {
		tf.Close()
		os.Remove(tarPath)
		return nil, err
	}
	if err := tf.Close(); err != nil {
		os.Remove(tarPath)
		return nil, err
	}
	diffID := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(th.Sum(nil))}

	l := &Layer{tarPath: tarPath, diffID: diffID, comp: comp, mediaType: comp.MediaType()}
	if comp == CompressionNone {
		l.blobPath = tarPath
		l.digest = diffID
		fi, err := os.Stat(tarPath)
		if err != nil {
			l.Close()
			return nil, err
		}
		l.size = fi.Size()
		return l, nil
	}

	bf, err := os.CreateTemp(tmpDir, "swcake-blob-*")
	if err != nil {
		l.Close()
		return nil, err
	}
	l.blobPath = bf.Name()
	bh := sha256.New()
	src, err := os.Open(tarPath)
	if err != nil {
		bf.Close()
		l.Close()
		return nil, err
	}
	cw, err := NewCompressor(io.MultiWriter(bf, bh), comp, level)
	if err == nil {
		_, err = io.Copy(cw, src)
	}
	src.Close()
	if cw != nil {
		if cerr := cw.Close(); err == nil {
			err = cerr
		}
	}
	if cerr := bf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		l.Close()
		return nil, err
	}
	l.digest = v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(bh.Sum(nil))}
	fi, err := os.Stat(l.blobPath)
	if err != nil {
		l.Close()
		return nil, err
	}
	l.size = fi.Size()
	return l, nil
}

// NewCompressor returns the deterministic gzip/zstd writer used for layer
// blobs (level 0 = default).
func NewCompressor(w io.Writer, comp Compression, level int) (io.WriteCloser, error) {
	switch comp {
	case CompressionGzip:
		if level == 0 {
			level = pgzip.DefaultCompression
		}
		zw, err := pgzip.NewWriterLevel(w, level)
		if err != nil {
			return nil, err
		}
		return zw, nil
	case CompressionZstd:
		lvl := zstd.SpeedDefault
		if level != 0 {
			lvl = zstd.EncoderLevelFromZstd(level)
		}
		zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(lvl))
		if err != nil {
			return nil, err
		}
		return zw, nil
	}
	return nil, fmt.Errorf("layer: unknown compression %q", comp)
}

// Digest implements v1.Layer.
func (l *Layer) Digest() (v1.Hash, error) { return l.digest, nil }

// DiffID implements v1.Layer.
func (l *Layer) DiffID() (v1.Hash, error) { return l.diffID, nil }

// Compressed implements v1.Layer.
func (l *Layer) Compressed() (io.ReadCloser, error) {
	return os.Open(l.blobPath)
}

// Uncompressed implements v1.Layer.
func (l *Layer) Uncompressed() (io.ReadCloser, error) {
	return os.Open(l.tarPath)
}

// Size implements v1.Layer.
func (l *Layer) Size() (int64, error) { return l.size, nil }

// MediaType implements v1.Layer.
func (l *Layer) MediaType() (types.MediaType, error) { return l.mediaType, nil }

// Close removes the layer's temp files.
func (l *Layer) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	os.Remove(l.blobPath)
	if l.tarPath != l.blobPath {
		os.Remove(l.tarPath)
	}
	return nil
}

// TarPath returns the on-disk tar path (for caching/inspection).
func (l *Layer) TarPath() string { return l.tarPath }

// BlobPath returns the on-disk compressed blob path.
func (l *Layer) BlobPath() string { return l.blobPath }

// Compression returns the compression used.
func (l *Layer) Compression() Compression { return l.comp }

// TempDir is a helper for a per-build scratch dir.
func TempDir(parent string) (string, error) {
	return os.MkdirTemp(parent, filepath.Base(os.Args[0])+"-*")
}

package slice

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

// tarStream produces a group's uncompressed tar from its precomputed
// layout, verifying every file against its expected digest and
// optionally teeing files into keepDir. It implements
// registry.BlobStream; its State captures every hash so an upload can
// resume at any chunk boundary in a new process.
type tarStream struct {
	layout  *layer.Layout
	entries []layer.Entry
	expect  map[string]expected // by Rel
	keepDir string

	off    int64
	pi     int // current piece
	cur    io.ReadCloser
	blob   hash.Hash
	file   hash.Hash // sha256 of the current file
	git    hash.Hash // git blob sha1 of the current file, if expected
	keep   *os.File
	hashes map[string]string // Rel -> sha256 of completed files
}

type expected struct {
	SHA256, GitSHA1 string
}

func newTarStream(l *layer.Layout, entries []layer.Entry, expect map[string]expected, keepDir string) *tarStream {
	return &tarStream{layout: l, entries: entries, expect: expect, keepDir: keepDir}
}

type tarState struct {
	Blob   []byte            `json:"blob"`
	File   []byte            `json:"file,omitempty"`
	Git    []byte            `json:"git,omitempty"`
	Hashes map[string]string `json:"hashes"`
}

func marshalHash(h hash.Hash) ([]byte, error) {
	if h == nil {
		return nil, nil
	}
	return h.(encoding.BinaryMarshaler).MarshalBinary()
}

func unmarshalHash(h hash.Hash, b []byte) (hash.Hash, error) {
	if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *tarStream) State() ([]byte, error) {
	st := tarState{Hashes: s.hashes}
	var err error
	if st.Blob, err = marshalHash(s.blob); err != nil {
		return nil, err
	}
	if st.File, err = marshalHash(s.file); err != nil {
		return nil, err
	}
	if st.Git, err = marshalHash(s.git); err != nil {
		return nil, err
	}
	return json.Marshal(st)
}

func (s *tarStream) Resume(off int64, state []byte) error {
	s.closeCur()
	s.closeKeep()
	s.off, s.blob, s.file, s.git, s.hashes = off, sha256.New(), nil, nil, map[string]string{}
	s.pi = s.layout.PieceAt(off)
	if off == 0 && state == nil {
		return s.enter()
	}
	if off < 0 || off > s.layout.Size || state == nil {
		return fmt.Errorf("slice: cannot resume at %d", off)
	}
	var st tarState
	if err := json.Unmarshal(state, &st); err != nil {
		return err
	}
	var err error
	if s.blob, err = unmarshalHash(sha256.New(), st.Blob); err != nil {
		return err
	}
	for k, v := range st.Hashes {
		s.hashes[k] = v
	}
	if s.pi >= len(s.layout.Pieces) {
		return nil
	}
	p := s.layout.Pieces[s.pi]
	within := off - p.Offset
	if p.Entry < 0 || within == 0 {
		return s.enter()
	}
	// Mid-file: restore the file's hashes and the kept partial file.
	e := s.entries[p.Entry]
	if st.File == nil {
		return fmt.Errorf("slice: resume state lacks file hash for %s", e.Rel)
	}
	if s.file, err = unmarshalHash(sha256.New(), st.File); err != nil {
		return err
	}
	if st.Git != nil {
		if s.git, err = unmarshalHash(sha1.New(), st.Git); err != nil {
			return err
		}
	} else if s.expect[e.Rel].GitSHA1 != "" {
		return fmt.Errorf("slice: resume state lacks git hash for %s", e.Rel)
	}
	if s.keepDir != "" {
		f, err := os.OpenFile(s.keepPath(e.Rel), os.O_RDWR, 0o644)
		if err != nil {
			return err
		}
		if fi, err := f.Stat(); err != nil || fi.Size() < within {
			f.Close()
			return fmt.Errorf("slice: kept partial %s is shorter than the upload", e.Rel)
		}
		if err := f.Truncate(within); err != nil {
			f.Close()
			return err
		}
		if _, err := f.Seek(within, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		s.keep = f
	}
	return nil
}

func (s *tarStream) keepPath(rel string) string {
	return filepath.Join(s.keepDir, filepath.FromSlash(rel)) + ".partial"
}

// enter initializes per-file state when the stream reaches the start of
// piece s.pi.
func (s *tarStream) enter() error {
	if s.pi >= len(s.layout.Pieces) {
		return nil
	}
	p := s.layout.Pieces[s.pi]
	if p.Entry < 0 {
		return nil
	}
	e := s.entries[p.Entry]
	s.file = sha256.New()
	s.git = nil
	if s.expect[e.Rel].GitSHA1 != "" {
		s.git = sha1.New()
		fmt.Fprintf(s.git, "blob %d\x00", e.Size)
	}
	if s.keepDir != "" {
		p := s.keepPath(e.Rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		s.keep = f
	}
	return nil
}

// finishFile verifies the completed file and promotes its kept copy.
func (s *tarStream) finishFile(e layer.Entry) error {
	sum := hex.EncodeToString(s.file.Sum(nil))
	want := s.expect[e.Rel]
	if want.SHA256 != "" && want.SHA256 != sum {
		return fmt.Errorf("slice: %s: sha256 %s, upstream says %s: %w", e.Rel, sum, want.SHA256, registry.ErrAbortUpload)
	}
	if s.git != nil {
		if got := hex.EncodeToString(s.git.Sum(nil)); got != want.GitSHA1 {
			return fmt.Errorf("slice: %s: git blob %s, upstream says %s: %w", e.Rel, got, want.GitSHA1, registry.ErrAbortUpload)
		}
	}
	s.hashes[e.Rel] = sum
	s.file, s.git = nil, nil
	if s.keep != nil {
		err := s.keep.Close()
		s.keep = nil
		if err != nil {
			return err
		}
		final := filepath.Join(s.keepDir, filepath.FromSlash(e.Rel))
		return os.Rename(final+".partial", final)
	}
	return nil
}

func (s *tarStream) Read(p []byte) (int, error) {
	for {
		if s.pi >= len(s.layout.Pieces) {
			return 0, io.EOF
		}
		pc := s.layout.Pieces[s.pi]
		within := s.off - pc.Offset
		if within >= pc.Len {
			if pc.Entry >= 0 {
				s.closeCur()
				if err := s.finishFile(s.entries[pc.Entry]); err != nil {
					return 0, err
				}
			}
			s.pi++
			if err := s.enter(); err != nil {
				return 0, err
			}
			continue
		}
		if len(p) == 0 {
			return 0, nil
		}
		if rest := pc.Len - within; int64(len(p)) > rest {
			p = p[:rest]
		}
		var n int
		if pc.Entry < 0 {
			n = copy(p, pc.Data[within:])
		} else {
			e := s.entries[pc.Entry]
			if s.cur == nil {
				rc, err := e.OpenAt(within)
				if err != nil {
					return 0, err
				}
				s.cur = rc
			}
			var err error
			n, err = s.cur.Read(p)
			if err == io.EOF && n == 0 {
				return 0, fmt.Errorf("slice: %s ended at %d of %d bytes", e.Rel, within, e.Size)
			}
			if err != nil && err != io.EOF {
				s.closeCur()
				return 0, err
			}
			s.file.Write(p[:n])
			if s.git != nil {
				s.git.Write(p[:n])
			}
			if s.keep != nil {
				if _, err := s.keep.Write(p[:n]); err != nil {
					return 0, err
				}
			}
		}
		s.blob.Write(p[:n])
		s.off += int64(n)
		return n, nil
	}
}

func (s *tarStream) closeCur() {
	if s.cur != nil {
		s.cur.Close()
		s.cur = nil
	}
}

func (s *tarStream) closeKeep() {
	if s.keep != nil {
		s.keep.Close()
		s.keep = nil
	}
}

// Close releases open readers and files.
func (s *tarStream) Close() {
	s.closeCur()
	s.closeKeep()
}

func (s *tarStream) Digest() v1.Hash {
	return v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(s.blob.Sum(nil))}
}

// compStream compresses a tarStream on the fly. Compressor state cannot be
// serialized, so it only restarts from zero.
type compStream struct {
	tar   *tarStream
	comp  layer.Compression
	level int

	pr     *io.PipeReader
	blob   hash.Hash
	diffID v1.Hash
	done   chan struct{}
}

func (s *compStream) Resume(off int64, state []byte) error {
	if off != 0 || state != nil {
		return fmt.Errorf("slice: compressed layers restart from zero")
	}
	s.stop()
	if err := s.tar.Resume(0, nil); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	s.pr, s.blob, s.done = pr, sha256.New(), make(chan struct{})
	go func() {
		defer close(s.done)
		cw, err := layer.NewCompressor(pw, s.comp, s.level)
		if err == nil {
			_, err = io.Copy(cw, s.tar)
			if cerr := cw.Close(); err == nil {
				err = cerr
			}
		}
		if err == nil {
			s.diffID = s.tar.Digest()
		}
		pw.CloseWithError(err)
	}()
	return nil
}

func (s *compStream) stop() {
	if s.pr != nil {
		s.pr.CloseWithError(io.ErrClosedPipe)
		<-s.done
		s.pr = nil
	}
}

func (s *compStream) Read(p []byte) (int, error) {
	n, err := s.pr.Read(p)
	s.blob.Write(p[:n])
	return n, err
}

func (s *compStream) State() ([]byte, error) { return nil, nil }

func (s *compStream) Digest() v1.Hash {
	return v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(s.blob.Sum(nil))}
}

// streamedLayer is a v1.Layer whose blob was already uploaded.
type streamedLayer struct {
	digest, diffID v1.Hash
	size           int64
	mt             string
}

func (l *streamedLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l *streamedLayer) DiffID() (v1.Hash, error) { return l.diffID, nil }
func (l *streamedLayer) Size() (int64, error)     { return l.size, nil }
func (l *streamedLayer) Compressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("slice: layer %s was streamed to the registry", l.digest)
}
func (l *streamedLayer) Uncompressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("slice: layer %s was streamed to the registry", l.diffID)
}
func (l *streamedLayer) MediaType() (types.MediaType, error) { return types.MediaType(l.mt), nil }

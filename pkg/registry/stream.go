package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/laiaqwq/sandwichcake/internal/cache"
)

// BlobStream produces a blob whose digest is only known at EOF (OCI
// chunked uploads take the digest on the closing PUT).
type BlobStream interface {
	io.Reader
	// Resume positions the stream at off from a State snapshot; (0, nil)
	// restarts from the beginning. An error means the snapshot is unusable.
	Resume(off int64, state []byte) error
	// State snapshots the stream at its current offset (nil = this stream
	// cannot be resumed across processes, e.g. compressed output).
	State() ([]byte, error)
	// Digest is valid once Read returned io.EOF.
	Digest() v1.Hash
}

// ErrAbortUpload, wrapped by a BlobStream read error, makes StreamBlob
// cancel the upload session instead of keeping it for a later resume
// (e.g. upstream content failed verification).
var ErrAbortUpload = errors.New("upload aborted")

// StreamBlob uploads s to repo with chunked PATCHes, computing the digest
// on the fly. With a cache and a non-empty key, progress (session URL,
// offset and the stream's State) is persisted after every chunk, so a
// later call with the same key resumes where the registry left off.
func (c *Client) StreamBlob(ctx context.Context, repo name.Repository, key string, s BlobStream) (v1.Hash, int64, error) {
	tr, err := c.transport(ctx, repo, repo.Scope(transport.PushScope))
	if err != nil {
		return v1.Hash{}, 0, err
	}
	persist := c.opts.Cache != nil && key != ""
	var loc string
	var offset, min int64
	saved := false // a resumable snapshot of this session is in the cache
	if persist {
		var st cache.StreamSession
		if c.opts.Cache.Get(cache.KindStream, key, &st) {
			if srv, ok := c.queryUpload(ctx, repo, st.Location); ok && srv >= st.Offset && s.Resume(st.Offset, st.State) == nil {
				loc, offset, saved = st.Location, st.Offset, true
				// The registry accepted bytes we never recorded (crash
				// between PATCH and persist): replay them through the
				// stream's hashes without re-sending.
				if srv > offset {
					if _, err := io.CopyN(io.Discard, s, srv-offset); err != nil {
						return v1.Hash{}, 0, err
					}
					offset = srv
				}
			} else {
				c.cancelUpload(ctx, tr, st.Location)
				c.opts.Cache.Delete(cache.KindStream, key)
			}
		}
	}
	if loc == "" {
		if err := s.Resume(0, nil); err != nil {
			return v1.Hash{}, 0, err
		}
		if loc, min, err = c.startUpload(ctx, repo); err != nil {
			return v1.Hash{}, 0, err
		}
	}
	fail := func(err error) (v1.Hash, int64, error) {
		if errors.Is(err, ErrAbortUpload) || !saved {
			c.cancelUpload(ctx, tr, loc)
			if persist {
				c.opts.Cache.Delete(cache.KindStream, key)
			}
		}
		return v1.Hash{}, 0, err
	}
	size := c.opts.ChunkSize
	if min > size {
		size = min
	}
	buf := make([]byte, size)
	for {
		n, rerr := io.ReadFull(s, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return fail(rerr)
		}
		if n > 0 {
			if loc, err = c.patchStream(ctx, repo, tr, loc, buf[:n], offset); err != nil {
				return fail(err)
			}
			offset += int64(n)
			if persist {
				state, err := s.State()
				if err != nil {
					return fail(err)
				}
				if state != nil {
					saved = c.opts.Cache.Set(cache.KindStream, key, cache.StreamSession{Location: loc, Offset: offset, State: state}) == nil
				}
			}
			if c.opts.Progress != nil {
				c.opts.Progress(ProgressEvent{Repo: repo.String(), Written: offset})
			}
		}
		if rerr != nil {
			break
		}
	}
	digest := s.Digest()
	if err := c.commitUpload(ctx, tr, loc, digest); err != nil {
		return fail(err)
	}
	if persist {
		c.opts.Cache.Delete(cache.KindStream, key)
	}
	return digest, offset, nil
}

// patchStream sends one chunk, retrying and reconciling with the
// registry's recorded offset.
func (c *Client) patchStream(ctx context.Context, repo name.Repository, tr http.RoundTripper, loc string, chunk []byte, offset int64) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			c.backoff(attempt)
			if srv, ok := c.queryUpload(ctx, repo, loc); ok {
				switch srv {
				case offset + int64(len(chunk)):
					return loc, nil // the failed PATCH was in fact applied
				case offset:
				default:
					return "", fmt.Errorf("registry: upload offset %d, expected %d", srv, offset)
				}
			}
		}
		newLoc, err := c.patchChunk(ctx, tr, loc, chunk, offset)
		if err == nil {
			return newLoc, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("registry: PATCH at %d: %w", offset, lastErr)
}

// cancelUpload DELETEs an upload session (best effort).
func (c *Client) cancelUpload(ctx context.Context, tr http.RoundTripper, loc string) {
	if loc == "" {
		return
	}
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodDelete, loc, nil)
	if err != nil {
		return
	}
	if resp, err := tr.RoundTrip(req); err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

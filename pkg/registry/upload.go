package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/laiaqwq/sandwichcake/internal/cache"
)

func jsonNew(r io.Reader) *json.Decoder { return json.NewDecoder(r) }

// transport returns a cached authenticated RoundTripper for repo with the
// given scopes (e.g. repo.Scope(transport.PushScope)).
func (c *Client) transport(ctx context.Context, repo name.Repository, scopes ...string) (http.RoundTripper, error) {
	sorted := append([]string{}, scopes...)
	sort.Strings(sorted)
	key := repo.RegistryStr() + "|" + strings.Join(sorted, " ")
	c.mu.Lock()
	if tr, ok := c.transports[key]; ok {
		c.mu.Unlock()
		return tr, nil
	}
	c.mu.Unlock()
	auth, err := c.opts.Keychain.Resolve(repo.Registry)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		auth = authn.Anonymous
	}
	tr, err := transport.NewWithContext(ctx, repo.Registry, auth, c.opts.Transport, scopes)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.transports[key] = tr
	c.mu.Unlock()
	return tr, nil
}

// isRetryable reports whether err/status is worth retrying.
func isRetryable(status int, err error) bool {
	if err != nil {
		return true
	}
	return status == http.StatusTooManyRequests || status >= 500
}

func (c *Client) backoff(attempt int) {
	time.Sleep(time.Duration(1<<attempt) * 200 * time.Millisecond)
}

// uploadBlobs ensures all blobs of img (layers + config) exist in target
// repo. Layers upload concurrently; the config goes last.
func (c *Client) uploadBlobs(ctx context.Context, repo name.Repository, img v1.Image) error {
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	sem := make(chan struct{}, c.opts.Concurrency)
	errCh := make(chan error, len(layers))
	var wg sync.WaitGroup
	for _, l := range layers {
		wg.Add(1)
		go func(l v1.Layer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := c.uploadBlob(ctx, repo, l); err != nil {
				errCh <- err
			}
		}(l)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	cfg, err := img.RawConfigFile()
	if err != nil {
		return err
	}
	cn, err := img.ConfigName()
	if err != nil {
		return err
	}
	return c.uploadBlobBytes(ctx, repo, cn, bytes.NewReader(cfg), int64(len(cfg)))
}

// uploadBlob uploads one layer blob, using reuse/mount/chunked as needed.
func (c *Client) uploadBlob(ctx context.Context, repo name.Repository, l v1.Layer) error {
	d, err := l.Digest()
	if err != nil {
		return err
	}
	size, err := l.Size()
	if err != nil {
		return err
	}

	// Reuse?
	exists, err := c.BlobExists(ctx, repo, d)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Mount?
	if ml, ok := l.(*remote.MountableLayer); ok {
		src := ml.Reference.Context()
		if src.RegistryStr() == repo.RegistryStr() && src.RepositoryStr() != repo.RepositoryStr() {
			if err := c.mount(ctx, repo, src, d); err == nil {
				return nil
			}
			// fall through to upload on mount failure
		}
	}

	rc, err := l.Compressed()
	if err != nil {
		return err
	}
	defer rc.Close()
	return c.uploadBlobBytes(ctx, repo, d, rc, size)
}

// mount attempts a cross-repo blob mount. Returns nil on 201 Created.
func (c *Client) mount(ctx context.Context, dst, src name.Repository, digest v1.Hash) error {
	tr, err := c.transport(ctx, dst,
		dst.Scope(transport.PushScope), src.Scope(transport.PullScope))
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/?mount=%s&from=%s",
		dst.Scheme(), dst.RegistryStr(), dst.RepositoryStr(),
		url.QueryEscape(digest.String()), src.RepositoryStr())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	return fmt.Errorf("registry: mount %s: %s", digest, resp.Status)
}

func uploadSessionKey(repo name.Repository, digest v1.Hash) string {
	return repo.String() + "-" + digest.String()
}

// uploadBlobBytes uploads size bytes from r as digest using chunked PATCHes
// with resume support.
func (c *Client) uploadBlobBytes(ctx context.Context, repo name.Repository, digest v1.Hash, r io.Reader, size int64) error {
	key := uploadSessionKey(repo, digest)
	var sess *cache.UploadSession
	if c.opts.Cache != nil {
		var s cache.UploadSession
		if c.opts.Cache.Get(cache.KindUpload, key, &s) {
			// Validate the session: query the offset the server has.
			if off, ok := c.queryUpload(ctx, repo, s.Location); ok {
				s.Offset = off
				sess = &s
			} else {
				c.opts.Cache.Delete(cache.KindUpload, key)
			}
		}
	}
	// Skip already-uploaded bytes.
	offset := int64(0)
	if sess != nil && sess.Offset > 0 {
		if _, err := io.CopyN(io.Discard, r, sess.Offset); err != nil {
			return fmt.Errorf("registry: resume %s: %w", digest, err)
		}
		offset = sess.Offset
	} else {
		loc, err := c.startUpload(ctx, repo)
		if err != nil {
			return err
		}
		sess = &cache.UploadSession{Location: loc}
	}

	tr, err := c.transport(ctx, repo, repo.Scope(transport.PushScope))
	if err != nil {
		return err
	}

	// Send chunks.
	buf := make([]byte, c.opts.ChunkSize)
	for offset < size {
		n, rerr := io.ReadFull(r, buf)
		if rerr == io.ErrUnexpectedEOF || rerr == io.EOF {
			if n == 0 {
				break
			}
		} else if rerr != nil {
			return fmt.Errorf("registry: reading blob %s: %w", digest, rerr)
		}
		chunk := buf[:n]
		var attemptErr error
		for attempt := 0; attempt <= c.opts.Retries; attempt++ {
			if attempt > 0 {
				c.backoff(attempt)
				// Reconcile the server-side offset.
				if off, ok := c.queryUpload(ctx, repo, sess.Location); ok {
					if off > offset {
						skip := off - offset
						if skip > int64(len(chunk)) {
							offset = off
							sess.Offset = off
							chunk = nil
							break
						}
						chunk = chunk[skip:]
						offset = off
					}
				}
			}
			newLoc, aerr := c.patchChunk(ctx, tr, sess.Location, chunk, offset)
			if aerr == nil {
				sess.Location = newLoc
				offset += int64(len(chunk))
				sess.Offset = offset
				if c.opts.Cache != nil {
					c.opts.Cache.Set(cache.KindUpload, key, *sess)
				}
				if c.opts.Progress != nil {
					c.opts.Progress(ProgressEvent{Repo: repo.String(), Digest: digest.String(), Written: offset, Total: size})
				}
				break
			}
			attemptErr = aerr
			if attempt == c.opts.Retries {
				return fmt.Errorf("registry: PATCH %s at %d: %w", digest, offset, attemptErr)
			}
		}
		if chunk == nil {
			continue
		}
	}
	// Commit.
	if err := c.commitUpload(ctx, tr, sess.Location, digest); err != nil {
		return err
	}
	if c.opts.Cache != nil {
		c.opts.Cache.Delete(cache.KindUpload, key)
	}
	return nil
}

// startUpload POSTs to /v2/<repo>/blobs/uploads/ and returns the session URL.
func (c *Client) startUpload(ctx context.Context, repo name.Repository) (string, error) {
	tr, err := c.transport(ctx, repo, repo.Scope(transport.PushScope))
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/",
		repo.Scheme(), repo.RegistryStr(), repo.RepositoryStr())
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			c.backoff(attempt)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
		if err != nil {
			return "", err
		}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, resp.Body)
		if resp.StatusCode == http.StatusAccepted {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if loc == "" {
				return "", fmt.Errorf("registry: upload start missing Location")
			}
			return resolveLocation(u, loc), nil
		}
		lastErr = fmt.Errorf("start upload: %s", resp.Status)
		resp.Body.Close()
		if !isRetryable(resp.StatusCode, nil) {
			break
		}
	}
	return "", fmt.Errorf("registry: %w", lastErr)
}

// queryUpload GETs the upload status and returns the server-side offset.
func (c *Client) queryUpload(ctx context.Context, repo name.Repository, location string) (int64, bool) {
	tr, err := c.transport(ctx, repo, repo.Scope(transport.PushScope))
	if err != nil {
		return 0, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return 0, false
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, false
	}
	rng := resp.Header.Get("Range")
	// Format: "0-<end>" meaning bytes 0..end received; offset = end+1.
	if rng == "" {
		return 0, true
	}
	_, endStr, _ := strings.Cut(rng, "-")
	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		return 0, true
	}
	return end + 1, true
}

// patchChunk sends one PATCH and returns the new Location.
func (c *Client) patchChunk(ctx context.Context, tr http.RoundTripper, location string, chunk []byte, offset int64) (string, error) {
	if len(chunk) == 0 {
		return location, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, location, bytes.NewReader(chunk))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("%d-%d", offset, offset+int64(len(chunk))-1))
	req.ContentLength = int64(len(chunk))
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("PATCH: %s", resp.Status)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		loc = location
	}
	return resolveLocation(location, loc), nil
}

// commitUpload PUTs ?digest= to finish the upload.
func (c *Client) commitUpload(ctx context.Context, tr http.RoundTripper, location string, digest v1.Hash) error {
	sep := "?"
	if strings.Contains(location, "?") {
		sep = "&"
	}
	u := location + sep + "digest=" + url.QueryEscape(digest.String())
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			c.backoff(attempt)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
		if err != nil {
			return err
		}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, resp.Body)
		if resp.StatusCode == http.StatusCreated {
			resp.Body.Close()
			return nil
		}
		lastErr = fmt.Errorf("commit upload: %s", resp.Status)
		resp.Body.Close()
		if !isRetryable(resp.StatusCode, nil) {
			break
		}
	}
	return fmt.Errorf("registry: %w", lastErr)
}

// resolveLocation resolves a possibly-relative Location header.
func resolveLocation(base, loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	if u.IsAbs() {
		return loc
	}
	b, err := url.Parse(base)
	if err != nil {
		return loc
	}
	return b.ResolveReference(u).String()
}

// UploadBlob uploads raw blob bytes (e.g. config or metadata blobs).
func (c *Client) UploadBlob(ctx context.Context, repo name.Repository, digest v1.Hash, r io.Reader, size int64) error {
	exists, err := c.BlobExists(ctx, repo, digest)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.uploadBlobBytes(ctx, repo, digest, r, size)
}

// PutManifest uploads a manifest/index to ref (tag or digest). It returns
// the response headers so callers can inspect OCI-Subject.
func (c *Client) PutManifest(ctx context.Context, ref name.Reference, raw []byte, mediaType string) (http.Header, error) {
	repo := ref.Context()
	tr, err := c.transport(ctx, repo, repo.Scope(transport.PushScope))
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s",
		repo.Scheme(), repo.RegistryStr(), repo.RepositoryStr(), ref.Identifier())
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			c.backoff(attempt)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mediaType)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			h := resp.Header
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return h, nil
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		lastErr = fmt.Errorf("put manifest %s: %s: %s", ref, resp.Status, body)
		if !isRetryable(resp.StatusCode, nil) {
			break
		}
	}
	return nil, lastErr
}

// WriteImage uploads blobs then puts the image manifest.
func (c *Client) WriteImage(ctx context.Context, ref string, img v1.Image) error {
	r, err := c.ParseRef(ref)
	if err != nil {
		return err
	}
	repo := r.Context()
	if err := c.uploadBlobs(ctx, repo, img); err != nil {
		return err
	}
	raw, err := img.RawManifest()
	if err != nil {
		return err
	}
	mt, err := img.MediaType()
	if err != nil {
		return err
	}
	_, err = c.PutManifest(ctx, r, raw, string(mt))
	return err
}

// WriteIndex uploads child manifests' blobs and manifests, then the index.
func (c *Client) WriteIndex(ctx context.Context, ref string, idx v1.ImageIndex) error {
	r, err := c.ParseRef(ref)
	if err != nil {
		return err
	}
	repo := r.Context()
	im, err := idx.IndexManifest()
	if err != nil {
		return err
	}
	for _, desc := range im.Manifests {
		child, err := idx.Image(desc.Digest)
		if err != nil {
			return err
		}
		if err := c.uploadBlobs(ctx, repo, child); err != nil {
			return err
		}
		raw, err := child.RawManifest()
		if err != nil {
			return err
		}
		mt, err := child.MediaType()
		if err != nil {
			return err
		}
		dref := repo.Digest(desc.Digest.String())
		if _, err := c.PutManifest(ctx, dref, raw, string(mt)); err != nil {
			return err
		}
	}
	raw, err := idx.RawManifest()
	if err != nil {
		return err
	}
	mt, err := idx.MediaType()
	if err != nil {
		return err
	}
	_, err = c.PutManifest(ctx, r, raw, string(mt))
	return err
}

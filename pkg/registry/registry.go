// Package registry wraps go-containerregistry's remote package and adds
// resumable chunked blob uploads, cross-repo mounts, and a local metadata
// cache. All network access in sandwichcake goes through this package.
package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/laiaqwq/sandwichcake/internal/cache"
)

// DefaultChunkSize for blob uploads.
const DefaultChunkSize = 64 << 20 // 64 MiB

// Options configures a Client.
type Options struct {
	Keychain    authn.Keychain // default authn.DefaultKeychain
	Insecure    bool           // use plain HTTP
	Retries     int            // retries on transient failures (default 3)
	ChunkSize   int64          // default DefaultChunkSize
	Concurrency int            // concurrent blob uploads per image (default 4)
	Cache       *cache.Cache   // upload session + metadata cache (nil = no cache)
	Transport   http.RoundTripper
	Progress    func(ProgressEvent) // optional upload progress callback
}

// ProgressEvent reports blob upload progress.
type ProgressEvent struct {
	Repo    string
	Digest  string
	Written int64
	Total   int64
}

// Client talks to OCI registries.
type Client struct {
	opts Options

	mu         sync.Mutex
	transports map[string]http.RoundTripper // key: registry + sorted scopes
}

// New returns a Client.
func New(opts Options) *Client {
	if opts.Keychain == nil {
		opts.Keychain = authn.DefaultKeychain
	}
	if opts.Retries <= 0 {
		opts.Retries = 3
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = DefaultChunkSize
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Transport == nil {
		opts.Transport = http.DefaultTransport
	}
	return &Client{opts: opts, transports: map[string]http.RoundTripper{}}
}

// Cache returns the client's metadata cache (nil if none).
func (c *Client) Cache() *cache.Cache { return c.opts.Cache }

func (c *Client) nameOpts() []name.Option {
	if c.opts.Insecure {
		return []name.Option{name.Insecure}
	}
	return nil
}

func (c *Client) remoteOpts() []remote.Option {
	opts := []remote.Option{
		remote.WithAuthFromKeychain(c.opts.Keychain),
		remote.WithTransport(c.opts.Transport),
	}
	return opts
}

// ParseRef parses ref honoring insecure mode.
func (c *Client) ParseRef(ref string) (name.Reference, error) {
	return name.ParseReference(ref, c.nameOpts()...)
}

// Head fetches the descriptor for ref.
func (c *Client) Head(ctx context.Context, ref string) (*v1.Descriptor, error) {
	r, err := c.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return remote.Head(r, append(c.withOpts(), remote.WithContext(ctx))...)
}

func (c *Client) withOpts() []remote.Option {
	opts := c.remoteOpts()
	return opts
}

// Get fetches the full descriptor (manifest included) for ref.
func (c *Client) Get(ctx context.Context, ref string) (*remote.Descriptor, error) {
	r, err := c.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return remote.Get(r, append(c.withOpts(), remote.WithContext(ctx))...)
}

// Image resolves ref to a v1.Image for platform (empty = manifest as-is).
func (c *Client) Image(ctx context.Context, ref string, platform *v1.Platform) (v1.Image, error) {
	r, err := c.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	opts := append(c.withOpts(), remote.WithContext(ctx))
	if platform != nil {
		opts = append(opts, remote.WithPlatform(*platform))
	}
	return remote.Image(r, opts...)
}

// Index resolves ref to a v1.ImageIndex.
func (c *Client) Index(ctx context.Context, ref string) (v1.ImageIndex, error) {
	r, err := c.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return remote.Index(r, append(c.withOpts(), remote.WithContext(ctx))...)
}

// BlobExists issues HEAD /v2/<repo>/blobs/<digest>.
func (c *Client) BlobExists(ctx context.Context, repo name.Repository, digest v1.Hash) (bool, error) {
	tr, err := c.transport(ctx, repo, repo.Scope(transport.PullScope))
	if err != nil {
		return false, err
	}
	u := repo.Scheme() + "://" + repo.RegistryStr() + "/v2/" + repo.RepositoryStr() + "/blobs/" + digest.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("registry: HEAD blob %s: %s", digest, resp.Status)
}

// Blob fetches a blob's content.
func (c *Client) Blob(ctx context.Context, repo string, digest v1.Hash) ([]byte, error) {
	r, err := name.NewRepository(repo, c.nameOpts()...)
	if err != nil {
		return nil, err
	}
	tr, err := c.transport(ctx, r, r.Scope(transport.PullScope))
	if err != nil {
		return nil, err
	}
	u := r.Scheme() + "://" + r.RegistryStr() + "/v2/" + r.RepositoryStr() + "/blobs/" + digest.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("registry: GET blob %s: %s", digest, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ListTags lists all tags of repo, following pagination.
func (c *Client) ListTags(ctx context.Context, repo string) ([]string, error) {
	r, err := name.NewRepository(repo, c.nameOpts()...)
	if err != nil {
		return nil, err
	}
	return remote.List(r, append(c.withOpts(), remote.WithContext(ctx))...)
}

// Catalog lists all repos in the registry (requires registry support).
func (c *Client) Catalog(ctx context.Context, registry string) ([]string, error) {
	reg, err := name.NewRegistry(registry, c.nameOpts()...)
	if err != nil {
		return nil, err
	}
	return remote.Catalog(ctx, reg, c.withOpts()...)
}

// GARChildren lists child repos from a GAR/GCR tags/list `child` field.
func (c *Client) GARChildren(ctx context.Context, repo string) ([]string, error) {
	r, err := name.NewRepository(repo, c.nameOpts()...)
	if err != nil {
		return nil, err
	}
	tr, err := c.transport(ctx, r, r.Scope(transport.PullScope))
	if err != nil {
		return nil, err
	}
	u := r.Scheme() + "://" + r.RegistryStr() + "/v2/" + r.RepositoryStr() + "/tags/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry: tags/list %s: %s", repo, resp.Status)
	}
	var body struct {
		Child []string `json:"child"`
	}
	if err := jsonNew(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Child, nil
}

// Referrers lists artifacts whose subject is digestRef (repo@sha256:...).
func (c *Client) Referrers(ctx context.Context, digestRef string) ([]v1.Descriptor, error) {
	r, err := c.ParseRef(digestRef)
	if err != nil {
		return nil, err
	}
	d, ok := r.(name.Digest)
	if !ok {
		return nil, fmt.Errorf("registry: referrers requires a digest ref, got %s", digestRef)
	}
	idx, err := remote.Referrers(d, append(c.withOpts(), remote.WithContext(ctx))...)
	if err != nil {
		return nil, err
	}
	im, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}
	return im.Manifests, nil
}

// Delete deletes a manifest/tag.
func (c *Client) Delete(ctx context.Context, ref string) error {
	r, err := c.ParseRef(ref)
	if err != nil {
		return err
	}
	return remote.Delete(r, append(c.withOpts(), remote.WithContext(ctx))...)
}

// Tag copies the manifest at ref to newTag (server-side manifest PUT).
func (c *Client) Tag(ctx context.Context, ref, newTag string) error {
	t, err := name.NewTag(newTag, c.nameOpts()...)
	if err != nil {
		return err
	}
	desc, err := c.Get(ctx, ref)
	if err != nil {
		return err
	}
	_, err = c.PutManifest(ctx, t, desc.Manifest, string(desc.Descriptor.MediaType))
	return err
}

// Strategy is how a blob reaches the target repo (DESIGN 3).
type Strategy string

const (
	StrategyReuse Strategy = "reuse" // already present
	StrategyMount Strategy = "mount" // cross-repo mount, same registry
	StrategyCopy  Strategy = "copy"  // must upload bytes
)

// Classify determines how layer would reach targetRepo.
func (c *Client) Classify(ctx context.Context, targetRepo name.Repository, l v1.Layer) (Strategy, error) {
	d, err := l.Digest()
	if err != nil {
		return "", err
	}
	exists, err := c.BlobExists(ctx, targetRepo, d)
	if err != nil {
		return "", err
	}
	if exists {
		return StrategyReuse, nil
	}
	if ml, ok := l.(*remote.MountableLayer); ok {
		src := ml.Reference.Context()
		if src.RegistryStr() == targetRepo.RegistryStr() && src.RepositoryStr() != targetRepo.RepositoryStr() {
			return StrategyMount, nil
		}
	}
	return StrategyCopy, nil
}

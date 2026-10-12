package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// FallbackTag returns the referrers tag schema tag for a subject digest:
// repo:sha256-<hex>.
func FallbackTag(d name.Digest) name.Tag {
	return d.Context().Tag(strings.Replace(d.DigestStr(), ":", "-", 1))
}

// readFallbackIndex GETs the fallback index for the subject; returns nil if absent.
func (c *Client) readFallbackIndex(ctx context.Context, subject name.Digest) (*v1.IndexManifest, error) {
	tag := FallbackTag(subject)
	repo := tag.Context()
	tr, err := c.transport(ctx, repo, repo.Scope("pull"))
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s",
		repo.Scheme(), repo.RegistryStr(), repo.RepositoryStr(), tag.TagStr())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", string(types.OCIImageIndex))
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry: GET fallback index %s: %s", tag, resp.Status)
	}
	var idx v1.IndexManifest
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("registry: fallback index at %s not an index: %w", tag, err)
	}
	return &idx, nil
}

// writeFallbackIndex PUTs the index at the fallback tag.
func (c *Client) writeFallbackIndex(ctx context.Context, subject name.Digest, idx *v1.IndexManifest) error {
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	_, err = c.PutManifest(ctx, FallbackTag(subject), raw, string(types.OCIImageIndex))
	return err
}

// AddReferrer records desc as a referrer of subject using the OCI 1.1
// tag-schema fallback (repo:sha256-<hex> holds an image index of referrers).
func (c *Client) AddReferrer(ctx context.Context, subject name.Digest, desc v1.Descriptor) error {
	idx, err := c.readFallbackIndex(ctx, subject)
	if err != nil {
		return err
	}
	if idx == nil {
		idx = &v1.IndexManifest{SchemaVersion: 2, MediaType: types.OCIImageIndex}
	}
	var kept []v1.Descriptor
	for _, m := range idx.Manifests {
		if m.Digest == desc.Digest {
			continue // replace
		}
		kept = append(kept, m)
	}
	idx.Manifests = append(kept, desc)
	return c.writeFallbackIndex(ctx, subject, idx)
}

// RemoveReferrer drops digest from the subject's fallback index; writes the
// updated index (empty manifests => still written, harmless).
func (c *Client) RemoveReferrer(ctx context.Context, subject name.Digest, digest v1.Hash) error {
	idx, err := c.readFallbackIndex(ctx, subject)
	if err != nil || idx == nil {
		return err
	}
	var kept []v1.Descriptor
	for _, m := range idx.Manifests {
		if m.Digest != digest {
			kept = append(kept, m)
		}
	}
	if len(kept) == len(idx.Manifests) {
		return nil
	}
	idx.Manifests = kept
	return c.writeFallbackIndex(ctx, subject, idx)
}

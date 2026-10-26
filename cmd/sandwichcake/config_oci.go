package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const projectArtifact = "application/vnd.sandwichcake.project.v1"
const projectFileMediaType = "application/vnd.sandwichcake.project.file.v1"

func ociRef(source string) (string, error) {
	ref, ok := strings.CutPrefix(source, "oci://")
	if !ok || ref == "" {
		return "", fmt.Errorf("invalid OCI project source %q", source)
	}
	return ref, nil
}

func remoteVersion(ctx context.Context, source string) (string, error) {
	ref, err := ociRef(source)
	if err != nil {
		return "", err
	}
	cl, err := client()
	if err != nil {
		return "", err
	}
	desc, err := cl.Head(ctx, ref)
	if err != nil {
		var httpErr *transport.Error
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return desc.Digest.String(), nil
}

func pullOCI(ctx context.Context, source string) (map[string][]byte, string, error) {
	ref, err := ociRef(source)
	if err != nil {
		return nil, "", err
	}
	cl, err := client()
	if err != nil {
		return nil, "", err
	}
	parsed, err := cl.ParseRef(ref)
	if err != nil {
		return nil, "", err
	}
	desc, err := cl.Get(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	var manifest v1.Manifest
	if err := json.Unmarshal(desc.Manifest, &manifest); err != nil {
		return nil, "", err
	}
	if manifest.ArtifactType != projectArtifact || manifest.MediaType != types.OCIManifestSchema1 {
		return nil, "", fmt.Errorf("%s is not a sandwichcake project artifact", source)
	}
	files := make(map[string][]byte, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		p := layer.Annotations["org.opencontainers.image.title"]
		if !validSyncPath(p) || layer.MediaType != projectFileMediaType {
			return nil, "", fmt.Errorf("invalid project file descriptor %q", p)
		}
		if _, ok := files[p]; ok {
			return nil, "", fmt.Errorf("duplicate project file %q", p)
		}
		data, err := cl.Blob(ctx, parsed.Context().Name(), layer.Digest)
		if err != nil {
			return nil, "", err
		}
		if fileHash(data) != layer.Digest.String() || int64(len(data)) != layer.Size {
			return nil, "", fmt.Errorf("digest mismatch for %s", p)
		}
		files[p] = data
	}
	return files, desc.Digest.String(), nil
}

func pushOCI(ctx context.Context, source string, files map[string][]byte, state syncState, force bool) (string, error) {
	ref, err := ociRef(source)
	if err != nil {
		return "", err
	}
	before, err := remoteVersion(ctx, source)
	if err != nil {
		return "", err
	}
	if !force && !((state.Source == "" && state.Version == "" && before == "") ||
		(state.Source == source && state.Version == before)) {
		return "", fmt.Errorf("remote tag changed (recorded %s, current %s); pull first or use --force", state.Version, before)
	}
	cl, err := client()
	if err != nil {
		return "", err
	}
	parsed, err := cl.ParseRef(ref)
	if err != nil {
		return "", err
	}
	repo := parsed.Context()
	cfg := []byte("{}")
	sum := sha256.Sum256(cfg)
	cfgHash := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
	if err := cl.UploadBlob(ctx, repo, cfgHash, bytes.NewReader(cfg), int64(len(cfg))); err != nil {
		return "", err
	}
	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		ArtifactType:  projectArtifact,
		Config:        v1.Descriptor{MediaType: types.OCIEmptyJSON, Digest: cfgHash, Size: int64(len(cfg))},
	}
	for _, p := range sortedFiles(files) {
		data := files[p]
		hash := sha256.Sum256(data)
		digest := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(hash[:])}
		if err := cl.UploadBlob(ctx, repo, digest, bytes.NewReader(data), int64(len(data))); err != nil {
			return "", err
		}
		manifest.Layers = append(manifest.Layers, v1.Descriptor{
			MediaType: projectFileMediaType, Digest: digest, Size: int64(len(data)),
			Annotations: map[string]string{"org.opencontainers.image.title": p},
		})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	d := sha256.Sum256(raw)
	version := "sha256:" + hex.EncodeToString(d[:])
	// PUT by digest first, then update the tag; an interrupted upload leaves
	// the old tag intact. The HEAD above is optimistic, not an atomic CAS.
	digestRef := repo.Digest(version)
	if _, err := cl.PutManifest(ctx, digestRef, raw, string(types.OCIManifestSchema1)); err != nil {
		return "", err
	}
	if _, err := cl.PutManifest(ctx, parsed, raw, string(types.OCIManifestSchema1)); err != nil {
		return "", err
	}
	return version, nil
}

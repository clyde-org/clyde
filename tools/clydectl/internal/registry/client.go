package registry

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// ResolveImageSize returns total layer size (bytes) for the selected image variant.
func (c *Client) ResolveImageSize(ctx context.Context, imageRef string) (int64, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return 0, fmt.Errorf("parsing image reference: %w", err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	// 1. Get the image descriptor to find layers
	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return 0, fmt.Errorf("fetching image descriptor: %w", err)
	}

	img, err := c.resolveImage(ref, desc, opts)
	if err != nil {
		return 0, err
	}

	totalSize, err := totalLayerSize(img)
	if err != nil {
		return 0, err
	}
	return totalSize, nil
}

// ResolveLayer finds the largest layer and returns (blobURL, authHeader, layerSizeBytes).
func (c *Client) ResolveLayer(ctx context.Context, imageRef string) (string, string, int64, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", "", 0, fmt.Errorf("parsing image reference: %w", err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return "", "", 0, fmt.Errorf("fetching image descriptor: %w", err)
	}

	img, err := c.resolveImage(ref, desc, opts)
	if err != nil {
		return "", "", 0, err
	}

	digest, size, err := largestLayer(img)
	if err != nil {
		return "", "", 0, err
	}

	scheme := "https"
	if strings.HasPrefix(imageRef, "localhost") || strings.HasPrefix(imageRef, "http://") {
		scheme = "http"
	}
	repo := ref.Context()
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s",
		scheme, repo.RegistryStr(), repo.RepositoryStr(), digest)

	authHeader := ""
	auth, err := authn.DefaultKeychain.Resolve(repo.Registry)
	if err == nil {
		authConfig, err := auth.Authorization()
		if err == nil {
			if authConfig.RegistryToken != "" {
				authHeader = "Bearer " + authConfig.RegistryToken
			} else if authConfig.Username != "" && authConfig.Password != "" {
				authHeader = "Basic " + basicAuth(authConfig.Username, authConfig.Password)
			}
		}
	}

	return blobURL, authHeader, size, nil
}

func totalLayerSize(img v1.Image) (int64, error) {
	layers, err := img.Layers()
	if err != nil {
		return 0, fmt.Errorf("getting layers: %w", err)
	}

	var total int64
	valid := 0

	for _, layer := range layers {
		size, err := layer.Size()
		if err != nil {
			continue
		}
		total += size
		valid++
	}

	if valid == 0 {
		return 0, fmt.Errorf("no readable layers found in image")
	}

	return total, nil
}

func largestLayer(img v1.Image) (string, int64, error) {
	layers, err := img.Layers()
	if err != nil {
		return "", 0, fmt.Errorf("getting layers: %w", err)
	}
	var (
		maxSize int64
		digest  string
	)
	for _, layer := range layers {
		size, err := layer.Size()
		if err != nil {
			continue
		}
		if size > maxSize {
			d, err := layer.Digest()
			if err != nil {
				continue
			}
			maxSize = size
			digest = d.String()
		}
	}
	if digest == "" {
		return "", 0, fmt.Errorf("no readable layers found in image")
	}
	return digest, maxSize, nil
}

func (c *Client) resolveImage(baseRef name.Reference, desc *remote.Descriptor, opts []remote.Option) (v1.Image, error) {
	img, err := desc.Image()
	if err == nil {
		return img, nil
	}

	idx, idxErr := desc.ImageIndex()
	if idxErr != nil {
		return nil, fmt.Errorf("getting image from descriptor: %w", err)
	}
	idxManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("getting index manifest: %w", err)
	}

	return c.resolveImageFromIndex(baseRef, idxManifest.Manifests, opts)
}

func (c *Client) resolveImageFromIndex(baseRef name.Reference, manifests []v1.Descriptor, opts []remote.Option) (v1.Image, error) {
	// Prefer linux/arm64 first, then any linux variant, then first available.
	passes := []func(v1.Descriptor) bool{
		func(d v1.Descriptor) bool {
			return d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == "arm64"
		},
		func(d v1.Descriptor) bool {
			return d.Platform != nil && d.Platform.OS == "linux"
		},
		func(d v1.Descriptor) bool {
			return true
		},
	}

	for _, want := range passes {
		for _, d := range manifests {
			if !want(d) {
				continue
			}

			childRef, err := name.ParseReference(fmt.Sprintf("%s@%s", baseRef.Context().Name(), d.Digest.String()))
			if err != nil {
				continue
			}

			childDesc, err := remote.Get(childRef, opts...)
			if err != nil {
				continue
			}

			img, err := childDesc.Image()
			if err != nil {
				continue
			}
			return img, nil
		}
	}

	return nil, fmt.Errorf("failed to resolve an image variant from index")
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

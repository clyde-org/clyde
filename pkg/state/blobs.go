package state

import (
	"context"
	"strings"
	"fmt"
	"io"
	"os"
	"time"
	"net/http"
	"net/netip"
	"path/filepath"
	"encoding/json"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"

	"clyde/pkg/oci"
)

// Obtain local content (e.g., images and associated layers in addition to relevant information)
func getLocalBlobs(ctx context.Context, ociClient oci.Client, includedImages []string) (string, map[string]struct{}, error) {
	log := logr.FromContextOrDiscard(ctx)

	// Use the default client to get the list of available images
	imgs, err := ociClient.ListImages(ctx)
	if err != nil {
		return "", nil, err
	}

	imageLayers := make([]ImageLayers, 0)

	for _, img := range imgs {

		// Use default function to walk through the image content
		dgsts, err := oci.WalkImage(ctx, ociClient, img)
		if err != nil {
			log.Error(err, "could not walk image", 
				"image", img.String())
			continue
		}

		ImageNameStr := img.String()

		ImageTagStr := img.Tag

		if isIncludedImage(ImageNameStr, includedImages) {
			log.Info("Current image found locally is required in the inclusion list, processing it...")

			// Process this image into an appropriate structure for sharing data about local content (e.g., name, digests, tag, registry) across distributed peers
			imageLayers = append(imageLayers, ImageLayers {
				ImageName: img.String(),
				LayerKeys: dgsts,
				Registry: img.Registry,
				Tag: img.Tag,
				Digest: img.Digest.String(),
			})
		} else {
			log.Info("Excluding image tag", ImageNameStr, ImageTagStr)
		}
	}

	// Return empty array if no content was found
	if len(imageLayers) == 0 {
		return "[]", nil, nil
	}

	// Construct data in json representation from local content that was processed
	data, err := json.Marshal(imageLayers)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal json data: %w", err)
	}

	// Process the keys into a key set that will be used for comparison logic in the state tracker
	localKeySet := make(map[string]struct{})
	var localImageLayers []map[string]interface{}
	if string(data) != "" {
		if err := json.Unmarshal([]byte(string(data)), &localImageLayers); err != nil {
			return "", nil, err
		}

		for _, image := range localImageLayers {
			if layers, ok := image["layer_keys"].([]interface{}); ok {
				for _, layer := range layers {
					if key, ok := layer.(string); ok {
						localKeySet[key] = struct{}{}
					}
				}
			}
		}

	}

	// Return both the json formatted data as a string representation, and the local key set
	return string(data), localKeySet, nil
}


func isIncludedImage(imageWithTag string, images []string) bool {
	
	if len(images) == 0 {
		return false
	}
	
	for _, image := range images {
		if strings.Contains(imageWithTag, image) {
			return true
		}
	}
	return false
}

func checkBlobExists(dgst digest.Digest, ContainerdContentPath string) bool {
	if dgst.Algorithm() == "" || dgst.Encoded() == "" {
		return false
	}

	filePath := filepath.Join(ContainerdContentPath, "blobs", dgst.Algorithm().String(), dgst.Encoded())
	_, err := os.Stat(filePath)
	return err == nil
}

func fetchBlob(ctx context.Context, peer netip.AddrPort, registry, name string, dgst digest.Digest)(io.ReadCloser, error) {
	log := logr.FromContextOrDiscard(ctx)

	// The input parameter, name, has the format of imag@sha256:xxxx, so we process this string appropriately
	parts := strings.SplitN(name, "@", 2)


	// For blob requests, we should use the repository name without tag
	repoWithTag := parts[0]
	repoParts := strings.SplitN(repoWithTag, ":", 2)
	repoName := repoParts[0]

	// Constructing the url used for the http request from current parameters
	url := fmt.Sprintf("http://%s/v2/%s/blobs/%s?ns=%s", peer.String(), repoName, dgst.String(), registry)

	log.Info("attempting to fetch blob", "url", url, "digest", dgst.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error(err, "failed to create request")
		return nil, err 
	}
	req.Header.Set("X-Clyde-Mirrored", "true")

	// Set a time out, could fail if too short so give sufficient time for the remote peer to respond accordingly
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "http request failed")
		return nil, err 
	}
	defer resp.Body.Close()

	// Read the response body for better error messages
	if resp.StatusCode != http.StatusOK {
		log.Error(err, "blob fetch unsuccessful, will retry shortly...")
		return nil, err
	} else {
		log.Info("successfully fetched blob",
			"status",resp.Status,  
			"digest", dgst.String())
	}

	return resp.Body, nil
}



// Writes the image layer/blob of a specific digest to the default path where local content is stored
func writeBlobToLocalPath(r io.Reader, dgst digest.Digest, path string) error {
	dst := filepath.Join(path, "blobs", dgst.Algorithm().String(), dgst.Encoded())

	// Make sure that the path exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// House keeping activities
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write blob: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

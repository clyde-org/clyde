package state

import (
	"context"
	"fmt"
	"time"
	"sync"
	"net/netip"
	"encoding/json"
	"math/rand"
	
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	
	"clyde/pkg/oci"
	"clyde/pkg/routing"
)

func synchronise(ctx context.Context, ociClient oci.Client, router routing.Router, images []string, ContainerdContentPath string) error {
	log := logr.FromContextOrDiscard(ctx)

	// Set the global indicator to true
	setBusy(true)

	// Obtain local content
	jsonData, localSet, err := getLocalBlobs(ctx, ociClient, images)
	if err != nil {
		log.Error(
			err, 
			"error collecting local keys")
	} else {
		log.Info("extracted local content")
	}

	// Serve our keys in a suitable format (e.g., json string) for trasnmission over to peers
	if err := router.ServeKeys(ctx, jsonData); err != nil {
		log.Error(
			err, 
			"failed to serve local keys")
	}

	// Advertise our presence using the peer index so that other peers can discover me
	if err := router.Advertise(ctx, []string{routing.PeerIndexKey}); err != nil {
		log.Error(
			err, 
			"failed to advertise peer presence")
	}

	missingKeyInfo := make(map[string][]KeySource) // key -> list of sources

	// Discover peers via the peer index and fetch relevant information (e.g., metadata about their content)
	peerCh, err := router.Resolve(ctx, routing.PeerIndexKey, 0)
	if err != nil {
		log.Error(
			err, 
			"failed to resolve peer index")
		return nil
	}

	for peerAddr := range peerCh {
		remoteKeys, err := router.FetchPeerKeys(ctx, peerAddr)
		if err != nil {
			log.Error(
				err, 
				"could not fetch keys from peer", 
				"peer", 
				peerAddr)
			continue 
		}

		// Skip empty responses
		if remoteKeys == "" {
			continue 
		}

		var imageLayers []map[string]interface{}
		if err := json.Unmarshal([]byte(remoteKeys), &imageLayers); err != nil {
			log.Error(
				err, 
				"failed to parse json data from peer", 
				"peer", 
				peerAddr, 
				"response", 
				remoteKeys)
			continue
		}

		for _, image := range imageLayers {
			imageName, ok := image["image_name"].(string)
			if !ok {
				log.Info(
					"missing or invalid layer_keys in peer data", 
					"peer", 
					peerAddr, 
					"image", 
					imageName)
				continue
			}

			layers, ok := image["layer_keys"].([]interface{})
			if !ok {
				log.Info(
					"missing or invalid layer_keys in peer data", 
					"peer", 
					peerAddr, 
					"image", 
					imageName)
				continue
			}

			registry, ok := image["registry"].(string)
			if !ok {
				log.Info(
					"missing or invalid registry in peer data", 
					"peer", 
					peerAddr, 
					"image", 
					imageName)
				continue	
			}

			layerDigests := make([]string, 0, len(layers))
			for _, layer := range layers {
				if digest, ok := layer.(string); ok {
					layerDigests = append(layerDigests, digest)

					// Check if this key is missing locally and track its image context
					if _, exists := localSet[digest]; !exists {
						source := KeySource {
							PeerAddr: peerAddr.String(),
							ImageName: imageName,
							Registry: registry,
						}

						if existingSources, found := missingKeyInfo[digest]; found {
							// Check if this exact source already exists to avoid duplicates
							duplicate := false
							for _, existing := range existingSources {
								if existing.PeerAddr == source.PeerAddr && existing.ImageName == source.ImageName {
									duplicate = true
									break
								}
							}
							if !duplicate {
								// Add to missing keys
								missingKeyInfo[digest] = append(existingSources, source)
							}
						} else {
							missingKeyInfo[digest] = []KeySource{source}
						}
					}
				}
			}

			log.Info(
				"discovered image from peer", 
				"peer", 
				peerAddr, 
				"image", 
				imageName,
				"registry", 
				registry, 
				"layer_count", 
				len(layerDigests), 
				"layers", 
				layerDigests,)
		}
	}

	// Wait group for parallelising image layer downloads
	var wg sync.WaitGroup

	// Handle missing keys here
	if len(missingKeyInfo) > 0 {
		missingKeys := make([]string, 0, len(missingKeyInfo))
		for key := range missingKeyInfo {
			missingKeys = append(missingKeys, key)
		}

		log.Info(
			"found potential keys to fetch", 
			"count", 
			len(missingKeys), 
			"keys", 
			missingKeys)

		// Fetch missing keys from remote peers with image context
		for key, sources := range missingKeyInfo {
			// Now using parallelised approach to obtain missing keys concurrently rather than in sequence
			wg.Add(1)

			go func(c context.Context, k string, src []KeySource, l logr.Logger, p string) {
				defer wg.Done()
				if err := worker(c, k, src, l, p); err != nil {
					fmt.Printf("	- %s\n", err)
				}
			}(ctx, key, sources, log, ContainerdContentPath)

			wg.Wait()

			// Set the indicator back to false
			setBusy(false)
		}

	} else {
		log.Info("no missing keys found from peers")
	}

	// Defensive programming here, setting indicator to false
	setBusy(false)
	return nil

}

func worker(ctx context.Context, key string, sources []KeySource, log logr.Logger, ContainerdContentPath string) error {
	// Log all available sources for this key
	for _, source := range sources {
		log.Info(
			"key availability from source peer", 
			"key", 
			key, 
			"peer", 
			source.PeerAddr, 
			"image", 
			source.ImageName)
	}

	if len(sources) > 0 {
		source := sources[0]

		if len(sources) == 1 {
			log.Info(
				"Selected a source (peer) to fetch key from", 
				"key", 
				key, 
				"peer", 
				source.PeerAddr, 
				"registry", 
				source.Registry, 
				"image", 
				source.ImageName)
		} else {
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			source = sources[rng.Intn(len(sources))]
			log.Info(
				"Selected a source (peer) to fetch key from", 
				"key", 
				key, 
				"peer", 
				source.PeerAddr, 
				"registry", 
				source.Registry, 
				"image", 
				source.ImageName)
		}

		peerAddr, err := netip.ParseAddrPort(source.PeerAddr)
		if err != nil {
			return fmt.Errorf("invalid peer address")
		}

		dgst, err := digest.Parse(key)
		if err != nil {
			return fmt.Errorf("invalid digest")
		}

		if checkBlobExists(dgst, ContainerdContentPath) {
			log.Info("Blob already exists, skipping fetch operation", "key", key)
		} else {
			start := time.Now()
			// Try to fetch missing data from source and store it
			resp, respErr := fetchBlob(ctx, peerAddr, source.Registry, source.ImageName, dgst)

			if respErr != nil {

			} else {
				if err := writeBlobToLocalPath(resp, dgst, ContainerdContentPath); err != nil {
					log.Error(
						err, 
						"failed to write blob to containerd")
				} else {
					log.Info(
						"blob successfully stored", 
						"digest", 
						dgst.String())
				}
			}

			elapsed := time.Since(start)

			if err != nil {
				// Error fetching, log the image
				log.Error(
					err, 
					"failed to fetch key", 
					"key", 
					key, 
					"peer", 
					source.PeerAddr, 
					"image", 
					source.ImageName)
			} else {
				log.Info(
					"fetched content of key", 
					"key", 
					key, 
					"peer", 
					source.PeerAddr, 
					"image", 
					source.ImageName, 
					"time in milliseconds:", 
					elapsed.Microseconds(), 
					"time in seconds:", 
					elapsed.Seconds())
			}
		}
	}

	return nil
}

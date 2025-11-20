package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	"clyde/internal/channel"
	"clyde/pkg/hf"
	"clyde/pkg/metrics"
	"clyde/pkg/oci"
	"clyde/pkg/pip"
	"clyde/pkg/routing"
)

func Track(ctx context.Context, ociClient oci.Client, router routing.Router, resolveLatestTag bool, pipClient pip.Pip, hfClient hf.Hf, includeImages []string, ContainerdContentPath string) error {
	log := logr.FromContextOrDiscard(ctx)
	eventCh, errCh, err := ociClient.Subscribe(ctx)
	if err != nil {
		return err
	}
	immediateCh := make(chan time.Time, 1)
	immediateCh <- time.Now()
	close(immediateCh)
	expirationTicker := time.NewTicker(routing.KeyTTL - time.Minute)
	defer expirationTicker.Stop()
	tickerCh := channel.Merge(immediateCh, expirationTicker.C)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tickerCh:
			log.Info("running scheduled state update of data artifacts")

			// Perform regular updates here by advertising for all content
			if !isBusy() {
				if err := all(ctx, ociClient, router, resolveLatestTag); err != nil {
					log.Error(err, "received errors when updating all images")
					// Continue with peer synchronisation anyway
					continue
				}

				// Start synchronisation operation with remote peers for automatic peer discovery and content retrieval
				if err := synchronise(ctx, ociClient, router, includeImages, ContainerdContentPath); err != nil {
					log.Error(err, "peer sync failed")
				}
			}

			// refresh pip keys
			if _, err := syncPip(ctx, pipClient, router); err != nil {
				log.Error(err, "errors during pip resync")
			}

			if _, err := syncHF(ctx, hfClient, router); err != nil {
				log.Error(err, "errors during hf resync")
			}

		case event, ok := <-eventCh:
			if !ok {
				return errors.New("image event channel closed")
			}
			log.Info("received image event", "image", event.Image.String(), "type", event.Type)
			if _, err := update(ctx, ociClient, router, event, false, resolveLatestTag); err != nil {
				log.Error(err, "received error when updating image")
				continue
			}
		case err, ok := <-errCh:
			if !ok {
				return errors.New("image error channel closed")
			}
			log.Error(err, "event channel error")
		}
	}
}

func all(ctx context.Context, ociClient oci.Client, router routing.Router, resolveLatestTag bool) error {
	imgs, err := ociClient.ListImages(ctx)
	if err != nil {
		return err
	}

	// TODO: Update metrics on subscribed events. This will require keeping state in memory to know about key count changes.
	metrics.AdvertisedKeys.Reset()
	metrics.AdvertisedImages.Reset()
	metrics.AdvertisedImageTags.Reset()
	metrics.AdvertisedImageDigests.Reset()
	errs := []error{}
	targets := map[string]any{}
	for _, img := range imgs {
		_, skipDigests := targets[img.Digest.String()]
		// Handle the list re-sync as update events; this will also prevent the
		// update function from setting metrics values.
		event := oci.ImageEvent{Image: img, Type: oci.UpdateEvent}
		keyTotal, err := update(ctx, ociClient, router, event, skipDigests, resolveLatestTag)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		targets[img.Digest.String()] = nil
		metrics.AdvertisedKeys.WithLabelValues(img.Registry).Add(float64(keyTotal))
		metrics.AdvertisedImages.WithLabelValues(img.Registry).Add(1)
		if img.Tag == "" {
			metrics.AdvertisedImageDigests.WithLabelValues(event.Image.Registry).Add(1)
		} else {
			metrics.AdvertisedImageTags.WithLabelValues(event.Image.Registry).Add(1)
		}
	}
	return errors.Join(errs...)
}

func update(ctx context.Context, ociClient oci.Client, router routing.Router, event oci.ImageEvent, skipDigests, resolveLatestTag bool) (int, error) {
	log := logr.FromContextOrDiscard(ctx).V(4)

	imageKeys := []string{}

	// Handle image tags
	if !(!resolveLatestTag && event.Image.IsLatestTag()) {
		if tagName, ok := event.Image.TagName(); ok {
			imageKeys = append(imageKeys, tagName)
		}
	}

	// Handle delete event
	if event.Type == oci.DeleteEvent {
		metrics.AdvertisedImages.WithLabelValues(event.Image.Registry).Sub(1)
		log.Info("delete event, skipping digest and pip advertisement", "image", event.Image.String())
		return 0, nil
	}

	// Handle image digests
	if !skipDigests {
		dgsts, err := oci.WalkImage(ctx, ociClient, event.Image)
		if err != nil {
			return 0, fmt.Errorf("could not get digests for image %s: %w", event.Image.String(), err)
		}
		imageKeys = append(imageKeys, dgsts...)
	}

	// Advertise image keys
	if len(imageKeys) > 0 {
		if err := router.Advertise(ctx, imageKeys); err != nil {
			return 0, fmt.Errorf("could not advertise image keys for image %s: %w", event.Image.String(), err)
		}
		log.Info("advertised image keys", "count", len(imageKeys))
	}

	// Update metrics for new images
	if event.Type == oci.CreateEvent {
		metrics.AdvertisedImages.WithLabelValues(event.Image.Registry).Add(1)
		if event.Image.Tag == "" {
			metrics.AdvertisedImageDigests.WithLabelValues(event.Image.Registry).Add(1)
		} else {
			metrics.AdvertisedImageTags.WithLabelValues(event.Image.Registry).Add(1)
		}
	}

	log.Info("update completed", "image", event.Image.String(), "imageKeys", len(imageKeys))
	return len(imageKeys), nil
}

func syncPip(ctx context.Context, pipClient pip.Pip, router routing.Router) (int, error) {
	log := logr.FromContextOrDiscard(ctx)

	if pipClient == nil {
		log.Info("pip client not configured, skipping pip sync")
		return 0, nil
	}

	metrics.AdvertisedPipPackage.Reset()

	// Walk the pip cache directory
	pipKeys, err := pipClient.WalkPipDir(ctx)
	if err != nil {
		log.Error(err, "could not walk pip cache directory")
		return 0, err
	}

	if len(pipKeys) == 0 {
		log.Info("no pip packages found, metric will be zero")
		metrics.AdvertisedPipPackage.WithLabelValues("pip-cache").Set(0)
		return 0, nil
	}

	// Advertise pip keys to router
	if err := router.Advertise(ctx, pipKeys); err != nil {
		log.Error(err, "could not advertise pip keys")
		return 0, fmt.Errorf("could not advertise pip keys: %w", err)
	}

	metrics.AdvertisedPipPackage.WithLabelValues("pip-cache").Set(float64(len(pipKeys)))

	return len(pipKeys), nil
}

func syncHF(ctx context.Context, hfClient hf.Hf, router routing.Router) (int, error) {
	log := logr.FromContextOrDiscard(ctx)

	if hfClient == nil {
		log.Info("Hugging Face client not configured, skipping HF sync")
		return 0, nil
	}

	metrics.AdvertisedHFModel.Reset()

	// Walk the Hugging Face cache directory
	hfKeys, err := hfClient.WalkHFCacheDir(ctx)
	if err != nil {
		log.Error(err, "could not walk HF cache directory")
		return 0, err
	}

	if len(hfKeys) == 0 {
		log.Info("no Hugging Face models found, metric will be zero")
		metrics.AdvertisedHFModel.WithLabelValues("hf-cache").Set(0)
		return 0, nil
	}

	// Advertise HF keys to router
	if err := router.Advertise(ctx, hfKeys); err != nil {
		log.Error(err, "could not advertise HF keys")
		return 0, fmt.Errorf("could not advertise HF keys: %w", err)
	}

	metrics.AdvertisedHFModel.WithLabelValues("hf-cache").Set(float64(len(hfKeys)))

	return len(hfKeys), nil
}

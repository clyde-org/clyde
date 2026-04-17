package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"

	"clyde/internal/option"
	"clyde/pkg/hf"
	"clyde/pkg/metrics"
	"clyde/pkg/oci"
	"clyde/pkg/pip"
	"clyde/pkg/routing"
)

type TrackerConfig struct {
	Filters   []oci.Filter
	PipClient pip.Pip
	HfClient  hf.Hf
}

type TrackerOption = option.Option[TrackerConfig]

func WithRegistryFilters(filters []oci.Filter) TrackerOption {
	return func(cfg *TrackerConfig) error {
		cfg.Filters = filters
		return nil
	}
}

func WithPipClient(pipClient pip.Pip) TrackerOption {
	return func(cfg *TrackerConfig) error {
		cfg.PipClient = pipClient
		return nil
	}
}

func WithHfClient(hfClient hf.Hf) TrackerOption {
	return func(cfg *TrackerConfig) error {
		cfg.HfClient = hfClient
		return nil
	}
}

func Track(ctx context.Context, ociStore oci.Store, router routing.Router, opts ...TrackerOption) error {
	cfg := TrackerConfig{}
	err := option.Apply(&cfg, opts...)
	if err != nil {
		return err
	}

	eventCh, err := ociStore.Subscribe(ctx)
	if err != nil {
		return err
	}

	keys := []string{}
	imgs, err := ociStore.ListImages(ctx)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		if oci.MatchesFilter(img.Reference, cfg.Filters) {
			continue
		}
		tagName, ok := img.TagName()
		if ok {
			keys = append(keys, tagName)
			metrics.AdvertisedImageTags.WithLabelValues(img.Registry).Inc()
		}
		metrics.AdvertisedImageDigests.WithLabelValues(img.Registry).Inc()
	}
	contents, err := ociStore.ListContent(ctx)
	if err != nil {
		return err
	}
	for _, refs := range contents {
		if allReferencesMatchFilter(refs, cfg.Filters) {
			continue
		}
		for _, ref := range refs {
			metrics.AdvertisedContentDigests.WithLabelValues(ref.Registry).Inc()
		}
		keys = append(keys, refs[0].Digest.String())
	}
	err = router.Advertise(ctx, keys)
	if err != nil {
		return err
	}

	if _, err := syncPip(ctx, cfg.PipClient, router); err != nil {
		logr.FromContextOrDiscard(ctx).Error(err, "errors during initial pip sync")
	}
	if _, err := syncHF(ctx, cfg.HfClient, router); err != nil {
		logr.FromContextOrDiscard(ctx).Error(err, "errors during initial HF sync")
	}

	logr.FromContextOrDiscard(ctx).Info("waiting for store events")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-eventCh:
			if !ok {
				return errors.New("event channel closed")
			}
			err := handleEvent(ctx, router, event, cfg.Filters)
			if err != nil {
				logr.FromContextOrDiscard(ctx).Error(err, "could not handle event")
				continue
			}
		}
	}
}

func handleEvent(ctx context.Context, router routing.Router, event oci.OCIEvent, filters []oci.Filter) error {
	if oci.MatchesFilter(event.Reference, filters) {
		return nil
	}
	logr.FromContextOrDiscard(ctx).Info("OCI event", "ref", event.Reference.String(), "type", event.Type)
	switch event.Type {
	case oci.CreateEvent:
		if event.Reference.Tag != "" {
			metrics.AdvertisedImageTags.WithLabelValues(event.Reference.Registry).Inc()
		} else {
			metrics.AdvertisedContentDigests.WithLabelValues(event.Reference.Registry).Inc()
		}
		err := router.Advertise(ctx, []string{event.Reference.Identifier()})
		if err != nil {
			return err
		}
		return nil
	case oci.DeleteEvent:
		if event.Reference.Tag != "" {
			metrics.AdvertisedImageTags.WithLabelValues(event.Reference.Registry).Dec()
		} else {
			metrics.AdvertisedContentDigests.WithLabelValues(event.Reference.Registry).Dec()
		}
		err := router.Withdraw(ctx, []string{event.Reference.Identifier()})
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unhandled event type %s", event.Type)
	}
}

func allReferencesMatchFilter(refs []oci.Reference, filters []oci.Filter) bool {
	for _, ref := range refs {
		if !oci.MatchesFilter(ref, filters) {
			return false
		}
	}
	return true
}

func syncPip(ctx context.Context, pipClient pip.Pip, router routing.Router) (int, error) {
	log := logr.FromContextOrDiscard(ctx)

	if pipClient == nil {
		log.Info("pip client not configured, skipping pip sync")
		return 0, nil
	}

	metrics.AdvertisedPipPackage.Reset()

	pipKeys, err := pipClient.WalkPipDir(ctx)
	if err != nil {
		log.Error(err, "could not walk pip cache directory")
		return 0, err
	}

	if len(pipKeys) == 0 {
		metrics.AdvertisedPipPackage.WithLabelValues("pip-cache").Set(0)
		return 0, nil
	}

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

	hfKeys, err := hfClient.WalkHFCacheDir(ctx)
	if err != nil {
		log.Error(err, "could not walk HF cache directory")
		return 0, err
	}

	if len(hfKeys) == 0 {
		metrics.AdvertisedHFModel.WithLabelValues("hf-cache").Set(0)
		return 0, nil
	}

	if err := router.Advertise(ctx, hfKeys); err != nil {
		log.Error(err, "could not advertise HF keys")
		return 0, fmt.Errorf("could not advertise HF keys: %w", err)
	}

	metrics.AdvertisedHFModel.WithLabelValues("hf-cache").Set(float64(len(hfKeys)))

	return len(hfKeys), nil
}

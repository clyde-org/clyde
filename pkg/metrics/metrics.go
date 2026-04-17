package metrics

import (
	"clyde/pkg/httpx"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "clyde"

var (
	DefaultRegisterer = prometheus.DefaultRegisterer
	DefaultGatherer   = prometheus.DefaultGatherer
)

var (
	MirrorRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "mirror_requests_total",
		Help:      "Total number of mirror requests.",
	}, []string{"registry", "cache"})

	MirrorLastSuccessTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "mirror_last_success_timestamp_seconds",
		Help:      "The timestamp of the last successful mirror request.",
	})

	ResolveDurHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "resolve_duration_seconds",
		Help:      "The duration for router to resolve a peer.",
	}, []string{"router"})

	AdvertisedImageTags = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_image_tags",
		Help:      "Number of image tags advertised to be available.",
	}, []string{"registry"})

	AdvertisedImageDigests = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_image_digests",
		Help:      "Number of image digests advertised to be available.",
	}, []string{"registry"})

	AdvertisedContentDigests = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_content_digests",
		Help:      "Number of content digests advertised to be available.",
	}, []string{"registry"})

	AdvertisedPipPackage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_pip_packages",
		Help:      "Number of pip packages advertised to be available.",
	}, []string{"source"})

	AdvertisedHFModel = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_hf_models",
		Help:      "Number of Hugging Face models advertised to be available.",
	}, []string{"source"})
)

func Register() {
	DefaultRegisterer.MustRegister(MirrorRequestsTotal)
	DefaultRegisterer.MustRegister(MirrorLastSuccessTimestamp)
	DefaultRegisterer.MustRegister(ResolveDurHistogram)
	DefaultRegisterer.MustRegister(AdvertisedImageTags)
	DefaultRegisterer.MustRegister(AdvertisedImageDigests)
	DefaultRegisterer.MustRegister(AdvertisedContentDigests)
	DefaultRegisterer.MustRegister(AdvertisedPipPackage)
	DefaultRegisterer.MustRegister(AdvertisedHFModel)
	httpx.RegisterMetrics(DefaultRegisterer)
}

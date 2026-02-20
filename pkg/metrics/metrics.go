package metrics

import (
	"clyde/pkg/mux"

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

	ResolveDurHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "resolve_duration_seconds",
		Help:      "The duration for router to resolve a peer.",
	}, []string{"router"})

	AdvertisedImages = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_images",
		Help:      "Number of images advertised to be available.",
	}, []string{"registry"})

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

	AdvertisedKeys = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "advertised_keys",
		Help:      "Number of keys advertised to be available.",
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
	DefaultRegisterer.MustRegister(ResolveDurHistogram)
	DefaultRegisterer.MustRegister(AdvertisedImages)
	DefaultRegisterer.MustRegister(AdvertisedImageTags)
	DefaultRegisterer.MustRegister(AdvertisedImageDigests)
	DefaultRegisterer.MustRegister(AdvertisedKeys)
	DefaultRegisterer.MustRegister(AdvertisedPipPackage)
	DefaultRegisterer.MustRegister(AdvertisedHFModel)
	mux.RegisterMetrics(DefaultRegisterer)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"

	"clyde/internal/cleanup"
	"clyde/internal/web"
	"clyde/pkg/hf"
	"clyde/pkg/metrics"
	"clyde/pkg/oci"
	"clyde/pkg/pip"
	"clyde/pkg/registry"
	"clyde/pkg/routing"
	"clyde/pkg/state"
)

type ConfigurationCmd struct {
	ContainerdRegistryConfigPath string    `arg:"--containerd-registry-config-path,env:CONTAINERD_REGISTRY_CONFIG_PATH" default:"/etc/containerd/certs.d" help:"Directory where mirror configuration is written."`
	MirroredRegistries           []url.URL `arg:"--mirrored-registries,env:MIRRORED_REGISTRIES" help:"Registries that are configured to be mirrored, if slice is empty all registires are mirrored."`
	MirrorTargets                []url.URL `arg:"--mirror-targets,env:MIRROR_TARGETS,required" help:"registries that are configured to act as mirrors."`
	ResolveTags                  bool      `arg:"--resolve-tags,env:RESOLVE_TAGS" default:"true" help:"When true Clyde will resolve tags to digests."`
	PrependExisting              bool      `arg:"--prepend-existing,env:PREPEND_EXISTING" default:"false" help:"When true existing mirror configuration will be kept and Clyde will prepend it's configuration."`
}

type BootstrapConfig struct {
	BootstrapKind        string   `arg:"--bootstrap-kind,env:BOOTSTRAP_KIND" help:"Kind of bootsrapper to use."`
	DNSBootstrapDomain   string   `arg:"--dns-bootstrap-domain,env:DNS_BOOTSTRAP_DOMAIN" help:"Domain to use when bootstrapping using DNS."`
	HTTPBootstrapAddr    string   `arg:"--http-bootstrap-addr,env:HTTP_BOOTSTRAP_ADDR" help:"Address to serve for HTTP bootstrap."`
	HTTPBootstrapPeer    string   `arg:"--http-bootstrap-peer,env:HTTP_BOOTSTRAP_PEER" help:"Peer to HTTP bootstrap with."`
	StaticBootstrapPeers []string `arg:"--static-bootstrap-peers,env:STATIC_BOOTSTRAP_PEERS" help:"Static list of peers to bootstrap with."`
}

type RegistryCmd struct {
	BootstrapConfig
	ContainerdRegistryConfigPath string        `arg:"--containerd-registry-config-path,env:CONTAINERD_REGISTRY_CONFIG_PATH" default:"/etc/containerd/certs.d" help:"Directory where mirror configuration is written."`
	MetricsAddr                  string        `arg:"--metrics-addr,env:METRICS_ADDR" default:":9090" help:"address to serve metrics."`
	ContainerdSock               string        `arg:"--containerd-sock,env:CONTAINERD_SOCK" default:"/run/containerd/containerd.sock" help:"Endpoint of containerd service."`
	ContainerdNamespace          string        `arg:"--containerd-namespace,env:CONTAINERD_NAMESPACE" default:"k8s.io" help:"Containerd namespace to fetch images from."`
	ContainerdContentPath        string        `arg:"--containerd-content-path,env:CONTAINERD_CONTENT_PATH" default:"/var/lib/containerd/io.containerd.content.v1.content" help:"Path to Containerd content store"`
	DataDir                      string        `arg:"--data-dir,env:DATA_DIR" default:"/var/lib/clyde" help:"Directory where Clyde persists data."`
	RouterAddr                   string        `arg:"--router-addr,env:ROUTER_ADDR" default:":5001" help:"address to serve router."`
	RegistryAddr                 string        `arg:"--registry-addr,env:REGISTRY_ADDR" default:":5000" help:"address to server image registry."`
	MirroredRegistries           []url.URL     `arg:"--mirrored-registries,env:MIRRORED_REGISTRIES" help:"Registries that are configured to be mirrored, if slice is empty all registires are mirrored."`
	MirrorResolveTimeout         time.Duration `arg:"--mirror-resolve-timeout,env:MIRROR_RESOLVE_TIMEOUT" default:"20ms" help:"Max duration spent finding a mirror."`
	MirrorResolveRetries         int           `arg:"--mirror-resolve-retries,env:MIRROR_RESOLVE_RETRIES" default:"3" help:"Max amount of mirrors to attempt."`
	ResolveLatestTag             bool          `arg:"--resolve-latest-tag,env:RESOLVE_LATEST_TAG" default:"true" help:"When true latest tags will be resolved to digests."`
	DebugWebEnabled              bool          `arg:"--debug-web-enabled,env:DEBUG_WEB_ENABLED" default:"false" help:"When true enables debug web page."`
	IncludeImages				 []string	   `arg:"--include-images,env:INCLUDE_IMAGES" help:"List of images to include and the system would look for and download automatically."`

	// pip specific settings
	EnablePipProxy   bool   `arg:"--enable-pip-proxy,env:ENABLE_PIP_PROXY" default:"false" help:"Enable pip proxy endpoint"`
	PipProxyPath     string `arg:"--pip-proxy-path,env:PIP_PROXY_PATH" default:"/simple/" help:"Path prefix for pip simple index"`
	PipFallbackIndex string `arg:"--pip-fallback-index,env:PIP_FALLBACK_INDEX" default:"https://pypi.org/simple" help:"Upstream index to use when package is not found in P2P"`
	PipConfigurationCmd
	HFConfigurationCmd
}

type CleanupCmd struct {
	Addr                         string `arg:"--addr,required,env:ADDR" help:"address to run readiness probe on."`
	ContainerdRegistryConfigPath string `arg:"--containerd-registry-config-path,env:CONTAINERD_REGISTRY_CONFIG_PATH" default:"/etc/containerd/certs.d" help:"Directory where mirror configuration is written."`
}

type CleanupWaitCmd struct {
	ProbeEndpoint string        `arg:"--probe-endpoint,required,env:PROBE_ENDPOINT" help:"endpoint to probe cleanup jobs from."`
	Threshold     int           `arg:"--threshold,env:THRESHOLD" default:"3" help:"amount of consecutive successful probes to consider cleanup done."`
	Period        time.Duration `arg:"--period,env:PERIOD" default:"2s" help:"address to run readiness probe on."`
}

type PipConfigurationCmd struct {
	PipConfigPath string `arg:"--pip-config-path,env:PIP_CONFIG_PATH" default:"/etc" help:"Path to the pip configuration file."`
	PipCacheDir   string `arg:"--pip-cache-dir,env:PIP_CACHE_DIR" default:"/data/cache/pip/wheel" help:"Path to the pip cache files."`
	IndexURL      string `arg:"--index-url,env:PIP_INDEX_URL" default:"https://pypi.org/simple" help:"Base URL of the Python package index (e.g. http://host:port/simple/)."`
	TrustedHost   string `arg:"--trusted-host,env:PIP_TRUSTED_HOST" help:"Hosts that pip will treat as trusted (e.g. no SSL validation)."`
	Timeout       int    `arg:"--timeout,env:PIP_TIMEOUT" default:"15" help:"Default timeout in seconds for pip operations."`
	Proxy         string `arg:"--proxy,env:PIP_PROXY" help:"Proxy server URL in the form host:port (e.g. http://proxy:8080)."`
}

type HFConfigurationCmd struct {
	// Path to cache models/datasets. Default: ~/.cache/huggingface/hub
	HFCacheDir string `arg:"--hf-cache-dir,env:HF_HUB_CACHE" default:"/data/cache/hf/model" help:"Directory to cache Hugging Face models/datasets."`
}

type Arguments struct {
	Configuration    *ConfigurationCmd    `arg:"subcommand:configuration"`
	Registry         *RegistryCmd         `arg:"subcommand:registry"`
	Cleanup          *CleanupCmd          `arg:"subcommand:cleanup"`
	CleanupWait      *CleanupWaitCmd      `arg:"subcommand:cleanup-wait"`
	PipConfiguration *PipConfigurationCmd `arg:"subcommand:pip-configuration"`
	HFConfiguration  *HFConfigurationCmd  `arg:"subcommand:hf-configuration"`
	LogLevel         slog.Level           `arg:"--log-level,env:LOG_LEVEL" default:"INFO" help:"Minimum log level to output. Value should be DEBUG, INFO, WARN, or ERROR."`
}

func main() {
	args := &Arguments{}
	arg.MustParse(args)

	opts := slog.HandlerOptions{
		AddSource: true,
		Level:     args.LogLevel,
	}
	handler := slog.NewJSONHandler(os.Stderr, &opts)
	log := logr.FromSlogHandler(handler)
	klog.SetLogger(log)
	ctx := logr.NewContext(context.Background(), log)

	err := run(ctx, args)
	if err != nil {
		log.Error(err, "run exit with error")
		os.Exit(1)
	}
	log.Info("gracefully shutdown")
}

func run(ctx context.Context, args *Arguments) error {
	log := logr.FromContextOrDiscard(ctx)
	log.Info("In run processing ", args)
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGTERM)
	defer cancel()
	switch {
	case args.Configuration != nil:
		return configurationCommand(ctx, args.Configuration)
	case args.Registry != nil:
		return registryCommand(ctx, args.Registry)
	case args.Cleanup != nil:
		return cleanupCommand(ctx, args.Cleanup)
	case args.PipConfiguration != nil:
		return pipConfigurationCommand(ctx, args.PipConfiguration)
	case args.HFConfiguration != nil:
		return hfConfigurationCommand(ctx, args.HFConfiguration)
	default:
		return errors.New("unknown subcommand")
	}
}

func configurationCommand(ctx context.Context, args *ConfigurationCmd) error {
	username, password, err := loadBasicAuth()
	if err != nil {
		return err
	}
	err = oci.AddMirrorConfiguration(ctx, args.ContainerdRegistryConfigPath, args.MirroredRegistries, args.MirrorTargets, args.ResolveTags, args.PrependExisting, username, password)
	if err != nil {
		return err
	}
	return nil
}

func pipConfigurationCommand(ctx context.Context, args *PipConfigurationCmd) error {
	err := pip.AddPipConfiguration(ctx, args.PipConfigPath, args.IndexURL, args.TrustedHost, args.Timeout, args.Proxy)
	if err != nil {
		return err
	}
	return nil
}

func hfConfigurationCommand(ctx context.Context, args *HFConfigurationCmd) error {
	// This sets env vars only — Hugging Face libs rely on them directly.
	err := hf.AddHFConfiguration(ctx, args.HFCacheDir)
	if err != nil {
		return err
	}
	return nil
}

func registryCommand(ctx context.Context, args *RegistryCmd) (err error) {
	log := logr.FromContextOrDiscard(ctx)
	g, ctx := errgroup.WithContext(ctx)

	username, password, err := loadBasicAuth()
	if err != nil {
		return err
	}

	// OCI Client
	ociClient, err := oci.NewContainerd(args.ContainerdSock, args.ContainerdNamespace, args.ContainerdRegistryConfigPath, args.MirroredRegistries, oci.WithContentPath(args.ContainerdContentPath))
	if err != nil {
		return err
	}
	err = ociClient.Verify(ctx)
	if err != nil {
		return err
	}

	// Router
	_, registryPort, err := net.SplitHostPort(args.RegistryAddr)
	if err != nil {
		return err
	}
	bootstrapper, err := getBootstrapper(args.BootstrapConfig)
	if err != nil {
		return err
	}
	routerOpts := []routing.P2PRouterOption{
		routing.WithDataDir(args.DataDir),
		routing.WithIncludeImages(args.IncludeImages),
	}
	router, err := routing.NewP2PRouter(ctx, args.RouterAddr, bootstrapper, registryPort, routerOpts...)
	if err != nil {
		return err
	}
	g.Go(func() error {
		return router.Run(ctx)
	})

	hfClient := hf.NewHFClient(
		router,
		args.HFCacheDir,
		hf.WithHFRetries(5),
		hf.WithHFTimeout(300*time.Second),
		hf.WithHFLogger(log),
	)

	// Create the proxy instance
	// "/data/cache/pip/wheel",   // pipArgs.PipCacheDir,
	// "https://pypi.org/simple", // pipArgs.IndexURL,
	pipClient := pip.NewPipClient(
		router,
		args.PipCacheDir,
		args.IndexURL, //same as args.PipConfigurationCmd.IndexURL
		pip.WithResolveTimeout(300*time.Second),
		pip.WithResolveRetries(5),
		pip.WithLogger(log),
	)

	// State tracking
	g.Go(func() error {
		err := state.Track(ctx, ociClient, router, args.ResolveLatestTag, pipClient, hfClient, args.IncludeImages, args.ContainerdContentPath)
		if err != nil {
			return err
		}
		return nil
	})

	// Registry
	registryOpts := []registry.RegistryOption{
		registry.WithResolveLatestTag(args.ResolveLatestTag),
		registry.WithResolveRetries(args.MirrorResolveRetries),
		registry.WithResolveTimeout(args.MirrorResolveTimeout),
		registry.WithLogger(log),
		registry.WithBasicAuth(username, password),
	}

	reg, err := registry.NewRegistry(ociClient, router, pipClient, hfClient, registryOpts...)
	if err != nil {
		return err
	}
	regSrv, err := reg.Server(args.RegistryAddr)
	if err != nil {
		return err
	}
	g.Go(func() error {
		if err := regSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return regSrv.Shutdown(shutdownCtx)
	})

	// Metrics
	metrics.Register()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.DefaultGatherer, promhttp.HandlerOpts{}))
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	if args.DebugWebEnabled {
		web, err := web.NewWeb(router)
		if err != nil {
			return err
		}
		mux.Handle("/debug/web/", web.Handler(log))
	}

	metricsSrv := &http.Server{
		Addr:    args.MetricsAddr,
		Handler: mux,
	}
	g.Go(func() error {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return metricsSrv.Shutdown(shutdownCtx)
	})

	log.Info("running Clyde", "registry", args.RegistryAddr, "router", args.RouterAddr)
	err = g.Wait()
	if err != nil {
		return err
	}
	return nil
}

func cleanupCommand(ctx context.Context, args *CleanupCmd) error {
	err := cleanup.Run(ctx, args.Addr, args.ContainerdRegistryConfigPath)
	if err != nil {
		return err
	}
	return nil
}

func cleanupWaitCommand(ctx context.Context, args *CleanupWaitCmd) error {
	err := cleanup.Wait(ctx, args.ProbeEndpoint, args.Period, args.Threshold)
	if err != nil {
		return err
	}
	return nil
}

func getBootstrapper(cfg BootstrapConfig) (routing.Bootstrapper, error) { //nolint: ireturn // Return type can be different structs.
	switch cfg.BootstrapKind {
	case "dns":
		return routing.NewDNSBootstrapper(cfg.DNSBootstrapDomain, 10), nil
	case "http":
		return routing.NewHTTPBootstrapper(cfg.HTTPBootstrapAddr, cfg.HTTPBootstrapPeer), nil
	case "static":
		return routing.NewStaticBootstrapperFromStrings(cfg.StaticBootstrapPeers)
	default:
		return nil, fmt.Errorf("unknown bootstrap kind %s", cfg.BootstrapKind)
	}
}

func loadBasicAuth() (string, string, error) {
	dirPath := "/etc/secrets/basic-auth"
	username, err := os.ReadFile(filepath.Join(dirPath, "username"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	password, err := os.ReadFile(filepath.Join(dirPath, "password"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	return string(username), string(password), nil
}

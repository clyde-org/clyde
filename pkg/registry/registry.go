package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"clyde/pkg/hf"
	"clyde/pkg/metrics"
	"clyde/pkg/mux"
	"clyde/pkg/oci"
	"clyde/pkg/pip"
	"clyde/pkg/routing"
)

const (
	MirroredHeaderKey = "X-Spegel-Mirrored"
)

type RegistryConfig struct {
	Client           *http.Client
	Log              logr.Logger
	Username         string
	Password         string
	ResolveRetries   int
	ResolveLatestTag bool
	ResolveTimeout   time.Duration
}

func (cfg *RegistryConfig) Apply(opts ...RegistryOption) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(cfg); err != nil {
			return err
		}
	}
	return nil
}

type RegistryOption func(cfg *RegistryConfig) error

func WithResolveRetries(resolveRetries int) RegistryOption {
	return func(cfg *RegistryConfig) error {
		cfg.ResolveRetries = resolveRetries
		return nil
	}
}

func WithResolveLatestTag(resolveLatestTag bool) RegistryOption {
	return func(cfg *RegistryConfig) error {
		cfg.ResolveLatestTag = resolveLatestTag
		return nil
	}
}

func WithResolveTimeout(resolveTimeout time.Duration) RegistryOption {
	return func(cfg *RegistryConfig) error {
		cfg.ResolveTimeout = resolveTimeout
		return nil
	}
}

func WithTransport(transport http.RoundTripper) RegistryOption {
	return func(cfg *RegistryConfig) error {
		if cfg.Client == nil {
			cfg.Client = &http.Client{}
		}
		cfg.Client.Transport = transport
		return nil
	}
}

func WithLogger(log logr.Logger) RegistryOption {
	return func(cfg *RegistryConfig) error {
		cfg.Log = log
		return nil
	}
}

func WithBasicAuth(username, password string) RegistryOption {
	return func(cfg *RegistryConfig) error {
		cfg.Username = username
		cfg.Password = password
		return nil
	}
}

type Registry struct {
	client           *http.Client
	bufferPool       *sync.Pool
	log              logr.Logger
	ociClient        oci.Client
	pipClient        pip.Pip
	hfClient         hf.Hf
	router           routing.Router
	username         string
	password         string
	resolveRetries   int
	resolveTimeout   time.Duration
	resolveLatestTag bool
}

func NewRegistry(ociClient oci.Client, router routing.Router, pipClient pip.Pip, hfClient hf.Hf, opts ...RegistryOption) (*Registry, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transporn is not of type http.Transport")
	}
	cfg := RegistryConfig{
		Client: &http.Client{
			Transport: transport.Clone(),
		},
		Log:              logr.Discard(),
		ResolveRetries:   3,
		ResolveLatestTag: true,
		ResolveTimeout:   20 * time.Millisecond,
	}
	err := cfg.Apply(opts...)
	if err != nil {
		return nil, err
	}

	bufferPool := &sync.Pool{
		New: func() any {
			buf := make([]byte, 32*1024)
			return &buf
		},
	}
	r := &Registry{
		ociClient:        ociClient,
		pipClient:        pipClient,
		hfClient:         hfClient,
		router:           router,
		client:           cfg.Client,
		log:              cfg.Log,
		resolveRetries:   cfg.ResolveRetries,
		resolveLatestTag: cfg.ResolveLatestTag,
		resolveTimeout:   cfg.ResolveTimeout,
		username:         cfg.Username,
		password:         cfg.Password,
		bufferPool:       bufferPool,
	}
	return r, nil
}

func (r *Registry) Server(addr string) (*http.Server, error) {
	r.log.Info("----------------------Starting Data Registry----------------------")
	m := mux.NewServeMux(r.log)
	m.Handle("GET /healthz", r.readyHandler)
	m.Handle("GET /v2/", r.registryHandler)
	m.Handle("HEAD /v2/", r.registryHandler)

	// Only register Pip routes if pipClient is not nil
	if r.pipClient != nil {
		m.Handle("GET /simple/", r.pipClient.PipRegistryHandler)
		m.Handle("HEAD /simple/", r.pipClient.PipRegistryHandler)
		m.Handle("GET /packages/", r.pipClient.PipRegistryHandler)
		m.Handle("HEAD /packages/", r.pipClient.PipRegistryHandler)
	}

	// Only register HF routes if hfClient is not nil
	if r.hfClient != nil {
		m.Handle("GET /huggingface/", r.hfClient.HuggingFaceRegistryHandler)
		m.Handle("HEAD /huggingface/", r.hfClient.HuggingFaceRegistryHandler)
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: m,
	}
	return srv, nil
}

func (r *Registry) readyHandler(rw mux.ResponseWriter, req *http.Request) {
	r.log.V(4).Info("entered readyHandler")
	rw.SetHandler("ready")

	ok, err := r.router.Ready(req.Context())
	if err != nil {
		r.log.V(4).Info("router readiness check failed", "error", err)
		rw.WriteError(http.StatusInternalServerError, fmt.Errorf("could not determine router readiness: %w", err))
		return
	}

	if !ok {
		r.log.V(4).Info("router not ready")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.log.V(4).Info("router ready, returning 200")
}

func (r *Registry) registryHandler(rw mux.ResponseWriter, req *http.Request) {
	r.log.V(4).Info("entered registryHandler", "path", req.URL.Path)
	rw.SetHandler("registry")

	// Check basic authentication
	if r.username != "" || r.password != "" {
		r.log.V(4).Info("checking basic authentication")
		username, password, _ := req.BasicAuth()
		if r.username != username || r.password != password {
			r.log.V(4).Info("invalid basic authentication", "username", username)
			rw.WriteError(http.StatusUnauthorized, errors.New("invalid basic authentication"))
			return
		}
	}

	// Quickly return 200 for /v2 to indicate that registry supports v2.
	if path.Clean(req.URL.Path) == "/v2" {
		r.log.V(4).Info("handling /v2 endpoint")
		rw.SetHandler("v2")
		rw.WriteHeader(http.StatusOK)
		return
	}

	// Parse out path components from request.
	r.log.V(4).Info("parsing OCI distribution path", "url", req.URL.String())
	dist, err := oci.ParseDistributionPath(req.URL)
	if err != nil {
		r.log.V(4).Info("failed to parse OCI distribution path", "error", err)
		rw.WriteError(http.StatusNotFound, fmt.Errorf("could not parse path according to OCI distribution spec: %w", err))
		return
	}

	// Request with mirror header are proxied.
	if req.Header.Get(MirroredHeaderKey) != "true" {
		r.log.V(4).Info("checking local cache before mirroring", "dist", dist.Reference())
		req.Header.Set(MirroredHeaderKey, "true")

		var ociErr error
		if dist.Digest == "" {
			_, ociErr = r.ociClient.Resolve(req.Context(), dist.Reference())
		} else {
			_, ociErr = r.ociClient.Size(req.Context(), dist.Digest)
		}

		if ociErr != nil {
			r.log.V(4).Info("local cache miss, forwarding to mirror", "error", ociErr)
			rw.SetHandler("mirror")
			r.handleMirror(rw, req, dist)
			return
		}
	}

	// Serve registry endpoints.
	switch dist.Kind {
	case oci.DistributionKindManifest:
		r.log.V(4).Info("serving manifest", "ref", dist.Reference())
		rw.SetHandler("manifest")
		r.handleManifest(rw, req, dist)
	case oci.DistributionKindBlob:
		r.log.V(4).Info("serving blob", "ref", dist.Reference())
		rw.SetHandler("blob")
		r.handleBlob(rw, req, dist)
	default:
		r.log.V(4).Info("unknown distribution path kind", "kind", dist.Kind)
		rw.WriteError(http.StatusNotFound, fmt.Errorf("unknown distribution path kind %s", dist.Kind))
	}
}

func (r *Registry) handleMirror(rw mux.ResponseWriter, req *http.Request, dist oci.DistributionPath) {
	log := r.log.WithValues("ref", dist.Reference(), "path", req.URL.Path)
	log.V(4).Info("entered handleMirror")

	defer func() {
		cacheType := "hit"
		if rw.Status() != http.StatusOK && rw.Status() != http.StatusPartialContent {
			cacheType = "miss"
		}
		log.V(4).Info("mirror request completed", "status", rw.Status(), "cacheType", cacheType)
		metrics.MirrorRequestsTotal.WithLabelValues(dist.Registry, cacheType).Inc()
	}()

	if !r.resolveLatestTag && dist.IsLatestTag() {
		log.V(4).Info("skipping mirror for latest tag", "image", dist.Reference())
		rw.WriteHeader(http.StatusNotFound)
		return
	}

	resolveCtx, cancel := context.WithTimeout(req.Context(), r.resolveTimeout)
	defer cancel()
	resolveCtx = logr.NewContext(resolveCtx, log)
	log.V(4).Info("resolving mirror", "ref", dist.Reference())

	peerCh, err := r.router.Resolve(resolveCtx, dist.Reference(), r.resolveRetries)
	if err != nil {
		log.V(4).Info("failed to resolve mirror", "error", err)
		rw.WriteError(http.StatusInternalServerError, fmt.Errorf("error occurred when attempting to resolve mirrors: %w", err))
		return
	}

	mirrorAttempts := 0
	for {
		select {
		case <-req.Context().Done():
			log.V(4).Info("mirror request context cancelled")
			rw.WriteError(http.StatusNotFound, fmt.Errorf("mirroring for image component %s has been cancelled: %w", dist.Reference(), resolveCtx.Err()))
			return
		case peer, ok := <-peerCh:
			if !ok {
				err = fmt.Errorf("mirror with image component %s could not be found", dist.Reference())
				if mirrorAttempts > 0 {
					err = errors.Join(err, fmt.Errorf("requests to %d mirrors failed, all attempts exhausted", mirrorAttempts))
				}
				log.V(4).Info("no mirrors available", "attempts", mirrorAttempts)
				rw.WriteError(http.StatusNotFound, err)
				return
			}

			mirrorAttempts++
			log.V(4).Info("attempting mirror request", "attempt", mirrorAttempts, "mirror", peer)

			err := r.forwardRequest(r.client, r.bufferPool, req, rw, peer)
			if err != nil {
				log.Error(err, "request to mirror failed", "attempt", mirrorAttempts, "mirror", peer)
				continue
			}

			log.V(4).Info("mirrored request successful", "attempt", mirrorAttempts, "mirror", peer)
			return
		}
	}
}

func (r *Registry) handleManifest(rw mux.ResponseWriter, req *http.Request, dist oci.DistributionPath) {
	r.log.V(4).Info("entered handleManifest", "ref", dist.Reference(), "digest", dist.Digest)

	if dist.Digest == "" {
		r.log.V(4).Info("resolving digest for reference", "ref", dist.Reference())
		dgst, err := r.ociClient.Resolve(req.Context(), dist.Reference())
		if err != nil {
			r.log.V(4).Info("failed to resolve digest", "error", err)
			rw.WriteError(http.StatusNotFound, fmt.Errorf("could not get digest for image %s: %w", dist.Reference(), err))
			return
		}
		dist.Digest = dgst
		r.log.V(4).Info("resolved digest successfully", "digest", dist.Digest)
	}

	r.log.V(4).Info("fetching manifest content", "digest", dist.Digest)
	b, mediaType, err := r.ociClient.GetManifest(req.Context(), dist.Digest)
	if err != nil {
		r.log.V(4).Info("failed to get manifest content", "digest", dist.Digest, "error", err)
		rw.WriteError(http.StatusNotFound, fmt.Errorf("could not get manifest content for digest %s: %w", dist.Digest.String(), err))
		return
	}

	rw.Header().Set("Content-Type", mediaType)
	rw.Header().Set("Content-Length", strconv.FormatInt(int64(len(b)), 10))
	rw.Header().Set("Docker-Content-Digest", dist.Digest.String())

	if req.Method == http.MethodHead {
		r.log.V(4).Info("HEAD request, headers only", "digest", dist.Digest)
		return
	}

	_, err = rw.Write(b)
	if err != nil {
		r.log.Error(err, "error occurred when writing manifest", "digest", dist.Digest)
		return
	}

	r.log.V(4).Info("completed handleManifest successfully", "digest", dist.Digest)
}

func (r *Registry) handleBlob(rw mux.ResponseWriter, req *http.Request, dist oci.DistributionPath) {
	r.log.V(4).Info("entered handleBlob", "digest", dist.Digest)

	size, err := r.ociClient.Size(req.Context(), dist.Digest)
	if err != nil {
		r.log.V(4).Info("failed to get blob size", "digest", dist.Digest, "error", err)
		rw.WriteError(http.StatusInternalServerError, fmt.Errorf("could not determine size of blob with digest %s: %w", dist.Digest.String(), err))
		return
	}

	r.log.V(4).Info("blob size determined", "digest", dist.Digest, "size", size)
	rw.Header().Set("Accept-Ranges", "bytes")
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	rw.Header().Set("Docker-Content-Digest", dist.Digest.String())

	if req.Method == http.MethodHead {
		r.log.V(4).Info("HEAD request, skipping content body", "digest", dist.Digest)
		return
	}

	r.log.V(4).Info("fetching blob content", "digest", dist.Digest)
	rc, err := r.ociClient.GetBlob(req.Context(), dist.Digest)
	if err != nil {
		r.log.V(4).Info("failed to get blob reader", "digest", dist.Digest, "error", err)
		rw.WriteError(http.StatusInternalServerError, fmt.Errorf("could not get reader for blob with digest %s: %w", dist.Digest.String(), err))
		return
	}
	defer rc.Close()

	http.ServeContent(rw, req, "", time.Time{}, rc)
	r.log.V(4).Info("completed handleBlob successfully", "digest", dist.Digest)
}

func (r *Registry) forwardRequest(client *http.Client, bufferPool *sync.Pool, req *http.Request, rw http.ResponseWriter, addrPort netip.AddrPort) error {
	log := r.log.WithValues("addrPort", addrPort, "path", req.URL.Path)
	log.V(4).Info("entered forwardRequest")
	r.log.V(4).Info("entered forwardRequest")

	forwardScheme := "http"
	if req.TLS != nil {
		forwardScheme = "https"
	}

	u := &url.URL{
		Scheme:   forwardScheme,
		Host:     addrPort.String(),
		Path:     req.URL.Path,
		RawQuery: req.URL.RawQuery,
	}

	log.V(4).Info("creating forward request", "url", u.String())
	forwardReq, err := http.NewRequestWithContext(req.Context(), req.Method, u.String(), nil)
	if err != nil {
		log.V(4).Info("failed to create forward request", "error", err)
		return err
	}
	copyHeader(forwardReq.Header, req.Header)

	forwardResp, err := client.Do(forwardReq)
	if err != nil {
		log.V(4).Info("forward request failed", "url", u.String(), "error", err)
		return err
	}
	defer forwardResp.Body.Close()

	if forwardResp.StatusCode != http.StatusOK && forwardResp.StatusCode != http.StatusPartialContent {
		log.V(4).Info("mirror returned non-success status", "status", forwardResp.Status)
		_, err = io.Copy(io.Discard, forwardResp.Body)
		if err != nil {
			log.V(4).Info("error discarding response body", "error", err)
			return err
		}
		return fmt.Errorf("expected mirror to respond with 200 OK or 206 PartialContent but received: %s", forwardResp.Status)
	}

	copyHeader(rw.Header(), forwardResp.Header)
	rw.WriteHeader(forwardResp.StatusCode)

	buf := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buf)

	log.V(4).Info("copying response body from mirror", "status", forwardResp.Status)
	_, err = io.CopyBuffer(rw, forwardResp.Body, *buf)
	if err != nil {
		log.Error(err, "failed to copy response body from mirror")
		return err
	}

	log.V(4).Info("completed forwardRequest successfully", "status", forwardResp.Status)
	return nil
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

package pip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"clyde/pkg/mux"
	"clyde/pkg/routing"

	"github.com/go-logr/logr"
)

type PipClient struct {
	Router         routing.Router
	PipCacheDir    string
	FallbackIndex  string
	ResolveTimeout time.Duration
	ResolveRetries int
	Log            logr.Logger
	Client         *http.Client
}

// Constructor for PipProxy
func NewPipClient(router routing.Router, pipCacheDir, fallbackIndex string, opts ...PipOption) *PipClient {
	// default config
	cfg := PipConfig{
		Router:         router,
		PipCacheDir:    pipCacheDir,
		FallbackIndex:  fallbackIndex,
		ResolveTimeout: 60 * time.Second,
		ResolveRetries: 3,
		Log:            logr.Discard(),
		Client:         &http.Client{},
	}

	// apply options
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &PipClient{
		Router:         cfg.Router,
		PipCacheDir:    cfg.PipCacheDir,
		FallbackIndex:  cfg.FallbackIndex,
		ResolveTimeout: cfg.ResolveTimeout,
		ResolveRetries: cfg.ResolveRetries,
		Log:            cfg.Log,
		Client:         cfg.Client,
	}
}

// PipConfig holds configuration for constructing PipProxy
type PipConfig struct {
	Router         routing.Router
	ConfigPath     string
	PipCacheDir    string
	FallbackIndex  string
	ResolveTimeout time.Duration
	ResolveRetries int
	Log            logr.Logger
	Client         *http.Client
}

// PipOption modifies PipConfig
type PipOption func(cfg *PipConfig)

// Options

func WithResolveTimeout(timeout time.Duration) PipOption {
	return func(cfg *PipConfig) {
		cfg.ResolveTimeout = timeout
	}
}

func WithResolveRetries(retries int) PipOption {
	return func(cfg *PipConfig) {
		cfg.ResolveRetries = retries
	}
}

func WithLogger(log logr.Logger) PipOption {
	return func(cfg *PipConfig) {
		cfg.Log = log
	}
}

func WithHTTPClient(client *http.Client) PipOption {
	return func(cfg *PipConfig) {
		cfg.Client = client
	}
}

func (p *PipClient) PipRegistryHandler(rw mux.ResponseWriter, req *http.Request) {
	start := time.Now()
	cleanPath := path.Clean(req.URL.Path)
	p.Log.Info("incoming pip request", "path", cleanPath, "remote", req.RemoteAddr, "method", req.Method)

	// Serve root simple index
	if cleanPath == "/simple" || cleanPath == "/simple/" {
		p.Log.Info("serving root simple index")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("clyde pip simple index"))
		return
	}

	// Determine type of request
	isArtifact := strings.HasPrefix(cleanPath, "/packages/")
	isIndex := strings.HasPrefix(cleanPath, "/simple/") && !isArtifact

	// Extract package/artifact name
	name := filepath.Base(cleanPath)
	if name == "" {
		p.Log.Error(nil, "missing package name or file", "path", cleanPath)
		http.Error(rw, "missing package name or file", http.StatusBadRequest)
		return
	}
	p.Log.Info("parsed package/artifact name from URL", "name", name, "isArtifact", isArtifact, "isIndex", isIndex)

	// Normalize path for upstream requests
	trimmedPath := cleanPath
	if isIndex {
		trimmedPath = strings.TrimPrefix(trimmedPath, "/simple/")
	} else if isArtifact {
		trimmedPath = strings.TrimPrefix(trimmedPath, "/packages/")
	}
	p.Log.Info("normalized path for upstream/fallback", "trimmedPath", trimmedPath)

	// Compute P2P key using full artifact name for uniqueness
	keyName := strings.ToLower(name)
	key := fmt.Sprintf("pip:%s", keyName)
	p.Log.Info("computed P2P key", "key", key, "isIndex", isIndex, "isArtifact", isArtifact)

	// ==== LOCAL CACHE CHECK ====
	cacheDir := filepath.Join(p.PipCacheDir)
	cacheFile := filepath.Join(cacheDir, name)
	if isIndex {
		cacheFile += ".html"
	}
	if _, err := os.Stat(cacheFile); err == nil {
		p.Log.Info("serving from local cache", "name", name, "file", cacheFile)
		http.ServeFile(rw, req, cacheFile)
		p.Log.Info("request completed from local cache", "duration", time.Since(start))
		return
	}
	p.Log.Info("local cache miss", "file", cacheFile)

	// ==== P2P RESOLUTION ====
	ctx, cancel := context.WithTimeout(req.Context(), p.ResolveTimeout)
	defer cancel()
	p.Log.Info("resolving package via P2P", "key", key)
	peerCh, err := p.Router.Resolve(ctx, key, p.ResolveRetries)
	if err == nil {
		count := 0
		for peer := range peerCh {
			count++
			p.Log.Info("got peer from P2P", "peer", peer, "key", key, "attempt", count)
			if err := p.forwardRequest(req, rw, peer.String(), name); err == nil {
				p.Log.Info("served pip resource from peer", "name", name, "peer", peer)
				p.Log.Info("request completed via P2P", "duration", time.Since(start))
				return
			} else {
				p.Log.Error(err, "peer lookup failed", "name", name, "peer", peer, "attempt", count)
			}
		}
		if count == 0 {
			p.Log.Info("no peers resolved for key", "key", key)
		}
	} else {
		p.Log.Error(err, "failed to resolve P2P peers", "key", key)
	}

	// ==== FALLBACK TO UPSTREAM ====
	p.Log.Info("falling back to upstream index/artifact", "name", name, "isArtifact", isArtifact, "isIndex", isIndex)
	p.serveFromFallback(rw, req, name, isIndex, isArtifact, trimmedPath)

	// Advertise after fallback caching
	// if isArtifact && (strings.HasSuffix(name, ".whl") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".metadata")) {
	// 	go p.cacheAndAdvertise(context.Background(), name, "", false)
	// } else if isIndex {
	// 	go p.cacheAndAdvertise(context.Background(), name, "", true)
	// }

	p.Log.Info("request completed via fallback", "duration", time.Since(start))
}

func (p *PipClient) serveFromFallback(
	rw http.ResponseWriter,
	req *http.Request,
	name string,
	isIndex bool,
	isArtifact bool,
	trimmedPath string,
) {
	start := time.Now()
	p.Log.Info("serveFromFallback started", "originalURL", req.URL.Path, "trimmedPath", trimmedPath, "name", name, "isIndex", isIndex, "isArtifact", isArtifact)

	if isIndex && !strings.HasSuffix(trimmedPath, "/") {
		trimmedPath += "/"
		p.Log.Info("appended trailing slash for index URL", "trimmedPath", trimmedPath)
	}

	// Determine upstream URL
	var upstreamURL string
	if isIndex {
		upstreamURL = fmt.Sprintf("%s/%s", strings.TrimSuffix(p.FallbackIndex, "/"), trimmedPath)
	} else if isArtifact {
		upstreamURL = fmt.Sprintf("https://files.pythonhosted.org/packages/%s", trimmedPath)
	} else {
		upstreamURL = fmt.Sprintf("%s/%s", strings.TrimSuffix(p.FallbackIndex, "/"), trimmedPath)
	}
	p.Log.Info("upstream URL computed", "upstreamURL", upstreamURL)

	client := &http.Client{
		Timeout: p.ResolveTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}
	p.Log.Info("HTTP client created with proxy from environment")

	reqUpstream, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, nil)
	if err != nil {
		p.Log.Error(err, "failed to create upstream request", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	reqUpstream.Header.Set("User-Agent", "Clyde-PipProxy/1.0")
	p.Log.Info("sending upstream request", "method", req.Method, "url", upstreamURL)

	resp, err := client.Do(reqUpstream)
	if err != nil {
		p.Log.Error(err, "failed to fetch from upstream", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to fetch from upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	p.Log.Info("upstream response received", "status", resp.StatusCode, "finalURL", resp.Request.URL.String())

	// Copy headers except Content-Length
	for k, vv := range resp.Header {
		if strings.ToLower(k) == "content-length" {
			continue
		}
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)

	finalName := name
	if isArtifact {
		finalName = filepath.Base(resp.Request.URL.Path)
		p.Log.Info("final artifact name determined from upstream", "finalName", finalName)
	}

	if isIndex {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			p.Log.Error(err, "failed to read index body")
			http.Error(rw, "failed to read index body", http.StatusInternalServerError)
			return
		}
		p.Log.Info("index body read", "bodySize", len(body))

		// Rewrite index HTML: replace PyPI URLs with local /packages
		rewritten := strings.ReplaceAll(string(body), "https://files.pythonhosted.org/packages/", "/packages/")
		rewritten = strings.ReplaceAll(rewritten, "https://pypi.org/simple/", "/simple/")

		// Serve rewritten body to client
		if _, err := rw.Write([]byte(rewritten)); err != nil {
			p.Log.Error(err, "failed to write rewritten index to client")
		} else {
			p.Log.Info("successfully served rewritten index HTML", "bytesCopied", len(rewritten), "duration", time.Since(start))
		}

		// Cache the rewritten body
		cacheDir := filepath.Join(p.PipCacheDir)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			p.Log.Error(err, "failed to create cache directory", "dir", cacheDir)
		} else {
			cacheFile := filepath.Join(cacheDir, finalName+".html")
			if err := os.WriteFile(cacheFile, []byte(rewritten), 0o644); err != nil {
				p.Log.Error(err, "failed to cache rewritten index", "file", cacheFile)
			} else {
				p.Log.Info("rewritten index cached successfully", "file", cacheFile, "bytesWritten", len(rewritten))
				// Advertise cached index
				go func() {
					keyName := strings.ToLower(finalName)
					key := fmt.Sprintf("pip:%s", keyName)
					if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
						p.Log.Error(err, "failed to advertise cached index", "name", finalName, "key", key)
					} else {
						p.Log.Info("advertised cached index", "name", finalName, "key", key)
					}
				}()
			}
		}

		return
	}

	// === Artifact caching (.whl, .tar.gz, .metadata) ===
	if isArtifact && (strings.HasSuffix(finalName, ".whl") || strings.HasSuffix(finalName, ".tar.gz")) {
		cacheDir := filepath.Join(p.PipCacheDir)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			p.Log.Error(err, "failed to create cache directory", "dir", cacheDir)
		} else {
			cachePath := filepath.Join(cacheDir, finalName)
			f, err := os.Create(cachePath)
			if err != nil {
				p.Log.Error(err, "failed to create cache file", "file", cachePath)
				_, _ = io.Copy(rw, resp.Body)
				return
			}
			defer f.Close()

			// TeeReader streams to both client and file
			tee := io.TeeReader(resp.Body, f)
			n, err := io.Copy(rw, tee)
			if err != nil {
				p.Log.Error(err, "failed to stream artifact to client/cache", "file", cachePath, "bytesCopied", n)
				return
			}

			// Advertise cached artifact
			go func() {
				keyName := strings.ToLower(finalName)
				key := fmt.Sprintf("pip:%s", keyName)
				if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
					p.Log.Error(err, "failed to advertise cached artifact", "name", finalName, "key", key)
				} else {
					p.Log.Info("advertised cached artifact", "name", finalName, "file", cachePath, "key", key)
				}
			}()

			p.Log.Info("served and cached artifact from upstream", "file", cachePath, "bytesCopied", n, "duration", time.Since(start))
			return
		}
	}

	// Default: stream upstream response
	n, err := io.Copy(rw, resp.Body)
	if err != nil {
		p.Log.Error(err, "failed to stream response to client", "package", name, "bytesCopied", n)
	} else {
		p.Log.Info("upstream response served to client", "package", name, "bytesCopied", n, "duration", time.Since(start))
	}

	p.Log.Info("serveFromFallback completed", "duration", time.Since(start))
}

func (p *PipClient) forwardRequest(req *http.Request, rw http.ResponseWriter, peerAddr, name string) error {
	start := time.Now()

	// Construct peer URL
	u := &url.URL{
		Scheme: "http",
		Host:   peerAddr,
		Path:   req.URL.Path,
	}
	p.Log.Info("forwarding request to peer",
		"peer", peerAddr,
		"package", name,
		"peerURL", u.String(),
		"method", req.Method,
	)

	forwardReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		p.Log.Error(err, "failed to create peer request", "peer", peerAddr, "url", u.String())
		return err
	}
	copyHeader(forwardReq.Header, req.Header)

	resp, err := p.Client.Do(forwardReq)
	if err != nil {
		p.Log.Error(err, "failed to contact peer", "peer", peerAddr, "url", u.String())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		p.Log.Error(nil, "unexpected peer status", "peer", peerAddr, "status", resp.Status, "package", name)
		return fmt.Errorf("unexpected peer status: %s", resp.Status)
	}

	// Copy headers to client
	copyHeader(rw.Header(), resp.Header)
	rw.WriteHeader(resp.StatusCode)

	// Determine local cache path
	cacheDir := filepath.Join(p.PipCacheDir)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		p.Log.Error(err, "failed to create cache directory", "dir", cacheDir)
	}
	cacheFile := filepath.Join(cacheDir, name)
	if strings.HasPrefix(req.URL.Path, "/simple/") {
		cacheFile += ".html"
	}

	// TeeReader: stream to client and write to cache
	var reader io.Reader = resp.Body
	var f *os.File
	if f, err = os.Create(cacheFile); err != nil {
		p.Log.Error(err, "failed to create cache file, serving client only", "file", cacheFile)
	} else {
		defer f.Close()
		reader = io.TeeReader(resp.Body, f)
	}

	// Stream response to client
	n, err := io.Copy(rw, reader)
	if err != nil {
		p.Log.Error(err, "failed streaming peer response to client/cache", "peer", peerAddr, "package", name, "bytesCopied", n)
		return err
	}

	p.Log.Info("successfully served from peer and cached locally",
		"peer", peerAddr,
		"package", name,
		"bytesCopied", n,
		"cacheFile", cacheFile,
		"duration", time.Since(start),
	)

	// Advertise to P2P
	keyName := strings.ToLower(name)
	key := fmt.Sprintf("pip:%s", keyName)
	if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
		p.Log.Error(err, "failed to advertise package from peer", "name", name, "key", key)
	} else {
		p.Log.Info("advertised package to P2P after peer fetch", "name", name, "key", key)
	}

	return nil
}

// ==== LOCAL SERVING ====
func (p *PipClient) serveLocalWheel(rw http.ResponseWriter, req *http.Request, name string) bool {
	cacheFile := filepath.Join(p.PipCacheDir, name)
	p.Log.Info("checking local wheel cache", "file", cacheFile)

	if _, err := os.Stat(cacheFile); err == nil {
		p.Log.Info("found cached wheel, serving to client", "file", cacheFile)
		if err := func() error {
			// Wrap in func to log errors from ServeFile
			defer func() { p.Log.Info("finished serving cached wheel", "file", cacheFile) }()
			http.ServeFile(rw, req, cacheFile)
			return nil
		}(); err != nil {
			p.Log.Error(err, "error while serving cached wheel", "file", cacheFile)
		}
		return true
	}

	p.Log.Info("local wheel not found", "file", cacheFile)
	return false
}

func (p *PipClient) serveLocalIndex(rw http.ResponseWriter, req *http.Request, pkg string) bool {
	cacheDir := filepath.Join(p.PipCacheDir)
	p.Log.Info("building local index from cached wheels", "package", pkg, "cacheDir", cacheDir)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		p.Log.Error(err, "failed to read cache directory", "dir", cacheDir)
		return false
	}

	var links []string
	for _, e := range entries {
		lowerName := strings.ToLower(e.Name())
		if strings.HasPrefix(lowerName, strings.ToLower(pkg)+"-") &&
			(strings.HasSuffix(lowerName, ".whl") || strings.HasSuffix(lowerName, ".tar.gz")) {
			links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, e.Name(), e.Name()))
			p.Log.Info("adding wheel to local index", "file", e.Name())
		}
	}

	if len(links) == 0 {
		p.Log.Info("no cached wheels found for package, cannot serve index", "package", pkg)
		return false
	}

	indexHTML := strings.Join(links, "\n")
	rw.Header().Set("Content-Type", "text/html")
	rw.WriteHeader(http.StatusOK)
	_, err = rw.Write([]byte(indexHTML))
	if err != nil {
		p.Log.Error(err, "failed to write index HTML", "package", pkg)
	} else {
		p.Log.Info("served local index successfully", "package", pkg, "count", len(links))
	}
	return true
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func AddPipConfiguration(ctx context.Context, configDir string, indexURL string, trustedHost string, timeout int, proxy string) error {
	log := logr.FromContextOrDiscard(ctx)

	// Always write to /etc/pip.conf
	pipConfigPath := filepath.Join(configDir, "pip.conf")

	// Build pip.conf
	conf := "[global]\n"
	if indexURL != "" {
		conf += fmt.Sprintf("index-url = %s\n", indexURL)
	}
	if trustedHost != "" {
		conf += fmt.Sprintf("trusted-host = %s\n", trustedHost)
	}
	if timeout > 0 {
		conf += fmt.Sprintf("timeout = %d\n", timeout)
	}
	if proxy != "" {
		conf += fmt.Sprintf("proxy = %s\n", proxy)
	}

	// Ensure directory exists (/etc)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create pip config directory %s: %w", configDir, err)
	}

	// Write pip.conf
	if err := os.WriteFile(pipConfigPath, []byte(conf), 0644); err != nil {
		return fmt.Errorf("failed to write pip config: %w", err)
	}

	log.Info("pip configuration written", "path", pipConfigPath)
	return nil
}

func (p *PipClient) WalkPipDir(ctx context.Context) ([]string, error) {
	var keys []string

	err := filepath.Walk(p.PipCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Only consider .whl, .tar.gz, and index HTML files
		lower := strings.ToLower(info.Name())
		if strings.HasSuffix(lower, ".whl") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".html") {
			key := fmt.Sprintf("pip:%s", strings.ToLower(info.Name()))
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk pip cache dir: %w", err)
	}

	return keys, nil
}

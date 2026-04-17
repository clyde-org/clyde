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

	"clyde/pkg/httpx"
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

func NewPipClient(router routing.Router, pipCacheDir, fallbackIndex string, opts ...PipOption) *PipClient {
	cfg := PipConfig{
		Router:         router,
		PipCacheDir:    pipCacheDir,
		FallbackIndex:  fallbackIndex,
		ResolveTimeout: 60 * time.Second,
		ResolveRetries: 3,
		Log:            logr.Discard(),
		Client:         &http.Client{},
	}

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

type PipOption func(cfg *PipConfig)

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

func (p *PipClient) PipRegistryHandler(rw httpx.ResponseWriter, req *http.Request) {
	start := time.Now()
	cleanPath := path.Clean(req.URL.Path)
	p.Log.Info("incoming pip request", "path", cleanPath, "remote", req.RemoteAddr, "method", req.Method)

	if cleanPath == "/simple" || cleanPath == "/simple/" {
		p.Log.Info("serving root simple index")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("clyde pip simple index"))
		return
	}

	isArtifact := strings.HasPrefix(cleanPath, "/packages/")
	isIndex := strings.HasPrefix(cleanPath, "/simple/") && !isArtifact

	name := filepath.Base(cleanPath)
	if name == "" {
		p.Log.Error(nil, "missing package name or file", "path", cleanPath)
		http.Error(rw, "missing package name or file", http.StatusBadRequest)
		return
	}
	p.Log.Info("parsed package/artifact name from URL", "name", name, "isArtifact", isArtifact, "isIndex", isIndex)

	trimmedPath := cleanPath
	if isIndex {
		trimmedPath = strings.TrimPrefix(trimmedPath, "/simple/")
	} else if isArtifact {
		trimmedPath = strings.TrimPrefix(trimmedPath, "/packages/")
	}

	keyName := strings.ToLower(name)
	key := fmt.Sprintf("pip:%s", keyName)
	p.Log.Info("computed P2P key", "key", key, "isIndex", isIndex, "isArtifact", isArtifact)

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

	ctx, cancel := context.WithTimeout(req.Context(), p.ResolveTimeout)
	defer cancel()
	p.Log.Info("resolving package via P2P", "key", key)
	balancer, err := p.Router.Lookup(ctx, key, p.ResolveRetries)
	if err == nil {
		for attempt := range p.ResolveRetries {
			peer, peerErr := balancer.Next()
			if peerErr != nil {
				p.Log.Info("no more peers available", "key", key, "error", peerErr)
				break
			}
			p.Log.Info("got peer from P2P", "peer", peer, "key", key, "attempt", attempt+1)
			if err := p.forwardRequest(req, rw, peer.String(), name); err == nil {
				p.Log.Info("served pip resource from peer", "name", name, "peer", peer)
				p.Log.Info("request completed via P2P", "duration", time.Since(start))
				return
			} else {
				p.Log.Error(err, "peer lookup failed", "name", name, "peer", peer, "attempt", attempt+1)
				balancer.Remove(peer)
			}
		}
	} else {
		p.Log.Error(err, "failed to resolve P2P peers", "key", key)
	}

	p.Log.Info("falling back to upstream index/artifact", "name", name, "isArtifact", isArtifact, "isIndex", isIndex)
	p.serveFromFallback(rw, req, name, isIndex, isArtifact, trimmedPath)
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
	}

	var upstreamURL string
	if isIndex {
		upstreamURL = fmt.Sprintf("%s/%s", strings.TrimSuffix(p.FallbackIndex, "/"), trimmedPath)
	} else if isArtifact {
		upstreamURL = fmt.Sprintf("https://files.pythonhosted.org/packages/%s", trimmedPath)
	} else {
		upstreamURL = fmt.Sprintf("%s/%s", strings.TrimSuffix(p.FallbackIndex, "/"), trimmedPath)
	}

	client := &http.Client{
		Timeout: p.ResolveTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	reqUpstream, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, nil)
	if err != nil {
		p.Log.Error(err, "failed to create upstream request", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	reqUpstream.Header.Set("User-Agent", "Clyde-PipProxy/1.0")

	resp, err := client.Do(reqUpstream)
	if err != nil {
		p.Log.Error(err, "failed to fetch from upstream", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to fetch from upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

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
	}

	if isIndex {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			p.Log.Error(err, "failed to read index body")
			http.Error(rw, "failed to read index body", http.StatusInternalServerError)
			return
		}

		rewritten := strings.ReplaceAll(string(body), "https://files.pythonhosted.org/packages/", "/packages/")
		rewritten = strings.ReplaceAll(rewritten, "https://pypi.org/simple/", "/simple/")

		if _, err := rw.Write([]byte(rewritten)); err != nil {
			p.Log.Error(err, "failed to write rewritten index to client")
		}

		cacheDir := filepath.Join(p.PipCacheDir)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			p.Log.Error(err, "failed to create cache directory", "dir", cacheDir)
		} else {
			cacheFile := filepath.Join(cacheDir, finalName+".html")
			if err := os.WriteFile(cacheFile, []byte(rewritten), 0o644); err != nil {
				p.Log.Error(err, "failed to cache rewritten index", "file", cacheFile)
			} else {
				go func() {
					keyName := strings.ToLower(finalName)
					key := fmt.Sprintf("pip:%s", keyName)
					if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
						p.Log.Error(err, "failed to advertise cached index", "name", finalName, "key", key)
					}
				}()
			}
		}

		return
	}

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

			tee := io.TeeReader(resp.Body, f)
			n, err := io.Copy(rw, tee)
			if err != nil {
				p.Log.Error(err, "failed to stream artifact to client/cache", "file", cachePath, "bytesCopied", n)
				return
			}

			go func() {
				keyName := strings.ToLower(finalName)
				key := fmt.Sprintf("pip:%s", keyName)
				if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
					p.Log.Error(err, "failed to advertise cached artifact", "name", finalName, "key", key)
				}
			}()

			p.Log.Info("served and cached artifact from upstream", "file", cachePath, "bytesCopied", n, "duration", time.Since(start))
			return
		}
	}

	n, err := io.Copy(rw, resp.Body)
	if err != nil {
		p.Log.Error(err, "failed to stream response to client", "package", name, "bytesCopied", n)
	}
}

func (p *PipClient) forwardRequest(req *http.Request, rw http.ResponseWriter, peerAddr, name string) error {
	start := time.Now()

	u := &url.URL{
		Scheme: "http",
		Host:   peerAddr,
		Path:   req.URL.Path,
	}

	forwardReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	copyHeader(forwardReq.Header, req.Header)

	resp, err := p.Client.Do(forwardReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected peer status: %s", resp.Status)
	}

	copyHeader(rw.Header(), resp.Header)
	rw.WriteHeader(resp.StatusCode)

	cacheDir := filepath.Join(p.PipCacheDir)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		p.Log.Error(err, "failed to create cache directory", "dir", cacheDir)
	}
	cacheFile := filepath.Join(cacheDir, name)
	if strings.HasPrefix(req.URL.Path, "/simple/") {
		cacheFile += ".html"
	}

	var reader io.Reader = resp.Body
	var f *os.File
	if f, err = os.Create(cacheFile); err != nil {
		p.Log.Error(err, "failed to create cache file, serving client only", "file", cacheFile)
	} else {
		defer f.Close()
		reader = io.TeeReader(resp.Body, f)
	}

	n, err := io.Copy(rw, reader)
	if err != nil {
		return err
	}

	p.Log.Info("successfully served from peer and cached locally",
		"peer", peerAddr,
		"package", name,
		"bytesCopied", n,
		"cacheFile", cacheFile,
		"duration", time.Since(start),
	)

	keyName := strings.ToLower(name)
	key := fmt.Sprintf("pip:%s", keyName)
	if err := p.Router.Advertise(context.Background(), []string{key}); err != nil {
		p.Log.Error(err, "failed to advertise package from peer", "name", name, "key", key)
	}

	return nil
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

	pipConfigPath := filepath.Join(configDir, "pip.conf")

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

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create pip config directory %s: %w", configDir, err)
	}

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

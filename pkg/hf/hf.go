package hf

import (
	"clyde/pkg/httpx"
	"clyde/pkg/routing"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

type HFClient struct {
	Log            logr.Logger
	HFCacheDir     string
	Router         routing.Router
	Client         *http.Client
	ResolveTimeout time.Duration
	ResolveRetries int
	BaseURL        string
}

type HFConfig struct {
	Router         routing.Router
	HFCacheDir     string
	ResolveTimeout time.Duration
	ResolveRetries int
	Log            logr.Logger
	Client         *http.Client
	BaseURL        string
}

type HFOption func(*HFConfig)

func WithHFLogger(log logr.Logger) HFOption {
	return func(cfg *HFConfig) {
		cfg.Log = log
	}
}

func WithHFTimeout(d time.Duration) HFOption {
	return func(cfg *HFConfig) {
		cfg.ResolveTimeout = d
	}
}

func WithHFRetries(n int) HFOption {
	return func(cfg *HFConfig) {
		cfg.ResolveRetries = n
	}
}

func WithHFHTTPClient(client *http.Client) HFOption {
	return func(cfg *HFConfig) {
		cfg.Client = client
	}
}

func WithHFBaseURL(url string) HFOption {
	return func(c *HFConfig) {
		c.BaseURL = url
	}
}

func NewHFClient(router routing.Router, cacheDir string, opts ...HFOption) *HFClient {
	cfg := HFConfig{
		Router:         router,
		HFCacheDir:     cacheDir,
		ResolveTimeout: 60 * time.Second,
		ResolveRetries: 3,
		Log:            logr.Discard(),
		BaseURL:        "https://huggingface.co",
		Client: &http.Client{
			Timeout:   60 * time.Minute,
			Transport: http.DefaultTransport,
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &HFClient{
		Router:         cfg.Router,
		HFCacheDir:     cfg.HFCacheDir,
		ResolveTimeout: cfg.ResolveTimeout,
		ResolveRetries: cfg.ResolveRetries,
		Log:            cfg.Log,
		Client:         cfg.Client,
		BaseURL:        cfg.BaseURL,
	}
}

func (h *HFClient) HuggingFaceRegistryHandler(rw httpx.ResponseWriter, req *http.Request) {
	start := time.Now()
	h.Log.Info("Original path", "path", req.URL.Path)
	cleanPath := path.Clean(req.URL.Path)

	h.Log.Info("incoming huggingface request",
		"cleanPath", cleanPath,
		"remote", req.RemoteAddr,
		"method", req.Method)

	isResolve := strings.Contains(cleanPath, "/resolve/")
	isBlob := strings.Contains(cleanPath, "/blobs/") || strings.Contains(cleanPath, "/cdn-lfs")
	isAPI := strings.Contains(cleanPath, "/api/") || strings.Contains(cleanPath, "/resolve-cache/")
	isXetAPI := strings.Contains(cleanPath, "/xet-read-token/")

	h.Log.Info("request classification",
		"isResolve", isResolve,
		"isBlob", isBlob,
		"isAPI", isAPI,
		"isXetAPI", isXetAPI)

	key := fmt.Sprintf("hf:%s", cleanPath)

	h.Log.Info("computed P2P key", "key", key, "isResolve", isResolve, "isBlob", isBlob, "isXetAPI", isXetAPI)

	if !isResolve && !isBlob && !isAPI {
		h.Log.Info("unsupported huggingface request", "path", cleanPath)
		http.Error(rw, "unsupported huggingface request", http.StatusBadRequest)
		return
	}

	h.Log.Info("processing model/blob request", "url", cleanPath)

	var cacheFilePath string
	var fileExists bool
	var filename string

	parts := strings.Split(strings.TrimPrefix(cleanPath, "/huggingface/"), "/")
	h.Log.Info("parsed path parts", "parts", parts)

	if len(parts) >= 2 {
		var orgModel string
		var modelDir string
		if isResolve {
			orgModel = fmt.Sprintf("models--%s--%s", parts[0], parts[1])
			modelDir = filepath.Join(h.HFCacheDir, orgModel)
			h.Log.Info("derived modelDir", "orgModel", orgModel, "modelDir", modelDir)
		} else if isAPI {
			orgModel = fmt.Sprintf("models--%s--%s", parts[2], parts[3])
			modelDir = filepath.Join(h.HFCacheDir, orgModel)
			h.Log.Info("derived modelDir", "orgModel", orgModel, "modelDir", modelDir)
		}

		if isResolve && len(parts) >= 5 {
			ref := parts[3]
			filename = strings.Join(parts[4:], "/")
			refFile := filepath.Join(modelDir, "refs", "main")

			h.Log.Info("checking snapshot ref file", "refFile", refFile, "ref", ref, "filename", filename)

			if shaBytes, err := os.ReadFile(refFile); err == nil {
				sha := strings.TrimSpace(string(shaBytes))
				snapshotFile := filepath.Join(modelDir, "snapshots", sha, filename)
				h.Log.Info("snapshot ref resolved", "sha", sha, "snapshotFile", snapshotFile)

				if _, err := os.Stat(snapshotFile); err == nil {
					h.Log.Info("serving locally (file exists in HF cache)",
						"orgModel", orgModel, "ref", ref, "sha", sha, "file", filename, "snapshotFile", snapshotFile)
					http.ServeFile(rw, req, snapshotFile)
					return
				} else {
					cacheFilePath = snapshotFile
					fileExists = false
					h.Log.Info("file missing, will use snapshotFile for P2P", "snapshotFile", snapshotFile)
				}
			} else {
				h.Log.Info("Cached missed for ref file", "refFile", refFile)
			}
		} else if isAPI && len(parts) > 5 {
			sha := parts[5]
			key_parts := strings.Split(key, "/")
			h.Log.Info("key path parts", "parts", key_parts)
			filename = key_parts[len(key_parts)-1]
			snapshotFile := filepath.Join(modelDir, "snapshots", sha, filename)
			h.Log.Info("snapshot ref resolved", "sha", sha, "snapshotFile", snapshotFile)
			if _, err := os.Stat(snapshotFile); err == nil {
				h.Log.Info("serving locally (file exists in HF cache)",
					"orgModel", orgModel, "sha", sha, "file", filename)
				http.ServeFile(rw, req, snapshotFile)
				return
			} else {
				cacheFilePath = snapshotFile
				h.Log.Info("file missing, will use snapshotFile for P2P", "snapshotFile", snapshotFile)
			}
		}

		if isBlob && len(parts) >= 3 {
			blobFile := filepath.Join(modelDir, "blobs", parts[len(parts)-1])
			h.Log.Info("checking blob file", "blobFile", blobFile)

			if _, err := os.Stat(blobFile); err == nil {
				h.Log.Info("blob exists in local HF cache, skipping P2P/upstream",
					"orgModel", orgModel, "file", parts[len(parts)-1])
				return
			}
			cacheFilePath = blobFile
			fileExists = false
		}
	} else {
		h.Log.Info("path did not have enough parts", "parts", parts)
	}

	excludeFiles := map[string]bool{
		"model.safetensors.index.json": true,
		"tokenizer.json":               true,
		"tokenizer_config.json":        true,
		"generation_config.json":       true,
	}
	if isResolve && cacheFilePath != "" && req.Method == "GET" && !excludeFiles[filename] {
		ctx, cancel := context.WithTimeout(req.Context(), h.ResolveTimeout)
		defer cancel()

		h.Log.Info("attempting P2P resolution", "key", key, "cacheFilePath", cacheFilePath, "fileExists", fileExists)

		balancer, err := h.Router.Lookup(ctx, key, h.ResolveRetries)
		if err == nil {
			for attempt := range h.ResolveRetries {
				peer, peerErr := balancer.Next()
				if peerErr != nil {
					h.Log.Info("no more peers available", "key", key, "error", peerErr)
					break
				}
				h.Log.Info("got peer from P2P resolution",
					"peer", peer,
					"attempt", attempt+1,
					"key", key,
					"cacheFilePath", cacheFilePath)

				if err := h.forwardRequest(req, rw, peer.String(), key, cacheFilePath, isResolve); err == nil {
					h.Log.Info("served huggingface resource from peer",
						"peer", peer,
						"key", key)
					h.Log.Info("request completed via P2P", "duration", time.Since(start))
					return
				} else {
					h.Log.Error(err, "peer lookup failed",
						"key", key,
						"peer", peer,
						"attempt", attempt+1)
					balancer.Remove(peer)
				}
			}
		} else {
			h.Log.Error(err, "failed to resolve P2P peers", "key", key)
		}
	} else {
		h.Log.Info("Cache file not available on local node or not resolve requests")
	}

	h.Log.Info("falling back to upstream", "path", cleanPath, "cacheFilePath", cacheFilePath)
	if h.serveFromFallback(rw, req, cleanPath, isResolve, isBlob, isAPI, key) {
		h.Log.Info("request completed via fallback", "duration", time.Since(start))
	}
}

func (h *HFClient) serveFromFallback(rw http.ResponseWriter, req *http.Request, cleanPath string, isResolve, isBlob, isAPI bool, key string) bool {
	h.Log.Info("serveFromFallback started",
		"path", cleanPath,
		"method", req.Method,
		"isResolve", isResolve,
		"isBlob", isBlob,
		"isAPI", isAPI)
	start := time.Now()

	var pathForUpstream string
	var source string

	if isAPI {
		pathForUpstream = strings.TrimPrefix(cleanPath, "/huggingface")
		source = "Hugging Face API"
	} else if isResolve {
		pathForUpstream = strings.TrimPrefix(cleanPath, "/huggingface")
		source = "Hugging Face website/API"
	} else {
		pathForUpstream = cleanPath
		source = "Hugging Face LFS"
	}

	h.Log.Info("upstream URL type", "source", source, "pathForUpstream", pathForUpstream)

	upstreamURL := fmt.Sprintf("%s%s", h.BaseURL, pathForUpstream)
	h.Log.Info("upstream URL computed", "finalUpstreamURL", upstreamURL, "method", req.Method)

	tr := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   30 * time.Minute,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.Method == "HEAD" && isXetURL(req.URL.String()) {
				h.Log.Info("CheckRedirect hit: preventing redirect for HEAD request")
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				h.Log.Error(nil, "CheckRedirect hit: too many redirects")
				return fmt.Errorf("too many redirects")
			}
			h.Log.Info("CheckRedirect hit: GET request following internal redirect", "from", via[len(via)-1].URL, "to", req.URL)
			return nil
		},
	}

	reqUpstream, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, nil)
	if err != nil {
		h.Log.Error(err, "failed to create upstream request", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return true
	}

	h.Log.Info("copying original request headers to upstream request")
	for key, values := range req.Header {
		for _, value := range values {
			reqUpstream.Header.Add(key, value)
		}
	}
	if reqUpstream.Header.Get("User-Agent") == "" {
		reqUpstream.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Clyde-HFProxy/1.0)")
	}
	if req.Method == "HEAD" {
		reqUpstream.Header.Set("Accept-Encoding", "identity")
	}

	resp, err := client.Do(reqUpstream)
	if err != nil {
		h.Log.Error(err, "failed to fetch from upstream", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to fetch from upstream: %v", err), http.StatusBadGateway)
		return true
	}
	defer resp.Body.Close()

	h.Log.Info("upstream response received", "status", resp.StatusCode, "method", req.Method,
		"content-length", resp.ContentLength, "location", resp.Header.Get("Location"))

	for k, vv := range resp.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if req.Method == "HEAD" {
			rw.WriteHeader(resp.StatusCode)
			h.Log.Info("HEAD redirect successfully proxied and completed", "status", resp.StatusCode,
				"location", rw.Header().Get("Location"), "duration", time.Since(start))
			return true
		}
		h.Log.Error(nil, "UNEXPECTED 3XX STATUS ON GET: Internal redirect failed to resolve content. Proceeding to write status.",
			"status", resp.StatusCode, "location", resp.Header.Get("Location"))
	}

	if req.Method == "HEAD" {
		rw.WriteHeader(resp.StatusCode)
		h.Log.Info("HEAD request completed (non-redirect path)", "status", resp.StatusCode,
			"content-length", resp.ContentLength, "duration", time.Since(start))
		return true
	}

	if resp.StatusCode == http.StatusNotFound {
		h.Log.Error(nil, "upstream returned 404 not found", "upstreamURL", upstreamURL)
	}

	rw.WriteHeader(resp.StatusCode)

	if req.Method == "GET" {
		n, err := io.Copy(rw, resp.Body)
		if err != nil {
			h.Log.Error(err, "failed to stream response to client", "file", filepath.Base(cleanPath), "bytesCopied", n)
		} else {
			h.Log.Info("File streamed successfully", "file", filepath.Base(cleanPath), "bytes", n)
		}
		go func() {
			if err := h.Router.Advertise(context.Background(), []string{key}); err != nil {
				h.Log.Error(err, "failed to advertise key", "key", key)
			} else {
				h.Log.Info("advertised key successfully", "key", key)
			}
		}()

		h.Log.Info("file cached and served successfully", "file", cleanPath, "bytes", n)
	}

	h.Log.Info("request handler execution flow completed", "duration", time.Since(start))
	return true
}

func isXetURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if strings.Contains(strings.ToLower(u.Host), "xet") {
		return true
	}
	query := u.Query()
	if _, ok := query["X-Xet-Cas-Uid"]; ok {
		return true
	}
	return false
}

func (h *HFClient) forwardRequest(req *http.Request, rw http.ResponseWriter, peerAddr, key string, _ string, isResolve bool) error {
	start := time.Now()
	var u *url.URL

	isXetAPI := strings.Contains(req.URL.Path, "/xet-read-token/")

	if isXetAPI && req.Method == "HEAD" {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   req.URL.Path,
		}
	} else if isXetAPI && req.Method == "GET" {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   strings.TrimPrefix(key, "hf:"),
		}
	} else if isResolve {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   req.URL.Path,
		}
	} else {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   strings.TrimPrefix(key, "hf:"),
		}
	}

	peerReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		h.Log.Error(err, "failed to create peer request")
		return err
	}

	copyHeader(peerReq.Header, req.Header)
	peerReq.Header.Set("User-Agent", "huggingface_hub/0.0.1")
	if host := req.Host; host != "" {
		peerReq.Header.Set("Host", host)
	}
	if isXetAPI && req.Method == "HEAD" {
		peerReq.Header.Set("Accept", "application/json")
	}
	if isXetAPI {
		peerReq.Header.Set("reqType", "XetAPI")
	}

	resp, err := h.Client.Do(peerReq)
	if err != nil {
		h.Log.Error(err, "failed to contact peer", "url", u.String())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		limitedBody := io.LimitReader(resp.Body, 4096)
		body, _ := io.ReadAll(limitedBody)
		h.Log.Error(nil, "unexpected peer status",
			"status", resp.Status,
			"statusCode", resp.StatusCode,
			"url", u.String(),
			"body", string(body))
		return fmt.Errorf("peer returned %s", resp.Status)
	}

	copyHeader(rw.Header(), resp.Header)
	rw.Header().Del("Content-Length")
	rw.Header().Del("Content-Encoding")

	if rw.Header().Get("Content-Type") == "" {
		if isXetAPI {
			rw.Header().Set("Content-Type", "application/json")
		} else {
			rw.Header().Set("Content-Type", "application/octet-stream")
		}
	}

	rw.WriteHeader(resp.StatusCode)

	if req.Method == http.MethodHead {
		h.Log.Info("HEAD request detected – skipping body copy",
			"method", req.Method,
			"url", u.String(),
			"contentLength", resp.ContentLength,
		)
	} else {
		bytesCopied, err := io.Copy(rw, resp.Body)
		if err != nil {
			h.Log.Error(err, "failed streaming peer response to client", "url", u.String(), "bytesCopied", bytesCopied)
			return err
		}
		h.Log.Info("successfully forwarded response",
			"peer", peerAddr,
			"bytesCopied", bytesCopied,
			"duration", time.Since(start),
			"url", u.String(),
		)
	}

	if err := h.Router.Advertise(context.Background(), []string{key}); err != nil {
		h.Log.Error(err, "failed to advertise key", "key", key)
	} else {
		h.Log.Info("advertised key successfully", "key", key)
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

func (h *HFClient) WalkHFCacheDir(ctx context.Context) ([]string, error) {
	var keys []string

	h.Log.Info("Starting WalkHFCacheDir", "root", h.HFCacheDir)

	err := filepath.Walk(h.HFCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			h.Log.Error(err, "Error walking path", "path", path)
			return err
		}

		h.Log.V(4).Info("Visiting", "path", path, "isDir", info.IsDir())

		if info.IsDir() {
			return nil
		}

		if !strings.Contains(path, "/snapshots/") {
			h.Log.V(4).Info("Skipping (not snapshot)", "path", path)
			return nil
		}

		lower := strings.ToLower(info.Name())
		if !(strings.HasSuffix(lower, ".bin") ||
			strings.HasSuffix(lower, ".json") ||
			strings.HasSuffix(lower, ".msgpack") ||
			strings.HasSuffix(lower, ".onnx") ||
			strings.HasSuffix(lower, ".safetensors") ||
			strings.HasSuffix(lower, ".md")) {

			h.Log.V(4).Info("Skipping (unsupported extension)", "file", info.Name())
			return nil
		}

		relPath, err := filepath.Rel(h.HFCacheDir, path)
		if err != nil {
			h.Log.Error(err, "Failed to compute relative path", "path", path)
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		relPath = filepath.ToSlash(relPath)
		h.Log.V(4).Info("Relative path", "relPath", relPath)

		parts := strings.SplitN(relPath, "/", 3)
		if len(parts) < 3 {
			h.Log.V(4).Info("Skipping (malformed path)", "relPath", relPath)
			return nil
		}

		modelDir := parts[0]
		rest := strings.TrimPrefix(relPath, modelDir+"/")

		modelPath := strings.TrimPrefix(modelDir, "models--")
		modelPath = strings.ReplaceAll(modelPath, "--", "/")
		modelPath = "/huggingface/" + modelPath

		rest = strings.Replace(rest, "snapshots/", "resolve/", 1)

		key := fmt.Sprintf("hf:%s/%s", modelPath, rest)
		h.Log.V(4).Info("Discovered HF key", "key", key, "path", path)

		keys = append(keys, key)
		return nil
	})

	if err != nil {
		h.Log.Error(err, "Failed to walk HF cache dir")
		return nil, fmt.Errorf("failed to walk HF cache dir: %w", err)
	}

	h.Log.Info("Completed WalkHFCacheDir", "totalKeys", len(keys))
	for _, k := range keys {
		h.Log.V(4).Info("Final key", "key", k)
	}

	return keys, nil
}

func AddHFConfiguration(ctx context.Context, hfCacheDir string) error {
	if hfCacheDir == "" {
		return fmt.Errorf("HF cache directory is empty")
	}

	if err := os.MkdirAll(hfCacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create Hugging Face cache directory %q: %w", hfCacheDir, err)
	}

	log := logr.FromContextOrDiscard(ctx)
	log.Info("Hugging Face configuration applied")

	return nil
}

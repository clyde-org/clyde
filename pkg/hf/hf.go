package hf

import (
	"clyde/pkg/mux"
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

// HFConfig holds constructor settings for HFClient
type HFConfig struct {
	Router         routing.Router
	HFCacheDir     string
	ResolveTimeout time.Duration
	ResolveRetries int
	Log            logr.Logger
	Client         *http.Client
	BaseURL        string
}

// Option type for functional configuration
type HFOption func(*HFConfig)

// WithHFLogger overrides the default logger
func WithHFLogger(log logr.Logger) HFOption {
	return func(cfg *HFConfig) {
		cfg.Log = log
	}
}

// WithHFTimeout sets the resolve timeout
func WithHFTimeout(d time.Duration) HFOption {
	return func(cfg *HFConfig) {
		cfg.ResolveTimeout = d
	}
}

// WithHFRetries sets the resolve retries
func WithHFRetries(n int) HFOption {
	return func(cfg *HFConfig) {
		cfg.ResolveRetries = n
	}
}

// WithHFHTTPClient replaces the default HTTP client
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

// NewHFClient constructs an HFClient with sane defaults + options
func NewHFClient(router routing.Router, cacheDir string, opts ...HFOption) *HFClient {
	// default config
	cfg := HFConfig{
		Router:         router,
		HFCacheDir:     cacheDir,
		ResolveTimeout: 60 * time.Second,
		ResolveRetries: 3,
		Log:            logr.Discard(),
		BaseURL:        "https://huggingface.co",
		Client: &http.Client{
			Timeout:   60 * time.Minute, //since huggingface fille can be large
			Transport: http.DefaultTransport,
		},
	}

	// apply options
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

// HuggingFaceRegistryHandler serves HF resources from P2P or upstream, logs if cached locally
func (h *HFClient) HuggingFaceRegistryHandler(rw mux.ResponseWriter, req *http.Request) {
	start := time.Now()
	h.Log.Info("Original path", "path", req.URL.Path)
	cleanPath := path.Clean(req.URL.Path)

	h.Log.Info("incoming huggingface request",
		"cleanPath", cleanPath,
		"remote", req.RemoteAddr,
		"method", req.Method)

	// ---- Classification ----
	isResolve := strings.Contains(cleanPath, "/resolve/")
	isBlob := strings.Contains(cleanPath, "/blobs/") || strings.Contains(cleanPath, "/cdn-lfs")
	isAPI := strings.Contains(cleanPath, "/api/") || strings.Contains(cleanPath, "/resolve-cache/")
	isXetAPI := strings.Contains(cleanPath, "/xet-read-token/")

	h.Log.Info("request classification",
		"isResolve", isResolve,
		"isBlob", isBlob,
		"isAPI", isAPI,
		"isXetAPI", isXetAPI)

	// ---- P2P RESOLUTION ----
	key := fmt.Sprintf("hf:%s", cleanPath)

	h.Log.Info("computed P2P key", "key", key, "isResolve", isResolve, "isBlob", isBlob, "isXetAPI", isXetAPI)

	if !isResolve && !isBlob && !isAPI {
		h.Log.Info("unsupported huggingface request", "path", cleanPath)
		http.Error(rw, "unsupported huggingface request", http.StatusBadRequest)
		return
	}

	h.Log.Info("processing model/blob request", "url", cleanPath)

	var cacheFilePath string // snapshotFile or blobFile candidate
	var fileExists bool
	var filename string

	// ---- LOCAL CACHE LOOKUP ----
	parts := strings.Split(strings.TrimPrefix(cleanPath, "/huggingface/"), "/")
	h.Log.Info("parsed path parts", "parts", parts)

	if len(parts) >= 2 {
		var orgModel string
		var modelDir string
		if isResolve {
			//For example /huggingface/Qwen/Qwen3-4B-Instruct-2507/resolve/main/model-00001-of-00003.safetensors"
			orgModel = fmt.Sprintf("models--%s--%s", parts[0], parts[1])
			modelDir = filepath.Join(h.HFCacheDir, orgModel)
			h.Log.Info("derived modelDir", "orgModel", orgModel, "modelDir", modelDir)
		} else if isAPI {
			// Handle other API requests (not Xet)
			orgModel = fmt.Sprintf("models--%s--%s", parts[2], parts[3])
			modelDir = filepath.Join(h.HFCacheDir, orgModel)
			h.Log.Info("derived modelDir", "orgModel", orgModel, "modelDir", modelDir)
		}

		// Handle /resolve/<ref>/<filename>
		if isResolve && len(parts) >= 5 {
			ref := parts[3]                         // e.g., "main"
			filename = strings.Join(parts[4:], "/") // e.g., "LICENSE" or nested file path
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

		// Handle /blobs/<sha>
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

	// ---- P2P RESOLUTION ATTEMPT ----
	//these files are small, let's serve them from remote and avoid inundating the p2p network
	//or shall we skip all .json files?
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

		peerCh, err := h.Router.Resolve(ctx, key, h.ResolveRetries)
		if err == nil {
			count := 0
			for peer := range peerCh {
				count++
				h.Log.Info("got peer from P2P resolution",
					"peer", peer,
					"attempt", count,
					"key", key,
					"cacheFilePath", cacheFilePath)

				if err := h.forwardRequest(req, rw, peer.String(), key, cacheFilePath, isResolve); err == nil {
					h.Log.Info("served huggingface resource from peer",
						"peer", peer,
						"key", key)
					h.Log.Info("request completed via P2P", "duration", time.Since(start))
					return
				}
				h.Log.Error(err, "peer lookup failed",
					"key", key,
					"peer", peer,
					"attempt", count)
			}
			if count == 0 {
				h.Log.Info("no peers resolved for key", "key", key)
			} else {
				h.Log.Info("P2P resolution attempted but all peers failed", "key", key, "attempts", count)
			}
		} else {
			h.Log.Error(err, "failed to resolve P2P peers", "key", key)
		}
	} else {
		h.Log.Info("Cache file not availablel on local node or noot resolve requests")
	}

	// ---- UPSTREAM FALLBACK ----
	h.Log.Info("falling back to upstream", "path", cleanPath, "cacheFilePath", cacheFilePath)
	if h.serveFromFallback(rw, req, cleanPath, isResolve, isBlob, isAPI, key) {
		h.Log.Info("request completed via fallback", "duration", time.Since(start))
	}
}

// If resource not found locally, serve them from the remote
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

	// Handle different path types for upstream
	if isAPI {
		// For API requests (including Xet), use the path as-is but remove /huggingface prefix
		pathForUpstream = strings.TrimPrefix(cleanPath, "/huggingface")
		source = "Hugging Face API"
	} else if isResolve {
		// For resolve requests, remove /huggingface prefix
		pathForUpstream = strings.TrimPrefix(cleanPath, "/huggingface")
		source = "Hugging Face website/API"
	} else {
		// For blob requests, use the path as-is
		pathForUpstream = cleanPath
		source = "Hugging Face LFS"
	}

	h.Log.Info("upstream URL type", "source", source, "pathForUpstream", pathForUpstream)

	upstreamURL := fmt.Sprintf("%s%s", h.BaseURL, pathForUpstream)
	h.Log.Info("upstream URL computed", "finalUpstreamURL", upstreamURL, "method", req.Method)

	// --- 2. Client Setup with CheckRedirect (Crucial Logic) ---
	tr := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   30 * time.Minute,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// CRUCIAL: Do NOT follow redirects for HEAD for large files.
			if req.Method == "HEAD" && isXetURL(req.URL.String()) {
				// VERBOSE LOG: Stopping redirect for HEAD
				h.Log.Info("CheckRedirect hit: preventing redirect for HEAD request")
				return http.ErrUseLastResponse
			}
			// CRUCIAL: Follow redirects internally for GET.
			if len(via) >= 10 {
				h.Log.Error(nil, "CheckRedirect hit: too many redirects")
				return fmt.Errorf("too many redirects")
			}
			// VERBOSE LOG: Following redirect for GET
			h.Log.Info("CheckRedirect hit: GET request following internal redirect", "from", via[len(via)-1].URL, "to", req.URL)
			return nil
		},
	}

	// --- 3. Create and Send Upstream Request ---
	reqUpstream, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, nil)
	if err != nil {
		h.Log.Error(err, "failed to create upstream request", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return true
	}

	// Copy headers
	h.Log.Info("copying original request headers to upstream request") // VERBOSE LOG
	for key, values := range req.Header {
		for _, value := range values {
			// Exclude hop-by-hop headers and ensure all headers are copied
			reqUpstream.Header.Add(key, value)
		}
	}
	// Check and set User-Agent
	if reqUpstream.Header.Get("User-Agent") == "" {
		reqUpstream.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Clyde-HFProxy/1.0)")
		h.Log.Info("User-Agent set (default)", "value", reqUpstream.Header.Get("User-Agent")) // VERBOSE LOG
	} else {
		h.Log.Info("User-Agent already present", "value", reqUpstream.Header.Get("User-Agent")) // VERBOSE LOG
	}
	// Head specific header
	if req.Method == "HEAD" {
		reqUpstream.Header.Set("Accept-Encoding", "identity")
		h.Log.Info("HEAD request: added Accept-Encoding identity header") // VERBOSE LOG
	}

	h.Log.Info("sending upstream request", "method", req.Method, "url", upstreamURL, "headers_count", len(reqUpstream.Header)) // VERBOSE LOG: added header count

	// Make the request
	resp, err := client.Do(reqUpstream)
	if err != nil {
		h.Log.Error(err, "failed to fetch from upstream", "url", upstreamURL)
		http.Error(rw, fmt.Sprintf("failed to fetch from upstream: %v", err), http.StatusBadGateway)
		return true
	}
	defer resp.Body.Close()

	// VERBOSE LOG: Received initial response
	h.Log.Info("upstream response received", "status", resp.StatusCode, "method", req.Method,
		"content-length", resp.ContentLength, "location", resp.Header.Get("Location"))

	// --- 4. Copy Headers ---
	h.Log.Info("copying all upstream response headers to client response writer") // VERBOSE LOG
	// Copy all headers immediately to the response writer.
	for k, vv := range resp.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}

	// --- 5. Handle Redirects (FIXED LOGIC) ---
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		h.Log.Info("redirect status received, handling by method", "status", resp.StatusCode) // VERBOSE LOG

		if req.Method == "HEAD" {
			// CORRECT: For HEAD, we proxy the redirect status and Location header to the client.
			rw.WriteHeader(resp.StatusCode)
			h.Log.Info("HEAD redirect successfully proxied and completed", "status", resp.StatusCode,
				"location", rw.Header().Get("Location"), "duration", time.Since(start))
			return true
		}

		// If req.Method is GET and we get a 3xx status, the internal client FAILED to follow
		// the redirect. We log the error but allow it to fall through.
		// We do NOT write the status or return here, as the status writing happens in Step 7.
		h.Log.Error(nil, "UNEXPECTED 3XX STATUS ON GET: Internal redirect failed to resolve content. Proceeding to write status.",
			"status", resp.StatusCode, "location", resp.Header.Get("Location"))
		// Execution continues to step 6/7.
	}

	// --- 6. Handle HEAD (Non-Redirect) ---
	if req.Method == "HEAD" {
		// Status is 200/404/etc. Headers already copied.
		rw.WriteHeader(resp.StatusCode)
		h.Log.Info("HEAD request completed (non-redirect path)", "status", resp.StatusCode, // VERBOSE LOG
			"content-length", resp.ContentLength, "duration", time.Since(start))
		return true
	}

	// --- 7. Handle GET (File Downloads) ---
	h.Log.Info("preparing to finalize response for GET request", "path", cleanPath, "final_status", resp.StatusCode)

	// CRITICAL: If the upstream response is 404, we log it before writing the status.
	if resp.StatusCode == http.StatusNotFound {
		h.Log.Error(nil, "upstream returned 404 not found", "upstreamURL", upstreamURL)
	}

	// Write the final status (200, 206, 404, or the unexpected 3xx from above)
	// If it's 3xx, the client will fail, but we haven't prematurely exited the function.
	rw.WriteHeader(resp.StatusCode)
	h.Log.Info("final HTTP status code written to client", "status", resp.StatusCode) // VERBOSE LOG

	// Stream/Cache logic follows...
	if req.Method == "GET" {
		h.Log.Info("streaming file directly", "file_name", filepath.Base(cleanPath)) // VERBOSE LOG

		n, err := io.Copy(rw, resp.Body)
		if err != nil {
			h.Log.Error(err, "failed to stream response to client", "file", filepath.Base(cleanPath), "bytesCopied", n)
		} else {
			h.Log.Info("File streamed successfully", "file", filepath.Base(cleanPath), "bytes", n)
		}
		// }
		go func() {
			if err := h.Router.Advertise(context.Background(), []string{key}); err != nil {
				h.Log.Error(err, "failed to advertise key", "key", key)
			} else {
				h.Log.Info("advertised key successfully", "key", key)
				// globalKeys = []string{}
				// h.Log.Info("cleared globalKeys", "key", globalKeys)

			}
		}()

		h.Log.Info("file cached and served successfully", "file", cleanPath, "bytes", n)
	}

	h.Log.Info("request handler execution flow completed", "duration", time.Since(start)) // VERBOSE LOG: final log
	return true
}

// Helper: perform a short HEAD (no redirects) and log interesting headers for debugging
// Returns true if the URL contains the X-Xet-Cas-Uid query parameter or host contains "xet"
func isXetURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Optional: check if host contains "xet"
	if strings.Contains(strings.ToLower(u.Host), "xet") {
		return true
	}

	// Check query parameters for X-Xet-Cas-Uid
	query := u.Query()
	if _, ok := query["X-Xet-Cas-Uid"]; ok {
		return true
	}

	return false
}

// forwardRequest proxies to peer
func (h *HFClient) forwardRequest(req *http.Request, rw http.ResponseWriter, peerAddr, key string, _ string, isResolve bool) error {
	start := time.Now()
	var u *url.URL

	h.Log.Info("=== FORWARD REQUEST DEBUG ===",
		"isResolve", isResolve,
		"originalPath", req.URL.Path,
		"key", key,
		"peerAddr", peerAddr,
		"method", req.Method,
	)

	isXetAPI := strings.Contains(req.URL.Path, "/xet-read-token/")

	if isXetAPI && req.Method == "HEAD" {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   req.URL.Path,
		}
		h.Log.V(4).Info("handling Xet API request HEAD", "apiPath", req.URL.Path)
	} else if isXetAPI && req.Method == "GET" {
		u = &url.URL{
			Scheme: "http",
			Host:   peerAddr,
			Path:   strings.TrimPrefix(key, "hf:"),
		}
		h.Log.V(4).Info("handling Xet API request GET", "apiPath", req.URL.Path)
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

	h.Log.Info("final target URL", "url", u.String(), "isXetAPI", isXetAPI)

	peerReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, u.String(), nil)

	if err != nil {
		h.Log.Error(err, "failed to create peer request")
		return err
	}

	// Copy headers
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

	// Execute peer request#
	h.Log.Info(fmt.Sprintf("Initiating Peer Request: %+v", peerReq))
	resp, err := h.Client.Do(peerReq)
	if err != nil {
		h.Log.Error(err, "failed to contact peer", "url", u.String())
		return err
	}
	defer resp.Body.Close()

	h.Log.Info("=== PEER RESPONSE ===",
		"status", resp.Status,
		"statusCode", resp.StatusCode,
		"contentLength", resp.ContentLength,
		"isXetAPI", isXetAPI,
	)

	// Accept only 200 and 206
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

	// Copy peer headers to client
	copyHeader(rw.Header(), resp.Header)

	// Remove headers that might cause mismatches
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

	// For HEAD requests, skip copying the body
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

	// Advertise to P2P network
	if err := h.Router.Advertise(context.Background(), []string{key}); err != nil {
		h.Log.Error(err, "failed to advertise key", "key", key)
	} else {
		h.Log.Info("advertised key successfully", "key", key)
	}

	return nil
}

// Helper function to copy headers
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

		// Only consider files under snapshots/
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

	// Ensure cache directory exists
	if err := os.MkdirAll(hfCacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create Hugging Face cache directory %q: %w", hfCacheDir, err)
	}

	log := logr.FromContextOrDiscard(ctx)
	log.Info("Hugging Face configuration applied")

	return nil
}

package hf

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clyde/pkg/routing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
)

type TestResponseWriter struct {
	*httptest.ResponseRecorder
	handler string
	lastErr error
}

func newTestResponseWriter() *TestResponseWriter {
	return &TestResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// Implement mux.ResponseWriter interface
func (w *TestResponseWriter) Error() error {
	return w.lastErr
}

func (w *TestResponseWriter) Header() http.Header {
	return w.ResponseRecorder.Header()
}

func (w *TestResponseWriter) Size() int64 {
	return int64(w.ResponseRecorder.Body.Len())
}

func (w *TestResponseWriter) Status() int {
	return w.ResponseRecorder.Code
}

func (w *TestResponseWriter) Write(b []byte) (int, error) {
	return w.ResponseRecorder.Write(b)
}

func (w *TestResponseWriter) WriteHeader(statusCode int) {
	w.ResponseRecorder.WriteHeader(statusCode)
}

func (w *TestResponseWriter) SetHandler(name string) {
	w.handler = name
}

func (w *TestResponseWriter) WriteError(code int, err error) {
	w.lastErr = err
	w.WriteHeader(code)
	_, _ = w.Write([]byte(err.Error()))
}

// ------------------------------------------------------------
// TEST HELPERS
// ------------------------------------------------------------
func newTestHFClient(t *testing.T, cacheDir string, router routing.Router) *HFClient {
	t.Helper()
	return NewHFClient(router, cacheDir,
		WithHFLogger(logr.Discard()),
		WithHFTimeout(3*time.Second),
		WithHFRetries(1),
	)
}

func createTempHFFile(t *testing.T, base, rel, content string) string {
	t.Helper()
	p := filepath.Join(base, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// ------------------------------------------------------------
// TEST: Client Configuration
// ------------------------------------------------------------
func TestHFClientOptions(t *testing.T) {
	t.Parallel()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	tmp := t.TempDir()

	opts := []HFOption{
		WithHFRetries(5),
		WithHFTimeout(10 * time.Minute),
		WithHFLogger(logr.Discard()),
	}

	client := NewHFClient(router, tmp, opts...)

	require.Equal(t, 5, client.ResolveRetries)
	require.Equal(t, 10*time.Minute, client.ResolveTimeout)
	require.Equal(t, logr.Discard(), client.Log)
}

// ------------------------------------------------------------
// TEST: Local Snapshot Resolution
// ------------------------------------------------------------
func TestHFHandler_LocalResolveHit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	modelDir := filepath.Join(tmp, "models--org--model")
	sha := "1234567890abcdef"
	createTempHFFile(t, modelDir, "refs/main", sha)
	createTempHFFile(t, modelDir, "snapshots/"+sha+"/LICENSE", "hello-license")

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := newTestHFClient(t, tmp, router)

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/huggingface/org/model/resolve/main/LICENSE", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello-license", string(body))
}

// ------------------------------------------------------------
// TEST: Blob Upstream Fallback
// ------------------------------------------------------------
func TestHFHandler_BlobFallbackToUpstream_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/blobs/") {
			w.Write([]byte("upstream-blob"))
			return
		}
		w.WriteHeader(404)
	}))
	defer upstream.Close()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := NewHFClient(router, tmp, WithHFBaseURL(upstream.URL))

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/huggingface/org/model/blobs/sha256deadbeef", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "upstream-blob", string(body))
}

// ------------------------------------------------------------
// TEST: P2P Resolution
// ------------------------------------------------------------
func TestHFHandler_ResolveP2PHit_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Set up the local cache structure that the handler expects
	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	// Create a fake SHA for the ref
	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))

	// Create the snapshot directory (but not the actual file, since we want P2P to handle it)
	require.NoError(t, os.MkdirAll(filepath.Join(snapshotsDir, sha), 0755))

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("peer-content"))
	}))
	defer peer.Close()

	peerAddr := netip.MustParseAddrPort(peer.Listener.Addr().String())
	resolver := map[string][]netip.AddrPort{
		"hf:/huggingface/org/model/resolve/main/model.bin": {peerAddr},
	}

	router := routing.NewMemoryRouter(resolver, netip.AddrPort{})

	client := NewHFClient(
		router,
		tmp,
		WithHFBaseURL("http://invalid-upstream"),
	)

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet,
		"/huggingface/org/model/resolve/main/model.bin", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "peer-content", string(body))
}

// ------------------------------------------------------------
// TEST: API Upstream Fallback
// ------------------------------------------------------------
func TestHFHandler_APIFallback_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("api-response"))
	}))
	defer upstream.Close()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := NewHFClient(router, tmp, WithHFBaseURL(upstream.URL))

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/huggingface/api/models/org/model", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "api-response", string(body))
}

// ------------------------------------------------------------
// TEST: Unsupported Paths
// ------------------------------------------------------------
func TestHFHandler_Unsupported(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := newTestHFClient(t, tmp, router)

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/huggingface/invalid/path", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ------------------------------------------------------------
// TEST: Local Cache Resolution
// ------------------------------------------------------------
func TestHFHandler_ResolveLocalCacheHit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Set up complete local cache structure with actual file
	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))

	// Create the actual file in snapshots directory
	snapshotDir := filepath.Join(snapshotsDir, sha)
	require.NoError(t, os.MkdirAll(snapshotDir, 0755))
	cacheFile := filepath.Join(snapshotDir, "model.bin")
	require.NoError(t, os.WriteFile(cacheFile, []byte("cached-content"), 0644))

	// Use empty router - no P2P peers, should serve from local cache
	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})

	client := NewHFClient(
		router,
		tmp,
		WithHFBaseURL("http://invalid-upstream"),
	)

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet,
		"/huggingface/org/model/resolve/main/model.bin", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "cached-content", string(body))
}

// ------------------------------------------------------------
// TEST: Upstream Fallback Resolution
// ------------------------------------------------------------
func TestHFHandler_ResolveUpstreamFallback(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Set up mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("upstream-content"))
	}))
	defer upstream.Close()

	// Set up partial local cache structure (ref exists but file doesn't)
	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))

	// Create snapshot directory but not the file - file should be fetched from upstream
	require.NoError(t, os.MkdirAll(filepath.Join(snapshotsDir, sha), 0755))

	// Use empty router - no P2P peers available
	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})

	client := NewHFClient(
		router,
		tmp,
		WithHFBaseURL(upstream.URL),
	)

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet,
		"/huggingface/org/model/resolve/main/model.bin", nil)

	client.HuggingFaceRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "upstream-content", string(body))
}

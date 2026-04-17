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

	"clyde/pkg/httpx"
	"clyde/pkg/routing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
)

var _ httpx.ResponseWriter = &testResponseWriter{}

type testResponseWriter struct {
	*httptest.ResponseRecorder
	attrs       map[string]any
	lastErr     error
	wroteHeader bool
}

func newTestResponseWriter() *testResponseWriter {
	return &testResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		attrs:            map[string]any{},
	}
}

func (w *testResponseWriter) Error() error        { return w.lastErr }
func (w *testResponseWriter) Size() int64          { return int64(w.ResponseRecorder.Body.Len()) }
func (w *testResponseWriter) Status() int          { return w.ResponseRecorder.Code }
func (w *testResponseWriter) HeadersWritten() bool { return w.wroteHeader }

func (w *testResponseWriter) SetAttrs(key string, value any) {
	w.attrs[key] = value
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.wroteHeader = true
	w.ResponseRecorder.WriteHeader(statusCode)
}

func (w *testResponseWriter) WriteError(code int, err error) {
	w.lastErr = err
	w.WriteHeader(code)
	_, _ = w.Write([]byte(err.Error()))
}

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

func TestHFHandler_BlobFallbackToUpstream_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/blobs/") {
			_, _ = w.Write([]byte("upstream-blob"))
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

func TestHFHandler_ResolveP2PHit_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(snapshotsDir, sha), 0755))

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("peer-content"))
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

func TestHFHandler_APIFallback_Local(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("api-response"))
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

func TestHFHandler_ResolveLocalCacheHit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))

	snapshotDir := filepath.Join(snapshotsDir, sha)
	require.NoError(t, os.MkdirAll(snapshotDir, 0755))
	cacheFile := filepath.Join(snapshotDir, "model.bin")
	require.NoError(t, os.WriteFile(cacheFile, []byte("cached-content"), 0644))

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

func TestHFHandler_ResolveUpstreamFallback(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream-content"))
	}))
	defer upstream.Close()

	modelDir := filepath.Join(tmp, "models--org--model")
	refsDir := filepath.Join(modelDir, "refs")
	snapshotsDir := filepath.Join(modelDir, "snapshots")

	require.NoError(t, os.MkdirAll(refsDir, 0755))
	require.NoError(t, os.MkdirAll(snapshotsDir, 0755))

	sha := "abc123def456"
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(snapshotsDir, sha), 0755))

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

func TestWalkHFCacheDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	modelDir := filepath.Join(tmp, "models--org--model")
	sha := "abc123"
	createTempHFFile(t, modelDir, "snapshots/"+sha+"/model.safetensors", "data")
	createTempHFFile(t, modelDir, "snapshots/"+sha+"/config.json", "cfg")
	createTempHFFile(t, modelDir, "blobs/sha256-deadbeef", "blob")

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := newTestHFClient(t, tmp, router)

	keys, err := client.WalkHFCacheDir(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 2)

	found := map[string]bool{}
	for _, k := range keys {
		found[k] = true
	}
	require.True(t, found["hf:/huggingface/org/model/resolve/"+sha+"/model.safetensors"])
	require.True(t, found["hf:/huggingface/org/model/resolve/"+sha+"/config.json"])
}

package pip

import (
	"context"
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

// --- TestResponseWriter adapts httptest.ResponseRecorder to mux.ResponseWriter ---

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

// --- PipClient option tests ---

func TestPipClientOptions(t *testing.T) {
	t.Parallel()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	tempDir := t.TempDir()

	opts := []PipOption{
		WithResolveRetries(5),
		WithResolveTimeout(10 * time.Minute),
		WithLogger(logr.Discard()),
		WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	}

	client := NewPipClient(router, tempDir, "https://pypi.org/simple/", opts...)

	require.Equal(t, 5, client.ResolveRetries)
	require.Equal(t, 10*time.Minute, client.ResolveTimeout)
	require.Equal(t, logr.Discard(), client.Log)
	require.Equal(t, 30*time.Second, client.Client.Timeout)
}

// --- PipRegistryHandler tests ---

func TestPipRegistryHandlerRootIndex(t *testing.T) {
	t.Parallel()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	tempDir := t.TempDir()

	client := NewPipClient(router, tempDir, "https://pypi.org/simple/")

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/simple/", nil)

	client.PipRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "clyde pip simple index")
}

func TestPipRegistryHandlerLocalCache(t *testing.T) {
	t.Parallel()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	tempDir := t.TempDir()

	cacheFile := filepath.Join(tempDir, "testpkg-1.0.0-py3-none-any.whl")
	require.NoError(t, os.WriteFile(cacheFile, []byte("cached wheel content"), 0644))

	client := NewPipClient(router, tempDir, "https://pypi.org/simple/")

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/packages/testpkg-1.0.0-py3-none-any.whl", nil)

	client.PipRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "cached wheel content")
}

func TestPipRegistryHandlerP2PResolution(t *testing.T) {
	t.Parallel()

	peerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("peer wheel content"))
	}))
	defer peerSrv.Close()

	peerAddrPort := netip.MustParseAddrPort(peerSrv.Listener.Addr().String())
	resolver := map[string][]netip.AddrPort{
		"pip:peerpkg-1.0.0-py3-none-any.whl": {peerAddrPort},
	}

	router := routing.NewMemoryRouter(resolver, netip.AddrPort{})
	tempDir := t.TempDir()

	client := NewPipClient(router, tempDir, "https://pypi.org/simple/")

	rw := newTestResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/packages/peerpkg-1.0.0-py3-none-any.whl", nil)

	client.PipRegistryHandler(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "peer wheel content")

	// Cached locally
	cacheFile := filepath.Join(tempDir, "peerpkg-1.0.0-py3-none-any.whl")
	require.FileExists(t, cacheFile)
}

func TestPipRegistryHandlerFallback(t *testing.T) {
	t.Parallel()

	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			w.Write([]byte("fallback index content"))
		} else {
			w.Write([]byte("fallback wheel content"))
		}
	}))
	defer fallbackSrv.Close()

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	tempDir := t.TempDir()
	client := NewPipClient(router, tempDir, fallbackSrv.URL+"/simple/")

	tests := []struct {
		name         string
		urlPath      string
		expectedBody string
	}{
		{"fallback index", "/simple/unknownpkg/", "fallback index content"},
		// {"fallback wheel", "/packages/unknownpkg-1.0.0-py3-none-any.whl", "fallback wheel content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := newTestResponseWriter()
			req := httptest.NewRequest(http.MethodGet, tt.urlPath, nil)

			client.PipRegistryHandler(rw, req)

			resp := rw.Result()
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), tt.expectedBody)
		})
	}
}

// --- Additional tests like index rewriting, artifact caching, error cases ---
// These can follow the same pattern: use newTestResponseWriter() as the mux.ResponseWriter

// Example of AddPipConfiguration test
func TestAddPipConfiguration(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	ctx := context.Background()

	err := AddPipConfiguration(ctx, tempDir, "https://pypi.org/simple/", "pypi.org", 30, "")
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "pip.conf")
	require.FileExists(t, configPath)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "index-url = https://pypi.org/simple/")
	require.Contains(t, string(content), "trusted-host = pypi.org")
}

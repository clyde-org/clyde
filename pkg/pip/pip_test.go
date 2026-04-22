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
		_, _ = w.Write([]byte("peer wheel content"))
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

	cacheFile := filepath.Join(tempDir, "peerpkg-1.0.0-py3-none-any.whl")
	require.FileExists(t, cacheFile)
}

func TestPipRegistryHandlerFallback(t *testing.T) {
	t.Parallel()

	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			_, _ = w.Write([]byte("fallback index content"))
		} else {
			_, _ = w.Write([]byte("fallback wheel content"))
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

func TestWalkPipDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "foo-1.0.whl"), []byte("whl"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "bar-2.0.tar.gz"), []byte("tgz"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "baz.html"), []byte("idx"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "README.txt"), []byte("skip"), 0644))

	router := routing.NewMemoryRouter(map[string][]netip.AddrPort{}, netip.AddrPort{})
	client := NewPipClient(router, tempDir, "https://pypi.org/simple/",
		WithLogger(logr.Discard()),
	)

	keys, err := client.WalkPipDir(context.Background())
	require.NoError(t, err)
	require.Len(t, keys, 3)

	found := map[string]bool{}
	for _, k := range keys {
		found[k] = true
	}
	require.True(t, found["pip:foo-1.0.whl"])
	require.True(t, found["pip:bar-2.0.tar.gz"])
	require.True(t, found["pip:baz.html"])
	require.False(t, found["pip:readme.txt"])
}

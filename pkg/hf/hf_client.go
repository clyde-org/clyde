package hf

import (
	"context"
	"net/http"

	"clyde/pkg/mux"
)

// Pip defines the minimal interface for a pip client/proxy.
type Hf interface {
	// PipRegistryHandler handles incoming pip registry requests (/simple, /packages).
	HuggingFaceRegistryHandler(rw mux.ResponseWriter, req *http.Request)
	// WalkPipDir scans the pip cache directory and returns all keys (.whl, .html).
	WalkHFCacheDir(ctx context.Context) ([]string, error)
}

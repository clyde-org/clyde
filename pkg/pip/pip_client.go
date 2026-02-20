package pip

import (
	"context"
	"net/http"

	"clyde/pkg/mux"
)

// Pip defines the minimal interface for a pip client/proxy.
type Pip interface {
	// PipRegistryHandler handles incoming pip registry requests (/simple, /packages).
	PipRegistryHandler(rw mux.ResponseWriter, req *http.Request)
	// WalkPipDir scans the pip cache directory and returns all keys (.whl, .html).
	WalkPipDir(ctx context.Context) ([]string, error)
}

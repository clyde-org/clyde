package pip

import (
	"context"
	"net/http"

	"clyde/pkg/httpx"
)

type Pip interface {
	PipRegistryHandler(rw httpx.ResponseWriter, req *http.Request)
	WalkPipDir(ctx context.Context) ([]string, error)
}

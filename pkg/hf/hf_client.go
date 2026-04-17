package hf

import (
	"context"
	"net/http"

	"clyde/pkg/httpx"
)

type Hf interface {
	HuggingFaceRegistryHandler(rw httpx.ResponseWriter, req *http.Request)
	WalkHFCacheDir(ctx context.Context) ([]string, error)
}

package httpserver

import (
	"net/http"

	"github.com/davibanfi/betledger/api"
)

// DocsHandler serves the OpenAPI specification and the Swagger UI page.
type DocsHandler struct{}

// NewDocsHandler builds the handler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// Register mounts the documentation endpoints.
func (h *DocsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(api.OpenAPI)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(api.SwaggerUI)
	})
}

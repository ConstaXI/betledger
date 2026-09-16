// Package httpserver wires the HTTP server and its lifecycle through Fx.
package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/config"
)

// Module exposes the HTTP server dependencies for Fx composition.
var Module = fx.Module("httpserver",
	fx.Provide(NewMux, NewServer),
	fx.Invoke(registerLifecycle),
)

// NewMux builds the root router of the application.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", handleLive)
	return mux
}

func handleLive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// NewServer builds the *http.Server from the configuration and the router.
func NewServer(cfg config.Config, mux *http.ServeMux) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.HTTPPort),
		Handler: mux,
	}
}

func registerLifecycle(lc fx.Lifecycle, server *http.Server) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return fmt.Errorf("failed to open listener on %s: %w", server.Addr, err)
			}
			go func() {
				_ = server.Serve(ln)
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Shutdown(ctx)
		},
	})
}

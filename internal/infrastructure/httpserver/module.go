// Package httpserver exposes the application over HTTP and manages the server
// lifecycle through Fx.
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

// Route registers endpoints on the router.
type Route interface {
	Register(mux *http.ServeMux)
}

// HealthCheck probes a dependency required to serve traffic.
type HealthCheck struct {
	// Name identifies the dependency in the readiness response.
	Name string
	// Check returns an error when the dependency cannot be used.
	Check func(ctx context.Context) error
}

// Module provides the HTTP handler, the server and its lifecycle. Routes and
// readiness checks are collected from the "routes" and "readiness" groups.
var Module = fx.Module("httpserver",
	fx.Provide(
		fx.Annotate(NewHandler, fx.ParamTags(`group:"routes"`, `group:"readiness"`)),
		NewServer,
		fx.Annotate(NewWalletHandler, fx.As(new(Route)), fx.ResultTags(`group:"routes"`)),
		fx.Annotate(NewWageringHandler, fx.As(new(Route)), fx.ResultTags(`group:"routes"`)),
		fx.Annotate(NewDocsHandler, fx.As(new(Route)), fx.ResultTags(`group:"routes"`)),
	),
	fx.Invoke(registerLifecycle),
)

// NewHandler builds the root handler with health endpoints, the registered
// routes and the correlation middleware.
func NewHandler(routes []Route, checks []HealthCheck, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", handleLive)
	mux.Handle("GET /health/ready", readinessHandler(checks, logger))
	for _, route := range routes {
		route.Register(mux)
	}
	return withCorrelationID(withRequestTimeout(mux))
}

// NewServer builds the HTTP server. The timeouts bound how long a slow client
// can hold a connection.
func NewServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.HTTPPort),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func registerLifecycle(lc fx.Lifecycle, server *http.Server) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return fmt.Errorf("failed to open listener on %s: %w", server.Addr, err)
			}
			go func() {
				_ = server.Serve(listener)
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Shutdown(ctx)
		},
	})
}

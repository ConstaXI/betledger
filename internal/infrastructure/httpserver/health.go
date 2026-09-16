package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

const readinessTimeout = 2 * time.Second

type healthResponse struct {
	Status  string   `json:"status"`
	Failing []string `json:"failing,omitempty"`
}

func handleLive(w http.ResponseWriter, _ *http.Request) {
	_ = writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// readinessHandler reports ready only when every dependency answers in time.
// The response names failing dependencies without exposing error details, which
// are logged instead.
func readinessHandler(checks []HealthCheck, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		var failing []string
		for _, check := range checks {
			if err := check.Check(ctx); err != nil {
				logger.WarnContext(ctx, "readiness check failed", "check", check.Name, "error", err)
				failing = append(failing, check.Name)
			}
		}

		if len(failing) > 0 {
			_ = writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable", Failing: failing})
			return
		}
		_ = writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
	}
}

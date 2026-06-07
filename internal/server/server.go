package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/projdocs/safe-convert/internal"
	"go.uber.org/zap"
)

// New builds and returns the fully configured HTTP handler.
func New(cfg *internal.Config, log *zap.Logger) http.Handler {
	r := chi.NewRouter()

	// -------------------------------------------------------------------------
	// Health check
	//
	// Registered first and outside of any auth middleware. This route must
	// respond to Docker / orchestrator liveness probes without a token.
	// It performs zero I/O and carries no sensitive information.
	// -------------------------------------------------------------------------
	r.Get("/health", handleHealth)

	// -------------------------------------------------------------------------
	// Conversion endpoint
	//
	// Placeholder until the middleware stack and handler are added in the
	// next steps.
	// -------------------------------------------------------------------------
	r.Post("/convert", handleNotImplemented)

	return r
}

// handleHealth responds to liveness probes.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleNotImplemented is a temporary placeholder for the /convert route.
func handleNotImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

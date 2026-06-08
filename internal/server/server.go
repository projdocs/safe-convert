package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/projdocs/safe-convert/internal"
	"github.com/projdocs/safe-convert/internal/server/handlers"
	"github.com/projdocs/safe-convert/internal/server/middleware"
	"go.uber.org/zap"
)

// New builds and returns the fully configured HTTP handler.
func New(cfg *internal.Config, log *zap.Logger) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.SecureHeaders)
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.RejectBrowserRequests)
	r.Use(middleware.MaxBodySize(cfg.MaxFileSizeBytes))

	// -------------------------------------------------------------------------
	// Public group — no authentication required.
	// Only routes that must be reachable without a token belong here.
	// -------------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		r.Get("/health", handlers.HandleHealth)
	})

	// -------------------------------------------------------------------------
	// Protected group — all routes require a valid bearer token.
	// -------------------------------------------------------------------------
	r.Group(func(r chi.Router) {

		// add auth
		r.Use(middleware.BearerAuth(cfg.AccessToken))

		// add conversion handler
		if converter, err := handlers.NewConvert(cfg, log); err != nil {
			log.Fatal("failed to instantiate converter", zap.Error(err))
		} else {
			// Conversion endpoint — placeholder until handler is built.
			r.Post("/convert", converter.ServeHTTP)
		}
	})

	return r
}

package handlers

import (
	"mime"
	"net/http"
	"strings"

	"github.com/projdocs/safe-convert/internal"
	"github.com/projdocs/safe-convert/internal/docker"
	"github.com/projdocs/safe-convert/internal/server/middleware"
	"go.uber.org/zap"
)

type Convert struct {
	cfg    *internal.Config
	docker *docker.Client
	log    *zap.Logger
}

func NewConvert(cfg *internal.Config, log *zap.Logger) (*Convert, error) {
	dkr, err := docker.NewClient(cfg, log)
	if err != nil {
		return nil, err
	}
	return &Convert{cfg: cfg, docker: dkr, log: log}, nil
}

func (h *Convert) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := h.log.With(zap.String("request_id", middleware.GetRequestID(r.Context())))

	// -------------------------------------------------------------------------
	// Parse and validate Content-Type.
	//
	// Three checks in order:
	//   1. Header must be present.
	//   2. Header must be well-formed (parseable by mime.ParseMediaType).
	//   3. Media type must be a known LibreOffice-convertible document type.
	//
	// We do not enforce an allowlist — any recognised document MIME type is
	// accepted. The worker receives the validated media type and selects the
	// appropriate LibreOffice import filter.
	// -------------------------------------------------------------------------
	rawCT := r.Header.Get("Content-Type")
	if rawCT == "" {
		log.Warn("missing Content-Type header")
		http.Error(w, "Content-Type header is required", http.StatusBadRequest)
		return
	}

	mediaType, _, err := mime.ParseMediaType(rawCT)
	if err != nil {
		log.Warn("malformed Content-Type header", zap.String("content_type", rawCT))
		http.Error(w, "Content-Type header is malformed", http.StatusBadRequest)
		return
	}

	mimeType := strings.ToLower(mediaType)
	if _, ok := internal.IsKnownMIMEType(mimeType); !ok {
		log.Warn("unrecognised Content-Type", zap.String("media_type", mediaType))
		http.Error(w, "Content-Type is not a supported document type", http.StatusUnsupportedMediaType)
		return
	}

	// -------------------------------------------------------------------------
	// Pipe r.Body to an ephemeral LibreOffice container.
	// -------------------------------------------------------------------------
	log.Info("dispatching conversion", zap.String("media_type", mediaType))
	if err := h.docker.Convert(
		r.Context(),
		r.Body,
		mimeType,
		w,
		r,
		log,
	); err != nil {
		log.Error("conversion failed", zap.Error(err))
		http.Error(w, "conversion failed", http.StatusInternalServerError)
	}
}

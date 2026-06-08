package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/projdocs/safe-convert/internal/docker"
)

// allowedRoutes defines every (method, path pattern) pair the proxy permits.
// Anything not in this list receives 403 immediately.
var allowedRoutes = []route{
	// Liveness probe
	{method: http.MethodGet, pattern: regexp.MustCompile(`^/_ping$`)},

	// Image pull — further validated against WorkerImage below
	{method: http.MethodPost, pattern: regexp.MustCompile(`^/images/create$`)},

	// Container lifecycle
	{method: http.MethodPost, pattern: regexp.MustCompile(`^/containers/create$`)},
	{method: http.MethodPost, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+/start$`)},
	{method: http.MethodPost, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+/wait$`)},
	{method: http.MethodGet, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+/logs$`)},
	{method: http.MethodDelete, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+$`)},

	// File copy in/out
	{method: http.MethodPut, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+/archive$`)},
	{method: http.MethodGet, pattern: regexp.MustCompile(`^/containers/[a-f0-9]+/archive$`)},
}

type route struct {
	method  string
	pattern *regexp.Regexp
}

func main() {
	if docker.WorkerImage == "" {
		fmt.Fprintln(os.Stderr, "proxy: WorkerImage not injected at build time")
		os.Exit(1)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	socketPath := "/var/run/docker.sock"
	if v := os.Getenv("DOCKER_SOCKET"); v != "" {
		socketPath = v
	}

	listenAddr := ":2375"
	if v := os.Getenv("PROXY_LISTEN"); v != "" {
		listenAddr = v
	}

	// Build a reverse proxy that dials the Docker socket over Unix.
	target, _ := url.Parse("http://docker")
	rp := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.Host = req.In.Host
		},
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return new(net.Dialer).Dial("unix", socketPath)
			},
		},
	}

	handler := &proxyHandler{rp: rp, log: log}

	log.Info("socket proxy starting",
		slog.String("listen", listenAddr),
		slog.String("socket", socketPath),
		slog.String("worker_image", docker.WorkerImage),
	)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: handler,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Error("proxy exited", slog.Any("error", err))
		os.Exit(1)
	}
}

type proxyHandler struct {
	rp  *httputil.ReverseProxy
	log *slog.Logger
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the API version prefix if present (/v1.xx/containers/... → /containers/...).
	path := regexp.MustCompile(`^/v[0-9]+\.[0-9]+`).ReplaceAllString(r.URL.Path, "")

	// -------------------------------------------------------------------------
	// 1. Check method + path against the allowlist.
	// -------------------------------------------------------------------------
	allowed := false
	for _, route := range allowedRoutes {
		if r.Method == route.method && route.pattern.MatchString(path) {
			allowed = true
			break
		}
	}

	if !allowed {
		h.log.Warn("denied",
			slog.String("method", r.Method),
			slog.String("path", path),
		)
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	// -------------------------------------------------------------------------
	// 2. For image pulls, enforce the pinned worker image.
	//
	// Docker's ImagePull uses POST /images/create?fromImage=<ref>&tag=<tag>
	// We reconstruct the full reference and compare against WorkerImage.
	// -------------------------------------------------------------------------
	if r.Method == http.MethodPost && path == "/images/create" {
		fromImage := r.URL.Query().Get("fromImage")
		tag := r.URL.Query().Get("tag")

		// Reconstruct the full reference the caller is requesting.
		var requested string
		if tag != "" && !strings.Contains(fromImage, "@") {
			requested = fromImage + ":" + tag
		} else {
			requested = fromImage
		}

		if requested != docker.WorkerImage {
			h.log.Warn("image pull denied — not the pinned worker image",
				slog.String("requested", requested),
				slog.String("allowed", docker.WorkerImage),
			)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		h.log.Info("image pull allowed",
			slog.String("image", requested),
		)
	}

	// -------------------------------------------------------------------------
	// 3. Forward to the Docker daemon.
	// -------------------------------------------------------------------------
	h.rp.ServeHTTP(w, r)
}

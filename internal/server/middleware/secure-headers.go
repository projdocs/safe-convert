package middleware

import "net/http"

// SecureHeaders sets conservative HTTP response headers on every response.
// These headers are applied unconditionally — including on error responses —
// because the middleware wraps the entire router.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent MIME-type sniffing. Forces the client to honour the
		// Content-Type we declare rather than guessing from the body bytes.
		h.Set("X-Content-Type-Options", "nosniff")

		// Deny framing entirely. This API has no UI; no legitimate client
		// should ever embed a response in a frame.
		h.Set("X-Frame-Options", "DENY")

		// Disable the legacy XSS auditor. It was removed from all major
		// browsers and its heuristics can themselves introduce vulnerabilities.
		h.Set("X-XSS-Protection", "0")

		// Emit no referrer information. Responses from this service must not
		// leak context to any other origin.
		h.Set("Referrer-Policy", "no-referrer")

		// Suppress the Server header. We do not advertise that this is a Go
		// HTTP server, its version, or anything else about the stack.
		h.Set("Server", "")

		// Instruct every cache layer never to store responses. Conversion
		// results are per-request and must never be served from a cache.
		h.Set("Cache-Control", "no-store")

		next.ServeHTTP(w, r)
	})
}

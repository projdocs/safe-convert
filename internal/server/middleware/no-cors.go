package middleware

import "net/http"

// RejectBrowserRequests blocks any request that carries an Origin header.
//
// Origin is set automatically by browsers on all cross-origin requests and on
// all requests made via fetch() or XMLHttpRequest, even same-origin ones in
// some configurations. Legitimate server-to-server callers (e.g. the
// safe-convert orchestrator using Go's net/http) never set Origin.
//
// This middleware does not implement a CORS allowlist — it implements the
// inverse: a total browser block. No Origin is ever permitted. This ensures
// the entry API cannot be called directly from a web page even if the network
// boundary is somehow misconfigured.
//
// Preflight requests (OPTIONS + Origin) are also rejected here before they
// reach the router, so no CORS headers are ever emitted that could encourage
// a browser to retry with credentials.
func RejectBrowserRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w,
				http.StatusText(http.StatusForbidden),
				http.StatusForbidden,
			)
			return
		}

		next.ServeHTTP(w, r)
	})
}

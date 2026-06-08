// Package middleware provides HTTP middleware for the safe-convert entry API.
// Each file in this package contains exactly one middleware and nothing else.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// contextKey is an unexported type for context keys in this package.
// Using a package-local type prevents collisions with keys from other packages
// that also store values in the request context.
type contextKey string

const requestIDKey contextKey = "request_id"

// RequestID generates a cryptographically random ID for every incoming request,
// attaches it to the request context, and echoes it back in the X-Request-ID
// response header.
//
// Downstream handlers and middleware retrieve it via GetRequestID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
		next.ServeHTTP(w, r)
	})
}

// GetRequestID retrieves the request ID from the context.
// Returns an empty string if none has been set.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// newRequestID returns a 32-character lowercase hex string derived from 16
// cryptographically random bytes. crypto/rand is used rather than math/rand
// to ensure IDs cannot be predicted or enumerated by an attacker observing
// responses.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is exceptional and indicates a serious OS-level
		// problem. We panic rather than silently falling back to a weak source,
		// which would be a worse outcome than a crash.
		panic("safe-convert: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

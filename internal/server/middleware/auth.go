package middleware

import (
	"crypto/subtle"
	"net/http"
)

// BearerAuth enforces a static bearer token on every request that passes
// through it. It must be the last middleware added to the stack so that
// RequestID, SecureHeaders, and RequestLogger all run before rejection——
// ensuring every rejected request is logged with a full request ID.
//
// The comparison is constant-time throughout. Both the prefix check and the
// token comparison use subtle.ConstantTimeCompare so that an attacker cannot
// infer the correct token by measuring response latency.
func BearerAuth(token string) func(http.Handler) http.Handler {

	expected := []byte("Bearer " + token)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actual := []byte(r.Header.Get("Authorization"))

			// subtle.ConstantTimeCompare returns 1 only if both slices are
			// identical in length and content. It does not short-circuit on
			// mismatch, preventing timing attacks regardless of where in the
			// token the first differing byte occurs.
			//
			// If the Authorization header is absent, actual is an empty slice.
			// ConstantTimeCompare handles unequal lengths correctly (returns 0)
			// without branching on length, so no separate absent-header check
			// is needed.
			if subtle.ConstantTimeCompare(actual, expected) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="safe-convert"`)
				http.Error(w,
					http.StatusText(http.StatusUnauthorized),
					http.StatusUnauthorized,
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

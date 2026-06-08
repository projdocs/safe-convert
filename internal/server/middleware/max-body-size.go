package middleware

import (
	"errors"
	"net/http"
)

// MaxBodySize rejects requests whose body exceeds the configured limit.
//
// Two enforcement layers are applied:
//
//  1. Content-Length check — if the client declares a Content-Length that
//     already exceeds the limit, the request is rejected immediately before
//     a single body byte is read off the wire.
//
//  2. http.MaxBytesReader — wraps r.Body so that any read beyond the limit
//     during handler execution is hard-stopped. This catches clients that
//     omit Content-Length or declare an incorrect one.
func MaxBodySize(limitBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Layer 1: reject on declared Content-Length before reading anything.
			// r.ContentLength is -1 when the header is absent; we only act when
			// a positive value is declared and already over the limit.
			if r.ContentLength > limitBytes {
				http.Error(w,
					http.StatusText(http.StatusRequestEntityTooLarge),
					http.StatusRequestEntityTooLarge,
				)
				return
			}

			// Layer 2: cap streaming reads regardless of Content-Length.
			r.Body = http.MaxBytesReader(w, r.Body, limitBytes)

			next.ServeHTTP(w, r)
		})
	}
}

// IsMaxBytesError reports whether err originated from http.MaxBytesReader
// exceeding its limit. Handlers use this to return 413 rather than 500 when
// a body read is cut short mid-stream.
func IsMaxBytesError(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

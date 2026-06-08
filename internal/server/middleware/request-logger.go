package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// responseRecorder wraps http.ResponseWriter to capture the status code and
// bytes written after the handler completes, so the logger can record them.
type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	wroteHeader  bool
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.wroteHeader {
		rr.statusCode = code
		rr.wroteHeader = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	n, err := rr.ResponseWriter.Write(b)
	rr.bytesWritten += int64(n)
	return n, err
}

// RequestLogger logs one structured line per request after it completes.
// It records method, path, status, duration, bytes written, and request ID.
//
// Deliberately omitted from logs:
//   - Client IP — this is an internal service; IPs are not meaningful and
//     logging them increases PII surface area unnecessarily.
//   - Query string — may contain sensitive parameters; only the path is logged.
//   - Request headers — may contain the Authorization header in malformed
//     requests; never log headers at this level.
//   - Request body size — logged by the handler where the full context is known.
func RequestLogger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rr := &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rr, r)

			log.Info("request",
				zap.String("request_id", GetRequestID(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rr.statusCode),
				zap.Int64("bytes_written", rr.bytesWritten),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}

package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type logEntry struct {
	Timestamp  string `json:"timestamp"`
	RequestID  string `json:"request_id,omitempty"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	BytesOut   int    `json:"bytes_out"`
	RemoteAddr string `json:"remote_addr"`
}

// StructuredLogging returns middleware that logs requests in JSON or text format.
func StructuredLogging(format string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, statusCode: 200}

			next.ServeHTTP(rec, r)

			duration := time.Since(start)
			reqID := GetRequestID(r.Context())

			if format == "json" {
				entry := logEntry{
					Timestamp:  start.UTC().Format(time.RFC3339),
					RequestID:  reqID,
					Method:     r.Method,
					Path:       r.URL.Path,
					Status:     rec.statusCode,
					DurationMs: duration.Milliseconds(),
					BytesOut:   rec.bytes,
					RemoteAddr: r.RemoteAddr,
				}
				b, _ := json.Marshal(entry)
				log.Println(string(b))
			} else {
				if reqID != "" {
					log.Printf("[http] %s %s %d %v [%s]", r.Method, r.URL.Path, rec.statusCode, duration, reqID)
				} else {
					log.Printf("[http] %s %s %d %v", r.Method, r.URL.Path, rec.statusCode, duration)
				}
			}
		})
	}
}

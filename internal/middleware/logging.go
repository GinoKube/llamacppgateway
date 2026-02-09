package middleware

import (
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

// Logging returns middleware that logs requests in text format.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: 200}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		reqID := GetRequestID(r.Context())

		if reqID != "" {
			log.Printf("[http] %s %s %d %v [%s]", r.Method, r.URL.Path, rec.statusCode, duration, reqID)
		} else {
			log.Printf("[http] %s %s %d %v", r.Method, r.URL.Path, rec.statusCode, duration)
		}
	})
}

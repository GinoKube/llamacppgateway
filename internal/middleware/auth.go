package middleware

import (
	"net/http"
	"strings"

	"github.com/llamawrapper/gateway/internal/config"
)

// Auth returns middleware that validates API keys from the Authorization header.
// Admin endpoints require keys from the admin_keys list.
func Auth(cfg config.AuthConfig) func(http.Handler) http.Handler {
	keySet := make(map[string]bool, len(cfg.Keys)+len(cfg.AdminKeys))
	for _, k := range cfg.Keys {
		keySet[k] = true
	}
	for _, k := range cfg.AdminKeys {
		keySet[k] = true
	}

	adminSet := make(map[string]bool, len(cfg.AdminKeys))
	for _, k := range cfg.AdminKeys {
		adminSet[k] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip auth for health and metrics endpoints
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			key := extractAPIKey(r)
			if key == "" || !keySet[key] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":{"message":"Invalid or missing API key","type":"authentication_error","code":"invalid_api_key"}}`))
				return
			}

			// Check admin access for admin endpoints
			if strings.HasPrefix(r.URL.Path, "/admin/") && !adminSet[key] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":{"message":"Admin access required","type":"authorization_error","code":"forbidden"}}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractAPIKey(r *http.Request) string {
	// Check Authorization: Bearer <key>
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Check X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	return ""
}

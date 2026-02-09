package dashboard

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/llamawrapper/gateway/internal/config"
	"github.com/llamawrapper/gateway/internal/metrics"
	"github.com/llamawrapper/gateway/internal/process"
)

type Handler struct {
	manager    *process.Manager
	metrics    *metrics.Metrics
	reloadFunc func() error
	startTime  time.Time
}

func NewHandler(manager *process.Manager, m *metrics.Metrics) *Handler {
	return &Handler{manager: manager, metrics: m, startTime: time.Now()}
}

// SetReloadFunc sets the config reload callback for the dashboard config editor.
func (h *Handler) SetReloadFunc(fn func() error) {
	h.reloadFunc = fn
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Existing routes
	mux.HandleFunc("/dashboard", h.authWrap(h.serveDashboard))
	mux.HandleFunc("/dashboard/api/data", h.authWrap(h.serveData))
	mux.HandleFunc("/dashboard/api/requests", h.authWrap(h.serveRequests))
	mux.HandleFunc("/dashboard/api/request/", h.authWrap(h.serveRequestDetail))
	mux.HandleFunc("/dashboard/api/timeseries", h.authWrap(h.serveTimeSeries))
	mux.HandleFunc("/dashboard/api/keys", h.authWrap(h.serveKeyUsage))
	mux.HandleFunc("/dashboard/api/config", h.authWrap(h.serveConfig))
	mux.HandleFunc("/dashboard/api/system", h.authWrap(h.serveSystemInfo))
	mux.HandleFunc("/dashboard/api/load", h.authWrap(h.handleLoad))
	mux.HandleFunc("/dashboard/api/unload", h.authWrap(h.handleUnload))
	// New routes
	mux.HandleFunc("/dashboard/api/sse", h.authWrap(h.serveSSE))
	mux.HandleFunc("/dashboard/api/events", h.authWrap(h.serveEvents))
	mux.HandleFunc("/dashboard/api/audit", h.authWrap(h.serveAudit))
	mux.HandleFunc("/dashboard/api/health-history", h.authWrap(h.serveHealthHistory))
	mux.HandleFunc("/dashboard/api/vram/", h.authWrap(h.serveVRAMEstimate))
	mux.HandleFunc("/dashboard/api/disk", h.authWrap(h.serveDiskUsage))
	mux.HandleFunc("/dashboard/api/warmup", h.authWrap(h.handleWarmup))
	mux.HandleFunc("/dashboard/api/compare", h.authWrap(h.serveCompare))
	mux.HandleFunc("/dashboard/api/sla", h.authWrap(h.serveSLA))
	mux.HandleFunc("/dashboard/api/hourly", h.authWrap(h.serveHourly))
	mux.HandleFunc("/dashboard/api/model-events", h.authWrap(h.serveModelEvents))
	mux.HandleFunc("/dashboard/api/export/requests", h.authWrap(h.exportRequests))
	mux.HandleFunc("/dashboard/api/export/timeseries", h.authWrap(h.exportTimeSeries))
	mux.HandleFunc("/dashboard/api/export/keys", h.authWrap(h.exportKeys))
	mux.HandleFunc("/dashboard/api/config/edit", h.authWrap(h.handleConfigEdit))
	mux.HandleFunc("/dashboard/api/config/reload", h.authWrap(h.handleConfigReload))
	mux.HandleFunc("/dashboard/api/toggles", h.authWrap(h.handleToggles))
	mux.HandleFunc("/dashboard/api/schedule", h.authWrap(h.handleSchedule))
	mux.HandleFunc("/dashboard/api/keys/manage", h.authWrap(h.handleKeyManage))
	mux.HandleFunc("/dashboard/api/model/add", h.authWrap(h.handleModelAdd))
}

// authWrap optionally protects dashboard endpoints with a password.
func (h *Handler) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := h.manager.GetConfig()
		pw := cfg.Dashboard.Password
		if pw == "" {
			next(w, r)
			return
		}
		// Check query param, header, or cookie
		if r.URL.Query().Get("token") == pw || r.Header.Get("X-Dashboard-Token") == pw {
			next(w, r)
			return
		}
		if c, err := r.Cookie("dashboard_token"); err == nil && c.Value == pw {
			next(w, r)
			return
		}
		// For the HTML page, show a login form
		if r.URL.Path == "/dashboard" && r.Method == http.MethodPost {
			r.ParseForm()
			if r.FormValue("password") == pw {
				http.SetCookie(w, &http.Cookie{Name: "dashboard_token", Value: pw, Path: "/dashboard", MaxAge: 86400})
				http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
				return
			}
		}
		if r.URL.Path == "/dashboard" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(loginHTML))
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

var loginHTML = "<html><head><title>Login</title><style>body{background:#0f172a;color:#e2e8f0;font-family:sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh}form{background:#1e293b;padding:32px;border-radius:12px;border:1px solid #334155}input{display:block;margin:12px 0;padding:8px 12px;border-radius:6px;border:1px solid #475569;background:#0f172a;color:#e2e8f0;width:250px}button{padding:8px 20px;background:#3b82f6;color:white;border:none;border-radius:6px;cursor:pointer;font-weight:600}</style></head><body><form method='POST'><h2>Dashboard Login</h2><input type='password' name='password' placeholder='Password' autofocus><button type='submit'>Login</button></form></body></html>"

func (h *Handler) serveData(w http.ResponseWriter, r *http.Request) {
	backends := h.manager.ListBackendStatus()
	configured := h.manager.ListConfiguredModels()
	gpuInfo := h.manager.GetGPUInfo()
	queueLen := h.manager.GetQueueLength()

	var metricsSnap *metrics.Snapshot
	if h.metrics != nil {
		s := h.metrics.GetSnapshot()
		metricsSnap = &s
	}

	type modelInfo struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases,omitempty"`
		Path    string   `json:"path"`
		Loaded  bool     `json:"loaded"`
	}

	var models []modelInfo
	loadedSet := make(map[string]bool)
	for _, b := range backends {
		loadedSet[b.ModelName] = true
	}
	for _, m := range configured {
		models = append(models, modelInfo{
			Name:    m.Name,
			Aliases: m.Aliases,
			Path:    m.ModelPath,
			Loaded:  loadedSet[m.Name],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"backends":    backends,
		"models":      models,
		"gpu":         gpuInfo,
		"queue_depth": queueLen,
		"metrics":     metricsSnap,
	})
}

func (h *Handler) serveRequests(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	var records []metrics.RequestRecord
	if h.metrics != nil {
		records = h.metrics.GetRequestHistory(limit)
	}
	if records == nil {
		records = []metrics.RequestRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

func (h *Handler) serveRequestDetail(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /dashboard/api/request/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "missing request ID", http.StatusBadRequest)
		return
	}
	id := parts[len(parts)-1]

	if h.metrics == nil {
		http.Error(w, "metrics not enabled", http.StatusNotFound)
		return
	}

	rec, found := h.metrics.GetRequestByID(id)
	if !found {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rec)
}

func (h *Handler) serveTimeSeries(w http.ResponseWriter, r *http.Request) {
	var points []metrics.TimeSeriesPoint
	if h.metrics != nil {
		points = h.metrics.GetTimeSeries()
	}
	if points == nil {
		points = []metrics.TimeSeriesPoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(points)
}

func (h *Handler) serveKeyUsage(w http.ResponseWriter, r *http.Request) {
	var usage map[string]*metrics.KeyUsage
	if h.metrics != nil {
		usage = h.metrics.GetKeyUsage()
	}
	if usage == nil {
		usage = make(map[string]*metrics.KeyUsage)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

func (h *Handler) serveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg := h.manager.GetConfig()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"listen_addr":       cfg.ListenAddr,
			"max_loaded_models": cfg.MaxLoadedModels,
			"health_check_sec":  cfg.HealthCheckSec,
			"auth_enabled":      cfg.Auth.Enabled,
			"rate_limit_enabled": cfg.RateLimit.Enabled,
			"cache_enabled":     cfg.Cache.Enabled,
			"queue_enabled":     cfg.Queue.Enabled,
			"metrics_enabled":   cfg.Metrics.Enabled,
			"dashboard_enabled": cfg.Dashboard.Enabled,
			"logging_format":    cfg.Logging.Format,
			"models_count":      len(cfg.Models),
			"config_path":       cfg.ConfigPath(),
		})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) serveSystemInfo(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hostname":      hostname,
		"go_version":    runtime.Version(),
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"mem_alloc_mb":  memStats.Alloc / 1024 / 1024,
		"mem_sys_mb":    memStats.Sys / 1024 / 1024,
		"uptime_sec":    time.Since(h.startTime).Seconds(),
	})
}

func (h *Handler) handleLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		http.Error(w, "model field required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	_, err := h.manager.EnsureModel(ctx, req.Model)
	if err != nil {
		log.Printf("[dashboard] Failed to load model %q: %v", req.Model, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	if h.metrics != nil {
		h.metrics.AddAuditEntry("load", r.RemoteAddr, req.Model, "model loaded via dashboard")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "loaded", "model": req.Model})
}

func (h *Handler) handleUnload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		http.Error(w, "model field required", http.StatusBadRequest)
		return
	}

	h.manager.UnloadModel(req.Model)
	if h.metrics != nil {
		h.metrics.AddAuditEntry("unload", r.RemoteAddr, req.Model, "model unloaded via dashboard")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "unloaded", "model": req.Model})
}

// --- SSE Live Feed ---
func (h *Handler) serveSSE(w http.ResponseWriter, r *http.Request) {
	if h.metrics == nil {
		http.Error(w, "metrics not enabled", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.metrics.SubscribeSSE()
	defer h.metrics.UnsubscribeSSE(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(rec)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- Events ---
func (h *Handler) serveEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, _ := strconv.Atoi(l); n > 0 && n <= 500 {
			limit = n
		}
	}
	var events []metrics.EventEntry
	if h.metrics != nil {
		events = h.metrics.GetEventLog(limit)
	}
	if events == nil {
		events = []metrics.EventEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// --- Audit Log ---
func (h *Handler) serveAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, _ := strconv.Atoi(l); n > 0 && n <= 200 {
			limit = n
		}
	}
	var entries []metrics.AuditEntry
	if h.metrics != nil {
		entries = h.metrics.GetAuditLog(limit)
	}
	if entries == nil {
		entries = []metrics.AuditEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// --- Health Check History ---
func (h *Handler) serveHealthHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, _ := strconv.Atoi(l); n > 0 && n <= 500 {
			limit = n
		}
	}
	var results []metrics.HealthCheckResult
	if h.metrics != nil {
		results = h.metrics.GetHealthHistory(limit)
	}
	if results == nil {
		results = []metrics.HealthCheckResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// --- VRAM Estimate ---
func (h *Handler) serveVRAMEstimate(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	model := parts[len(parts)-1]
	if model == "" {
		// Return estimates for all models
		cfg := h.manager.GetConfig()
		var estimates []*process.VRAMEstimate
		for _, m := range cfg.Models {
			if est := h.manager.EstimateVRAM(m.Name); est != nil {
				estimates = append(estimates, est)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(estimates)
		return
	}
	est := h.manager.EstimateVRAM(model)
	if est == nil {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(est)
}

// --- Disk Usage ---
func (h *Handler) serveDiskUsage(w http.ResponseWriter, r *http.Request) {
	usage := h.manager.GetDiskUsage()
	if usage == nil {
		usage = []process.DiskUsageInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

// --- Warmup ---
func (h *Handler) handleWarmup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		http.Error(w, "model field required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := h.manager.WarmupModel(ctx, req.Model); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	if h.metrics != nil {
		h.metrics.AddAuditEntry("warmup", r.RemoteAddr, req.Model, "KV cache warmed")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "warmed", "model": req.Model})
}

// --- Compare ---
func (h *Handler) serveCompare(w http.ResponseWriter, r *http.Request) {
	var comparisons []metrics.ModelComparison
	if h.metrics != nil {
		comparisons = h.metrics.GetModelComparisons()
	}
	if comparisons == nil {
		comparisons = []metrics.ModelComparison{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(comparisons)
}

// --- SLA ---
func (h *Handler) serveSLA(w http.ResponseWriter, r *http.Request) {
	var sla metrics.SLAStatus
	if h.metrics != nil {
		sla = h.metrics.GetSLAStatus()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sla)
}

// --- Hourly Aggregation ---
func (h *Handler) serveHourly(w http.ResponseWriter, r *http.Request) {
	var buckets []metrics.HourlyBucket
	if h.metrics != nil {
		buckets = h.metrics.GetHourlyAggregation()
	}
	if buckets == nil {
		buckets = []metrics.HourlyBucket{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buckets)
}

// --- Model Events ---
func (h *Handler) serveModelEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, _ := strconv.Atoi(l); n > 0 && n <= 200 {
			limit = n
		}
	}
	events := h.manager.GetModelEvents(limit)
	if events == nil {
		events = []process.ModelEvent{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// --- Export CSV ---
func (h *Handler) exportRequests(w http.ResponseWriter, r *http.Request) {
	var records []metrics.RequestRecord
	if h.metrics != nil {
		records = h.metrics.GetRequestHistory(500)
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=requests.csv")
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "timestamp", "model", "endpoint", "duration_ms", "status", "is_error", "is_stream", "is_cache_hit", "prompt_tokens", "completion_tokens", "remote_addr", "api_key"})
	for _, rec := range records {
		cw.Write([]string{rec.ID, rec.Timestamp, rec.Model, rec.Endpoint, fmt.Sprintf("%.1f", rec.DurationMs), strconv.Itoa(rec.Status), strconv.FormatBool(rec.IsError), strconv.FormatBool(rec.IsStream), strconv.FormatBool(rec.IsCacheHit), strconv.Itoa(rec.PromptTokens), strconv.Itoa(rec.CompletionTokens), rec.RemoteAddr, rec.APIKey})
	}
	cw.Flush()
}

func (h *Handler) exportTimeSeries(w http.ResponseWriter, r *http.Request) {
	var points []metrics.TimeSeriesPoint
	if h.metrics != nil {
		points = h.metrics.GetTimeSeries()
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=timeseries.csv")
	cw := csv.NewWriter(w)
	cw.Write([]string{"timestamp", "rps", "tps", "avg_latency_ms", "active_reqs", "error_rate", "gpu_pct"})
	for _, p := range points {
		cw.Write([]string{strconv.FormatInt(p.Timestamp, 10), fmt.Sprintf("%.2f", p.RequestsPerSec), fmt.Sprintf("%.2f", p.TokensPerSec), fmt.Sprintf("%.1f", p.AvgLatencyMs), strconv.FormatInt(p.ActiveReqs, 10), fmt.Sprintf("%.4f", p.ErrorRate), fmt.Sprintf("%.1f", p.GPUMemUsedPct)})
	}
	cw.Flush()
}

func (h *Handler) exportKeys(w http.ResponseWriter, r *http.Request) {
	var usage map[string]*metrics.KeyUsage
	if h.metrics != nil {
		usage = h.metrics.GetKeyUsage()
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=api_keys.csv")
	cw := csv.NewWriter(w)
	cw.Write([]string{"key", "requests", "tokens", "errors", "last_request"})
	for k, v := range usage {
		cw.Write([]string{k, strconv.FormatInt(v.Requests, 10), strconv.FormatInt(v.Tokens, 10), strconv.FormatInt(v.Errors, 10), v.LastRequestTime})
	}
	cw.Flush()
}

// --- Config Edit ---
func (h *Handler) handleConfigEdit(w http.ResponseWriter, r *http.Request) {
	cfg := h.manager.GetConfig()
	configPath := cfg.ConfigPath()

	if r.Method == http.MethodGet {
		data, err := os.ReadFile(configPath)
		if err != nil {
			http.Error(w, "cannot read config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path":    configPath,
			"content": string(data),
		})
		return
	}

	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate the YAML by trying to parse it
		if _, err := config.Load(configPath); err == nil {
			// Write the new config
			if err := os.WriteFile(configPath, []byte(req.Content), 0644); err != nil {
				http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if h.metrics != nil {
				h.metrics.AddAuditEntry("config_edit", r.RemoteAddr, configPath, "config file updated")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "saved"})
			return
		}

		// Just save anyway (user might be fixing a broken config)
		if err := os.WriteFile(configPath, []byte(req.Content), 0644); err != nil {
			http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if h.metrics != nil {
			h.metrics.AddAuditEntry("config_edit", r.RemoteAddr, configPath, "config file updated")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "saved"})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Config Reload ---
func (h *Handler) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.reloadFunc == nil {
		http.Error(w, "reload not configured", http.StatusNotImplemented)
		return
	}
	if err := h.reloadFunc(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	if h.metrics != nil {
		h.metrics.AddAuditEntry("config_reload", r.RemoteAddr, "", "hot reload triggered")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "reloaded"})
}

// --- Feature Toggles ---
func (h *Handler) handleToggles(w http.ResponseWriter, r *http.Request) {
	cfg := h.manager.GetConfig()
	configPath := cfg.ConfigPath()

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"auth":       cfg.Auth.Enabled,
			"rate_limit": cfg.RateLimit.Enabled,
			"cache":      cfg.Cache.Enabled,
			"queue":      cfg.Queue.Enabled,
			"metrics":    cfg.Metrics.Enabled,
			"dashboard":  cfg.Dashboard.Enabled,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Feature string `json:"feature"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Read, modify, write config
		data, err := os.ReadFile(configPath)
		if err != nil {
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		content := string(data)
		oldVal := "enabled: true"
		newVal := "enabled: false"
		if req.Enabled {
			oldVal = "enabled: false"
			newVal = "enabled: true"
		}

		// Simple text replacement for the specific section
		sections := map[string]string{
			"auth": "auth:", "rate_limit": "rate_limit:", "cache": "cache:",
			"queue": "queue:", "metrics": "metrics:", "dashboard": "dashboard:",
		}
		if section, ok := sections[req.Feature]; ok {
			idx := strings.Index(content, section)
			if idx >= 0 {
				// Find the enabled: line within next 100 chars
				sub := content[idx : idx+min(100, len(content)-idx)]
				oldIdx := strings.Index(sub, oldVal)
				if oldIdx >= 0 {
					content = content[:idx+oldIdx] + newVal + content[idx+oldIdx+len(oldVal):]
					os.WriteFile(configPath, []byte(content), 0644)
					if h.reloadFunc != nil {
						h.reloadFunc()
					}
					if h.metrics != nil {
						h.metrics.AddAuditEntry("toggle", r.RemoteAddr, req.Feature, fmt.Sprintf("set to %v", req.Enabled))
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "toggled", "feature": req.Feature, "enabled": req.Enabled})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Scheduled Actions ---
func (h *Handler) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		actions := h.manager.GetScheduledActions()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(actions)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Type     string `json:"type"` // unload_idle
			Model    string `json:"model"`
			AfterMin int    `json:"after_min"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || req.AfterMin <= 0 {
			http.Error(w, "type, model, after_min required", http.StatusBadRequest)
			return
		}
		id := h.manager.AddScheduledAction(req.Type, req.Model, req.AfterMin)
		if h.metrics != nil {
			h.metrics.AddAuditEntry("schedule_add", r.RemoteAddr, req.Model, fmt.Sprintf("%s after %d min", req.Type, req.AfterMin))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "created", "id": id})
		return
	}
	if r.Method == http.MethodDelete {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ok := h.manager.RemoveScheduledAction(req.ID)
		if h.metrics != nil {
			h.metrics.AddAuditEntry("schedule_remove", r.RemoteAddr, req.ID, "")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"removed": ok})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- API Key Management ---
func (h *Handler) handleKeyManage(w http.ResponseWriter, r *http.Request) {
	cfg := h.manager.GetConfig()
	configPath := cfg.ConfigPath()

	if r.Method == http.MethodGet {
		// Return current keys (masked)
		var masked []string
		for _, k := range cfg.Auth.Keys {
			if len(k) > 12 {
				masked = append(masked, k[:8]+"..."+k[len(k)-4:])
			} else {
				masked = append(masked, "***")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"keys": masked, "count": len(cfg.Auth.Keys)})
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"` // create, revoke
			Key    string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		content := string(data)

		switch req.Action {
		case "create":
			if req.Key == "" {
				// Generate a random key
				req.Key = fmt.Sprintf("sk-%d", time.Now().UnixNano())
			}
			// Add to keys list in YAML
			keysIdx := strings.Index(content, "keys:")
			if keysIdx >= 0 {
				insertPoint := strings.Index(content[keysIdx:], "\n") + keysIdx + 1
				content = content[:insertPoint] + "    - " + req.Key + "\n" + content[insertPoint:]
				os.WriteFile(configPath, []byte(content), 0644)
				if h.reloadFunc != nil {
					h.reloadFunc()
				}
			}
			if h.metrics != nil {
				h.metrics.AddAuditEntry("key_create", r.RemoteAddr, req.Key[:min(8, len(req.Key))], "API key created")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "created", "key": req.Key})
		case "revoke":
			// Remove key from YAML
			content = strings.Replace(content, "    - "+req.Key+"\n", "", 1)
			os.WriteFile(configPath, []byte(content), 0644)
			if h.reloadFunc != nil {
				h.reloadFunc()
			}
			if h.metrics != nil {
				h.metrics.AddAuditEntry("key_revoke", r.RemoteAddr, req.Key[:min(8, len(req.Key))], "API key revoked")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "revoked"})
		default:
			http.Error(w, "action must be 'create' or 'revoke'", http.StatusBadRequest)
		}
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Add Model ---
func (h *Handler) handleModelAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name        string `json:"name"`
		ModelPath   string `json:"model_path"`
		GPULayers   int    `json:"gpu_layers"`
		ContextSize int    `json:"context_size"`
		Threads     int    `json:"threads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.ModelPath == "" {
		http.Error(w, "name and model_path required", http.StatusBadRequest)
		return
	}

	cfg := h.manager.GetConfig()
	configPath := cfg.ConfigPath()

	// Defaults
	if req.ContextSize == 0 {
		req.ContextSize = 4096
	}
	if req.Threads == 0 {
		req.Threads = 4
	}

	// Append model to config YAML
	data, err := os.ReadFile(configPath)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}

	modelYaml := fmt.Sprintf("\n  - name: %s\n    model_path: %s\n    gpu_layers: %d\n    context_size: %d\n    threads: %d\n    batch_size: 512\n",
		req.Name, req.ModelPath, req.GPULayers, req.ContextSize, req.Threads)

	content := string(data) + modelYaml
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.reloadFunc != nil {
		h.reloadFunc()
	}
	if h.metrics != nil {
		h.metrics.AddAuditEntry("model_add", r.RemoteAddr, req.Name, fmt.Sprintf("path=%s gpu=%d ctx=%d", req.ModelPath, req.GPULayers, req.ContextSize))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "added", "model": req.Name})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (h *Handler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML2))
}

// Old dashboard HTML removed - now using dashboardHTML2 from html.go
const _oldHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LlamaWrapper Dashboard (OLD)</title>
<style>
:root{--bg:#0f172a;--surface:#1e293b;--border:#334155;--text:#e2e8f0;--text-dim:#94a3b8;--text-muted:#64748b;--accent:#3b82f6;--green:#22c55e;--yellow:#eab308;--red:#ef4444;--orange:#f97316}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;display:flex;flex-direction:column}

/* Header */
.hdr{background:var(--surface);border-bottom:1px solid var(--border);padding:12px 24px;display:flex;align-items:center;gap:12px;position:sticky;top:0;z-index:100}
.hdr h1{font-size:18px;font-weight:700;color:#f8fafc;flex:1}
.hdr .dot{width:8px;height:8px;border-radius:50%;background:var(--green);animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}
.hdr .uptime{font-size:12px;color:var(--text-muted)}

/* Alerts */
.alerts{padding:0 24px}
.alert{padding:10px 16px;border-radius:8px;font-size:13px;margin-top:8px;display:flex;align-items:center;gap:8px}
.alert.warn{background:#713f1240;border:1px solid #92400e;color:#fbbf24}
.alert.error{background:#7f1d1d40;border:1px solid #991b1b;color:#f87171}
.alert .icon{font-size:16px}

/* Tabs */
.tabs{display:flex;gap:0;padding:0 24px;margin-top:12px;border-bottom:1px solid var(--border);overflow-x:auto}
.tab{padding:10px 20px;font-size:13px;font-weight:500;color:var(--text-muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .2s;white-space:nowrap}
.tab:hover{color:var(--text)}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.tab-content{display:none;padding:20px 24px;max-width:1400px;margin:0 auto;width:100%}
.tab-content.active{display:block}

/* Stats grid */
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px;margin-bottom:20px}
.stat-card{background:var(--surface);border:1px solid var(--border);border-radius:10px;padding:16px}
.stat-card .label{font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:.5px;margin-bottom:2px}
.stat-card .val{font-size:26px;font-weight:700;color:#f8fafc}
.stat-card .sub{font-size:11px;color:var(--text-muted);margin-top:2px}

/* Cards & sections */
.card{background:var(--surface);border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:16px}
.card h3{font-size:14px;font-weight:600;margin-bottom:12px;color:var(--text-dim)}
.section-title{font-size:14px;font-weight:600;color:var(--text-dim);margin-bottom:10px}

/* Tables */
table{width:100%;border-collapse:collapse}
th{text-align:left;padding:8px 10px;font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:.5px;border-bottom:1px solid var(--border)}
td{padding:8px 10px;border-bottom:1px solid var(--border);font-size:13px}
tr:hover{background:#ffffff06}
.clickable{cursor:pointer}
.clickable:hover{background:#ffffff0d}

/* Badges */
.badge{display:inline-block;padding:2px 8px;border-radius:9999px;font-size:11px;font-weight:600}
.badge.ready{background:#064e3b;color:#34d399}
.badge.starting{background:#713f12;color:#fbbf24}
.badge.failed{background:#7f1d1d;color:#f87171}
.badge.stopped{background:#1e293b;color:var(--text-muted)}
.badge.stream{background:#1e3a5f;color:#60a5fa}
.badge.cached{background:#064e3b;color:#34d399}
.badge.error{background:#7f1d1d;color:#f87171}

/* Buttons */
.btn{padding:6px 14px;border-radius:6px;font-size:12px;font-weight:600;border:none;cursor:pointer;transition:all .15s}
.btn-primary{background:var(--accent);color:white}
.btn-primary:hover{background:#2563eb}
.btn-danger{background:#dc2626;color:white}
.btn-danger:hover{background:#b91c1c}
.btn-sm{padding:4px 10px;font-size:11px}
.btn:disabled{opacity:.5;cursor:not-allowed}

/* GPU bar */
.gpu-bar{width:100%;height:8px;background:#334155;border-radius:4px;overflow:hidden;margin-top:6px}
.gpu-fill{height:100%;border-radius:4px;transition:width .5s}

/* Model cards grid */
.model-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:12px}
.model-card{background:var(--surface);border:1px solid var(--border);border-radius:10px;padding:16px;position:relative}
.model-card h3{font-size:14px;font-weight:600;margin-bottom:4px;display:flex;align-items:center;gap:8px}
.model-card .aliases{font-size:11px;color:var(--text-muted);margin-bottom:8px}
.model-card .model-stat{display:flex;justify-content:space-between;font-size:12px;padding:4px 0;border-bottom:1px solid #ffffff08}
.model-card .model-stat .k{color:var(--text-dim)}
.model-card .model-stat .v{color:#f8fafc;font-weight:500}
.model-card .actions{margin-top:10px;display:flex;gap:6px}

/* Charts */
.chart-container{position:relative;height:200px;margin:8px 0}
canvas{width:100%!important;height:100%!important}
.chart-row{display:grid;grid-template-columns:1fr 1fr;gap:12px}
@media(max-width:900px){.chart-row{grid-template-columns:1fr}}

/* Request detail modal */
.modal-overlay{display:none;position:fixed;top:0;left:0;right:0;bottom:0;background:#00000080;z-index:200;align-items:center;justify-content:center}
.modal-overlay.active{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:12px;max-width:700px;width:90%;max-height:80vh;overflow-y:auto;padding:24px}
.modal h2{font-size:16px;font-weight:600;margin-bottom:16px;display:flex;align-items:center;justify-content:space-between}
.modal .close{cursor:pointer;color:var(--text-muted);font-size:20px}
.modal .field{margin-bottom:12px}
.modal .field .flabel{font-size:11px;color:var(--text-muted);text-transform:uppercase;margin-bottom:2px}
.modal .field .fval{font-size:13px;color:var(--text);word-break:break-all}
.modal .field pre{background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:10px;font-size:12px;max-height:200px;overflow-y:auto;white-space:pre-wrap;word-break:break-word}

/* Config/System info */
.info-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}
.info-item{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:12px}
.info-item .ilabel{font-size:11px;color:var(--text-muted);text-transform:uppercase;margin-bottom:2px}
.info-item .ival{font-size:14px;font-weight:500}

/* Latency colors */
.lat-fast{color:var(--green)}.lat-med{color:var(--yellow)}.lat-slow{color:var(--orange)}.lat-bad{color:var(--red)}

/* Scrollbar */
::-webkit-scrollbar{width:6px}::-webkit-scrollbar-track{background:var(--bg)}::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
</style>
</head>
<body>

<div class="hdr">
<div class="dot" id="status-dot"></div>
<h1>LlamaWrapper Dashboard</h1>
<span class="uptime" id="uptime"></span>
</div>

<div class="alerts" id="alerts"></div>

<div class="tabs" id="tabs">
<div class="tab active" data-tab="overview">Overview</div>
<div class="tab" data-tab="models">Models</div>
<div class="tab" data-tab="requests">Requests</div>
<div class="tab" data-tab="charts">Charts</div>
<div class="tab" data-tab="gpu">GPU</div>
<div class="tab" data-tab="keys">API Keys</div>
<div class="tab" data-tab="system">System</div>
</div>

<!-- Overview Tab -->
<div class="tab-content active" id="tab-overview">
<div class="stats" id="stats"></div>
<div class="card"><h3>Backends</h3>
<table><thead><tr><th>Model</th><th>Port</th><th>State</th><th>Active</th><th>Last Used</th><th>Action</th></tr></thead>
<tbody id="backends"></tbody></table>
</div>
<div class="section-title">Recent Requests</div>
<div class="card" style="max-height:320px;overflow-y:auto">
<table><thead><tr><th>Time</th><th>Model</th><th>Endpoint</th><th>Latency</th><th>Tokens</th><th>Status</th></tr></thead>
<tbody id="recent-reqs"></tbody></table>
</div>
</div>

<!-- Models Tab -->
<div class="tab-content" id="tab-models">
<div class="model-grid" id="model-cards"></div>
</div>

<!-- Requests Tab -->
<div class="tab-content" id="tab-requests">
<div class="card">
<div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
<h3 style="margin:0;flex:1">Request History</h3>
<select id="req-filter" style="background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:6px;padding:4px 8px;font-size:12px">
<option value="all">All Requests</option>
<option value="errors">Errors Only</option>
<option value="slow">Slow (&gt;2s)</option>
<option value="cached">Cache Hits</option>
</select>
</div>
<table><thead><tr><th>Time</th><th>ID</th><th>Model</th><th>Endpoint</th><th>Latency</th><th>Tokens</th><th>Status</th><th>Source</th></tr></thead>
<tbody id="all-reqs"></tbody></table>
</div>
</div>

<!-- Charts Tab -->
<div class="tab-content" id="tab-charts">
<div class="chart-row">
<div class="card"><h3>Throughput (req/s)</h3><div class="chart-container"><canvas id="chart-rps"></canvas></div></div>
<div class="card"><h3>Tokens/sec</h3><div class="chart-container"><canvas id="chart-tps"></canvas></div></div>
</div>
<div class="chart-row">
<div class="card"><h3>Avg Latency (ms)</h3><div class="chart-container"><canvas id="chart-lat"></canvas></div></div>
<div class="card"><h3>GPU Memory %</h3><div class="chart-container"><canvas id="chart-gpu"></canvas></div></div>
</div>
</div>

<!-- GPU Tab -->
<div class="tab-content" id="tab-gpu">
<div id="gpu-cards"></div>
</div>

<!-- API Keys Tab -->
<div class="tab-content" id="tab-keys">
<div class="card"><h3>API Key Usage</h3>
<table><thead><tr><th>Key</th><th>Requests</th><th>Tokens</th><th>Errors</th><th>Last Request</th></tr></thead>
<tbody id="key-table"></tbody></table>
</div>
</div>

<!-- System Tab -->
<div class="tab-content" id="tab-system">
<div class="section-title">System Information</div>
<div class="info-grid" id="sys-info"></div>
<div class="section-title" style="margin-top:20px">Configuration</div>
<div class="info-grid" id="cfg-info"></div>
</div>

<!-- Request detail modal -->
<div class="modal-overlay" id="modal">
<div class="modal">
<h2><span>Request Detail</span><span class="close" onclick="closeModal()">&times;</span></h2>
<div id="modal-body"></div>
</div>
</div>

<script>
/* --- State --- */
let data={},reqs=[],ts=[],keys={},sys={},cfg={};
let selectedReqId=null;

/* --- Helpers --- */
const $=id=>document.getElementById(id);
const fmt=n=>(n==null)?'—':typeof n==='number'?n.toLocaleString():n;
const fmtMs=n=>(n==null)?'—':n<1000?Math.round(n)+'ms':(n/1000).toFixed(1)+'s';
const fmtTime=t=>{try{return new Date(t).toLocaleTimeString()}catch(e){return t||'—'}};
const fmtUptime=s=>{const h=Math.floor(s/3600),m=Math.floor((s%3600)/60),ss=Math.floor(s%60);return h>0?h+'h '+m+'m':m>0?m+'m '+ss+'s':ss+'s'};
const latClass=ms=>ms<500?'lat-fast':ms<2000?'lat-med':ms<5000?'lat-slow':'lat-bad';
const gpuColor=pct=>pct<50?'var(--green)':pct<80?'var(--yellow)':'var(--red)';

/* --- Tabs --- */
document.querySelectorAll('.tab').forEach(tab=>{
  tab.onclick=()=>{
    document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
    tab.classList.add('active');
    $('tab-'+tab.dataset.tab).classList.add('active');
  };
});

/* --- API calls --- */
async function fetchAll(){
  try{
    const [dRes,rRes,tRes,kRes,sRes,cRes]=await Promise.all([
      fetch('/dashboard/api/data'),fetch('/dashboard/api/requests?limit=200'),
      fetch('/dashboard/api/timeseries'),fetch('/dashboard/api/keys'),
      fetch('/dashboard/api/system'),fetch('/dashboard/api/config')
    ]);
    data=await dRes.json();reqs=await rRes.json();ts=await tRes.json();
    keys=await kRes.json();sys=await sRes.json();cfg=await cRes.json();
    render();
  }catch(e){console.error('fetch error',e)}
}

/* --- Render --- */
function render(){
  const m=data.metrics||{};
  const ms=m.model_stats||{};
  const models=data.models||[];
  const backends=data.backends||[];
  const gpu=data.gpu||[];

  // Uptime
  $('uptime').textContent=fmtUptime(m.uptime_seconds||sys.uptime_sec||0);

  // Alerts
  let alertsHtml='';
  backends.forEach(b=>{if(b.state==='failed')alertsHtml+='<div class="alert error"><span class="icon">&#9888;</span>Backend <b>'+b.model_name+'</b> (port '+b.port+') has failed</div>'});
  if((data.queue_depth||0)>20)alertsHtml+='<div class="alert warn"><span class="icon">&#9888;</span>Queue depth is high: '+data.queue_depth+' requests waiting</div>';
  gpu.forEach(g=>{if(g.MemTotalMB>0&&(g.MemUsedMB/g.MemTotalMB)>.9)alertsHtml+='<div class="alert warn"><span class="icon">&#9888;</span>GPU '+g.Index+' memory usage above 90%: '+g.MemUsedMB+'/'+g.MemTotalMB+' MB</div>'});
  $('alerts').innerHTML=alertsHtml;

  // Stats
  const cacheTotal=(m.cache_hits||0)+(m.cache_misses||0);
  const cacheRate=cacheTotal>0?((m.cache_hits/cacheTotal)*100).toFixed(0)+'%':'—';
  const errRate=(m.requests_total||0)>0?((m.errors_total/(m.requests_total))*100).toFixed(1)+'%':'0%';
  $('stats').innerHTML=` +
	"`" + `
<div class="stat-card"><div class="label">Requests</div><div class="val">${fmt(m.requests_total||0)}</div><div class="sub">${errRate} error rate</div></div>
<div class="stat-card"><div class="label">Active</div><div class="val">${fmt(m.active_requests||0)}</div><div class="sub">in-flight</div></div>
<div class="stat-card"><div class="label">Models</div><div class="val">${fmt(m.loaded_models||0)}/${models.length}</div><div class="sub">loaded / configured</div></div>
<div class="stat-card"><div class="label">Queue</div><div class="val">${fmt(data.queue_depth||0)}</div><div class="sub">waiting</div></div>
<div class="stat-card"><div class="label">Tokens</div><div class="val">${fmt(m.tokens_generated||0)}</div><div class="sub">generated</div></div>
<div class="stat-card"><div class="label">Cache</div><div class="val">${cacheRate}</div><div class="sub">${fmt(m.cache_hits||0)} hits / ${fmt(cacheTotal)} total</div></div>
` + "`" + `;

  // Backends table
  let bh='';
  if(backends.length>0){
    backends.forEach(b=>{
      bh+=` + "`" + `<tr>
<td><b>${b.model_name}</b></td><td>${b.port}</td>
<td><span class="badge ${b.state}">${b.state}</span></td>
<td>${b.active_requests}</td><td>${fmtTime(b.last_used)}</td>
<td><button class="btn btn-danger btn-sm" onclick="unloadModel('${b.model_name}')">Unload</button></td>
</tr>` + "`" + `;
    });
  }else{bh='<tr><td colspan="6" style="color:var(--text-muted);text-align:center;padding:16px">No backends running</td></tr>'}
  $('backends').innerHTML=bh;

  // Recent requests (overview, last 15)
  $('recent-reqs').innerHTML=renderReqRows(reqs.slice(0,15),true);

  // Model cards
  let mc='';
  models.forEach(model=>{
    const s=ms[model.name]||{};
    const loaded=model.loaded;
    const aliases=model.aliases&&model.aliases.length>0?model.aliases.join(', '):'none';
    mc+=` + "`" + `<div class="model-card">
<h3><span class="badge ${loaded?'ready':'stopped'}">${loaded?'LOADED':'UNLOADED'}</span> ${model.name}</h3>
<div class="aliases">Aliases: ${aliases}</div>
<div class="model-stat"><span class="k">Path</span><span class="v" style="font-size:11px;max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${model.path||'auto-download'}</span></div>
<div class="model-stat"><span class="k">Requests</span><span class="v">${fmt(s.requests||0)}</span></div>
<div class="model-stat"><span class="k">Avg Latency</span><span class="v ${latClass(s.avg_ms)}">${fmtMs(s.avg_ms)}</span></div>
<div class="model-stat"><span class="k">P50</span><span class="v ${latClass(s.p50_ms)}">${fmtMs(s.p50_ms)}</span></div>
<div class="model-stat"><span class="k">P95</span><span class="v ${latClass(s.p95_ms)}">${fmtMs(s.p95_ms)}</span></div>
<div class="actions">
${loaded
  ?'<button class="btn btn-danger btn-sm" onclick="unloadModel(\''+model.name+'\')">Unload</button>'
  :'<button class="btn btn-primary btn-sm" onclick="loadModel(\''+model.name+'\')">Load</button>'}
</div></div>` + "`" + `;
  });
  if(!mc)mc='<div class="card" style="text-align:center;color:var(--text-muted)">No models configured</div>';
  $('model-cards').innerHTML=mc;

  // All requests tab
  renderRequestsTab();

  // Charts
  renderCharts();

  // GPU tab
  let gh='';
  if(gpu.length>0){
    gpu.forEach(g=>{
      const pct=g.MemTotalMB>0?((g.MemUsedMB/g.MemTotalMB)*100):0;
      gh+=` + "`" + `<div class="card">
<h3>GPU ${g.Index}: ${g.Name}</h3>
<div style="display:flex;justify-content:space-between;margin-top:8px">
<span style="font-size:24px;font-weight:700">${g.MemUsedMB||0} MB</span>
<span style="color:var(--text-muted)">/ ${g.MemTotalMB} MB</span>
</div>
<div class="gpu-bar"><div class="gpu-fill" style="width:${pct.toFixed(1)}%;background:${gpuColor(pct)}"></div></div>
<div style="display:flex;justify-content:space-between;margin-top:6px;font-size:12px;color:var(--text-muted)">
<span>${pct.toFixed(1)}% used</span><span>${g.MemFreeMB} MB free</span>
</div></div>` + "`" + `;
    });
  }else{gh='<div class="card" style="text-align:center;color:var(--text-muted)">No GPU info available</div>'}
  $('gpu-cards').innerHTML=gh;

  // Key usage
  let kh='';
  const keyEntries=Object.entries(keys);
  if(keyEntries.length>0){
    keyEntries.sort((a,b)=>b[1].requests-a[1].requests);
    keyEntries.forEach(([k,v])=>{
      kh+=` + "`" + `<tr><td><code>${k}</code></td><td>${fmt(v.requests)}</td><td>${fmt(v.tokens)}</td><td>${fmt(v.errors)}</td><td>${fmtTime(v.last_request)}</td></tr>` + "`" + `;
    });
  }else{kh='<tr><td colspan="5" style="color:var(--text-muted);text-align:center;padding:16px">No API key usage recorded</td></tr>'}
  $('key-table').innerHTML=kh;

  // System info
  $('sys-info').innerHTML=` + "`" + `
<div class="info-item"><div class="ilabel">Hostname</div><div class="ival">${sys.hostname||'—'}</div></div>
<div class="info-item"><div class="ilabel">Go Version</div><div class="ival">${sys.go_version||'—'}</div></div>
<div class="info-item"><div class="ilabel">OS / Arch</div><div class="ival">${sys.os||''}/${sys.arch||''}</div></div>
<div class="info-item"><div class="ilabel">CPUs</div><div class="ival">${sys.num_cpu||'—'}</div></div>
<div class="info-item"><div class="ilabel">Goroutines</div><div class="ival">${fmt(sys.num_goroutine)}</div></div>
<div class="info-item"><div class="ilabel">Memory (alloc)</div><div class="ival">${fmt(sys.mem_alloc_mb)} MB</div></div>
<div class="info-item"><div class="ilabel">Memory (sys)</div><div class="ival">${fmt(sys.mem_sys_mb)} MB</div></div>
<div class="info-item"><div class="ilabel">Uptime</div><div class="ival">${fmtUptime(sys.uptime_sec||0)}</div></div>
` + "`" + `;

  $('cfg-info').innerHTML=` + "`" + `
<div class="info-item"><div class="ilabel">Listen Address</div><div class="ival">${cfg.listen_addr||'—'}</div></div>
<div class="info-item"><div class="ilabel">Max Loaded Models</div><div class="ival">${cfg.max_loaded_models||'—'}</div></div>
<div class="info-item"><div class="ilabel">Health Check</div><div class="ival">${cfg.health_check_sec||'—'}s</div></div>
<div class="info-item"><div class="ilabel">Auth</div><div class="ival">${cfg.auth_enabled?'<span style="color:var(--green)">Enabled</span>':'Disabled'}</div></div>
<div class="info-item"><div class="ilabel">Rate Limit</div><div class="ival">${cfg.rate_limit_enabled?'<span style="color:var(--green)">Enabled</span>':'Disabled'}</div></div>
<div class="info-item"><div class="ilabel">Cache</div><div class="ival">${cfg.cache_enabled?'<span style="color:var(--green)">Enabled</span>':'Disabled'}</div></div>
<div class="info-item"><div class="ilabel">Queue</div><div class="ival">${cfg.queue_enabled?'<span style="color:var(--green)">Enabled</span>':'Disabled'}</div></div>
<div class="info-item"><div class="ilabel">Logging Format</div><div class="ival">${cfg.logging_format||'text'}</div></div>
` + "`" + `;
}

/* --- Request rows --- */
function renderReqRows(list,compact){
  if(!list||list.length===0)return '<tr><td colspan="'+(compact?6:8)+'" style="color:var(--text-muted);text-align:center;padding:16px">No requests yet</td></tr>';
  return list.map(r=>{
    const badges=[];
    if(r.is_error)badges.push('<span class="badge error">ERR '+r.status+'</span>');
    else badges.push('<span class="badge ready">'+r.status+'</span>');
    if(r.is_stream)badges.push('<span class="badge stream">SSE</span>');
    if(r.is_cache_hit)badges.push('<span class="badge cached">CACHE</span>');
    const tokens=(r.prompt_tokens||0)+(r.completion_tokens||0);
    const latCls=latClass(r.duration_ms);
    if(compact){
      return '<tr class="clickable" onclick="showRequest(\''+r.id+'\')"><td>'+fmtTime(r.timestamp)+'</td><td>'+r.model+'</td><td>'+r.endpoint+'</td><td class="'+latCls+'">'+fmtMs(r.duration_ms)+'</td><td>'+tokens+'</td><td>'+badges.join(' ')+'</td></tr>';
    }
    return '<tr class="clickable" onclick="showRequest(\''+r.id+'\')"><td>'+fmtTime(r.timestamp)+'</td><td style="font-size:11px;max-width:120px;overflow:hidden;text-overflow:ellipsis">'+(r.id||'—')+'</td><td>'+r.model+'</td><td>'+r.endpoint+'</td><td class="'+latCls+'">'+fmtMs(r.duration_ms)+'</td><td>'+tokens+'</td><td>'+badges.join(' ')+'</td><td style="font-size:11px">'+r.remote_addr+'</td></tr>';
  }).join('');
}

function renderRequestsTab(){
  const filter=$('req-filter').value;
  let filtered=reqs;
  if(filter==='errors')filtered=reqs.filter(r=>r.is_error);
  else if(filter==='slow')filtered=reqs.filter(r=>r.duration_ms>2000);
  else if(filter==='cached')filtered=reqs.filter(r=>r.is_cache_hit);
  $('all-reqs').innerHTML=renderReqRows(filtered,false);
}

$('req-filter').addEventListener('change',renderRequestsTab);

/* --- Request detail modal --- */
function showRequest(id){
  if(!id)return;
  const r=reqs.find(x=>x.id===id);
  if(!r){$('modal-body').innerHTML='Request not found';$('modal').classList.add('active');return}
  const fields=[
    ['Request ID',r.id],['Timestamp',r.timestamp],['Model',r.model],['Endpoint',r.endpoint],
    ['Duration',fmtMs(r.duration_ms)],['Status',r.status+(r.is_error?' (error)':'')],
    ['Stream',r.is_stream?'Yes':'No'],['Cache Hit',r.is_cache_hit?'Yes':'No'],
    ['Prompt Tokens',r.prompt_tokens],['Completion Tokens',r.completion_tokens],
    ['Remote Address',r.remote_addr],['API Key',r.api_key||'none']
  ];
  let html=fields.map(([l,v])=>'<div class="field"><div class="flabel">'+l+'</div><div class="fval">'+v+'</div></div>').join('');
  if(r.prompt)html+='<div class="field"><div class="flabel">Prompt</div><pre>'+escHtml(r.prompt)+'</pre></div>';
  if(r.response)html+='<div class="field"><div class="flabel">Response</div><pre>'+escHtml(r.response)+'</pre></div>';
  $('modal-body').innerHTML=html;
  $('modal').classList.add('active');
}
function closeModal(){$('modal').classList.remove('active')}
$('modal').addEventListener('click',e=>{if(e.target===$('modal'))closeModal()});
function escHtml(s){const d=document.createElement('div');d.textContent=s;return d.innerHTML}

/* --- Charts (canvas) --- */
function renderCharts(){
  drawChart('chart-rps',ts.map(p=>p.rps),'#3b82f6');
  drawChart('chart-tps',ts.map(p=>p.tps),'#22c55e');
  drawChart('chart-lat',ts.map(p=>p.lat),'#f97316');
  drawChart('chart-gpu',ts.map(p=>p.gpu_pct),'#a855f7',100);
}

function drawChart(canvasId,values,color,fixedMax){
  const canvas=$(canvasId);
  if(!canvas)return;
  const ctx=canvas.getContext('2d');
  const rect=canvas.parentElement.getBoundingClientRect();
  const dpr=window.devicePixelRatio||1;
  canvas.width=rect.width*dpr;
  canvas.height=rect.height*dpr;
  ctx.scale(dpr,dpr);
  const w=rect.width,h=rect.height;
  ctx.clearRect(0,0,w,h);

  if(!values||values.length<2){
    ctx.fillStyle='#475569';ctx.font='13px sans-serif';ctx.textAlign='center';
    ctx.fillText('Waiting for data...',w/2,h/2);return;
  }

  const max=fixedMax||Math.max(...values,1)*1.1;
  const pad={t:10,b:24,l:50,r:10};
  const cw=w-pad.l-pad.r,ch=h-pad.t-pad.b;
  const step=cw/(values.length-1);

  // Grid
  ctx.strokeStyle='#1e293b';ctx.lineWidth=1;
  for(let i=0;i<=4;i++){
    const y=pad.t+(ch/4)*i;
    ctx.beginPath();ctx.moveTo(pad.l,y);ctx.lineTo(w-pad.r,y);ctx.stroke();
    ctx.fillStyle='#475569';ctx.font='10px sans-serif';ctx.textAlign='right';
    ctx.fillText(((4-i)/4*max).toFixed(max>100?0:1),pad.l-6,y+3);
  }

  // Area
  ctx.beginPath();ctx.moveTo(pad.l,pad.t+ch);
  values.forEach((v,i)=>{const x=pad.l+i*step;const y=pad.t+ch-(v/max)*ch;ctx.lineTo(x,y)});
  ctx.lineTo(pad.l+(values.length-1)*step,pad.t+ch);ctx.closePath();
  ctx.fillStyle=color+'18';ctx.fill();

  // Line
  ctx.beginPath();
  values.forEach((v,i)=>{const x=pad.l+i*step;const y=pad.t+ch-(v/max)*ch;i===0?ctx.moveTo(x,y):ctx.lineTo(x,y)});
  ctx.strokeStyle=color;ctx.lineWidth=2;ctx.stroke();

  // Latest value
  const last=values[values.length-1];
  ctx.fillStyle=color;ctx.font='bold 12px sans-serif';ctx.textAlign='right';
  ctx.fillText(last.toFixed(last>100?0:2),w-pad.r,pad.t-2);
}

/* --- Actions --- */
async function loadModel(name){
  try{
    const btn=event.target;btn.disabled=true;btn.textContent='Loading...';
    await fetch('/dashboard/api/load',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({model:name})});
    await fetchAll();
  }catch(e){alert('Failed to load: '+e)}
}
async function unloadModel(name){
  try{
    await fetch('/dashboard/api/unload',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({model:name})});
    await fetchAll();
  }catch(e){alert('Failed to unload: '+e)}
}

/* --- Init --- */
fetchAll();
setInterval(fetchAll,3000);
</script>
</body>
</html>`

package metrics

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// RequestRecord stores details about a single completed request.
type RequestRecord struct {
	ID          string  `json:"id"`
	Timestamp   string  `json:"timestamp"`
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	DurationMs  float64 `json:"duration_ms"`
	Status      int     `json:"status"`
	IsError     bool    `json:"is_error"`
	IsStream    bool    `json:"is_stream"`
	IsCacheHit  bool    `json:"is_cache_hit"`
	PromptTokens int    `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	APIKey      string  `json:"api_key,omitempty"` // masked
	Prompt      string  `json:"prompt,omitempty"`  // first 200 chars
	Response    string  `json:"response,omitempty"` // first 500 chars
	RemoteAddr  string  `json:"remote_addr"`
	// Waterfall timing breakdown
	QueueMs     float64 `json:"queue_ms,omitempty"`
	LoadMs      float64 `json:"load_ms,omitempty"`
	InferenceMs float64 `json:"inference_ms,omitempty"`
}

// AuditEntry records an admin action.
type AuditEntry struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`  // load, unload, reload, toggle, key_create, key_revoke, schedule, etc.
	Actor     string `json:"actor"`   // IP or API key
	Target    string `json:"target"`  // model name, feature name, etc.
	Detail    string `json:"detail"` // extra info
}

// EventEntry records a system event (error, restart, health check, etc.).
type EventEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // info, warn, error
	Source    string `json:"source"` // process, health, download, etc.
	Model     string `json:"model,omitempty"`
	Message   string `json:"message"`
}

// HealthCheckResult records a single health check outcome.
type HealthCheckResult struct {
	Timestamp string `json:"timestamp"`
	Model     string `json:"model"`
	Port      int    `json:"port"`
	OK        bool   `json:"ok"`
	LatencyMs float64 `json:"latency_ms"`
}

// HourlyBucket aggregates metrics for a one-hour window.
type HourlyBucket struct {
	Hour          string  `json:"hour"` // "2026-02-06T16"
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	Tokens        int64   `json:"tokens"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	latencySum    float64
	latencyCount  int64
}

// SLAStatus tracks SLA compliance.
type SLAStatus struct {
	TargetP95Ms     float64 `json:"target_p95_ms"`
	CurrentP95Ms    float64 `json:"current_p95_ms"`
	Compliant       bool    `json:"compliant"`
	UptimePct       float64 `json:"uptime_pct"`
	ErrorBudgetPct  float64 `json:"error_budget_pct"` // % of error budget remaining
	TotalChecks     int64   `json:"total_checks"`
	PassedChecks    int64   `json:"passed_checks"`
}

// TimeSeriesPoint stores a data point at a specific time.
type TimeSeriesPoint struct {
	Timestamp      int64   `json:"t"` // unix seconds
	RequestsPerSec float64 `json:"rps"`
	TokensPerSec   float64 `json:"tps"`
	AvgLatencyMs   float64 `json:"lat"`
	ActiveReqs     int64   `json:"active"`
	ErrorRate      float64 `json:"err_rate"`
	GPUMemUsedPct  float64 `json:"gpu_pct"`
}

// KeyUsage tracks per-API-key usage.
type KeyUsage struct {
	Requests        int64 `json:"requests"`
	Tokens          int64 `json:"tokens"`
	Errors          int64 `json:"errors"`
	LastRequestTime string `json:"last_request"`
}

// Metrics collects Prometheus-compatible metrics for the gateway.
type Metrics struct {
	mu sync.RWMutex

	// Counters
	RequestsTotal   int64
	RequestsByModel map[string]*int64
	ErrorsTotal     int64
	CacheHits       int64
	CacheMisses     int64

	// Histograms (latency buckets in ms)
	LatencyBuckets []float64
	LatencyCounts  []int64 // count per bucket

	// Gauges
	ActiveRequests  int64
	LoadedModels    int64
	QueueDepth      int64

	// Token tracking
	TokensGenerated int64

	// Per-model latency samples (ring buffer)
	modelLatencies map[string]*latencyRing

	// Request history (ring buffer of last 500 requests)
	requestHistory [500]RequestRecord
	requestHistIdx int
	requestHistCnt int

	// Time-series data (1 point per 5 seconds, keep ~30 min)
	timeSeries    [360]TimeSeriesPoint
	tsIdx         int
	tsCnt         int
	tsLastReqs    int64
	tsLastTokens  int64
	tsLastErrors  int64
	tsLastTime    time.Time

	// Per-API-key usage
	keyUsage map[string]*KeyUsage

	// Audit log (ring buffer of last 200 entries)
	auditLog    [200]AuditEntry
	auditIdx    int
	auditCnt    int

	// Event log (ring buffer of last 500 entries)
	eventLog    [500]EventEntry
	eventIdx    int
	eventCnt    int

	// Health check history (ring buffer of last 500)
	healthHistory    [500]HealthCheckResult
	healthHistIdx    int
	healthHistCnt    int

	// Hourly aggregation (last 168 hours = 7 days)
	hourlyBuckets    [168]HourlyBucket
	hourlyIdx        int
	hourlyCnt        int
	currentHourKey   string

	// SLA tracking
	SLATargetP95Ms   float64 // configurable target
	SLATargetErrPct  float64 // configurable max error %
	healthTotalChecks int64
	healthPassChecks  int64

	// SSE subscribers
	sseSubscribers map[chan RequestRecord]struct{}
	sseMu          sync.Mutex

	// GPU memory percentage callback
	GPUMemPctFunc func() float64

	startTime time.Time
}

type latencyRing struct {
	samples [1000]float64
	idx     int
	count   int
}

func (lr *latencyRing) add(ms float64) {
	lr.samples[lr.idx%len(lr.samples)] = ms
	lr.idx++
	if lr.count < len(lr.samples) {
		lr.count++
	}
}

func (lr *latencyRing) percentile(p float64) float64 {
	if lr.count == 0 {
		return 0
	}
	sorted := make([]float64, lr.count)
	start := 0
	if lr.idx > len(lr.samples) {
		start = lr.idx % len(lr.samples)
	}
	for i := 0; i < lr.count; i++ {
		sorted[i] = lr.samples[(start+i)%len(lr.samples)]
	}
	sort.Float64s(sorted)
	rank := p / 100.0 * float64(lr.count-1)
	return sorted[int(math.Round(rank))]
}

func (lr *latencyRing) avg() float64 {
	if lr.count == 0 {
		return 0
	}
	sum := 0.0
	start := 0
	if lr.idx > len(lr.samples) {
		start = lr.idx % len(lr.samples)
	}
	for i := 0; i < lr.count; i++ {
		sum += lr.samples[(start+i)%len(lr.samples)]
	}
	return sum / float64(lr.count)
}

// New creates a new Metrics collector.
func New() *Metrics {
	m := &Metrics{
		RequestsByModel: make(map[string]*int64),
		LatencyBuckets:  []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000},
		LatencyCounts:   make([]int64, 12), // len(buckets) + 1 for +Inf
		modelLatencies:  make(map[string]*latencyRing),
		keyUsage:        make(map[string]*KeyUsage),
		sseSubscribers:  make(map[chan RequestRecord]struct{}),
		tsLastTime:      time.Now(),
		startTime:       time.Now(),
		SLATargetP95Ms:  2000, // default 2s P95 target
		SLATargetErrPct: 1.0,  // default 1% error budget
	}

	// Start time-series collector (every 5 seconds)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m.collectTimeSeriesPoint()
		}
	}()

	return m
}

// AddRequestRecord adds a detailed request record to the history ring buffer.
func (m *Metrics) AddRequestRecord(rec RequestRecord) {
	m.mu.Lock()
	m.requestHistory[m.requestHistIdx%len(m.requestHistory)] = rec
	m.requestHistIdx++
	if m.requestHistCnt < len(m.requestHistory) {
		m.requestHistCnt++
	}
	// Update hourly aggregation
	m.updateHourlyBucket(rec)
	m.mu.Unlock()

	// Broadcast to SSE subscribers (non-blocking)
	m.sseMu.Lock()
	for ch := range m.sseSubscribers {
		select {
		case ch <- rec:
		default: // drop if subscriber is slow
		}
	}
	m.sseMu.Unlock()
}

// GetRequestHistory returns the most recent N request records.
func (m *Metrics) GetRequestHistory(limit int) []RequestRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > m.requestHistCnt {
		limit = m.requestHistCnt
	}
	if limit == 0 {
		return nil
	}

	result := make([]RequestRecord, limit)
	for i := 0; i < limit; i++ {
		idx := (m.requestHistIdx - 1 - i)
		if idx < 0 {
			idx += len(m.requestHistory)
		}
		result[i] = m.requestHistory[idx%len(m.requestHistory)]
	}
	return result
}

// GetRequestByID returns a single request record by ID.
func (m *Metrics) GetRequestByID(id string) (RequestRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := 0; i < m.requestHistCnt; i++ {
		idx := (m.requestHistIdx - 1 - i)
		if idx < 0 {
			idx += len(m.requestHistory)
		}
		rec := m.requestHistory[idx%len(m.requestHistory)]
		if rec.ID == id {
			return rec, true
		}
	}
	return RequestRecord{}, false
}

// collectTimeSeriesPoint samples current metrics into the time-series ring buffer.
func (m *Metrics) collectTimeSeriesPoint() {
	now := time.Now()
	elapsed := now.Sub(m.tsLastTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	curReqs := atomic.LoadInt64(&m.RequestsTotal)
	curTokens := atomic.LoadInt64(&m.TokensGenerated)
	curErrors := atomic.LoadInt64(&m.ErrorsTotal)

	deltaReqs := curReqs - m.tsLastReqs
	deltaTokens := curTokens - m.tsLastTokens
	deltaErrors := curErrors - m.tsLastErrors

	rps := float64(deltaReqs) / elapsed
	tps := float64(deltaTokens) / elapsed
	errRate := 0.0
	if deltaReqs > 0 {
		errRate = float64(deltaErrors) / float64(deltaReqs)
	}

	// Compute avg latency from recent model latencies
	avgLat := 0.0
	m.mu.RLock()
	count := 0
	for _, lr := range m.modelLatencies {
		if lr.count > 0 {
			avgLat += lr.avg()
			count++
		}
	}
	m.mu.RUnlock()
	if count > 0 {
		avgLat /= float64(count)
	}

	gpuPct := 0.0
	if m.GPUMemPctFunc != nil {
		gpuPct = m.GPUMemPctFunc()
	}

	point := TimeSeriesPoint{
		Timestamp:      now.Unix(),
		RequestsPerSec: rps,
		TokensPerSec:   tps,
		AvgLatencyMs:   avgLat,
		ActiveReqs:     atomic.LoadInt64(&m.ActiveRequests),
		ErrorRate:      errRate,
		GPUMemUsedPct:  gpuPct,
	}

	m.mu.Lock()
	m.timeSeries[m.tsIdx%len(m.timeSeries)] = point
	m.tsIdx++
	if m.tsCnt < len(m.timeSeries) {
		m.tsCnt++
	}
	m.tsLastReqs = curReqs
	m.tsLastTokens = curTokens
	m.tsLastErrors = curErrors
	m.tsLastTime = now
	m.mu.Unlock()
}

// GetTimeSeries returns all collected time-series points in chronological order.
func (m *Metrics) GetTimeSeries() []TimeSeriesPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.tsCnt == 0 {
		return nil
	}

	result := make([]TimeSeriesPoint, m.tsCnt)
	start := 0
	if m.tsIdx > len(m.timeSeries) {
		start = m.tsIdx % len(m.timeSeries)
	}
	for i := 0; i < m.tsCnt; i++ {
		result[i] = m.timeSeries[(start+i)%len(m.timeSeries)]
	}
	return result
}

// RecordKeyUsage tracks usage for an API key.
func (m *Metrics) RecordKeyUsage(key string, tokens int, isError bool) {
	if key == "" {
		return
	}
	// Mask key for storage: show first 8 and last 4 chars
	masked := maskKey(key)

	m.mu.Lock()
	defer m.mu.Unlock()

	ku, ok := m.keyUsage[masked]
	if !ok {
		ku = &KeyUsage{}
		m.keyUsage[masked] = ku
	}
	ku.Requests++
	ku.Tokens += int64(tokens)
	if isError {
		ku.Errors++
	}
	ku.LastRequestTime = time.Now().Format(time.RFC3339)
}

// GetKeyUsage returns per-key usage stats.
func (m *Metrics) GetKeyUsage() map[string]*KeyUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy
	result := make(map[string]*KeyUsage, len(m.keyUsage))
	for k, v := range m.keyUsage {
		copy := *v
		result[k] = &copy
	}
	return result
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// RecordRequest records a completed request.
func (m *Metrics) RecordRequest(model string, durationMs float64, isError bool) {
	atomic.AddInt64(&m.RequestsTotal, 1)
	if isError {
		atomic.AddInt64(&m.ErrorsTotal, 1)
	}

	// Per-model counter
	m.mu.Lock()
	counter, ok := m.RequestsByModel[model]
	if !ok {
		var c int64
		counter = &c
		m.RequestsByModel[model] = counter
	}

	// Per-model latency
	lr, ok := m.modelLatencies[model]
	if !ok {
		lr = &latencyRing{}
		m.modelLatencies[model] = lr
	}
	lr.add(durationMs)
	m.mu.Unlock()

	atomic.AddInt64(counter, 1)

	// Histogram buckets (non-cumulative: only increment the first matching bucket)
	placed := false
	for i, bound := range m.LatencyBuckets {
		if !placed && durationMs <= bound {
			atomic.AddInt64(&m.LatencyCounts[i], 1)
			placed = true
			break
		}
	}
	if !placed {
		// Exceeds all defined buckets — goes into the +Inf overflow bucket
		atomic.AddInt64(&m.LatencyCounts[len(m.LatencyBuckets)], 1)
	}
}

// RecordTokens adds to the token counter.
func (m *Metrics) RecordTokens(count int) {
	atomic.AddInt64(&m.TokensGenerated, int64(count))
}

// RecordCacheHit increments cache hit counter.
func (m *Metrics) RecordCacheHit()  { atomic.AddInt64(&m.CacheHits, 1) }
func (m *Metrics) RecordCacheMiss() { atomic.AddInt64(&m.CacheMisses, 1) }

// SetActiveRequests sets the active request gauge.
func (m *Metrics) IncrActive()              { atomic.AddInt64(&m.ActiveRequests, 1) }
func (m *Metrics) DecrActive()              { atomic.AddInt64(&m.ActiveRequests, -1) }
func (m *Metrics) SetLoadedModels(n int64)  { atomic.StoreInt64(&m.LoadedModels, n) }
func (m *Metrics) SetQueueDepth(n int64)    { atomic.StoreInt64(&m.QueueDepth, n) }

// Handler returns an HTTP handler that serves Prometheus-format metrics.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		uptime := time.Since(m.startTime).Seconds()
		fmt.Fprintf(w, "# HELP llamawrapper_uptime_seconds Gateway uptime in seconds\n")
		fmt.Fprintf(w, "llamawrapper_uptime_seconds %f\n\n", uptime)

		fmt.Fprintf(w, "# HELP llamawrapper_requests_total Total number of requests\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_requests_total counter\n")
		fmt.Fprintf(w, "llamawrapper_requests_total %d\n\n", atomic.LoadInt64(&m.RequestsTotal))

		fmt.Fprintf(w, "# HELP llamawrapper_errors_total Total number of errors\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_errors_total counter\n")
		fmt.Fprintf(w, "llamawrapper_errors_total %d\n\n", atomic.LoadInt64(&m.ErrorsTotal))

		fmt.Fprintf(w, "# HELP llamawrapper_active_requests Current in-flight requests\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_active_requests gauge\n")
		fmt.Fprintf(w, "llamawrapper_active_requests %d\n\n", atomic.LoadInt64(&m.ActiveRequests))

		fmt.Fprintf(w, "# HELP llamawrapper_loaded_models Number of loaded models\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_loaded_models gauge\n")
		fmt.Fprintf(w, "llamawrapper_loaded_models %d\n\n", atomic.LoadInt64(&m.LoadedModels))

		fmt.Fprintf(w, "# HELP llamawrapper_queue_depth Current request queue depth\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_queue_depth gauge\n")
		fmt.Fprintf(w, "llamawrapper_queue_depth %d\n\n", atomic.LoadInt64(&m.QueueDepth))

		fmt.Fprintf(w, "# HELP llamawrapper_tokens_generated_total Total tokens generated\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_tokens_generated_total counter\n")
		fmt.Fprintf(w, "llamawrapper_tokens_generated_total %d\n\n", atomic.LoadInt64(&m.TokensGenerated))

		fmt.Fprintf(w, "# HELP llamawrapper_cache_hits_total Cache hits\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_cache_hits_total counter\n")
		fmt.Fprintf(w, "llamawrapper_cache_hits_total %d\n\n", atomic.LoadInt64(&m.CacheHits))

		fmt.Fprintf(w, "# HELP llamawrapper_cache_misses_total Cache misses\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_cache_misses_total counter\n")
		fmt.Fprintf(w, "llamawrapper_cache_misses_total %d\n\n", atomic.LoadInt64(&m.CacheMisses))

		// Per-model request counts
		fmt.Fprintf(w, "# HELP llamawrapper_model_requests_total Requests per model\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_model_requests_total counter\n")
		m.mu.RLock()
		for model, counter := range m.RequestsByModel {
			fmt.Fprintf(w, "llamawrapper_model_requests_total{model=%q} %d\n", model, atomic.LoadInt64(counter))
		}
		m.mu.RUnlock()
		fmt.Fprintln(w)

		// Latency histogram
		fmt.Fprintf(w, "# HELP llamawrapper_request_duration_ms Request duration histogram\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_request_duration_ms histogram\n")
		cumulative := int64(0)
		for i, bound := range m.LatencyBuckets {
			cumulative += atomic.LoadInt64(&m.LatencyCounts[i])
			fmt.Fprintf(w, "llamawrapper_request_duration_ms_bucket{le=\"%.0f\"} %d\n", bound, cumulative)
		}
		cumulative += atomic.LoadInt64(&m.LatencyCounts[len(m.LatencyBuckets)])
		fmt.Fprintf(w, "llamawrapper_request_duration_ms_bucket{le=\"+Inf\"} %d\n", cumulative)
		fmt.Fprintln(w)

		// Per-model latency percentiles
		fmt.Fprintf(w, "# HELP llamawrapper_model_latency_p50_ms P50 latency per model\n")
		fmt.Fprintf(w, "# TYPE llamawrapper_model_latency_p50_ms gauge\n")
		m.mu.RLock()
		for model, lr := range m.modelLatencies {
			fmt.Fprintf(w, "llamawrapper_model_latency_p50_ms{model=%q} %.1f\n", model, lr.percentile(50))
			fmt.Fprintf(w, "llamawrapper_model_latency_p95_ms{model=%q} %.1f\n", model, lr.percentile(95))
			fmt.Fprintf(w, "llamawrapper_model_latency_avg_ms{model=%q} %.1f\n", model, lr.avg())
		}
		m.mu.RUnlock()
	}
}

// Snapshot returns a JSON-friendly summary for the dashboard.
type Snapshot struct {
	Uptime          float64            `json:"uptime_seconds"`
	RequestsTotal   int64              `json:"requests_total"`
	ErrorsTotal     int64              `json:"errors_total"`
	ActiveRequests  int64              `json:"active_requests"`
	LoadedModels    int64              `json:"loaded_models"`
	QueueDepth      int64              `json:"queue_depth"`
	TokensGenerated int64              `json:"tokens_generated"`
	CacheHits       int64              `json:"cache_hits"`
	CacheMisses     int64              `json:"cache_misses"`
	ModelStats      map[string]ModelStat `json:"model_stats"`
}

type ModelStat struct {
	Requests  int64   `json:"requests"`
	AvgMs     float64 `json:"avg_ms"`
	P50Ms     float64 `json:"p50_ms"`
	P95Ms     float64 `json:"p95_ms"`
}

func (m *Metrics) GetSnapshot() Snapshot {
	s := Snapshot{
		Uptime:          time.Since(m.startTime).Seconds(),
		RequestsTotal:   atomic.LoadInt64(&m.RequestsTotal),
		ErrorsTotal:     atomic.LoadInt64(&m.ErrorsTotal),
		ActiveRequests:  atomic.LoadInt64(&m.ActiveRequests),
		LoadedModels:    atomic.LoadInt64(&m.LoadedModels),
		QueueDepth:      atomic.LoadInt64(&m.QueueDepth),
		TokensGenerated: atomic.LoadInt64(&m.TokensGenerated),
		CacheHits:       atomic.LoadInt64(&m.CacheHits),
		CacheMisses:     atomic.LoadInt64(&m.CacheMisses),
		ModelStats:      make(map[string]ModelStat),
	}

	m.mu.RLock()
	for model, counter := range m.RequestsByModel {
		ms := ModelStat{Requests: atomic.LoadInt64(counter)}
		if lr, ok := m.modelLatencies[model]; ok {
			ms.AvgMs = lr.avg()
			ms.P50Ms = lr.percentile(50)
			ms.P95Ms = lr.percentile(95)
		}
		s.ModelStats[model] = ms
	}
	m.mu.RUnlock()

	return s
}

// --- Audit Log ---

// AddAuditEntry records an admin action.
func (m *Metrics) AddAuditEntry(action, actor, target, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditLog[m.auditIdx%len(m.auditLog)] = AuditEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Action:    action,
		Actor:     actor,
		Target:    target,
		Detail:    detail,
	}
	m.auditIdx++
	if m.auditCnt < len(m.auditLog) {
		m.auditCnt++
	}
}

// GetAuditLog returns the most recent audit entries.
func (m *Metrics) GetAuditLog(limit int) []AuditEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > m.auditCnt {
		limit = m.auditCnt
	}
	if limit == 0 {
		return nil
	}
	result := make([]AuditEntry, limit)
	for i := 0; i < limit; i++ {
		idx := (m.auditIdx - 1 - i)
		if idx < 0 {
			idx += len(m.auditLog)
		}
		result[i] = m.auditLog[idx%len(m.auditLog)]
	}
	return result
}

// --- Event Log ---

// AddEvent records a system event.
func (m *Metrics) AddEvent(level, source, model, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventLog[m.eventIdx%len(m.eventLog)] = EventEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Source:    source,
		Model:     model,
		Message:   message,
	}
	m.eventIdx++
	if m.eventCnt < len(m.eventLog) {
		m.eventCnt++
	}
}

// GetEventLog returns the most recent event entries.
func (m *Metrics) GetEventLog(limit int) []EventEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > m.eventCnt {
		limit = m.eventCnt
	}
	if limit == 0 {
		return nil
	}
	result := make([]EventEntry, limit)
	for i := 0; i < limit; i++ {
		idx := (m.eventIdx - 1 - i)
		if idx < 0 {
			idx += len(m.eventLog)
		}
		result[i] = m.eventLog[idx%len(m.eventLog)]
	}
	return result
}

// --- Health Check History ---

// AddHealthCheck records a health check result.
func (m *Metrics) AddHealthCheck(model string, port int, ok bool, latencyMs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthHistory[m.healthHistIdx%len(m.healthHistory)] = HealthCheckResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Model:     model,
		Port:      port,
		OK:        ok,
		LatencyMs: latencyMs,
	}
	m.healthHistIdx++
	if m.healthHistCnt < len(m.healthHistory) {
		m.healthHistCnt++
	}
	// SLA tracking
	atomic.AddInt64(&m.healthTotalChecks, 1)
	if ok {
		atomic.AddInt64(&m.healthPassChecks, 1)
	}
}

// GetHealthHistory returns recent health check results.
func (m *Metrics) GetHealthHistory(limit int) []HealthCheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > m.healthHistCnt {
		limit = m.healthHistCnt
	}
	if limit == 0 {
		return nil
	}
	result := make([]HealthCheckResult, limit)
	for i := 0; i < limit; i++ {
		idx := (m.healthHistIdx - 1 - i)
		if idx < 0 {
			idx += len(m.healthHistory)
		}
		result[i] = m.healthHistory[idx%len(m.healthHistory)]
	}
	return result
}

// --- Hourly Aggregation ---

// updateHourlyBucket updates the current hour's aggregation. Must be called with m.mu held.
func (m *Metrics) updateHourlyBucket(rec RequestRecord) {
	hourKey := time.Now().UTC().Format("2006-01-02T15")
	if hourKey != m.currentHourKey {
		// New hour — finalize previous bucket if exists
		if m.currentHourKey != "" && m.hourlyCnt > 0 {
			prevIdx := (m.hourlyIdx - 1)
			if prevIdx < 0 {
				prevIdx += len(m.hourlyBuckets)
			}
			b := &m.hourlyBuckets[prevIdx%len(m.hourlyBuckets)]
			if b.latencyCount > 0 {
				b.AvgLatencyMs = b.latencySum / float64(b.latencyCount)
			}
		}
		m.hourlyBuckets[m.hourlyIdx%len(m.hourlyBuckets)] = HourlyBucket{Hour: hourKey}
		m.hourlyIdx++
		if m.hourlyCnt < len(m.hourlyBuckets) {
			m.hourlyCnt++
		}
		m.currentHourKey = hourKey
	}

	idx := (m.hourlyIdx - 1)
	if idx < 0 {
		idx += len(m.hourlyBuckets)
	}
	b := &m.hourlyBuckets[idx%len(m.hourlyBuckets)]
	b.Requests++
	b.Tokens += int64(rec.PromptTokens + rec.CompletionTokens)
	if rec.IsError {
		b.Errors++
	}
	b.latencySum += rec.DurationMs
	b.latencyCount++
	b.AvgLatencyMs = b.latencySum / float64(b.latencyCount)
}

// GetHourlyAggregation returns hourly buckets in chronological order.
func (m *Metrics) GetHourlyAggregation() []HourlyBucket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.hourlyCnt == 0 {
		return nil
	}
	result := make([]HourlyBucket, m.hourlyCnt)
	start := 0
	if m.hourlyIdx > len(m.hourlyBuckets) {
		start = m.hourlyIdx % len(m.hourlyBuckets)
	}
	for i := 0; i < m.hourlyCnt; i++ {
		result[i] = m.hourlyBuckets[(start+i)%len(m.hourlyBuckets)]
	}
	return result
}

// --- SLA ---

// GetSLAStatus returns current SLA compliance status.
func (m *Metrics) GetSLAStatus() SLAStatus {
	// Compute current P95 across all models
	m.mu.RLock()
	var allP95s []float64
	for _, lr := range m.modelLatencies {
		if lr.count > 0 {
			allP95s = append(allP95s, lr.percentile(95))
		}
	}
	m.mu.RUnlock()

	currentP95 := 0.0
	if len(allP95s) > 0 {
		for _, p := range allP95s {
			if p > currentP95 {
				currentP95 = p
			}
		}
	}

	totalChecks := atomic.LoadInt64(&m.healthTotalChecks)
	passChecks := atomic.LoadInt64(&m.healthPassChecks)
	uptimePct := 100.0
	if totalChecks > 0 {
		uptimePct = float64(passChecks) / float64(totalChecks) * 100
	}

	totalReqs := atomic.LoadInt64(&m.RequestsTotal)
	totalErrs := atomic.LoadInt64(&m.ErrorsTotal)
	errPct := 0.0
	if totalReqs > 0 {
		errPct = float64(totalErrs) / float64(totalReqs) * 100
	}
	errorBudgetRemaining := m.SLATargetErrPct - errPct
	if errorBudgetRemaining < 0 {
		errorBudgetRemaining = 0
	}

	return SLAStatus{
		TargetP95Ms:    m.SLATargetP95Ms,
		CurrentP95Ms:   currentP95,
		Compliant:      currentP95 <= m.SLATargetP95Ms && errPct <= m.SLATargetErrPct,
		UptimePct:      uptimePct,
		ErrorBudgetPct: errorBudgetRemaining,
		TotalChecks:    totalChecks,
		PassedChecks:   passChecks,
	}
}

// --- SSE ---

// SubscribeSSE returns a channel that receives new request records in real-time.
func (m *Metrics) SubscribeSSE() chan RequestRecord {
	ch := make(chan RequestRecord, 32)
	m.sseMu.Lock()
	m.sseSubscribers[ch] = struct{}{}
	m.sseMu.Unlock()
	return ch
}

// UnsubscribeSSE removes a subscriber channel.
func (m *Metrics) UnsubscribeSSE(ch chan RequestRecord) {
	m.sseMu.Lock()
	delete(m.sseSubscribers, ch)
	m.sseMu.Unlock()
	close(ch)
}

// --- Comparison ---

// ModelComparison returns comparison data for all models.
type ModelComparison struct {
	Name         string  `json:"name"`
	Requests     int64   `json:"requests"`
	AvgMs        float64 `json:"avg_ms"`
	P50Ms        float64 `json:"p50_ms"`
	P95Ms        float64 `json:"p95_ms"`
	P99Ms        float64 `json:"p99_ms"`
	ErrorRate    float64 `json:"error_rate"`
	TokensTotal  int64   `json:"tokens_total"`
	AvgTokens    float64 `json:"avg_tokens"`
}

// GetModelComparisons returns comparison data for all models.
func (m *Metrics) GetModelComparisons() []ModelComparison {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ModelComparison
	for model, counter := range m.RequestsByModel {
		reqs := atomic.LoadInt64(counter)
		mc := ModelComparison{
			Name:     model,
			Requests: reqs,
		}
		if lr, ok := m.modelLatencies[model]; ok && lr.count > 0 {
			mc.AvgMs = lr.avg()
			mc.P50Ms = lr.percentile(50)
			mc.P95Ms = lr.percentile(95)
			mc.P99Ms = lr.percentile(99)
		}
		result = append(result, mc)
	}
	return result
}

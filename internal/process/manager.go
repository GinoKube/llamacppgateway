package process

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/llamawrapper/gateway/internal/config"
)

type BackendState int

const (
	StateStopped BackendState = iota
	StateStarting
	StateReady
	StateFailed
)

type Backend struct {
	Model       config.ModelConfig
	Port        int
	State       BackendState
	Process     *exec.Cmd
	LastUsed    time.Time
	cancel      context.CancelFunc
	ActiveReqs  int64 // atomic: number of in-flight requests
	instanceIdx int   // for load-balanced instances
}

func (b *Backend) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", b.Port)
}

func (b *Backend) IncrActiveReqs() { atomic.AddInt64(&b.ActiveReqs, 1) }
func (b *Backend) DecrActiveReqs() { atomic.AddInt64(&b.ActiveReqs, -1) }
func (b *Backend) GetActiveReqs() int64 { return atomic.LoadInt64(&b.ActiveReqs) }

// modelBackends holds one or more backends for a single model (load balancing).
type modelBackends struct {
	backends []*Backend
	rrIdx    uint64 // round-robin index (atomic)
}

// GPUInfo holds GPU memory information.
type GPUInfo struct {
	Index      int
	Name       string
	MemTotalMB int
	MemUsedMB  int
	MemFreeMB  int
}

// QueueEntry represents a queued request waiting for a model slot.
type QueueEntry struct {
	ModelName string
	Ready     chan *Backend
	Err       chan error
	ctx       context.Context
}

// ModelEvent records a lifecycle event for a model.
type ModelEvent struct {
	Timestamp string `json:"timestamp"`
	Model     string `json:"model"`
	Event     string `json:"event"` // loaded, unloaded, crashed, restarted, health_fail, health_ok
	Detail    string `json:"detail,omitempty"`
}

// DiskUsageInfo reports disk usage for model files.
type DiskUsageInfo struct {
	Model    string `json:"model"`
	Path     string `json:"path"`
	SizeMB   int64  `json:"size_mb"`
	Exists   bool   `json:"exists"`
}

// VRAMEstimate estimates VRAM usage for a model.
type VRAMEstimate struct {
	Model        string `json:"model"`
	FileSizeMB   int64  `json:"file_size_mb"`
	EstVRAMMB    int64  `json:"est_vram_mb"`
	GPULayers    int    `json:"gpu_layers"`
	ContextSize  int    `json:"context_size"`
	CanFit       bool   `json:"can_fit"`
	AvailVRAMMB  int    `json:"avail_vram_mb"`
}

// ScheduledAction is a timer-based action.
type ScheduledAction struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // unload_idle
	Model    string `json:"model"`
	AfterMin int    `json:"after_min"`
	Active   bool   `json:"active"`
	cancel   context.CancelFunc
}

type Manager struct {
	mu              sync.Mutex
	cfg             *config.Config
	backends        map[string]*modelBackends // model name -> backends
	nextPort        int
	maxLoaded       int
	llamaServerPath string

	// Request queue
	queue     []*QueueEntry
	queueMu   sync.Mutex
	queueCond *sync.Cond

	// GPU info cache
	gpuInfo     []GPUInfo
	gpuInfoTime time.Time

	// Model timeline events (ring buffer of last 200)
	modelEvents    [200]ModelEvent
	modelEventIdx  int
	modelEventCnt  int
	modelEventMu   sync.Mutex

	// Scheduled actions
	schedActions   []*ScheduledAction
	schedMu        sync.Mutex

	// Callbacks for metrics integration (avoids circular import)
	HealthCheckCallback func(model string, port int, ok bool, latencyMs float64)
	EventCallback       func(level, source, model, message string)
}

func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		cfg:             cfg,
		backends:        make(map[string]*modelBackends),
		nextPort:        cfg.PortRangeStart,
		maxLoaded:       cfg.MaxLoadedModels,
		llamaServerPath: cfg.LlamaServerPath,
	}
	m.queueCond = sync.NewCond(&m.queueMu)
	return m
}

// UpdateConfig replaces the running config (hot reload).
func (m *Manager) UpdateConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.maxLoaded = cfg.MaxLoadedModels
	m.llamaServerPath = cfg.LlamaServerPath
	log.Printf("[process] Config reloaded: %d models, max loaded: %d", len(cfg.Models), cfg.MaxLoadedModels)
}

// GetConfig returns the current config.
func (m *Manager) GetConfig() *config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// EnsureModel starts a model if not already running, performing LRU eviction if needed.
// Returns the backend once it's ready. Supports load-balanced round-robin if instances > 1.
func (m *Manager) EnsureModel(ctx context.Context, modelName string) (*Backend, error) {
	m.mu.Lock()

	// Check if already loaded — pick backend via round-robin
	if mb, ok := m.backends[modelName]; ok {
		for _, b := range mb.backends {
			if b.State == StateReady {
				b.LastUsed = time.Now()
				// Round-robin across ready instances
				idx := atomic.AddUint64(&mb.rrIdx, 1) - 1
				readyBacks := m.getReadyBackends(mb)
				if len(readyBacks) > 0 {
					chosen := readyBacks[idx%uint64(len(readyBacks))]
					chosen.LastUsed = time.Now()
					m.mu.Unlock()
					return chosen, nil
				}
			}
		}
		// Some may be starting
		for _, b := range mb.backends {
			if b.State == StateStarting {
				m.mu.Unlock()
				return m.waitForReady(ctx, b)
			}
		}
	}

	// Find model config
	var modelCfg *config.ModelConfig
	for i := range m.cfg.Models {
		if m.cfg.Models[i].Name == modelName {
			modelCfg = &m.cfg.Models[i]
			break
		}
	}
	if modelCfg == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("model %q not found in config", modelName)
	}

	// Auto-download if needed
	if modelCfg.ModelPath == "" && modelCfg.AutoDownload != nil {
		m.mu.Unlock()
		if err := m.autoDownloadModel(modelCfg); err != nil {
			return nil, fmt.Errorf("auto-download failed for %q: %w", modelName, err)
		}
		m.mu.Lock()
	}

	// Evict if at capacity
	if err := m.evictIfNeeded(); err != nil {
		m.mu.Unlock()

		// If queue is enabled, wait in queue
		if m.cfg.Queue.Enabled {
			return m.enqueue(ctx, modelName)
		}
		return nil, fmt.Errorf("eviction failed: %w", err)
	}

	// Start instance(s)
	mb := &modelBackends{}
	instances := modelCfg.Instances
	if instances < 1 {
		instances = 1
	}

	for i := 0; i < instances; i++ {
		port := m.nextPort
		m.nextPort++

		b := &Backend{
			Model:       *modelCfg,
			Port:        port,
			State:       StateStarting,
			LastUsed:    time.Now(),
			instanceIdx: i,
		}
		mb.backends = append(mb.backends, b)
	}
	m.backends[modelName] = mb
	m.mu.Unlock()

	// Start all instances
	for _, b := range mb.backends {
		if err := m.startBackend(b); err != nil {
			m.mu.Lock()
			b.State = StateFailed
			m.mu.Unlock()
			return nil, fmt.Errorf("starting backend for %q instance %d: %w", modelName, b.instanceIdx, err)
		}
	}

	return m.waitForReady(ctx, mb.backends[0])
}

func (m *Manager) getReadyBackends(mb *modelBackends) []*Backend {
	var ready []*Backend
	for _, b := range mb.backends {
		if b.State == StateReady {
			ready = append(ready, b)
		}
	}
	return ready
}

func (m *Manager) startBackend(b *Backend) error {
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	args := []string{
		"--model", b.Model.ModelPath,
		"--port", strconv.Itoa(b.Port),
		"--host", "127.0.0.1",
		"--ctx-size", strconv.Itoa(b.Model.ContextSize * 8), // multiply by --parallel so each slot gets full context
		"--threads", strconv.Itoa(b.Model.Threads),
		"--batch-size", strconv.Itoa(b.Model.BatchSize),
		"--cont-batching",
		"--parallel", "8",
		"--cache-reuse", "256",
	}

	if b.Model.GPULayers != 0 {
		args = append(args, "--n-gpu-layers", strconv.Itoa(b.Model.GPULayers))
	}

	args = append(args, b.Model.ExtraArgs...)

	cmd := exec.CommandContext(ctx, m.llamaServerPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set library path so llama-server can find its shared libraries (Linux + macOS)
	serverDir := filepath.Dir(m.llamaServerPath)
	env := os.Environ()
	env = append(env,
		"LD_LIBRARY_PATH="+serverDir+":"+os.Getenv("LD_LIBRARY_PATH"),
		"DYLD_LIBRARY_PATH="+serverDir+":"+os.Getenv("DYLD_LIBRARY_PATH"),
	)

	// Multi-GPU: set CUDA_VISIBLE_DEVICES per model
	if b.Model.GPUDevices != "" {
		env = append(env, "CUDA_VISIBLE_DEVICES="+b.Model.GPUDevices)
	}

	cmd.Env = env

	log.Printf("[process] Starting %s (instance %d) on port %d: %s %v",
		b.Model.Name, b.instanceIdx, b.Port, m.llamaServerPath, args)

	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}

	b.Process = cmd

	// Monitor process in background — auto-restart on crash
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if err != nil && ctx.Err() == nil {
			log.Printf("[process] %s (instance %d) crashed: %v — will auto-restart",
				b.Model.Name, b.instanceIdx, err)
			b.State = StateFailed
			b.Process = nil
			m.mu.Unlock()
			m.addModelEvent(b.Model.Name, "crashed", fmt.Sprintf("instance %d: %v", b.instanceIdx, err))

			// Auto-restart after a brief delay
			time.Sleep(2 * time.Second)
			m.mu.Lock()
			if b.State == StateFailed {
				b.State = StateStarting
				m.mu.Unlock()
				m.addModelEvent(b.Model.Name, "restarting", fmt.Sprintf("instance %d", b.instanceIdx))
				if restartErr := m.startBackend(b); restartErr != nil {
					log.Printf("[process] Auto-restart failed for %s: %v", b.Model.Name, restartErr)
					m.addModelEvent(b.Model.Name, "restart_failed", restartErr.Error())
					m.mu.Lock()
					b.State = StateFailed
					m.mu.Unlock()
				}
			} else {
				m.mu.Unlock()
			}
		} else {
			log.Printf("[process] %s (instance %d) stopped", b.Model.Name, b.instanceIdx)
			b.State = StateStopped
			m.mu.Unlock()
		}
	}()

	return nil
}

func (m *Manager) waitForReady(ctx context.Context, b *Backend) (*Backend, error) {
	healthURL := fmt.Sprintf("%s/health", b.URL())
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(120 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for %s to become ready", b.Model.Name)
		case <-ticker.C:
			if b.State == StateFailed {
				return nil, fmt.Errorf("backend %s failed to start", b.Model.Name)
			}

			resp, err := http.Get(healthURL)
			if err != nil {
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				m.mu.Lock()
				b.State = StateReady
				m.mu.Unlock()
				log.Printf("[process] %s (instance %d) is ready on port %d",
					b.Model.Name, b.instanceIdx, b.Port)
				m.addModelEvent(b.Model.Name, "loaded", fmt.Sprintf("instance %d on port %d", b.instanceIdx, b.Port))

				// Drain queue for this model
				m.drainQueue(b.Model.Name)

				return b, nil
			}
		}
	}
}

// --- Request Queue ---

func (m *Manager) enqueue(ctx context.Context, modelName string) (*Backend, error) {
	if !m.cfg.Queue.Enabled {
		return nil, fmt.Errorf("all model slots full and queuing is disabled")
	}

	m.queueMu.Lock()
	if len(m.queue) >= m.cfg.Queue.MaxSize {
		m.queueMu.Unlock()
		return nil, fmt.Errorf("request queue is full (%d/%d)", len(m.queue), m.cfg.Queue.MaxSize)
	}

	entry := &QueueEntry{
		ModelName: modelName,
		Ready:     make(chan *Backend, 1),
		Err:       make(chan error, 1),
		ctx:       ctx,
	}
	m.queue = append(m.queue, entry)
	pos := len(m.queue)
	m.queueMu.Unlock()

	log.Printf("[queue] Request for %s queued at position %d", modelName, pos)

	timeout := time.Duration(m.cfg.Queue.TimeoutSec) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case b := <-entry.Ready:
		return b, nil
	case err := <-entry.Err:
		return nil, err
	case <-ctx.Done():
		m.removeFromQueue(entry)
		return nil, ctx.Err()
	case <-timer.C:
		m.removeFromQueue(entry)
		return nil, fmt.Errorf("queue timeout after %v", timeout)
	}
}

func (m *Manager) removeFromQueue(entry *QueueEntry) {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	for i, e := range m.queue {
		if e == entry {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *Manager) drainQueue(modelName string) {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	var remaining []*QueueEntry
	for _, entry := range m.queue {
		if entry.ModelName == modelName {
			m.mu.Lock()
			if mb, ok := m.backends[modelName]; ok {
				ready := m.getReadyBackends(mb)
				if len(ready) > 0 {
					chosen := ready[0]
					chosen.LastUsed = time.Now()
					m.mu.Unlock()
					entry.Ready <- chosen
					continue
				}
			}
			m.mu.Unlock()
		}
		remaining = append(remaining, entry)
	}
	m.queue = remaining
}

// GetQueueLength returns the current queue depth.
func (m *Manager) GetQueueLength() int {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	return len(m.queue)
}

// --- Eviction ---

// evictIfNeeded removes the least recently used model if at capacity.
// Must be called with m.mu held.
func (m *Manager) evictIfNeeded() error {
	loaded := 0
	for _, mb := range m.backends {
		for _, b := range mb.backends {
			if b.State == StateReady || b.State == StateStarting {
				loaded++
			}
		}
	}

	if loaded < m.maxLoaded {
		return nil
	}

	// Find LRU model with no active requests (graceful drain)
	var lruName string
	var lruTime time.Time
	for name, mb := range m.backends {
		for _, b := range mb.backends {
			if b.State != StateReady {
				continue
			}
			if b.GetActiveReqs() > 0 {
				continue // graceful drain: don't evict models with in-flight requests
			}
			if lruName == "" || b.LastUsed.Before(lruTime) {
				lruName = name
				lruTime = b.LastUsed
			}
		}
	}

	if lruName == "" {
		return fmt.Errorf("no evictable models found (all busy or starting)")
	}

	log.Printf("[process] Evicting LRU model %s (last used: %s)", lruName, lruTime.Format(time.RFC3339))
	return m.stopModel(lruName)
}

func (m *Manager) stopModel(name string) error {
	mb, ok := m.backends[name]
	if !ok {
		return nil
	}

	for _, b := range mb.backends {
		if b.cancel != nil {
			b.cancel()
		}
		b.State = StateStopped
	}

	delete(m.backends, name)
	log.Printf("[process] Stopped all instances of %s", name)
	m.addModelEvent(name, "unloaded", "all instances stopped")
	return nil
}

// StopBackendByName stops a specific model (for admin API).
func (m *Manager) StopBackendByName(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopModel(name)
}

// UnloadModel stops a model by name (alias for StopBackendByName).
func (m *Manager) UnloadModel(name string) { m.StopBackendByName(name) }

// ForceLoadModel loads a model immediately (for admin API).
func (m *Manager) ForceLoadModel(ctx context.Context, modelName string) (*Backend, error) {
	return m.EnsureModel(ctx, modelName)
}

// --- Listing ---

// ListLoaded returns the names of currently loaded models.
func (m *Manager) ListLoaded() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var names []string
	for name, mb := range m.backends {
		for _, b := range mb.backends {
			if b.State == StateReady {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

// BackendStatus holds status info for a single backend.
type BackendStatus struct {
	ModelName   string `json:"model_name"`
	Port        int    `json:"port"`
	State       string `json:"state"`
	Instance    int    `json:"instance"`
	ActiveReqs  int64  `json:"active_requests"`
	LastUsed    string `json:"last_used"`
}

// ListBackendStatus returns detailed status of all backends.
func (m *Manager) ListBackendStatus() []BackendStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []BackendStatus
	for name, mb := range m.backends {
		for _, b := range mb.backends {
			stateStr := "stopped"
			switch b.State {
			case StateStarting:
				stateStr = "starting"
			case StateReady:
				stateStr = "ready"
			case StateFailed:
				stateStr = "failed"
			}
			statuses = append(statuses, BackendStatus{
				ModelName:  name,
				Port:       b.Port,
				State:      stateStr,
				Instance:   b.instanceIdx,
				ActiveReqs: b.GetActiveReqs(),
				LastUsed:   b.LastUsed.Format(time.RFC3339),
			})
		}
	}
	return statuses
}

// GetBackend returns the backend for a model if loaded and ready.
func (m *Manager) GetBackend(modelName string) (*Backend, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mb, ok := m.backends[modelName]
	if !ok {
		return nil, false
	}

	ready := m.getReadyBackends(mb)
	if len(ready) == 0 {
		return nil, false
	}

	// Round-robin
	idx := atomic.AddUint64(&mb.rrIdx, 1) - 1
	chosen := ready[idx%uint64(len(ready))]
	chosen.LastUsed = time.Now()
	return chosen, true
}

// ListConfiguredModels returns all model names from config.
func (m *Manager) ListConfiguredModels() []config.ModelConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Models
}

// --- Shutdown ---

// Shutdown stops all backends gracefully — waits for in-flight requests.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, mb := range m.backends {
		// Wait up to 30s for in-flight requests to drain
		deadline := time.Now().Add(30 * time.Second)
		for _, b := range mb.backends {
			for b.GetActiveReqs() > 0 && time.Now().Before(deadline) {
				m.mu.Unlock()
				log.Printf("[process] Waiting for %d in-flight requests on %s...", b.GetActiveReqs(), name)
				time.Sleep(1 * time.Second)
				m.mu.Lock()
			}
		}
		m.stopModel(name)
	}
	log.Printf("[process] All backends stopped")
}

// --- Health Check ---

// HealthCheck runs periodic health checks on loaded models.
func (m *Manager) HealthCheck(ctx context.Context, intervalSec int) {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			for name, mb := range m.backends {
				for _, b := range mb.backends {
					if b.State != StateReady {
						continue
					}

					healthURL := fmt.Sprintf("%s/health", b.URL())
					start := time.Now()
					resp, err := http.Get(healthURL)
					latMs := float64(time.Since(start).Milliseconds())
					ok := err == nil && resp != nil && resp.StatusCode == http.StatusOK
					if !ok {
						log.Printf("[health] %s (instance %d) failed health check, marking as failed",
							name, b.instanceIdx)
						b.State = StateFailed
						m.addModelEvent(name, "health_fail", fmt.Sprintf("instance %d", b.instanceIdx))
						if m.EventCallback != nil {
							m.EventCallback("error", "health", name, fmt.Sprintf("health check failed on port %d", b.Port))
						}
					}
					if resp != nil {
						resp.Body.Close()
					}
					if m.HealthCheckCallback != nil {
						m.HealthCheckCallback(name, b.Port, ok, latMs)
					}
				}
			}
			m.mu.Unlock()
		}
	}
}

// --- GPU Memory Tracking ---

// GetGPUInfo returns current GPU memory usage.
func (m *Manager) GetGPUInfo() []GPUInfo {
	// Cache for 5 seconds
	if time.Since(m.gpuInfoTime) < 5*time.Second && m.gpuInfo != nil {
		return m.gpuInfo
	}

	info := queryGPUInfo()
	m.gpuInfo = info
	m.gpuInfoTime = time.Now()
	return info
}

func queryGPUInfo() []GPUInfo {
	// Try nvidia-smi first
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,name,memory.total,memory.used,memory.free",
		"--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Not NVIDIA or nvidia-smi not available — try macOS
		return queryMacGPUInfo()
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		parts := strings.Split(line, ", ")
		if len(parts) < 5 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		total, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		used, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
		free, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
		gpus = append(gpus, GPUInfo{
			Index:      idx,
			Name:       strings.TrimSpace(parts[1]),
			MemTotalMB: total,
			MemUsedMB:  used,
			MemFreeMB:  free,
		})
	}
	return gpus
}

func queryMacGPUInfo() []GPUInfo {
	// On macOS, get unified memory info via sysctl
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	totalBytes, _ := strconv.ParseInt(strings.TrimSpace(out.String()), 10, 64)
	if totalBytes == 0 {
		return nil
	}
	totalMB := int(totalBytes / (1024 * 1024))
	return []GPUInfo{{
		Index:      0,
		Name:       "Apple Silicon (Unified Memory)",
		MemTotalMB: totalMB,
		MemFreeMB:  totalMB, // can't easily determine used without vm_stat parsing
	}}
}

// --- Model Events ---

func (m *Manager) addModelEvent(model, event, detail string) {
	m.modelEventMu.Lock()
	defer m.modelEventMu.Unlock()
	m.modelEvents[m.modelEventIdx%len(m.modelEvents)] = ModelEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Model:     model,
		Event:     event,
		Detail:    detail,
	}
	m.modelEventIdx++
	if m.modelEventCnt < len(m.modelEvents) {
		m.modelEventCnt++
	}
}

// GetModelEvents returns recent model lifecycle events.
func (m *Manager) GetModelEvents(limit int) []ModelEvent {
	m.modelEventMu.Lock()
	defer m.modelEventMu.Unlock()
	if limit <= 0 || limit > m.modelEventCnt {
		limit = m.modelEventCnt
	}
	if limit == 0 {
		return nil
	}
	result := make([]ModelEvent, limit)
	for i := 0; i < limit; i++ {
		idx := (m.modelEventIdx - 1 - i)
		if idx < 0 {
			idx += len(m.modelEvents)
		}
		result[i] = m.modelEvents[idx%len(m.modelEvents)]
	}
	return result
}

// --- VRAM Estimation ---

// EstimateVRAM estimates VRAM usage for a model based on file size, GPU layers, and context.
func (m *Manager) EstimateVRAM(modelName string) *VRAMEstimate {
	m.mu.Lock()
	var modelCfg *config.ModelConfig
	for i := range m.cfg.Models {
		if m.cfg.Models[i].Name == modelName {
			modelCfg = &m.cfg.Models[i]
			break
		}
	}
	m.mu.Unlock()
	if modelCfg == nil {
		return nil
	}

	est := &VRAMEstimate{
		Model:       modelName,
		GPULayers:   modelCfg.GPULayers,
		ContextSize: modelCfg.ContextSize,
	}

	// Get file size
	if info, err := os.Stat(modelCfg.ModelPath); err == nil {
		est.FileSizeMB = info.Size() / (1024 * 1024)
	}

	// Estimate: model weights + KV cache + overhead
	// Rough formula: VRAM ~ file_size * (gpu_layers_pct) + context_size * 2MB/1024ctx + 500MB overhead
	gpuLayersPct := 1.0
	if modelCfg.GPULayers > 0 && modelCfg.GPULayers < 100 {
		gpuLayersPct = float64(modelCfg.GPULayers) / 40.0 // assume ~40 layers typical
		if gpuLayersPct > 1.0 {
			gpuLayersPct = 1.0
		}
	}
	if modelCfg.GPULayers == 0 {
		gpuLayersPct = 0.0
	}

	modelVRAM := float64(est.FileSizeMB) * gpuLayersPct
	kvCache := float64(modelCfg.ContextSize) * 2.0 / 1024.0 // ~2MB per 1K context
	overhead := 500.0                                         // CUDA/driver overhead
	est.EstVRAMMB = int64(modelVRAM + kvCache + overhead)

	// Check available VRAM
	gpus := m.GetGPUInfo()
	if len(gpus) > 0 {
		totalFree := 0
		for _, g := range gpus {
			totalFree += g.MemFreeMB
		}
		est.AvailVRAMMB = totalFree
		est.CanFit = int64(totalFree) >= est.EstVRAMMB
	}

	return est
}

// --- Disk Usage ---

// GetDiskUsage returns disk usage info for all configured model files.
func (m *Manager) GetDiskUsage() []DiskUsageInfo {
	m.mu.Lock()
	models := m.cfg.Models
	m.mu.Unlock()

	var result []DiskUsageInfo
	for _, mc := range models {
		du := DiskUsageInfo{
			Model: mc.Name,
			Path:  mc.ModelPath,
		}
		if info, err := os.Stat(mc.ModelPath); err == nil {
			du.SizeMB = info.Size() / (1024 * 1024)
			du.Exists = true
		}
		result = append(result, du)
	}

	// Also get total disk space for the model directory
	return result
}

// --- Warmup ---

// WarmupModel sends a small request to a loaded model to pre-fill KV cache.
func (m *Manager) WarmupModel(ctx context.Context, modelName string) error {
	m.mu.Lock()
	mb, ok := m.backends[modelName]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("model %q not loaded", modelName)
	}
	var target *Backend
	for _, b := range mb.backends {
		if b.State == StateReady {
			target = b
			break
		}
	}
	m.mu.Unlock()
	if target == nil {
		return fmt.Errorf("no ready backend for %q", modelName)
	}

	// Send a small completion request
	url := fmt.Sprintf("%s/v1/chat/completions", target.URL())
	body := strings.NewReader(`{"model":"` + modelName + `","messages":[{"role":"user","content":"Hello"}],"max_tokens":1}`)
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("warmup request failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("warmup returned status %d", resp.StatusCode)
	}

	m.addModelEvent(modelName, "warmup", "KV cache pre-filled")
	return nil
}

// --- Scheduled Actions ---

// AddScheduledAction creates a new scheduled action (e.g., unload after idle).
func (m *Manager) AddScheduledAction(actionType, model string, afterMin int) string {
	id := fmt.Sprintf("sched_%d", time.Now().UnixNano()%100000)
	ctx, cancel := context.WithCancel(context.Background())

	action := &ScheduledAction{
		ID:       id,
		Type:     actionType,
		Model:    model,
		AfterMin: afterMin,
		Active:   true,
		cancel:   cancel,
	}

	m.schedMu.Lock()
	m.schedActions = append(m.schedActions, action)
	m.schedMu.Unlock()

	go m.runScheduledAction(ctx, action)
	return id
}

func (m *Manager) runScheduledAction(ctx context.Context, action *ScheduledAction) {
	ticker := time.NewTicker(time.Duration(action.AfterMin) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			switch action.Type {
			case "unload_idle":
				m.mu.Lock()
				mb, ok := m.backends[action.Model]
				if ok {
					allIdle := true
					for _, b := range mb.backends {
						if b.GetActiveReqs() > 0 || time.Since(b.LastUsed) < time.Duration(action.AfterMin)*time.Minute {
							allIdle = false
							break
						}
					}
					if allIdle {
						m.mu.Unlock()
						m.UnloadModel(action.Model)
						m.addModelEvent(action.Model, "scheduled_unload", fmt.Sprintf("idle for %d min", action.AfterMin))
						log.Printf("[schedule] Unloaded %s (idle for %d min)", action.Model, action.AfterMin)
						continue
					}
				}
				m.mu.Unlock()
			}
		}
	}
}

// RemoveScheduledAction cancels and removes a scheduled action.
func (m *Manager) RemoveScheduledAction(id string) bool {
	m.schedMu.Lock()
	defer m.schedMu.Unlock()
	for i, a := range m.schedActions {
		if a.ID == id {
			a.cancel()
			a.Active = false
			m.schedActions = append(m.schedActions[:i], m.schedActions[i+1:]...)
			return true
		}
	}
	return false
}

// GetScheduledActions returns active scheduled actions.
func (m *Manager) GetScheduledActions() []*ScheduledAction {
	m.schedMu.Lock()
	defer m.schedMu.Unlock()
	result := make([]*ScheduledAction, len(m.schedActions))
	copy(result, m.schedActions)
	return result
}

// --- Auto Download ---

func (m *Manager) autoDownloadModel(modelCfg *config.ModelConfig) error {
	ad := modelCfg.AutoDownload
	if ad == nil {
		return fmt.Errorf("no auto_download config")
	}

	localDir := ad.LocalDir
	if localDir == "" {
		localDir = filepath.Join(os.Getenv("HOME"), "models")
	}

	destPath := filepath.Join(localDir, ad.File)

	// Check if already downloaded
	if _, err := os.Stat(destPath); err == nil {
		log.Printf("[download] %s already exists at %s", ad.File, destPath)
		modelCfg.ModelPath = destPath
		return nil
	}

	// Create directory
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("creating dir %s: %w", localDir, err)
	}

	log.Printf("[download] Downloading %s/%s to %s...", ad.Repo, ad.File, destPath)

	// Use huggingface-cli if available, otherwise curl
	cmd := exec.Command("huggingface-cli", "download", ad.Repo, ad.File, "--local-dir", localDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Fallback to direct URL
		url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", ad.Repo, ad.File)
		log.Printf("[download] huggingface-cli failed, trying curl: %s", url)
		cmd = exec.Command("curl", "-L", "-o", destPath, url)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	}

	modelCfg.ModelPath = destPath
	log.Printf("[download] Downloaded %s successfully", ad.File)
	return nil
}

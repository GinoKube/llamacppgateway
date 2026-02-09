package process

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

const maxAutoRestarts = 5

type Backend struct {
	Model        config.ModelConfig
	Port         int
	State        BackendState
	Process      *exec.Cmd
	LastUsed     time.Time
	cancel       context.CancelFunc
	ActiveReqs   int64 // atomic: number of in-flight requests
	instanceIdx  int
	restartCount int
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

// QueueEntry represents a queued request waiting for a model slot.
type QueueEntry struct {
	ModelName string
	Ready     chan *Backend
	Err       chan error
	ctx       context.Context
}

type Manager struct {
	mu              sync.Mutex
	cfg             *config.Config
	backends        map[string]*modelBackends
	nextPort        int
	freedPorts      []int
	maxLoaded       int
	llamaServerPath string

	// Request queue
	queue     []*QueueEntry
	queueMu   sync.Mutex
	queueCond *sync.Cond
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

// allocPort returns a recycled port or the next sequential port. Must be called with m.mu held.
func (m *Manager) allocPort() int {
	if len(m.freedPorts) > 0 {
		port := m.freedPorts[len(m.freedPorts)-1]
		m.freedPorts = m.freedPorts[:len(m.freedPorts)-1]
		return port
	}
	port := m.nextPort
	m.nextPort++
	return port
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
func (m *Manager) EnsureModel(ctx context.Context, modelName string) (*Backend, error) {
	m.mu.Lock()

	// Check if already loaded — pick backend via round-robin
	if mb, ok := m.backends[modelName]; ok {
		for _, b := range mb.backends {
			if b.State == StateReady {
				b.LastUsed = time.Now()
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
		return m.enqueue(ctx, modelName)
	}

	// Start instance(s)
	mb := &modelBackends{}
	instances := modelCfg.Instances
	if instances < 1 {
		instances = 1
	}

	for i := 0; i < instances; i++ {
		port := m.allocPort()
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
		"--ctx-size", strconv.Itoa(b.Model.ContextSize * 8),
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

	serverDir := filepath.Dir(m.llamaServerPath)
	env := os.Environ()
	env = append(env,
		"LD_LIBRARY_PATH="+serverDir+":"+os.Getenv("LD_LIBRARY_PATH"),
		"DYLD_LIBRARY_PATH="+serverDir+":"+os.Getenv("DYLD_LIBRARY_PATH"),
	)

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
			b.restartCount++
			restartCount := b.restartCount
			log.Printf("[process] %s (instance %d) crashed: %v (restart %d/%d)",
				b.Model.Name, b.instanceIdx, err, restartCount, maxAutoRestarts)
			b.State = StateFailed
			b.Process = nil
			m.mu.Unlock()

			if restartCount > maxAutoRestarts {
				log.Printf("[process] %s (instance %d) exceeded max auto-restarts (%d), giving up",
					b.Model.Name, b.instanceIdx, maxAutoRestarts)
				return
			}

			time.Sleep(2 * time.Second)
			m.mu.Lock()
			if b.State == StateFailed {
				b.State = StateStarting
				m.mu.Unlock()
				if restartErr := m.startBackend(b); restartErr != nil {
					log.Printf("[process] Auto-restart failed for %s: %v", b.Model.Name, restartErr)
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
				b.restartCount = 0
				m.mu.Unlock()
				log.Printf("[process] %s (instance %d) is ready on port %d",
					b.Model.Name, b.instanceIdx, b.Port)

				m.drainQueue(b.Model.Name)

				return b, nil
			}
		}
	}
}

// --- Request Queue ---

func (m *Manager) enqueue(ctx context.Context, modelName string) (*Backend, error) {
	m.queueMu.Lock()
	if len(m.queue) >= 100 {
		m.queueMu.Unlock()
		return nil, fmt.Errorf("request queue is full (%d/100)", len(m.queue))
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

	timeout := 300 * time.Second
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

	var lruName string
	var lruTime time.Time
	for name, mb := range m.backends {
		for _, b := range mb.backends {
			if b.State != StateReady {
				continue
			}
			if b.GetActiveReqs() > 0 {
				continue
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
		m.freedPorts = append(m.freedPorts, b.Port)
	}

	delete(m.backends, name)
	log.Printf("[process] Stopped all instances of %s", name)
	return nil
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

// ListConfiguredModels returns all model configs.
func (m *Manager) ListConfiguredModels() []config.ModelConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Models
}

// --- Shutdown ---

func (m *Manager) Shutdown() {
	m.mu.Lock()
	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}

	for _, name := range names {
		mb, ok := m.backends[name]
		if !ok {
			continue
		}
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
	m.mu.Unlock()
	log.Printf("[process] All backends stopped")
}

// --- Health Check ---

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
			type healthTarget struct {
				name        string
				instanceIdx int
				port        int
				healthURL   string
				backend     *Backend
			}
			var targets []healthTarget

			m.mu.Lock()
			for name, mb := range m.backends {
				for _, b := range mb.backends {
					if b.State != StateReady {
						continue
					}
					targets = append(targets, healthTarget{
						name:        name,
						instanceIdx: b.instanceIdx,
						port:        b.Port,
						healthURL:   fmt.Sprintf("%s/health", b.URL()),
						backend:     b,
					})
				}
			}
			m.mu.Unlock()

			for _, t := range targets {
				resp, err := http.Get(t.healthURL)
				ok := err == nil && resp != nil && resp.StatusCode == http.StatusOK
				if resp != nil {
					resp.Body.Close()
				}
				if !ok {
					log.Printf("[health] %s (instance %d) failed health check, marking as failed",
						t.name, t.instanceIdx)
					m.mu.Lock()
					t.backend.State = StateFailed
					m.mu.Unlock()
				}
			}
		}
	}
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

	if _, err := os.Stat(destPath); err == nil {
		log.Printf("[download] %s already exists at %s", ad.File, destPath)
		modelCfg.ModelPath = destPath
		return nil
	}

	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("creating dir %s: %w", localDir, err)
	}

	log.Printf("[download] Downloading %s/%s to %s...", ad.Repo, ad.File, destPath)

	cmd := exec.Command("huggingface-cli", "download", ad.Repo, ad.File, "--local-dir", localDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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

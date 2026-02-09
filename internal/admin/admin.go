package admin

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/llamawrapper/gateway/internal/metrics"
	"github.com/llamawrapper/gateway/internal/process"
)

// Handler provides admin API endpoints.
type Handler struct {
	manager    *process.Manager
	metrics    *metrics.Metrics
	reloadFunc func() error // callback to reload config
}

func NewHandler(manager *process.Manager, m *metrics.Metrics, reloadFunc func() error) *Handler {
	return &Handler{
		manager:    manager,
		metrics:    m,
		reloadFunc: reloadFunc,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/status", h.handleStatus)
	mux.HandleFunc("/admin/load", h.handleLoad)
	mux.HandleFunc("/admin/unload", h.handleUnload)
	mux.HandleFunc("/admin/reload", h.handleReload)
	mux.HandleFunc("/admin/gpu", h.handleGPU)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backends := h.manager.ListBackendStatus()
	configured := h.manager.ListConfiguredModels()
	gpuInfo := h.manager.GetGPUInfo()
	queueLen := h.manager.GetQueueLength()

	var metricsSnap *metrics.Snapshot
	if h.metrics != nil {
		s := h.metrics.GetSnapshot()
		metricsSnap = &s
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"backends":         backends,
		"configured_models": len(configured),
		"gpu_info":         gpuInfo,
		"queue_depth":      queueLen,
		"metrics":          metricsSnap,
	})
}

type loadRequest struct {
	Model string `json:"model"`
}

func (h *Handler) handleLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model field is required"})
		return
	}

	// Resolve alias
	cfg := h.manager.GetConfig()
	modelName := cfg.ResolveAlias(req.Model)
	if modelName == "" {
		modelName = req.Model
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	log.Printf("[admin] Loading model %s", modelName)
	backend, err := h.manager.ForceLoadModel(ctx, modelName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "loaded",
		"model":  modelName,
		"port":   backend.Port,
	})
}

func (h *Handler) handleUnload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model field is required"})
		return
	}

	cfg := h.manager.GetConfig()
	modelName := cfg.ResolveAlias(req.Model)
	if modelName == "" {
		modelName = req.Model
	}

	log.Printf("[admin] Unloading model %s", modelName)
	if err := h.manager.StopBackendByName(modelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unloaded", "model": modelName})
}

func (h *Handler) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.reloadFunc == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reload not configured"})
		return
	}

	log.Printf("[admin] Reloading configuration")
	if err := h.reloadFunc(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (h *Handler) handleGPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gpuInfo := h.manager.GetGPUInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"gpus": gpuInfo,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

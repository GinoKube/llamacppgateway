package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/llamawrapper/gateway/internal/config"
	"github.com/llamawrapper/gateway/internal/process"
)

type Handler struct {
	manager *process.Manager
}

func NewHandler(manager *process.Manager) *Handler {
	return &Handler{manager: manager}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("/v1/completions", h.handleCompletions)
	mux.HandleFunc("/v1/embeddings", h.handleEmbeddings)
	mux.HandleFunc("/v1/models", h.handleModels)
	mux.HandleFunc("/health", h.handleHealth)
}

type modelRequest struct {
	Model string `json:"model"`
}

type openaiModelsResponse struct {
	Object string            `json:"object"`
	Data   []openaiModelItem `json:"data"`
}

type openaiModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	models := h.manager.ListConfiguredModels()
	var data []openaiModelItem

	for _, m := range models {
		data = append(data, openaiModelItem{
			ID:      m.Name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "llamawrapper",
		})
		for _, alias := range m.Aliases {
			data = append(data, openaiModelItem{
				ID:      alias,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "llamawrapper",
			})
		}
	}

	resp := openaiModelsResponse{Object: "list", Data: data}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	loaded := h.manager.ListLoaded()
	queueLen := h.manager.GetQueueLength()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"loaded_models": loaded,
		"queue_depth":   queueLen,
	})
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyToModel(w, r, "/v1/chat/completions")
}

func (h *Handler) handleCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyToModel(w, r, "/v1/completions")
}

func (h *Handler) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	h.proxyToModel(w, r, "/v1/embeddings")
}

func (h *Handler) proxyToModel(w http.ResponseWriter, r *http.Request, endpoint string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	const maxBodySize = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	r.Body.Close()
	if len(body) >= maxBodySize {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	var req modelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON in request body")
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model field is required")
		return
	}

	cfg := h.manager.GetConfig()
	modelName := cfg.ResolveAlias(req.Model)
	if modelName == "" {
		modelName = resolveModelName(req.Model, h.manager.ListConfiguredModels())
	}
	if modelName == "" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("model %q not found", req.Model))
		return
	}

	log.Printf("[api] Request for model %q -> %s", modelName, endpoint)

	// Find model config for per-model timeout
	var timeoutSec int
	for _, m := range cfg.Models {
		if m.Name == modelName {
			timeoutSec = m.TimeoutSec
			break
		}
	}

	// Ensure model is loaded (lazy loading)
	loadTimeout := 180 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), loadTimeout)
	defer cancel()

	backend, err := h.manager.EnsureModel(ctx, modelName)
	if err != nil {
		log.Printf("[api] Failed to ensure model %q: %v", modelName, err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("failed to load model: %v", err))
		return
	}

	backend.IncrActiveReqs()
	defer backend.DecrActiveReqs()

	// Check if streaming is requested
	isStream := false
	var bodyMap map[string]interface{}
	json.Unmarshal(body, &bodyMap)
	if s, ok := bodyMap["stream"]; ok {
		if sb, ok := s.(bool); ok {
			isStream = sb
		}
	}

	targetURL := fmt.Sprintf("%s%s", backend.URL(), endpoint)

	var reqCtx context.Context
	var reqCancel context.CancelFunc
	if timeoutSec > 0 && !isStream {
		reqCtx, reqCancel = context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	} else {
		reqCtx, reqCancel = context.WithCancel(r.Context())
	}
	defer reqCancel()

	proxyReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, strings.NewReader(string(body)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proxy request")
		return
	}

	for key, values := range r.Header {
		for _, v := range values {
			proxyReq.Header.Add(key, v)
		}
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 0}

	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[api] Proxy request failed: %v", err)
		writeError(w, http.StatusBadGateway, "backend request failed")
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(resp.StatusCode)

		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Printf("[api] ResponseWriter does not support Flusher")
			return
		}

		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				_, writeErr := w.Write(buf[:n])
				if writeErr != nil {
					log.Printf("[api] Error writing stream: %v", writeErr)
					break
				}
				flusher.Flush()
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[api] Error reading stream: %v", err)
				}
				break
			}
		}
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}

func resolveModelName(requested string, models []config.ModelConfig) string {
	for _, m := range models {
		if m.Name == requested {
			return m.Name
		}
	}
	for _, m := range models {
		for _, alias := range m.Aliases {
			if alias == requested {
				return m.Name
			}
		}
	}
	lower := strings.ToLower(requested)
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Name), lower) {
			return m.Name
		}
	}
	return ""
}

type openaiError struct {
	Error openaiErrorBody `json:"error"`
}

type openaiErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(openaiError{
		Error: openaiErrorBody{
			Message: message,
			Type:    "invalid_request_error",
			Code:    http.StatusText(status),
		},
	})
}

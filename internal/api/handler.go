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

	"github.com/llamawrapper/gateway/internal/cache"
	"github.com/llamawrapper/gateway/internal/config"
	"github.com/llamawrapper/gateway/internal/metrics"
	"github.com/llamawrapper/gateway/internal/process"
)

type Handler struct {
	manager *process.Manager
	cache   *cache.ResponseCache // nil if disabled
	metrics *metrics.Metrics     // nil if disabled
}

func NewHandler(manager *process.Manager, c *cache.ResponseCache, m *metrics.Metrics) *Handler {
	return &Handler{manager: manager, cache: c, metrics: m}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("/v1/completions", h.handleCompletions)
	mux.HandleFunc("/v1/embeddings", h.handleEmbeddings)
	mux.HandleFunc("/v1/models", h.handleModels)
	mux.HandleFunc("/health", h.handleHealth)
}

// modelRequest is the minimal structure to extract the model name from any OpenAI request.
type modelRequest struct {
	Model string `json:"model"`
}

// openaiModelsResponse mimics the OpenAI /v1/models response.
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
		// Also list aliases
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
	gpuInfo := h.manager.GetGPUInfo()
	queueLen := h.manager.GetQueueLength()

	result := map[string]interface{}{
		"status":        "ok",
		"loaded_models": loaded,
		"queue_depth":   queueLen,
	}
	if len(gpuInfo) > 0 {
		result["gpu"] = gpuInfo
	}
	json.NewEncoder(w).Encode(result)
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
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the body to extract the model name (limit to 10MB)
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

	// Resolve model name: check aliases first, then partial match
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

	// Track active requests in metrics
	if h.metrics != nil {
		h.metrics.IncrActive()
		defer h.metrics.DecrActive()
	}

	// Check response cache (only for non-streaming, temperature=0)
	if h.cache != nil {
		if cacheKey, ok := cache.CacheKey(body); ok {
			if cached, found := h.cache.Get(cacheKey); found {
				log.Printf("[api] Cache hit for %s", modelName)
				if h.metrics != nil {
					h.metrics.RecordCacheHit()
					h.metrics.RecordRequest(modelName, float64(time.Since(start).Milliseconds()), false)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(http.StatusOK)
				w.Write(cached)
				return
			}
			if h.metrics != nil {
				h.metrics.RecordCacheMiss()
			}
		}
	}

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
		if h.metrics != nil {
			h.metrics.RecordRequest(modelName, float64(time.Since(start).Milliseconds()), true)
		}
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("failed to load model: %v", err))
		return
	}

	// Track in-flight requests on the backend (for graceful drain)
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

	// Proxy the request to the backend
	targetURL := fmt.Sprintf("%s%s", backend.URL(), endpoint)

	// Per-model request timeout
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

	// Copy headers
	for key, values := range r.Header {
		for _, v := range values {
			proxyReq.Header.Add(key, v)
		}
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 0, // No timeout for streaming
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[api] Proxy request failed: %v", err)
		if h.metrics != nil {
			h.metrics.RecordRequest(modelName, float64(time.Since(start).Milliseconds()), true)
		}
		writeError(w, http.StatusBadGateway, "backend request failed")
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	// Extract prompt snippet for request history
	promptSnippet := extractPromptSnippet(bodyMap)
	apiKey := extractAPIKeyFromRequest(r)
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = w.Header().Get("X-Request-Id")
	}

	if isStream {
		// SSE streaming response
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

		durationMs := float64(time.Since(start).Milliseconds())
		if h.metrics != nil {
			h.metrics.RecordRequest(modelName, durationMs, false)
			h.metrics.RecordKeyUsage(apiKey, 0, false)
			h.metrics.AddRequestRecord(metrics.RequestRecord{
				ID:         reqID,
				Timestamp:  start.UTC().Format(time.RFC3339),
				Model:      modelName,
				Endpoint:   endpoint,
				DurationMs: durationMs,
				Status:     resp.StatusCode,
				IsStream:   true,
				Prompt:     promptSnippet,
				Response:   "(streaming)",
				RemoteAddr: r.RemoteAddr,
				APIKey:     maskAPIKey(apiKey),
			})
		}
	} else {
		// Regular response — read full body for caching
		respBody, _ := io.ReadAll(resp.Body)

		// Set cache header before writing response
		if h.cache != nil && resp.StatusCode == http.StatusOK {
			if _, ok := cache.CacheKey(body); ok {
				w.Header().Set("X-Cache", "MISS")
			}
		}

		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)

		isErr := resp.StatusCode >= 400
		durationMs := float64(time.Since(start).Milliseconds())

		var promptTokens, completionTokens int
		responseSnippet := ""

		// Try to extract token count and response from response
		var respMap map[string]interface{}
		if json.Unmarshal(respBody, &respMap) == nil {
			if usage, ok := respMap["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					promptTokens = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					completionTokens = int(ct)
					if h.metrics != nil {
						h.metrics.RecordTokens(int(ct))
					}
				}
			}
			// Extract response text
			responseSnippet = extractResponseSnippet(respMap)
		}

		if h.metrics != nil {
			h.metrics.RecordRequest(modelName, durationMs, isErr)
			h.metrics.RecordKeyUsage(apiKey, promptTokens+completionTokens, isErr)
			h.metrics.AddRequestRecord(metrics.RequestRecord{
				ID:               reqID,
				Timestamp:        start.UTC().Format(time.RFC3339),
				Model:            modelName,
				Endpoint:         endpoint,
				DurationMs:       durationMs,
				Status:           resp.StatusCode,
				IsError:          isErr,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				Prompt:           promptSnippet,
				Response:         responseSnippet,
				RemoteAddr:       r.RemoteAddr,
				APIKey:           maskAPIKey(apiKey),
			})
		}

		// Cache the response if applicable
		if h.cache != nil && resp.StatusCode == http.StatusOK {
			if cacheKey, ok := cache.CacheKey(body); ok {
				h.cache.Set(cacheKey, respBody)
			}
		}
	}
}

func extractPromptSnippet(bodyMap map[string]interface{}) string {
	// Try chat messages format
	if msgs, ok := bodyMap["messages"].([]interface{}); ok && len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if m, ok := last.(map[string]interface{}); ok {
			if content, ok := m["content"].(string); ok {
				if len(content) > 200 {
					return content[:200] + "..."
				}
				return content
			}
		}
	}
	// Try prompt format
	if prompt, ok := bodyMap["prompt"].(string); ok {
		if len(prompt) > 200 {
			return prompt[:200] + "..."
		}
		return prompt
	}
	return ""
}

func extractResponseSnippet(respMap map[string]interface{}) string {
	if choices, ok := respMap["choices"].([]interface{}); ok && len(choices) > 0 {
		choice := choices[0].(map[string]interface{})
		// Chat completion
		if msg, ok := choice["message"].(map[string]interface{}); ok {
			if content, ok := msg["content"].(string); ok {
				if len(content) > 500 {
					return content[:500] + "..."
				}
				return content
			}
		}
		// Text completion
		if text, ok := choice["text"].(string); ok {
			if len(text) > 500 {
				return text[:500] + "..."
			}
			return text
		}
	}
	return ""
}

func extractAPIKeyFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return r.Header.Get("X-API-Key")
}

func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return "***"
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// resolveModelName tries to match the requested model name against configured models.
// Supports exact match and partial match (e.g. "llama3" matches "meta/llama3-8b").
func resolveModelName(requested string, models []config.ModelConfig) string {
	// Exact match first
	for _, m := range models {
		if m.Name == requested {
			return m.Name
		}
	}

	// Check aliases
	for _, m := range models {
		for _, alias := range m.Aliases {
			if alias == requested {
				return m.Name
			}
		}
	}

	// Partial match (requested is a substring of model name)
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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

LlamaWrapper Gateway — a Go-based OpenAI-compatible API gateway for llama.cpp. Manages multiple GGUF models behind a single endpoint with lazy loading, LRU eviction, streaming support, and observability.

## Build & Run Commands

```bash
# Build
go build -o gateway ./cmd/gateway

# Production build (matches Dockerfile)
CGO_ENABLED=0 go build -ldflags="-s -w" -o gateway ./cmd/gateway

# Run
./gateway -config config.yaml

# Hot reload config (no restart needed)
kill -SIGHUP $(pgrep gateway)
```

There are no unit tests, linting, or formatting tools configured in this project.

## Architecture

**Single binary, minimal dependencies** — only `gopkg.in/yaml.v3` for config; everything else is Go stdlib. CGO is disabled.

### Entry Point

`cmd/gateway/main.go` — Parses config, creates the process manager, registers routes, builds the middleware stack, starts the HTTP server, and handles signals (SIGHUP for hot reload, SIGINT/SIGTERM for graceful shutdown with 30s drain).

### Core Packages (all under `internal/`)

- **process/manager.go** (~1100 lines, the heart of the system) — Manages llama-server subprocesses. Handles lazy model loading on first request, LRU eviction when at capacity, round-robin load balancing across multiple instances per model, health checks, auto-restart of crashed backends, and request queuing.
- **api/handler.go** — OpenAI-compatible HTTP handlers. Proxies requests directly to backend llama-server processes. Supports streaming (SSE).
- **config/config.go** — YAML config parsing and validation. Models can have aliases (e.g., "gpt-4" → a local model).
- **middleware/** — Composable middleware stack applied in order: CORS → Logging → RequestID → RateLimit → Auth.
- **cache/cache.go** — LRU response cache for deterministic requests (temperature=0). SHA256 key, TTL expiration.
- **metrics/metrics.go** — Prometheus-format metrics and request telemetry (latency histograms, token counts, SLA tracking).
- **admin/admin.go** — Admin API for manual model load/unload, config reload, GPU info.
- **dashboard/** — Embedded web dashboard (HTML/CSS/JS in `html.go`) with real-time model status, request history, and system metrics.

### Key Design Patterns

- **Process Manager state machine**: Backend states flow Stopped → Starting → Ready → Failed. Mutex-protected shared state with atomic operations for request counting.
- **Direct subprocess management**: Models run as llama-server child processes managed via `os/exec`. No intermediate server layer.
- **Middleware composition**: Functional middleware pattern wrapping `http.Handler`.
- **Context propagation**: Full context cancellation flows from HTTP requests through to backend proxying.

### API Routes

| Endpoint | Purpose |
|----------|---------|
| `POST /v1/chat/completions` | Chat completion (streaming supported) |
| `POST /v1/completions` | Text completion |
| `POST /v1/embeddings` | Embeddings |
| `GET /v1/models` | List models + aliases |
| `GET /health` | Gateway health status |
| `GET /metrics` | Prometheus metrics |
| `GET /dashboard` | Web dashboard |
| `POST /admin/{status,load,unload,reload,gpu}` | Admin operations |

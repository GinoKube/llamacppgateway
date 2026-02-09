# LlamaWrapper Gateway — Command Reference

## Start the Gateway

```bash
./gateway -config config.yaml
```

## OpenAI-Compatible API

```bash
# Chat completions
curl http://localhost:8001/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4","messages":[{"role":"user","content":"Hello"}]}'

# Chat completions using an alias (e.g. "gpt-4" → qwen3-2b-q4)
curl http://localhost:8001/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# Streaming
curl http://localhost:8001/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4","messages":[{"role":"user","content":"Hello"}],"stream":true}'

# Text completions
curl http://localhost:8001/v1/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4","prompt":"Once upon a time"}'

# Embeddings
curl http://localhost:8001/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4","input":"Hello world"}'

# List models (includes aliases)
curl http://localhost:8001/v1/models
```

## Health & Status

```bash
# Health check (includes GPU info and queue depth)
curl http://localhost:8001/health

# Prometheus metrics
curl http://localhost:8001/metrics

# Web dashboard (open in browser)
open http://localhost:8001/dashboard
```

## Admin API

```bash
# View full status (backends, GPU, metrics, queue)
curl http://localhost:8001/admin/status

# Manually load a model
curl -X POST http://localhost:8001/admin/load \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4"}'

# Unload a model
curl -X POST http://localhost:8001/admin/unload \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-2b-q4"}'

# Hot-reload config (re-reads config.yaml without restart)
curl -X POST http://localhost:8001/admin/reload

# View GPU memory usage
curl http://localhost:8001/admin/gpu
```

## Hot Reload via Signal

```bash
# Reload config without restarting the gateway
kill -SIGHUP $(pgrep gateway)
```

## With API Key Authentication

If `auth.enabled: true` in config:

```bash
# Using Bearer token
curl http://localhost:8001/v1/chat/completions \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# Using X-API-Key header
curl http://localhost:8001/v1/chat/completions \
  -H "X-API-Key: sk-your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# Admin endpoints require admin_keys
curl -X POST http://localhost:8001/admin/status \
  -H "Authorization: Bearer sk-admin-key"
```

## OpenAI Python SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8001/v1",
    api_key="sk-your-api-key"  # or "no-key" if auth is disabled
)

response = client.chat.completions.create(
    model="gpt-4",  # uses alias → routes to configured model
    messages=[{"role": "user", "content": "Hello!"}]
)
print(response.choices[0].message.content)
```

## Docker

```bash
# Build
docker build -t llamawrapper .

# Run (mount config and models)
docker run -p 8001:8000 \
  -v $(pwd)/config.yaml:/etc/llamawrapper/config.yaml \
  -v /path/to/models:/models \
  --gpus all \
  llamawrapper
```

## Build from Source

```bash
git clone <repo>
cd gateway
go build -o gateway ./cmd/gateway
./gateway -config config.yaml
```

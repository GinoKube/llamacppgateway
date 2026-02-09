# LlamaWrapper Gateway — Quick Start

OpenAI-compatible API gateway for llama.cpp. Multi-model, concurrent, streaming.

## Install

```bash
# 1. Build llama.cpp
git clone https://github.com/ggerganov/llama.cpp.git && cd llama.cpp
cmake -B build -DGGML_CUDA=ON    # Linux GPU (or omit flag for CPU / macOS Metal)
cmake --build build --config Release -j$(nproc)

# 2. Download a model
pip install huggingface-hub
huggingface-cli download Qwen/Qwen3-8B-GGUF qwen3-8b-q4_k_m.gguf --local-dir ~/models

# 3. Install Go & build gateway
sudo snap install go --classic    # or: brew install go (macOS)
cd ~/llamawrapper-gateway
go build -o gateway ./cmd/gateway

# 4. Configure
cp config.example.yaml config.yaml
# Edit config.yaml — set llama_server_path and model paths
```

## Config (config.yaml)

```yaml
listen_addr: ":8000"
llama_server_path: "/path/to/llama.cpp/build/bin/llama-server"
port_range_start: 8081
max_loaded_models: 2

models:
  - name: "qwen3-8b"
    model_path: "/path/to/models/qwen3-8b-q4_k_m.gguf"
    gpu_layers: -1      # -1 = all GPU, 0 = CPU only
    context_size: 8192
    threads: 4
    batch_size: 512
```

## Run

```bash
./gateway -config config.yaml
```

## Use

```bash
# Chat completion
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-8b","messages":[{"role":"user","content":"Hello!"}]}'

# Streaming
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-8b","messages":[{"role":"user","content":"Hello!"}],"stream":true}'

# List models
curl http://localhost:8000/v1/models
```

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8000/v1", api_key="x")
r = client.chat.completions.create(model="qwen3-8b", messages=[{"role":"user","content":"Hi"}])
print(r.choices[0].message.content)
```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/chat/completions` | Chat (supports `stream: true`) |
| POST | `/v1/completions` | Text completion |
| POST | `/v1/embeddings` | Embeddings |
| GET | `/v1/models` | List models |
| GET | `/health` | Health check |

## Run as service (Linux)

```bash
# /etc/systemd/system/llamawrapper.service
# ExecStart=/path/to/gateway -config /path/to/config.yaml
# Restart=always
sudo systemctl enable --now llamawrapper
```

See [README.md](README.md) for full docs, macOS launchd setup, VRAM guide, and troubleshooting.

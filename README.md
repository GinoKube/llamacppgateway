# LlamaWrapper Gateway

A lightweight, high-performance OpenAI-compatible API gateway for [llama.cpp](https://github.com/ggerganov/llama.cpp). Manages multiple GGUF models behind a single endpoint with lazy loading, LRU eviction, and full streaming support.

**Use it as a drop-in replacement for the OpenAI API** — works with any OpenAI SDK, LangChain, LlamaIndex, Open WebUI, or raw HTTP.

## Why use this instead of Ollama?

| | LlamaWrapper Gateway | Ollama |
|---|---|---|
| **Concurrency** | Full — llama.cpp continuous batching, 8 parallel slots per model | Limited — sequential request processing |
| **Speed** | Direct llama.cpp, minimal proxy overhead (~20ms) | Additional abstraction layer overhead |
| **Multi-model** | Automatic lazy loading + LRU eviction across models | Manual model management |
| **Transparency** | Full control over every llama-server flag | Opaque defaults |
| **API** | OpenAI-compatible (`/v1/chat/completions`, etc.) | OpenAI-compatible + custom API |

## Features

- **Single endpoint** — One port for all models, routes by `model` field
- **OpenAI-compatible** — Drop-in replacement for any OpenAI SDK
- **Lazy loading** — Models start on first request, no wasted GPU memory
- **LRU eviction** — Auto-unloads least-recently-used models when at capacity
- **Concurrent requests** — 8 parallel slots per model via continuous batching
- **SSE streaming** — Full support for streaming chat completions
- **Health checks** — Periodic checks, auto-detects crashed backends
- **CORS enabled** — Ready for web frontends
- **Single binary** — No runtime dependencies, just copy and run

---

## Installation (from scratch on any server)

### Prerequisites

- **Linux** (Ubuntu 20.04+ / Debian 11+ / RHEL 8+ / any modern distro) **or macOS** (13.0+ Ventura, Apple Silicon M1/M2/M3/M4)
- **GPU** — NVIDIA with drivers (Linux) or Apple Metal (macOS, automatic) — CPU-only works too
- **Git**, **CMake** (3.14+), **C++ compiler** (gcc/g++ on Linux, Xcode Command Line Tools on macOS)

### Step 1: Install system dependencies

**Ubuntu / Debian:**

```bash
sudo apt update
sudo apt install -y build-essential cmake git curl wget
```

**RHEL / CentOS / Amazon Linux:**

```bash
sudo yum groupinstall -y "Development Tools"
sudo yum install -y cmake3 git curl wget
```

**macOS (Apple Silicon):**

```bash
# Install Xcode Command Line Tools (includes clang, git, make)
xcode-select --install

# Install Homebrew if not already installed
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install cmake
brew install cmake
```

### Step 2: Install CUDA (Linux with NVIDIA GPU only)

> **macOS users: skip this step.** Metal GPU acceleration is built-in and requires no extra installation.

Skip this if CUDA is already installed (`nvidia-smi` works) or if running CPU-only.

```bash
# Ubuntu 22.04 example — check https://developer.nvidia.com/cuda-downloads for your distro
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt update
sudo apt install -y cuda-toolkit
export PATH=/usr/local/cuda/bin:$PATH
export LD_LIBRARY_PATH=/usr/local/cuda/lib64:$LD_LIBRARY_PATH
```

Verify: `nvcc --version` and `nvidia-smi`

### Step 3: Clone and build llama.cpp

```bash
cd ~
git clone https://github.com/ggerganov/llama.cpp.git
cd llama.cpp
```

Then build for your platform:

**Linux with NVIDIA GPU:**

```bash
cmake -B build -DGGML_CUDA=ON
cmake --build build --config Release -j$(nproc)
```

**Linux CPU-only:**

```bash
cmake -B build
cmake --build build --config Release -j$(nproc)
```

**macOS Apple Silicon (M1/M2/M3/M4):**

```bash
# Metal is enabled by default on macOS — just build:
cmake -B build
cmake --build build --config Release -j$(sysctl -n hw.ncpu)
```

> Metal GPU acceleration is auto-detected on Apple Silicon. You do **not** need `-DGGML_METAL=ON` on recent llama.cpp versions — it's on by default.

Verify:

```bash
ls build/bin/llama-server  # should exist
```

### Step 4: Download GGUF models

Download models from [Hugging Face](https://huggingface.co/models?search=gguf). Example:

```bash
mkdir -p ~/models

# Example: download Qwen3 8B (Q4_K_M quantization — good balance of speed/quality)
pip install huggingface-hub
huggingface-cli download Qwen/Qwen3-8B-GGUF qwen3-8b-q4_k_m.gguf --local-dir ~/models

# Example: download Llama 3.1 8B
huggingface-cli download bartowski/Meta-Llama-3.1-8B-Instruct-GGUF Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf --local-dir ~/models
```

### Step 5: Install Go and build the gateway

**Linux:**

```bash
# Option A: snap
sudo snap install go --classic

# Option B: manual
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

**macOS:**

```bash
# Option A: Homebrew (recommended)
brew install go

# Option B: manual
curl -LO https://go.dev/dl/go1.23.4.darwin-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.darwin-arm64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

**Build the gateway:**

```bash
cd ~/llamawrapper-gateway   # or wherever you cloned this repo
go build -o gateway ./cmd/gateway
```

### Step 6: Configure

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml`:

**Linux example:**

```yaml
listen_addr: ":8000"
llama_server_path: "/root/llama.cpp/build/bin/llama-server"
port_range_start: 8081
max_loaded_models: 3
health_check_sec: 30

models:
  - name: "qwen3-8b"
    model_path: "/root/models/qwen3-8b-q4_k_m.gguf"
    gpu_layers: -1        # -1 = all layers on GPU
    context_size: 8192
    threads: 4
    batch_size: 512

  - name: "llama3.1-8b"
    model_path: "/root/models/Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf"
    gpu_layers: -1
    context_size: 8192
    threads: 4
    batch_size: 512
```

**macOS example:**

```yaml
listen_addr: ":8000"
llama_server_path: "/Users/yourname/llama.cpp/build/bin/llama-server"
port_range_start: 8081
max_loaded_models: 2
health_check_sec: 30

models:
  - name: "qwen3-8b"
    model_path: "/Users/yourname/models/qwen3-8b-q4_k_m.gguf"
    gpu_layers: -1        # -1 = all layers on Metal GPU (Apple Silicon)
    context_size: 8192
    threads: 8             # Apple Silicon has 8+ performance cores
    batch_size: 512
```

**Key settings:**
- `gpu_layers: -1` → offload all layers to GPU (CUDA on Linux, Metal on macOS)
- `gpu_layers: 0` → CPU only
- `gpu_layers: 20` → partial offload (for models that don't fully fit in VRAM / unified memory)
- `max_loaded_models` → how many models can be in memory simultaneously
- `context_size` → larger = more memory, supports longer conversations
- `threads` → set to number of performance cores (Linux: `nproc`, macOS: `sysctl -n hw.perflevel0.physicalcpu`)

### Step 7: Run

```bash
./gateway -config config.yaml
```

Output:

```
LlamaWrapper Gateway starting...
Loaded 2 model(s), max concurrent: 3
  - qwen3-8b (/root/models/qwen3-8b-q4_k_m.gguf)
  - llama3.1-8b (/root/models/Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf)
Gateway listening on :8000
OpenAI-compatible API available at:
  POST http://localhost:8000/v1/chat/completions
  POST http://localhost:8000/v1/completions
  POST http://localhost:8000/v1/embeddings
  GET  http://localhost:8000/v1/models
  GET  http://localhost:8000/health
```

---

## Run as a background service (production)

### Linux (systemd)

Create `/etc/systemd/system/llamawrapper.service`:

```ini
[Unit]
Description=LlamaWrapper Gateway
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/llamawrapper-gateway
ExecStart=/root/llamawrapper-gateway/gateway -config /root/llamawrapper-gateway/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535

# GPU support — adjust path if needed
Environment="LD_LIBRARY_PATH=/usr/local/cuda/lib64"

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable llamawrapper
sudo systemctl start llamawrapper
sudo systemctl status llamawrapper

# View logs
sudo journalctl -u llamawrapper -f
```

### macOS (launchd)

Create `~/Library/LaunchAgents/com.llamawrapper.gateway.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.llamawrapper.gateway</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/yourname/llamawrapper-gateway/gateway</string>
        <string>-config</string>
        <string>/Users/yourname/llamawrapper-gateway/config.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/llamawrapper.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/llamawrapper.err</string>
</dict>
</plist>
```

```bash
# Load and start
launchctl load ~/Library/LaunchAgents/com.llamawrapper.gateway.plist

# Stop
launchctl unload ~/Library/LaunchAgents/com.llamawrapper.gateway.plist

# View logs
tail -f /tmp/llamawrapper.log
```

---

## Usage

### curl

```bash
# List available models
curl http://localhost:8000/v1/models

# Chat completion (non-streaming)
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-8b",
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 200
  }'

# Chat completion (streaming)
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-8b",
    "messages": [{"role": "user", "content": "Explain quantum computing"}],
    "max_tokens": 500,
    "stream": true
  }'

# Text completion
curl http://localhost:8000/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-8b",
    "prompt": "The meaning of life is",
    "max_tokens": 100
  }'

# Embeddings
curl http://localhost:8000/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-8b",
    "input": "Hello world"
  }'

# Health check
curl http://localhost:8000/health
```

### Python (OpenAI SDK)

```bash
pip install openai
```

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8000/v1",
    api_key="not-needed",  # any string works
)

# Non-streaming
response = client.chat.completions.create(
    model="qwen3-8b",
    messages=[{"role": "user", "content": "What is Python?"}],
    max_tokens=200,
)
print(response.choices[0].message.content)

# Streaming
stream = client.chat.completions.create(
    model="qwen3-8b",
    messages=[{"role": "user", "content": "Write a haiku about coding"}],
    stream=True,
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

### JavaScript / TypeScript

```bash
npm install openai
```

```javascript
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://localhost:8000/v1",
  apiKey: "not-needed",
});

const response = await client.chat.completions.create({
  model: "qwen3-8b",
  messages: [{ role: "user", content: "Hello!" }],
  stream: true,
});

for await (const chunk of response) {
  process.stdout.write(chunk.choices[0]?.delta?.content || "");
}
```

### LangChain

```python
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(
    base_url="http://localhost:8000/v1",
    api_key="not-needed",
    model="qwen3-8b",
)

response = llm.invoke("Explain recursion in simple terms")
print(response.content)
```

### Open WebUI

Point Open WebUI at the gateway as an OpenAI-compatible endpoint:

```
OPENAI_API_BASE_URL=http://localhost:8000/v1
OPENAI_API_KEY=not-needed
```

---

## Configuration Reference

### Gateway Settings

| Field | Default | Description |
|-------|---------|-------------|
| `listen_addr` | `:8000` | Address to listen on (e.g. `:8000`, `0.0.0.0:8080`) |
| `llama_server_path` | **(required)** | Absolute path to `llama-server` binary |
| `port_range_start` | `8081` | First port allocated for backend llama-server instances |
| `max_loaded_models` | `2` | Max models loaded simultaneously — excess triggers LRU eviction |
| `health_check_sec` | `30` | Seconds between health checks on loaded backends |

### Model Settings

| Field | Default | Description |
|-------|---------|-------------|
| `name` | **(required)** | Model identifier — this is what you pass as `"model"` in API requests |
| `model_path` | **(required)** | Absolute path to the `.gguf` model file |
| `gpu_layers` | `0` | Number of layers to offload to GPU. `-1` = all (fastest). `0` = CPU only |
| `context_size` | `4096` | Max context window. Higher = more memory. Common: 4096, 8192, 32768 |
| `threads` | `4` | CPU threads for inference. Set to number of **performance** cores |
| `batch_size` | `512` | Batch size for prompt processing. Higher = faster prefill, more memory |
| `extra_args` | `[]` | Any additional CLI flags passed directly to `llama-server` |

### Memory Guidelines

| Model Size | Quantization | Approx Memory | Recommended Hardware |
|-----------|-------------|---------------|----------------------|
| 7-8B | Q4_K_M | ~5 GB | M1/M2 8GB, RTX 3060 12GB, T4, L4 |
| 7-8B | Q8_0 | ~8 GB | M1/M2 16GB, RTX 3080, RTX 4070 |
| 13B | Q4_K_M | ~8 GB | M1/M2 16GB, RTX 3080, RTX 4070, L4 |
| 30B (MoE 3B active) | Q4_K_M | ~18 GB | M2 Pro/Max 32GB, RTX 3090, RTX 4090, L4 |
| 70B | Q4_K_M | ~40 GB | M2 Ultra 64GB, A100 40GB, 2x RTX 3090 |

> **Apple Silicon note:** macOS uses unified memory — the same RAM is shared between CPU and GPU. A MacBook Pro with 32GB unified memory can run models that need ~28GB. No separate "VRAM" to worry about.

---

## Architecture

```
                         ┌──────────────────────────┐
                         │   LlamaWrapper Gateway    │
  Any OpenAI Client ────▶│       :8000               │
  (SDK, curl, UI)        │                          │
                         │  ┌─ model router ──────┐ │
                         │  │ parse "model" field  │ │
                         │  │ lazy load if needed  │ │
                         │  │ LRU evict if full    │ │
                         │  │ proxy request + SSE  │ │
                         │  └──────────────────────┘ │
                         └────┬────────┬─────────┬───┘
                              │        │         │
                     ┌────────▼──┐ ┌───▼───────┐ │
                     │llama-server│ │llama-server│ │ (more as needed)
                     │  :8081    │ │  :8082    │ ...
                     │  Model A  │ │  Model B  │
                     │ (8 slots) │ │ (8 slots) │
                     └───────────┘ └───────────┘
```

**Request flow:**

1. Client sends request with `"model": "qwen3-8b"` to `:8000`
2. Gateway extracts model name, checks if backend is running
3. If not loaded → spawns `llama-server` on next available port (evicts LRU if at `max_loaded_models`)
4. Waits for backend health check to pass (~2-10s for first load)
5. Proxies request to backend, including SSE streaming
6. Updates last-used timestamp for LRU tracking

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | Chat completion (streaming supported via `"stream": true`) |
| `POST` | `/v1/completions` | Text completion |
| `POST` | `/v1/embeddings` | Generate embeddings |
| `GET` | `/v1/models` | List all configured models |
| `GET` | `/health` | Gateway health status + currently loaded models |

---

## Troubleshooting

### `llama-server: error while loading shared libraries: libmtmd.so.0` (Linux)

The gateway automatically sets `LD_LIBRARY_PATH` to the directory containing `llama-server`. If running manually, set it yourself:

```bash
export LD_LIBRARY_PATH=/path/to/llama.cpp/build/bin:$LD_LIBRARY_PATH
```

### `dylib` errors on macOS

If you see errors about missing `.dylib` files, ensure you're running from the correct directory or set the library path:

```bash
export DYLD_LIBRARY_PATH=/path/to/llama.cpp/build/bin:$DYLD_LIBRARY_PATH
```

> The gateway handles this automatically, but you may need it when running `llama-server` directly.

### Model takes too long to load

First load of a model takes 2-30s depending on model size and disk speed. Subsequent requests to the same model are instant (model stays in memory until evicted).

### Out of memory (VRAM / unified memory)

- Reduce `gpu_layers` to offload fewer layers (partial offload)
- Use a smaller quantization (Q4_K_M instead of Q8_0)
- Reduce `max_loaded_models` to 1
- Reduce `context_size`
- **macOS:** Close other memory-heavy apps — unified memory is shared with the system

### Port conflicts

Change `port_range_start` in config to a range that doesn't conflict with other services. The gateway allocates ports sequentially starting from this value.

### Firewall

If accessing from another machine, open the gateway port:

```bash
# Linux (Ubuntu)
sudo ufw allow 8000/tcp

# Linux (RHEL/CentOS)
sudo firewall-cmd --add-port=8000/tcp --permanent && sudo firewall-cmd --reload

# macOS — the firewall prompt should appear automatically.
# Or allow manually in System Settings → Network → Firewall → Options.
```

---

## Project Structure

```
gateway/
├── cmd/gateway/main.go          # Entry point, HTTP server, CORS, signal handling
├── internal/
│   ├── config/config.go         # YAML config parsing + validation
│   ├── process/manager.go       # Process manager: lazy load, LRU eviction, health checks
│   └── api/handler.go           # OpenAI-compatible API routes, SSE streaming proxy
├── config.example.yaml          # Example configuration
├── go.mod / go.sum              # Go module files
└── README.md                    # This file
```

# LlamaWrapper Gateway — Install Commands

Assumes llama.cpp is already built and you know the path to `llama-server`.

---

## Linux

```bash
# 1. Install Go
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

# 2. Clone and build the gateway
git clone <your-repo-url> llamawrapper-gateway
cd llamawrapper-gateway/gateway
go build -o gateway ./cmd/gateway

# 3. Create config
cp config.example.yaml config.yaml
nano config.yaml
# Set llama_server_path to your llama-server binary
# Set model name, model_path, gpu_layers, etc.

# 4. Run
./gateway -config config.yaml
```

---

## macOS (Apple Silicon)

```bash
# 1. Install Go
brew install go

# 2. Clone and build the gateway
git clone <your-repo-url> llamawrapper-gateway
cd llamawrapper-gateway/gateway
go build -o gateway ./cmd/gateway

# 3. Create config
cp config.example.yaml config.yaml
nano config.yaml
# Set llama_server_path to your llama-server binary
# Set model name, model_path, gpu_layers, etc.

# 4. Run
./gateway -config config.yaml
```

---

## Verify

```bash
# Check it's running
curl http://localhost:8000/health

# List models
curl http://localhost:8000/v1/models

# Send a request
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"your-model-name","messages":[{"role":"user","content":"Hello!"}]}'
```

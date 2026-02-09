package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	Name        string   `yaml:"name"`         // Model name used in API requests
	ModelPath   string   `yaml:"model_path"`   // Path to GGUF file
	GPULayers   int      `yaml:"gpu_layers"`   // Number of layers to offload to GPU (-1 = all)
	ContextSize int      `yaml:"context_size"`  // Context window size
	Threads     int      `yaml:"threads"`      // Number of CPU threads
	BatchSize   int      `yaml:"batch_size"`   // Batch size for prompt processing
	ExtraArgs   []string `yaml:"extra_args"`   // Additional llama-server arguments
	Aliases     []string `yaml:"aliases"`      // Alternative names (e.g. "gpt-4" -> this model)
	GPUDevices  string   `yaml:"gpu_devices"`  // CUDA_VISIBLE_DEVICES value (e.g. "0", "0,1")
	TimeoutSec  int      `yaml:"timeout_sec"`  // Per-model request timeout in seconds (0 = no timeout)
	MaxTokens   int      `yaml:"max_tokens"`   // Max tokens limit (0 = unlimited)
	Instances    int      `yaml:"instances"`      // Number of llama-server instances for load balancing (default 1)
	CostPerToken float64  `yaml:"cost_per_token"` // Override cost per token for this model (0 = use default)
	AutoDownload *AutoDownloadConfig `yaml:"auto_download"` // Auto-download from HuggingFace
}

type AutoDownloadConfig struct {
	Repo     string `yaml:"repo"`     // HuggingFace repo (e.g. "Qwen/Qwen3-8B-GGUF")
	File     string `yaml:"file"`     // File name in repo (e.g. "qwen3-8b-q4_k_m.gguf")
	LocalDir string `yaml:"local_dir"` // Local directory to download into
}

type AuthConfig struct {
	Enabled bool     `yaml:"enabled"`   // Enable API key authentication
	Keys    []string `yaml:"keys"`      // List of valid API keys
	AdminKeys []string `yaml:"admin_keys"` // Keys with admin access
}

type RateLimitConfig struct {
	Enabled       bool    `yaml:"enabled"`         // Enable rate limiting
	RequestsPerMin int    `yaml:"requests_per_min"` // Max requests per minute per IP/key
	BurstSize     int     `yaml:"burst_size"`       // Burst allowance
}

type QueueConfig struct {
	Enabled    bool `yaml:"enabled"`     // Enable request queuing
	MaxSize    int  `yaml:"max_size"`    // Max queue depth
	TimeoutSec int  `yaml:"timeout_sec"` // Max wait time in queue
}

type CacheConfig struct {
	Enabled    bool `yaml:"enabled"`     // Enable response caching
	MaxEntries int  `yaml:"max_entries"` // Max cached responses
	TTLSec     int  `yaml:"ttl_sec"`     // Cache entry TTL in seconds
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"` // Enable Prometheus metrics at /metrics
}

type DashboardConfig struct {
	Enabled  bool   `yaml:"enabled"`  // Enable web dashboard at /dashboard
	Password string `yaml:"password"` // Optional password to protect dashboard
}

type SLAConfig struct {
	TargetP95Ms    float64 `yaml:"target_p95_ms"`    // P95 latency target in ms (default 2000)
	MaxErrorPct    float64 `yaml:"max_error_pct"`     // Max error percentage (default 1.0)
}

type CostConfig struct {
	DefaultPerToken float64 `yaml:"default_per_token"` // Default $/token (e.g. 0.000002)
}

type LoggingConfig struct {
	Format string `yaml:"format"` // "json" or "text" (default "text")
}

type Config struct {
	ListenAddr      string           `yaml:"listen_addr"`       // Gateway listen address (e.g. ":8000")
	LlamaServerPath string           `yaml:"llama_server_path"` // Path to llama-server binary
	PortRangeStart  int              `yaml:"port_range_start"`  // Start of port range for backends
	MaxLoadedModels int              `yaml:"max_loaded_models"` // Max concurrent loaded models (LRU eviction)
	HealthCheckSec  int              `yaml:"health_check_sec"`  // Health check interval in seconds
	Models          []ModelConfig    `yaml:"models"`
	Auth            AuthConfig       `yaml:"auth"`
	RateLimit       RateLimitConfig  `yaml:"rate_limit"`
	Queue           QueueConfig      `yaml:"queue"`
	Cache           CacheConfig      `yaml:"cache"`
	Metrics         MetricsConfig    `yaml:"metrics"`
	Dashboard       DashboardConfig  `yaml:"dashboard"`
	Logging         LoggingConfig    `yaml:"logging"`
	SLA             SLAConfig        `yaml:"sla"`
	Cost            CostConfig       `yaml:"cost"`

	configPath string `yaml:"-"` // Path to config file (set during Load)
}

// ConfigPath returns the path to the loaded config file.
func (c *Config) ConfigPath() string { return c.configPath }

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		ListenAddr:      ":8000",
		PortRangeStart:  8081,
		MaxLoadedModels: 2,
		HealthCheckSec:  30,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.LlamaServerPath == "" {
		return nil, fmt.Errorf("llama_server_path is required")
	}
	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("at least one model must be configured")
	}

	// Defaults for rate limiting
	if cfg.RateLimit.Enabled && cfg.RateLimit.RequestsPerMin == 0 {
		cfg.RateLimit.RequestsPerMin = 60
	}
	if cfg.RateLimit.Enabled && cfg.RateLimit.BurstSize == 0 {
		cfg.RateLimit.BurstSize = 10
	}

	// Defaults for queue
	if cfg.Queue.Enabled && cfg.Queue.MaxSize == 0 {
		cfg.Queue.MaxSize = 100
	}
	if cfg.Queue.Enabled && cfg.Queue.TimeoutSec == 0 {
		cfg.Queue.TimeoutSec = 300
	}

	// Defaults for cache
	if cfg.Cache.Enabled && cfg.Cache.MaxEntries == 0 {
		cfg.Cache.MaxEntries = 1000
	}
	if cfg.Cache.Enabled && cfg.Cache.TTLSec == 0 {
		cfg.Cache.TTLSec = 3600
	}

	// Defaults for logging
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}

	for i, m := range cfg.Models {
		if m.Name == "" {
			return nil, fmt.Errorf("model[%d]: name is required", i)
		}
		if m.ModelPath == "" && m.AutoDownload == nil {
			return nil, fmt.Errorf("model[%d] (%s): model_path or auto_download is required", i, m.Name)
		}
		if m.ContextSize == 0 {
			cfg.Models[i].ContextSize = 4096
		}
		if m.Threads == 0 {
			cfg.Models[i].Threads = 4
		}
		if m.BatchSize == 0 {
			cfg.Models[i].BatchSize = 512
		}
		if m.Instances == 0 {
			cfg.Models[i].Instances = 1
		}
	}

	cfg.configPath = path

	return cfg, nil
}

// ResolveAlias checks if a requested model name matches any configured alias.
// Returns the canonical model name or empty string.
func (c *Config) ResolveAlias(requested string) string {
	for _, m := range c.Models {
		if m.Name == requested {
			return m.Name
		}
		for _, alias := range m.Aliases {
			if alias == requested {
				return m.Name
			}
		}
	}
	return ""
}

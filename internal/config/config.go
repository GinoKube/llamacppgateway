package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	Name        string   `yaml:"name"`
	ModelPath   string   `yaml:"model_path"`
	GPULayers   int      `yaml:"gpu_layers"`
	ContextSize int      `yaml:"context_size"`
	Threads     int      `yaml:"threads"`
	BatchSize   int      `yaml:"batch_size"`
	ExtraArgs   []string `yaml:"extra_args"`
	Aliases     []string `yaml:"aliases"`
	GPUDevices  string   `yaml:"gpu_devices"`
	TimeoutSec  int      `yaml:"timeout_sec"`
	MaxTokens   int      `yaml:"max_tokens"`
	Instances   int      `yaml:"instances"`
	AutoDownload *AutoDownloadConfig `yaml:"auto_download"`
}

type AutoDownloadConfig struct {
	Repo     string `yaml:"repo"`
	File     string `yaml:"file"`
	LocalDir string `yaml:"local_dir"`
}

type Config struct {
	ListenAddr      string        `yaml:"listen_addr"`
	LlamaServerPath string        `yaml:"llama_server_path"`
	PortRangeStart  int           `yaml:"port_range_start"`
	MaxLoadedModels int           `yaml:"max_loaded_models"`
	HealthCheckSec  int           `yaml:"health_check_sec"`
	ModelsDir       string        `yaml:"models_dir"`
	Models          []ModelConfig `yaml:"models"`

	configPath string `yaml:"-"`
}

func (c *Config) ConfigPath() string { return c.configPath }

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

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

	// Expand ~ in paths
	cfg.LlamaServerPath = expandHome(cfg.LlamaServerPath)
	cfg.ModelsDir = expandHome(cfg.ModelsDir)
	for i := range cfg.Models {
		cfg.Models[i].ModelPath = expandHome(cfg.Models[i].ModelPath)
		for j := range cfg.Models[i].ExtraArgs {
			cfg.Models[i].ExtraArgs[j] = expandHome(cfg.Models[i].ExtraArgs[j])
		}
	}

	// Auto-detect models from models_dir
	if cfg.ModelsDir != "" {
		discovered, err := ScanModelsDir(cfg.ModelsDir)
		if err != nil {
			return nil, fmt.Errorf("scanning models_dir %q: %w", cfg.ModelsDir, err)
		}
		nameSet := make(map[string]bool)
		pathSet := make(map[string]bool)
		for _, m := range cfg.Models {
			nameSet[m.Name] = true
			pathSet[m.ModelPath] = true
		}
		for _, d := range discovered {
			if !nameSet[d.Name] && !pathSet[d.ModelPath] {
				cfg.Models = append(cfg.Models, d)
			}
		}
	}

	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("at least one model must be configured (or set models_dir)")
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

// ScanModelsDir scans a directory for .gguf files and returns auto-configured ModelConfigs.
func ScanModelsDir(dir string) ([]ModelConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var mmprojs []string
	var modelFiles []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), "mmproj") {
			mmprojs = append(mmprojs, name)
		} else {
			modelFiles = append(modelFiles, name)
		}
	}

	var mmprojPath string
	if len(mmprojs) == 1 {
		mmprojPath = filepath.Join(dir, mmprojs[0])
	}

	var configs []ModelConfig
	for _, f := range modelFiles {
		name := strings.TrimSuffix(f, filepath.Ext(f))
		mc := ModelConfig{
			Name:        name,
			ModelPath:   filepath.Join(dir, f),
			GPULayers:   -1,
			ContextSize: 4096,
			Threads:     4,
			BatchSize:   512,
			Instances:   1,
		}
		if mmprojPath != "" {
			mc.ExtraArgs = []string{"--mmproj", mmprojPath}
		}
		configs = append(configs, mc)
	}
	return configs, nil
}

// ResolveAlias checks if a requested model name matches any configured alias.
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

package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/llamawrapper/gateway/internal/admin"
	"github.com/llamawrapper/gateway/internal/api"
	"github.com/llamawrapper/gateway/internal/cache"
	"github.com/llamawrapper/gateway/internal/config"
	"github.com/llamawrapper/gateway/internal/dashboard"
	"github.com/llamawrapper/gateway/internal/metrics"
	"github.com/llamawrapper/gateway/internal/middleware"
	"github.com/llamawrapper/gateway/internal/process"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("LlamaWrapper Gateway starting...")

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loaded %d model(s), max concurrent: %d", len(cfg.Models), cfg.MaxLoadedModels)
	for _, m := range cfg.Models {
		aliases := ""
		if len(m.Aliases) > 0 {
			aliases = " (aliases: "
			for i, a := range m.Aliases {
				if i > 0 {
					aliases += ", "
				}
				aliases += a
			}
			aliases += ")"
		}
		log.Printf("  - %s (%s)%s", m.Name, m.ModelPath, aliases)
	}

	// Log enabled features
	if cfg.Auth.Enabled {
		log.Printf("  [feature] API key authentication enabled (%d keys, %d admin keys)", len(cfg.Auth.Keys), len(cfg.Auth.AdminKeys))
	}
	if cfg.RateLimit.Enabled {
		log.Printf("  [feature] Rate limiting enabled (%d req/min, burst %d)", cfg.RateLimit.RequestsPerMin, cfg.RateLimit.BurstSize)
	}
	if cfg.Queue.Enabled {
		log.Printf("  [feature] Request queuing enabled (max %d, timeout %ds)", cfg.Queue.MaxSize, cfg.Queue.TimeoutSec)
	}
	if cfg.Cache.Enabled {
		log.Printf("  [feature] Response caching enabled (%d entries, TTL %ds)", cfg.Cache.MaxEntries, cfg.Cache.TTLSec)
	}
	if cfg.Metrics.Enabled {
		log.Printf("  [feature] Prometheus metrics enabled at /metrics")
	}
	if cfg.Dashboard.Enabled {
		log.Printf("  [feature] Web dashboard enabled at /dashboard")
	}

	// Create process manager
	manager := process.NewManager(cfg)

	// Start health checker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.HealthCheck(ctx, cfg.HealthCheckSec)

	// Optional: response cache
	var responseCache *cache.ResponseCache
	if cfg.Cache.Enabled {
		responseCache = cache.New(cfg.Cache.MaxEntries, cfg.Cache.TTLSec)
	}

	// Optional: metrics
	var m *metrics.Metrics
	if cfg.Metrics.Enabled {
		m = metrics.New()

		// Periodic metrics gauge updates
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					m.SetLoadedModels(int64(len(manager.ListLoaded())))
					m.SetQueueDepth(int64(manager.GetQueueLength()))
				}
			}
		}()
	}

	// Create API handler
	handler := api.NewHandler(manager, responseCache, m)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Hot reload function
	reloadFunc := func() error {
		newCfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		manager.UpdateConfig(newCfg)
		log.Printf("Configuration reloaded successfully")
		return nil
	}

	// Admin API
	adminHandler := admin.NewHandler(manager, m, reloadFunc)
	adminHandler.RegisterRoutes(mux)

	// Prometheus metrics endpoint
	if m != nil {
		mux.HandleFunc("/metrics", m.Handler())
	}

	// Web dashboard
	if cfg.Dashboard.Enabled {
		dashHandler := dashboard.NewHandler(manager, m)
		dashHandler.SetReloadFunc(reloadFunc)
		dashHandler.RegisterRoutes(mux)
	}

	// Wire health check and event callbacks for metrics integration
	if m != nil {
		manager.HealthCheckCallback = func(model string, port int, ok bool, latencyMs float64) {
			m.AddHealthCheck(model, port, ok, latencyMs)
		}
		manager.EventCallback = func(level, source, model, message string) {
			m.AddEvent(level, source, model, message)
		}
	}

	// Wire SLA targets from config
	if m != nil && cfg.SLA.TargetP95Ms > 0 {
		m.SLATargetP95Ms = cfg.SLA.TargetP95Ms
	}
	if m != nil && cfg.SLA.MaxErrorPct > 0 {
		m.SLATargetErrPct = cfg.SLA.MaxErrorPct
	}

	// Wire GPU memory % callback for time-series charts
	if m != nil {
		m.GPUMemPctFunc = func() float64 {
			gpus := manager.GetGPUInfo()
			if len(gpus) == 0 {
				return 0
			}
			totalMem, usedMem := 0, 0
			for _, g := range gpus {
				totalMem += g.MemTotalMB
				usedMem += g.MemUsedMB
			}
			if totalMem == 0 {
				return 0
			}
			return float64(usedMem) / float64(totalMem) * 100
		}
	}

	// Build middleware chain (applied in reverse order)
	var handler_ http.Handler = mux

	// CORS
	handler_ = corsMiddleware(handler_)

	// Structured logging (replaces old logging middleware)
	handler_ = middleware.StructuredLogging(cfg.Logging.Format)(handler_)

	// Request ID tracing
	handler_ = middleware.RequestID(handler_)

	// Rate limiting
	if cfg.RateLimit.Enabled {
		handler_ = middleware.RateLimit(cfg.RateLimit)(handler_)
	}

	// API key auth
	if cfg.Auth.Enabled {
		handler_ = middleware.Auth(cfg.Auth)(handler_)
	}

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler_,
		ReadTimeout:  0, // No timeout for streaming
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// Signal handling: SIGINT/SIGTERM for shutdown, SIGHUP for config reload
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				log.Printf("Received SIGHUP — reloading configuration...")
				if err := reloadFunc(); err != nil {
					log.Printf("Config reload failed: %v", err)
				}
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("Shutting down gracefully...")
				manager.Shutdown()

				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer shutdownCancel()
				server.Shutdown(shutdownCtx)
				return
			}
		}
	}()

	log.Printf("Gateway listening on %s", cfg.ListenAddr)
	log.Printf("OpenAI-compatible API available at:")
	log.Printf("  POST http://localhost%s/v1/chat/completions", cfg.ListenAddr)
	log.Printf("  POST http://localhost%s/v1/completions", cfg.ListenAddr)
	log.Printf("  POST http://localhost%s/v1/embeddings", cfg.ListenAddr)
	log.Printf("  GET  http://localhost%s/v1/models", cfg.ListenAddr)
	log.Printf("  GET  http://localhost%s/health", cfg.ListenAddr)
	if cfg.Metrics.Enabled {
		log.Printf("  GET  http://localhost%s/metrics", cfg.ListenAddr)
	}
	if cfg.Dashboard.Enabled {
		log.Printf("  GET  http://localhost%s/dashboard", cfg.ListenAddr)
	}
	log.Printf("  POST http://localhost%s/admin/status", cfg.ListenAddr)
	log.Printf("  POST http://localhost%s/admin/load", cfg.ListenAddr)
	log.Printf("  POST http://localhost%s/admin/unload", cfg.ListenAddr)
	log.Printf("  POST http://localhost%s/admin/reload", cfg.ListenAddr)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Request-Id")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

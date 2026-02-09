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

	"github.com/llamawrapper/gateway/internal/api"
	"github.com/llamawrapper/gateway/internal/config"
	"github.com/llamawrapper/gateway/internal/middleware"
	"github.com/llamawrapper/gateway/internal/process"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("LlamaWrapper Gateway starting...")

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

	manager := process.NewManager(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.HealthCheck(ctx, cfg.HealthCheckSec)

	handler := api.NewHandler(manager)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Build middleware chain: CORS -> Logging -> RequestID
	var h http.Handler = mux
	h = middleware.RequestID(h)
	h = middleware.Logging(h)
	h = corsMiddleware(h)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      h,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				log.Printf("Received SIGHUP — reloading configuration...")
				newCfg, err := config.Load(*configPath)
				if err != nil {
					log.Printf("Config reload failed: %v", err)
				} else {
					manager.UpdateConfig(newCfg)
					log.Printf("Configuration reloaded successfully")
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
	log.Printf("  POST %s/v1/chat/completions", cfg.ListenAddr)
	log.Printf("  POST %s/v1/completions", cfg.ListenAddr)
	log.Printf("  POST %s/v1/embeddings", cfg.ListenAddr)
	log.Printf("  GET  %s/v1/models", cfg.ListenAddr)
	log.Printf("  GET  %s/health", cfg.ListenAddr)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

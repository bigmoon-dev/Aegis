package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bigmoon-dev/aegis/internal/api"
	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
	"github.com/bigmoon-dev/aegis/internal/proxy"
)

func main() {
	configPath := "config/aegis.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("aegis starting, config=%s", configPath)

	// Load configuration
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg := cfgMgr.Get()
	log.Printf("config loaded: %d backends, %d agents", len(cfg.Backends), len(cfg.Agents))

	// Ensure data directory exists
	dbDir := filepath.Dir(cfg.Audit.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("create data dir %s: %v", dbDir, err)
	}

	// Initialize audit logger
	auditLog, err := audit.NewLogger(cfg.Audit.DBPath)
	if err != nil {
		log.Fatalf("init audit logger: %v", err)
	}
	defer auditLog.Close()
	auditLog.StartPurgeLoop(cfg.Audit.RetentionDays)

	// Initialize components
	forwarder := proxy.NewForwarder(cfgMgr)
	sessions := proxy.NewSessionManager()

	// Approval system
	var notifier approval.Notifier
	if cfg.Approval.Feishu.WebhookURL != "" {
		notifier = approval.NewFeishuNotifier(cfg.Approval.Feishu.WebhookURL)
	}
	approvalStore := approval.NewStore(cfgMgr, notifier)

	// Pipeline stages
	acl := pipeline.NewACL(cfgMgr)
	rateLimiter := pipeline.NewRateLimiter(cfgMgr, auditLog)
	approvalGate := pipeline.NewApprovalGate(cfgMgr, approvalStore)
	stages := []pipeline.Stage{acl, rateLimiter, approvalGate}

	// FIFO queue
	queue := pipeline.NewFIFOQueue(cfgMgr, forwarder.Forward)
	queue.Start()
	defer queue.Stop()

	// MCP proxy handler
	mcpHandler := proxy.NewHandler(cfgMgr, forwarder, sessions, stages, queue, auditLog)

	// Approval callback handler
	callbackHandler := approval.NewCallbackHandler(approvalStore)

	// Management API
	apiRouter := api.NewRouter(cfgMgr, queue, approvalStore, auditLog)

	// Build HTTP mux
	mux := proxy.NewMux(mcpHandler, proxy.HealthCheck(cfgMgr), callbackHandler, apiRouter)

	// HTTP server
	server := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", cfg.Server.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ReadTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}

	log.Println("aegis stopped")
}

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bigmoon-dev/aegis/internal/api"
	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
	"github.com/bigmoon-dev/aegis/internal/proxy"
)

func main() {
	subcmd := ""
	if len(os.Args) > 1 {
		subcmd = os.Args[1]
	}

	switch subcmd {
	case "setup":
		if err := runSetup(); err != nil {
			log.Fatalf("setup: %v", err)
		}
	case "demo":
		runDemo()
	default:
		configPath := "config/aegis.yaml"
		if subcmd != "" {
			configPath = subcmd
		}
		runServer(configPath)
	}
}

// runServer starts the Aegis proxy with the given config and blocks until
// SIGINT/SIGTERM is received.
func runServer(configPath string) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("aegis starting, config=%s", configPath)

	srv, shutdown, err := startServer(configPath)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	shutdown()

	log.Println("aegis stopped")
}

// startServer initializes all components and returns the HTTP server ready to
// ListenAndServe, plus a cleanup function. The caller is responsible for
// starting the server and calling shutdown when done.
func startServer(configPath string) (*http.Server, func(), error) {
	// Load configuration
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		return nil, nil, err
	}
	cfg := cfgMgr.Get()
	log.Printf("config loaded: %d backends, %d agents", len(cfg.Backends), len(cfg.Agents))

	// Ensure data directory exists
	dbDir := filepath.Dir(cfg.Audit.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, nil, err
	}

	// Initialize audit logger
	auditLog, err := audit.NewLogger(cfg.Audit.DBPath)
	if err != nil {
		return nil, nil, err
	}
	auditLog.StartPurgeLoop(cfg.Audit.RetentionDays)

	// Initialize components
	forwarder := proxy.NewForwarder(cfgMgr)
	sessions := proxy.NewSessionManager()

	// Approval system
	var notifiers []approval.Notifier
	if cfg.Approval.Feishu.WebhookURL != "" {
		notifiers = append(notifiers, approval.NewFeishuNotifier(cfg.Approval.Feishu.WebhookURL))
	}
	if cfg.Approval.Generic.WebhookURL != "" {
		notifiers = append(notifiers, approval.NewGenericWebhookNotifier(cfg.Approval.Generic.WebhookURL))
	}
	var notifier approval.Notifier
	switch len(notifiers) {
	case 0:
		// No notifier configured; approval still works via management API
	case 1:
		notifier = notifiers[0]
	default:
		notifier = approval.NewMultiNotifier(notifiers...)
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

	cleanup := func() {
		queue.Stop()
		auditLog.Close()
	}

	return server, cleanup, nil
}

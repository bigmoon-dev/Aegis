package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed demo_assets/demo-server.mjs
var demoServerJS []byte

//go:embed demo_assets/demo-config.yaml
var demoConfigYAML []byte

func runDemo() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Check node is available
	nodeExe, err := exec.LookPath("node")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: Node.js is required but not found in PATH.")
		fmt.Fprintln(os.Stderr, "Install it from https://nodejs.org/ and try again.")
		os.Exit(1)
	}

	// Check node version (need >= 18 for built-in fetch, ES modules)
	out, err := exec.Command(nodeExe, "--version").Output()
	if err == nil {
		log.Printf("using %s %s", nodeExe, string(out[:len(out)-1]))
	}

	// Create temp directory for demo assets
	tmpDir, err := os.MkdirTemp("", "aegis-demo-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	serverPath := filepath.Join(tmpDir, "demo-server.mjs")
	configPath := filepath.Join(tmpDir, "demo-config.yaml")

	if err := os.WriteFile(serverPath, demoServerJS, 0644); err != nil {
		log.Fatalf("write demo server: %v", err)
	}

	// Rewrite audit db_path to temp dir so demo doesn't leave files in CWD
	auditPath := filepath.Join(tmpDir, "demo-audit.db")
	configData := bytes.Replace(demoConfigYAML,
		[]byte(`"./data/demo-audit.db"`),
		[]byte(`"`+auditPath+`"`), 1)
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		log.Fatalf("write demo config: %v", err)
	}

	// Start demo MCP server (node subprocess)
	nodeCmd := exec.Command(nodeExe, serverPath)
	nodeCmd.Dir = tmpDir
	nodeCmd.Stdout = os.Stdout
	nodeCmd.Stderr = os.Stderr
	if err := nodeCmd.Start(); err != nil {
		log.Fatalf("start demo server: %v", err)
	}

	// Wait for demo server to be ready
	if err := waitForHealth("http://127.0.0.1:9100/health", 10*time.Second); err != nil {
		nodeCmd.Process.Kill()
		nodeCmd.Wait()
		log.Fatalf("demo server not ready: %v", err)
	}
	log.Println("demo MCP server ready on :9100")

	// Start Aegis proxy
	srv, cleanup, err := startServer(configPath)
	if err != nil {
		nodeCmd.Process.Kill()
		nodeCmd.Wait()
		log.Fatalf("start aegis: %v", err)
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("aegis server error: %v", err)
		}
	}()
	log.Println("aegis proxy ready on :18070")

	printDemoGuide()

	// Wait for Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	cleanup()

	nodeCmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		nodeCmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		nodeCmd.Process.Kill()
		nodeCmd.Wait()
	}

	fmt.Println("Demo stopped. Goodbye!")
}

func waitForHealth(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("health check at %s timed out after %s", url, timeout)
}

const demoHeader = `
=== Aegis Demo ===

Demo MCP server started on :9100
Aegis proxy started on :18070

Agent URL: http://127.0.0.1:18070/agents/demo-agent/mcp
`

func printDemoGuide() {
	h := `-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream'`
	base := `localhost:18070/agents/demo-agent/mcp`

	fmt.Print(demoHeader)
	fmt.Println("Try these:")
	fmt.Println()

	// 1. List tools
	fmt.Println("  1. List tools (admin_reset is hidden by ACL):")
	fmt.Printf("     curl -s %s %s \\\n", base, h)
	fmt.Println(`       -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[].name'`)
	fmt.Println()

	// 2. Echo
	fmt.Println("  2. Echo (no limits):")
	fmt.Printf("     curl -s %s %s \\\n", base, h)
	fmt.Println(`       -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}' | jq`)
	fmt.Println()

	// 3. Rate limiting
	fmt.Println("  3. Rate limiting — call get_weather 4 times (4th is rejected):")
	fmt.Printf("     for i in 1 2 3 4; do curl -s %s %s \\\n", base, h)
	fmt.Println(`       -d '{"jsonrpc":"2.0","id":'$i',"method":"tools/call","params":{"name":"get_weather","arguments":{"city":"Tokyo"}}}' \`)
	fmt.Println(`       | jq -r '.result.content[0].text // .error.message'; done`)
	fmt.Println()

	// 4. Approval
	fmt.Println("  4. Approval — publish_post blocks until approved:")
	fmt.Printf("     curl -s %s %s \\\n", base, h)
	fmt.Println(`       -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish_post","arguments":{"title":"Hello","content":"World"}}}' &`)
	fmt.Println(`     sleep 1`)
	fmt.Println(`     curl -s localhost:18070/api/v1/approvals/pending | jq`)
	fmt.Println(`     # Copy the id, then:`)
	fmt.Println(`     curl -s -X POST localhost:18070/api/v1/approvals/{id}/approve`)
	fmt.Println()

	// 5. Audit log
	fmt.Println("  5. Audit log:")
	fmt.Println(`     curl -s 'localhost:18070/api/v1/audit/logs?limit=10' | jq`)
	fmt.Println()

	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
)

func testRouter(t *testing.T) (*Router, *audit.Logger) {
	t.Helper()

	cfg := &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:9100/mcp", Timeout: 5 * time.Second},
		},
		Queue: map[string]config.QueueConfig{
			"demo": {
				Enabled:    true,
				DelayMin:   time.Second,
				DelayMax:   2 * time.Second,
				MaxPending: 10,
				GlobalRateLimits: map[string]config.RateLimitConfig{
					"search": {Window: time.Hour, MaxCount: 15},
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed: true,
						RateLimits: map[string]config.RateLimitConfig{
							"publish": {Window: 24 * time.Hour, MaxCount: 1},
						},
					},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout:         2 * time.Second,
			CallbackBaseURL: "http://localhost:18070",
		},
		Audit: config.AuditConfig{
			DBPath:        filepath.Join(t.TempDir(), "test.db"),
			RetentionDays: 90,
		},
	}

	cfgMgr := config.NewManagerFromConfig(cfg)
	logger, err := audit.NewLogger(cfg.Audit.DBPath)
	if err != nil {
		t.Fatalf("failed to create audit logger: %v", err)
	}
	t.Cleanup(func() { logger.Close() })

	queue := pipeline.NewFIFOQueue(cfgMgr, nil)
	queue.Start()
	t.Cleanup(func() { queue.Stop() })

	store := approval.NewStore(cfgMgr, nil)

	r := NewRouter(cfgMgr, queue, store, logger)
	return r, logger
}

func TestRouter_APIToken_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{APIToken: "secret-token-123"},
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:9100/mcp", Timeout: 5 * time.Second},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends:    map[string]config.AgentBackendConfig{"demo": {Allowed: true}},
			},
		},
		Audit: config.AuditConfig{DBPath: filepath.Join(t.TempDir(), "test.db")},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	logger, _ := audit.NewLogger(cfg.Audit.DBPath)
	t.Cleanup(func() { logger.Close() })
	queue := pipeline.NewFIFOQueue(cfgMgr, nil)
	queue.Start()
	t.Cleanup(func() { queue.Stop() })
	store := approval.NewStore(cfgMgr, nil)
	r := NewRouter(cfgMgr, queue, store, logger)

	// No token → 401
	req := httptest.NewRequest("GET", "/api/v1/queue/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}

	// Wrong token → 401
	req = httptest.NewRequest("GET", "/api/v1/queue/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}

	// Correct token → 200
	req = httptest.NewRequest("GET", "/api/v1/queue/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", w.Code)
	}
}

func TestRouter_APIToken_NotConfigured(t *testing.T) {
	// No api_token set → all requests allowed (backwards compatible)
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/queue/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 without api_token config, got %d", w.Code)
	}
}

func TestRouter_QueueStatus(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/queue/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result == nil {
		t.Error("expected JSON response")
	}
}

func TestRouter_ListAgents(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var agents []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}
	if agents[0]["id"] != "agent-a" {
		t.Errorf("expected agent-a, got %v", agents[0]["id"])
	}
}

func TestRouter_AgentRateLimits(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/agents/agent-a/rate-limits", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var limits []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&limits); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should have per-agent (publish) + global (search)
	if len(limits) < 2 {
		t.Errorf("expected at least 2 rate limit entries, got %d", len(limits))
	}

	hasAgent := false
	hasGlobal := false
	for _, l := range limits {
		if l["scope"] == "agent" {
			hasAgent = true
		}
		if l["scope"] == "global" {
			hasGlobal = true
		}
	}
	if !hasAgent {
		t.Error("expected at least one 'agent' scope rate limit")
	}
	if !hasGlobal {
		t.Error("expected at least one 'global' scope rate limit")
	}
}

func TestRouter_AgentRateLimits_UnknownAgent(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/agents/unknown/rate-limits", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestRouter_AgentRateLimits_InvalidPath(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/agents/agent-a/invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRouter_PendingApprovals_Empty(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/approvals/pending", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var pending []any
	_ = json.NewDecoder(w.Body).Decode(&pending)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestRouter_ApproveAndReject(t *testing.T) {
	cfg := &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:9100/mcp", Timeout: 5 * time.Second},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]config.AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout:         5 * time.Second,
			CallbackBaseURL: "http://localhost:18070",
		},
		Audit: config.AuditConfig{
			DBPath: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	cfgMgr := config.NewManagerFromConfig(cfg)
	logger, _ := audit.NewLogger(cfg.Audit.DBPath)
	t.Cleanup(func() { logger.Close() })

	queue := pipeline.NewFIFOQueue(cfgMgr, nil)
	queue.Start()
	t.Cleanup(func() { queue.Stop() })

	store := approval.NewStore(cfgMgr, nil)
	r := NewRouter(cfgMgr, queue, store, logger)

	// Create a pending approval
	done := make(chan bool, 1)
	go func() {
		approved, _ := store.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
		done <- approved
	}()

	// Poll until the request is registered
	deadline := time.After(2 * time.Second)
	var pending []map[string]any
	for {
		req := httptest.NewRequest("GET", "/api/v1/approvals/pending", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_ = json.NewDecoder(w.Body).Decode(&pending)
		if len(pending) == 1 {
			break
		}
		pending = nil
		select {
		case <-deadline:
			t.Fatal("timed out waiting for pending approval")
		case <-time.After(10 * time.Millisecond):
		}
	}

	id := pending[0]["id"].(string)

	// Approve it
	req := httptest.NewRequest("POST", "/api/v1/approvals/"+id+"/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case approved := <-done:
		if !approved {
			t.Error("expected approved=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestRouter_ApprovalAction_GETNotAllowed(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/approvals/some-id/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestRouter_ApprovalAction_InvalidAction(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("POST", "/api/v1/approvals/some-id/invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRouter_ApprovalAction_NonExistent(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("POST", "/api/v1/approvals/no-such-id/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestRouter_AuditLogs_Empty(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/audit/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRouter_AuditLogs_WithPagination(t *testing.T) {
	r, logger := testRouter(t)

	// Insert some audit entries
	for i := 0; i < 5; i++ {
		logger.Log(&model.AuditEntry{
			RequestID: "req-" + time.Now().Format("150405.000") + "-" + string(rune('a'+i)),
			AgentID:   "agent-a",
			ToolName:  "echo",
		})
	}

	req := httptest.NewRequest("GET", "/api/v1/audit/logs?limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var logs []map[string]any
	_ = json.NewDecoder(w.Body).Decode(&logs)
	if len(logs) != 2 {
		t.Errorf("expected 2 logs with limit=2, got %d", len(logs))
	}
}

func TestRouter_ConfigReload_GETNotAllowed(t *testing.T) {
	r, _ := testRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/config/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestRouter_ConfigReload_NoFilePath(t *testing.T) {
	r, _ := testRouter(t)

	// NewManagerFromConfig has no filePath, so Reload will fail
	req := httptest.NewRequest("POST", "/api/v1/config/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (no config file), got %d", w.Code)
	}
}

func TestRouter_ConfigReload_Success(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `
backends:
  demo:
    url: "http://localhost:9100/mcp"
agents:
  test:
    display_name: "Test"
    backends:
      demo:
        allowed: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	logger, err := audit.NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { logger.Close() })

	queue := pipeline.NewFIFOQueue(cfgMgr, nil)
	queue.Start()
	t.Cleanup(func() { queue.Stop() })

	store := approval.NewStore(cfgMgr, nil)
	r := NewRouter(cfgMgr, queue, store, logger)

	req := httptest.NewRequest("POST", "/api/v1/config/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", result["status"])
	}
}

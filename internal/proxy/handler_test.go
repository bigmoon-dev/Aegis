package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"context"

	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
)

func testConfig() *config.Config {
	return &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:9100/mcp", Timeout: 5 * time.Second},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
			"agent-denied": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {Allowed: false},
				},
			},
		},
	}
}

func TestHandler_InvalidPath(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("POST", "/invalid/path", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GETNotAllowed(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("GET", "/agents/agent-a/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandler_PUTNotAllowed(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("PUT", "/agents/agent-a/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandler_UnknownAgent(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("POST", "/agents/unknown-agent/mcp", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown agent, got %d", w.Code)
	}
}

func TestHandler_AgentNoAllowedBackend(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("POST", "/agents/agent-denied/mcp", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for agent with no allowed backends, got %d", w.Code)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader([]byte(`not json`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Should return 200 with JSON-RPC parse error
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (JSON-RPC error), got %d", w.Code)
	}

	var resp model.Response
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil || resp.Error.Code != model.ErrCodeParseError {
		t.Errorf("expected parse error -32700, got %+v", resp.Error)
	}
}

func TestHandler_Passthrough(t *testing.T) {
	// Backend mock
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test", "version": "0.1"},
			},
		})
	}))
	defer backend.Close()

	cfg := testConfig()
	cfg.Backends["demo"] = config.BackendConfig{URL: backend.URL, Timeout: 5 * time.Second}
	cfgMgr := config.NewManagerFromConfig(cfg)

	f := NewForwarder(cfgMgr)
	sessions := NewSessionManager()
	h := NewHandler(cfgMgr, f, sessions, nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Session should be stored
	if got := sessions.Get("agent-a"); got != "sess-1" {
		t.Errorf("expected session sess-1, got %q", got)
	}
}

func TestHandler_ParsePath_ValidCases(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	agentID, backendID := h.parsePath("/agents/agent-a/mcp")
	if agentID != "agent-a" || backendID != "demo" {
		t.Errorf("expected agent-a/demo, got %s/%s", agentID, backendID)
	}
}

func TestHandler_ParsePath_InvalidCases(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	cases := []string{
		"/",
		"/agents",
		"/agents/agent-a",
		"/agents/agent-a/other",
		"/other/agent-a/mcp",
		"",
	}

	for _, path := range cases {
		agentID, backendID := h.parsePath(path)
		if agentID != "" || backendID != "" {
			t.Errorf("parsePath(%q) = %q/%q, expected empty", path, agentID, backendID)
		}
	}
}

func TestHealthCheck_AllHealthy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: backend.URL + "/mcp", HealthURL: backend.URL + "/health"},
		},
	})

	handler := HealthCheck(cfgMgr)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", result["status"])
	}
}

func TestHealthCheck_BackendDown(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:1/mcp", HealthURL: "http://127.0.0.1:1/health"},
		},
	})

	handler := HealthCheck(cfgMgr)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "degraded" {
		t.Errorf("expected status=degraded, got %v", result["status"])
	}
}

func TestHealthCheck_NoHealthURL(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:9100/mcp"},
		},
	})

	handler := HealthCheck(cfgMgr)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	backends := result["backends"].(map[string]any)
	if backends["demo"] != "no_health_url" {
		t.Errorf("expected no_health_url, got %v", backends["demo"])
	}
}

func TestNewMux_RoutesRegistered(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(testConfig())
	handler := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)
	healthHandler := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	callbackHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("cb")) })
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("api")) })

	mux := NewMux(handler, healthHandler, callbackHandler, apiHandler)

	// Verify health route
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Body.String() != "ok" {
		t.Errorf("expected ok from health, got %s", w.Body.String())
	}
}

func TestHandler_ToolsCall(t *testing.T) {
	// Backend mock that returns a tools/call result
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(model.Response{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"echoed: hello"}]}`),
		})
	}))
	defer backend.Close()

	cfg := &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: backend.URL, Timeout: 5 * time.Second},
		},
		Queue: map[string]config.QueueConfig{
			"demo": {Enabled: true, DelayMin: time.Millisecond, DelayMax: time.Millisecond, MaxPending: 10},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]config.AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
		},
		Audit: config.AuditConfig{
			DBPath:        filepath.Join(t.TempDir(), "test.db"),
			RetentionDays: 1,
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	f := NewForwarder(cfgMgr)
	sessions := NewSessionManager()
	logger, _ := audit.NewLogger(cfg.Audit.DBPath)
	t.Cleanup(func() { logger.Close() })

	acl := pipeline.NewACL(cfgMgr)
	queue := pipeline.NewFIFOQueue(cfgMgr, func(ctx context.Context, backendID string, rpcReq *model.Request, sessionID string) (*model.Response, string, error) {
		resp, sid, err := f.Forward(ctx, backendID, rpcReq, sessionID)
		return resp, sid, err
	})
	queue.Start()
	t.Cleanup(func() { queue.Stop() })

	h := NewHandler(cfgMgr, f, sessions, []pipeline.Stage{acl}, queue, logger)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"echo","arguments":{"message":"hello"}}`),
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.Response
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Errorf("expected no error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsList(t *testing.T) {
	// Backend mock that returns a tools/list result
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tools := model.ToolsListResult{
			Tools: []model.ToolInfo{
				{Name: "echo", Description: "Echo message"},
				{Name: "admin_reset", Description: "Reset data"},
			},
		}
		resultJSON, _ := json.Marshal(tools)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(model.Response{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Result:  resultJSON,
		})
	}))
	defer backend.Close()

	cfg := &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: backend.URL, Timeout: 5 * time.Second},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:      true,
						ToolDenylist: []string{"admin_reset"},
					},
				},
			},
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	f := NewForwarder(cfgMgr)
	sessions := NewSessionManager()
	h := NewHandler(cfgMgr, f, sessions, nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.Response
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}

	var result model.ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	// admin_reset should be filtered out
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool (admin_reset filtered), got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "echo" {
		t.Errorf("expected echo tool, got %s", result.Tools[0].Name)
	}
}

func TestHandler_ToolsCall_InvalidParams(t *testing.T) {
	cfg := testConfig()
	cfgMgr := config.NewManagerFromConfig(cfg)
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`"invalid"`), // not an object
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp model.Response
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil || resp.Error.Code != model.ErrCodeInvalidParams {
		t.Errorf("expected invalid params error, got %+v", resp.Error)
	}
}

func TestHandler_Auth_NoTokenConfigured(t *testing.T) {
	// Agent without auth_token — request should pass through (not get 401)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer backend.Close()

	cfg := testConfig()
	cfg.Backends["demo"] = config.BackendConfig{URL: backend.URL, Timeout: 5 * time.Second}
	cfgMgr := config.NewManagerFromConfig(cfg)
	f := NewForwarder(cfgMgr)
	h := NewHandler(cfgMgr, f, NewSessionManager(), nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("expected request to pass without auth when no auth_token configured")
	}
}

func TestHandler_Auth_MissingHeader(t *testing.T) {
	cfg := testConfig()
	cfg.Agents["agent-a"] = config.AgentConfig{
		DisplayName: "Agent A",
		AuthToken:   "secret-token-12345678",
		Backends: map[string]config.AgentBackendConfig{
			"demo": {Allowed: true},
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	// No Authorization header
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing auth header, got %d", w.Code)
	}
}

func TestHandler_Auth_WrongToken(t *testing.T) {
	cfg := testConfig()
	cfg.Agents["agent-a"] = config.AgentConfig{
		DisplayName: "Agent A",
		AuthToken:   "secret-token-12345678",
		Backends: map[string]config.AgentBackendConfig{
			"demo": {Allowed: true},
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	h := NewHandler(cfgMgr, nil, NewSessionManager(), nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-token-abcdefgh")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", w.Code)
	}
}

func TestHandler_Auth_CorrectToken(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer backend.Close()

	cfg := testConfig()
	cfg.Backends["demo"] = config.BackendConfig{URL: backend.URL, Timeout: 5 * time.Second}
	cfg.Agents["agent-a"] = config.AgentConfig{
		DisplayName: "Agent A",
		AuthToken:   "secret-token-12345678",
		Backends: map[string]config.AgentBackendConfig{
			"demo": {Allowed: true},
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	f := NewForwarder(cfgMgr)
	h := NewHandler(cfgMgr, f, NewSessionManager(), nil, nil, nil)

	body, _ := json.Marshal(model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	req := httptest.NewRequest("POST", "/agents/agent-a/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token-12345678")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("expected request to pass with correct auth token")
	}
}

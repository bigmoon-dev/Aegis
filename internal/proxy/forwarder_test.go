package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func TestForwarder_Forward_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Errorf("expected Accept header with both types")
		}

		body, _ := io.ReadAll(r.Body)
		var req model.Request
		json.Unmarshal(body, &req)

		resp := model.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"hello"}]}`),
		}

		w.Header().Set("Mcp-Session-Id", "backend-session-1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: srv.URL, Timeout: 5 * time.Second},
		},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
	}

	resp, sessionID, err := f.Forward(context.Background(), "demo", rpcReq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionID != "backend-session-1" {
		t.Errorf("expected session ID backend-session-1, got %q", sessionID)
	}
	if resp.Error != nil {
		t.Errorf("expected no error in response, got %v", resp.Error)
	}
}

func TestForwarder_Forward_WithSessionID(t *testing.T) {
	var receivedSessionID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("Mcp-Session-Id")
		resp := model.Response{JSONRPC: "2.0", ID: json.RawMessage(`1`)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: srv.URL, Timeout: 5 * time.Second},
		},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}

	_, _, err := f.Forward(context.Background(), "demo", rpcReq, "my-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedSessionID != "my-session" {
		t.Errorf("expected Mcp-Session-Id=my-session, got %q", receivedSessionID)
	}
}

func TestForwarder_Forward_UnknownBackend(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}

	_, _, err := f.Forward(context.Background(), "nonexistent", rpcReq, "")
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestForwarder_Forward_BackendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: srv.URL, Timeout: 5 * time.Second},
		},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}

	_, _, err := f.Forward(context.Background(), "demo", rpcReq, "")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestForwarder_Forward_ConnectionRefused(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: "http://127.0.0.1:1", Timeout: 2 * time.Second},
		},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}

	_, _, err := f.Forward(context.Background(), "demo", rpcReq, "")
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestForwarder_ForwardRaw_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "raw-session")
		w.Write(body) // echo back
	}))
	defer srv.Close()

	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: srv.URL, Timeout: 5 * time.Second},
		},
	})

	f := NewForwarder(cfgMgr)
	input := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)

	respBody, statusCode, sessionID, err := f.ForwardRaw(context.Background(), "demo", input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCode != 200 {
		t.Errorf("expected 200, got %d", statusCode)
	}
	if sessionID != "raw-session" {
		t.Errorf("expected raw-session, got %q", sessionID)
	}
	if string(respBody) != string(input) {
		t.Errorf("expected echo response")
	}
}

func TestForwarder_ForwardRaw_UnknownBackend(t *testing.T) {
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{},
	})

	f := NewForwarder(cfgMgr)
	_, _, _, err := f.ForwardRaw(context.Background(), "nonexistent", []byte(`{}`), "")
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestForwarder_Forward_DefaultTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := model.Response{JSONRPC: "2.0", ID: json.RawMessage(`1`)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// No Timeout set — should default to 120s (just verify it doesn't panic)
	cfgMgr := config.NewManagerFromConfig(&config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {URL: srv.URL},
		},
	})

	f := NewForwarder(cfgMgr)
	rpcReq := &model.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}

	_, _, err := f.Forward(context.Background(), "demo", rpcReq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

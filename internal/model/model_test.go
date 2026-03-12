package model

import (
	"encoding/json"
	"testing"
)

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(json.RawMessage(`1`), ErrCodeRateLimited, "rate limited")

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected 2.0, got %s", resp.JSONRPC)
	}
	if string(resp.ID) != "1" {
		t.Errorf("expected ID=1, got %s", string(resp.ID))
	}
	if resp.Error == nil {
		t.Fatal("expected error object")
	}
	if resp.Error.Code != ErrCodeRateLimited {
		t.Errorf("expected code %d, got %d", ErrCodeRateLimited, resp.Error.Code)
	}
	if resp.Error.Message != "rate limited" {
		t.Errorf("expected message 'rate limited', got %s", resp.Error.Message)
	}
}

func TestNewErrorResponse_NilID(t *testing.T) {
	resp := NewErrorResponse(nil, ErrCodeParseError, "parse error")
	if resp.ID != nil {
		t.Errorf("expected nil ID, got %s", string(resp.ID))
	}
}

func TestIsNotification_NoID(t *testing.T) {
	req := &Request{JSONRPC: "2.0", Method: "notifications/initialized"}
	if !req.IsNotification() {
		t.Error("expected true for nil ID")
	}
}

func TestIsNotification_NullID(t *testing.T) {
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage(`null`), Method: "ping"}
	if !req.IsNotification() {
		t.Error("expected true for null ID")
	}
}

func TestIsNotification_WithID(t *testing.T) {
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}
	if req.IsNotification() {
		t.Error("expected false for numeric ID")
	}
}

func TestToolsCallParams_Unmarshal(t *testing.T) {
	raw := json.RawMessage(`{"name":"publish","arguments":{"title":"test"}}`)
	var params ToolsCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if params.Name != "publish" {
		t.Errorf("expected name=publish, got %s", params.Name)
	}
}

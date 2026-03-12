package proxy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func TestEnhanceToolsList_FiltersDenylist(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:      true,
						ToolDenylist: []string{"admin_reset"},
					},
				},
			},
		},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "echo", Description: "Echo message"},
			{Name: "admin_reset", Description: "Reset data"},
			{Name: "publish", Description: "Publish post"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "demo", result)

	if len(enhanced.Tools) != 2 {
		t.Fatalf("expected 2 tools after filtering, got %d", len(enhanced.Tools))
	}
	for _, tool := range enhanced.Tools {
		if tool.Name == "admin_reset" {
			t.Error("admin_reset should have been filtered out")
		}
	}
}

func TestEnhanceToolsList_InjectsRateLimit(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
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
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "publish", Description: "Publish post"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "demo", result)
	if len(enhanced.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(enhanced.Tools))
	}

	desc := enhanced.Tools[0].Description
	if desc != "[Rate:1/1d] Publish post" {
		t.Errorf("unexpected description: %s", desc)
	}
}

func TestEnhanceToolsList_InjectsGlobalRateLimit(t *testing.T) {
	cfg := &config.Config{
		Queue: map[string]config.QueueConfig{
			"demo": {
				GlobalRateLimits: map[string]config.RateLimitConfig{
					"search": {Window: time.Hour, MaxCount: 15},
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed: true,
						RateLimits: map[string]config.RateLimitConfig{
							"search": {Window: time.Hour, MaxCount: 10},
						},
					},
				},
			},
		},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "search", Description: "Search feeds"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "demo", result)
	desc := enhanced.Tools[0].Description
	if desc != "[Rate:10/1h|GlobalRate:15/1h] Search feeds" {
		t.Errorf("unexpected description: %s", desc)
	}
}

func TestEnhanceToolsList_InjectsApprovalRequired(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
						RateLimits: map[string]config.RateLimitConfig{
							"publish": {Window: 24 * time.Hour, MaxCount: 1},
						},
					},
				},
			},
		},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "publish", Description: "Publish post"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "demo", result)
	desc := enhanced.Tools[0].Description
	if desc != "[Rate:1/1d|ApprovalRequired] Publish post" {
		t.Errorf("unexpected description: %s", desc)
	}
}

func TestEnhanceToolsList_NoAnnotationsForUnconstrainedTool(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
		},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "echo", Description: "Echo message"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "demo", result)
	if enhanced.Tools[0].Description != "Echo message" {
		t.Errorf("expected unmodified description, got %s", enhanced.Tools[0].Description)
	}
}

func TestEnhanceToolsList_UnknownAgent(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "echo", Description: "Echo message"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "unknown", "demo", result)
	// Should return original result unchanged
	if len(enhanced.Tools) != 1 || enhanced.Tools[0].Description != "Echo message" {
		t.Error("expected original result for unknown agent")
	}
}

func TestEnhanceToolsList_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{},
			},
		},
	}

	result := &model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "echo", Description: "Echo message"},
		},
	}

	enhanced := EnhanceToolsList(cfg, "agent-a", "unknown", result)
	if len(enhanced.Tools) != 1 || enhanced.Tools[0].Description != "Echo message" {
		t.Error("expected original result for unknown backend")
	}
}

func TestParseToolsListResult_Success(t *testing.T) {
	resultJSON, _ := json.Marshal(model.ToolsListResult{
		Tools: []model.ToolInfo{
			{Name: "echo", Description: "Echo"},
		},
	})

	resp := &model.Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  resultJSON,
	}

	result, err := ParseToolsListResult(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestParseToolsListResult_BackendError(t *testing.T) {
	resp := &model.Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Error:   &model.RPCError{Code: -32600, Message: "bad request"},
	}

	_, err := ParseToolsListResult(resp)
	if err == nil {
		t.Error("expected error for backend error response")
	}
}

func TestParseToolsListResult_InvalidJSON(t *testing.T) {
	resp := &model.Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  json.RawMessage(`not json`),
	}

	_, err := ParseToolsListResult(resp)
	if err == nil {
		t.Error("expected error for invalid JSON result")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{time.Minute, "1m"},
		{30 * time.Minute, "30m"},
		{time.Hour, "1h"},
		{2 * time.Hour, "2h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.dur)
		if got != tt.expect {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.dur, got, tt.expect)
		}
	}
}

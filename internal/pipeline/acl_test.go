package pipeline

import (
	"context"
	"testing"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func testConfig() *config.Config {
	return &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]config.AgentBackendConfig{
					"backend-1": {
						Allowed:      true,
						ToolDenylist: []string{"admin_reset"},
					},
					"backend-2": {
						Allowed: false,
					},
				},
			},
		},
	}
}

func TestACL_AllowedAgent(t *testing.T) {
	cfg := testConfig()
	mgr := config.NewManagerFromConfig(cfg)
	acl := NewACL(mgr)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "echo",
	}
	result, err := acl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Errorf("expected allow, got %v", result.Verdict)
	}
}

func TestACL_UnknownAgent(t *testing.T) {
	cfg := testConfig()
	mgr := config.NewManagerFromConfig(cfg)
	acl := NewACL(mgr)

	req := &model.PipelineRequest{
		AgentID:   "unknown-agent",
		BackendID: "backend-1",
		ToolName:  "echo",
	}
	result, err := acl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny for unknown agent, got %v", result.Verdict)
	}
	if result.ErrorCode != model.ErrCodeACLDenied {
		t.Errorf("expected error code %d, got %d", model.ErrCodeACLDenied, result.ErrorCode)
	}
}

func TestACL_DisallowedBackend(t *testing.T) {
	cfg := testConfig()
	mgr := config.NewManagerFromConfig(cfg)
	acl := NewACL(mgr)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-2",
		ToolName:  "echo",
	}
	result, err := acl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny for disallowed backend, got %v", result.Verdict)
	}
}

func TestACL_UnknownBackend(t *testing.T) {
	cfg := testConfig()
	mgr := config.NewManagerFromConfig(cfg)
	acl := NewACL(mgr)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "nonexistent",
		ToolName:  "echo",
	}
	result, err := acl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny for unknown backend, got %v", result.Verdict)
	}
}

func TestACL_ToolDenylist(t *testing.T) {
	cfg := testConfig()
	mgr := config.NewManagerFromConfig(cfg)
	acl := NewACL(mgr)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "admin_reset",
	}
	result, err := acl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny for denylisted tool, got %v", result.Verdict)
	}
}

func TestACL_Name(t *testing.T) {
	acl := NewACL(config.NewManagerFromConfig(&config.Config{}))
	if acl.Name() != "acl" {
		t.Errorf("expected name 'acl', got %q", acl.Name())
	}
}

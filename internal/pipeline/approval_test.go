package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func TestApprovalGate_NoApprovalRequired(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
					},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout: 2 * time.Second,
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	if gate.Name() != "approval" {
		t.Errorf("expected name=approval, got %s", gate.Name())
	}

	// echo is not in approval_required list
	result, err := gate.Process(context.Background(), &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "echo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Error("expected allow for non-approval tool")
	}
}

func TestApprovalGate_UnknownAgent(t *testing.T) {
	cfg := &config.Config{
		Agents:   map[string]config.AgentConfig{},
		Approval: config.ApprovalConfig{Timeout: 2 * time.Second},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	result, err := gate.Process(context.Background(), &model.PipelineRequest{
		AgentID:   "unknown",
		BackendID: "demo",
		ToolName:  "publish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Error("expected allow for unknown agent")
	}
}

func TestApprovalGate_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{},
			},
		},
		Approval: config.ApprovalConfig{Timeout: 2 * time.Second},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	result, err := gate.Process(context.Background(), &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "unknown",
		ToolName:  "publish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Error("expected allow for unknown backend")
	}
}

func TestApprovalGate_Approved(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
					},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout: 5 * time.Second,
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	done := make(chan *model.StageResult, 1)
	go func() {
		result, _ := gate.Process(context.Background(), &model.PipelineRequest{
			AgentID:   "agent-a",
			BackendID: "demo",
			ToolName:  "publish",
		})
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)
	pending := store.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	store.Resolve(pending[0].ID, true)

	select {
	case result := <-done:
		if result.Verdict != model.VerdictAllow {
			t.Error("expected allow after approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestApprovalGate_Rejected(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
					},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout: 5 * time.Second,
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	done := make(chan *model.StageResult, 1)
	go func() {
		result, _ := gate.Process(context.Background(), &model.PipelineRequest{
			AgentID:   "agent-a",
			BackendID: "demo",
			ToolName:  "publish",
		})
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)
	pending := store.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	store.Resolve(pending[0].ID, false)

	select {
	case result := <-done:
		if result.Verdict != model.VerdictDeny {
			t.Error("expected deny after rejection")
		}
		if result.ErrorCode != model.ErrCodeApprovalDeny {
			t.Errorf("expected error code %d, got %d", model.ErrCodeApprovalDeny, result.ErrorCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestApprovalGate_Timeout(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
					},
				},
			},
		},
		Approval: config.ApprovalConfig{
			Timeout: 100 * time.Millisecond,
		},
	}
	cfgMgr := config.NewManagerFromConfig(cfg)
	store := approval.NewStore(cfgMgr, nil)
	gate := NewApprovalGate(cfgMgr, store)

	result, err := gate.Process(context.Background(), &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "publish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Error("expected deny on timeout")
	}
}

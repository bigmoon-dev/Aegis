package pipeline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func rateLimitConfig() *config.Config {
	return &config.Config{
		Queue: map[string]config.QueueConfig{
			"backend-1": {
				Enabled:    true,
				MaxPending: 10,
				GlobalRateLimits: map[string]config.RateLimitConfig{
					"global_tool": {Window: 1 * time.Minute, MaxCount: 5},
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"backend-1": {
						Allowed: true,
						RateLimits: map[string]config.RateLimitConfig{
							"get_weather": {Window: 1 * time.Minute, MaxCount: 3},
						},
					},
				},
			},
		},
	}
}

func testLogger(t *testing.T) *audit.Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := audit.NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestRateLimiter_BelowLimit(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "get_weather",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Errorf("expected allow, got %v", result.Verdict)
	}
}

func TestRateLimiter_AtLimit(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	// Record 3 calls (max is 3)
	for i := 0; i < 3; i++ {
		logger.RecordCall("agent-a", "get_weather")
	}

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "get_weather",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny at limit, got %v", result.Verdict)
	}
	if result.ErrorCode != model.ErrCodeRateLimited {
		t.Errorf("expected error code %d, got %d", model.ErrCodeRateLimited, result.ErrorCode)
	}
}

func TestRateLimiter_NoLimitConfigured(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "echo", // no rate limit configured
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Errorf("expected allow for unconfigured tool, got %v", result.Verdict)
	}
}

func TestRateLimiter_UnknownAgent(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	req := &model.PipelineRequest{
		AgentID:   "unknown",
		BackendID: "backend-1",
		ToolName:  "get_weather",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Errorf("expected allow for unknown agent (no config = no limit), got %v", result.Verdict)
	}
}

func TestRateLimiter_GlobalLimit_Exceeded(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	// Record 5 global calls from different agents
	for i := 0; i < 5; i++ {
		logger.RecordCall("agent-"+string(rune('a'+i)), "global_tool")
	}

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "global_tool",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny for exceeded global limit, got %v", result.Verdict)
	}
}

func TestRateLimiter_GlobalLimit_BelowLimit(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	// Record 4 global calls (max is 5)
	for i := 0; i < 4; i++ {
		logger.RecordCall("agent-"+string(rune('a'+i)), "global_tool")
	}

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "global_tool",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictAllow {
		t.Errorf("expected allow below global limit, got %v", result.Verdict)
	}
}

func TestRateLimiter_DBError_FailClosed(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)

	// Create a logger and close it to simulate DB errors
	dir := t.TempDir()
	logger, err := audit.NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	logger.Close() // close DB to force errors

	rl := NewRateLimiter(mgr, logger)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "get_weather",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny on DB error (fail-closed), got %v", result.Verdict)
	}
}

func TestRateLimiter_DBError_GlobalLimit_FailClosed(t *testing.T) {
	cfg := rateLimitConfig()
	mgr := config.NewManagerFromConfig(cfg)

	dir := t.TempDir()
	logger, err := audit.NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	logger.Close()

	rl := NewRateLimiter(mgr, logger)

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "global_tool",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny on global DB error (fail-closed), got %v", result.Verdict)
	}
}

func TestRateLimiter_Name(t *testing.T) {
	rl := NewRateLimiter(config.NewManagerFromConfig(&config.Config{}), nil)
	if rl.Name() != "rate_limiter" {
		t.Errorf("expected name 'rate_limiter', got %q", rl.Name())
	}
}

func TestRateLimiter_BothGlobalAndPerAgent(t *testing.T) {
	// Tool has both global limit (5/1m) and per-agent limit (2/1m)
	cfg := &config.Config{
		Queue: map[string]config.QueueConfig{
			"backend-1": {
				Enabled:    true,
				MaxPending: 10,
				GlobalRateLimits: map[string]config.RateLimitConfig{
					"shared_tool": {Window: 1 * time.Minute, MaxCount: 5},
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"backend-1": {
						Allowed: true,
						RateLimits: map[string]config.RateLimitConfig{
							"shared_tool": {Window: 1 * time.Minute, MaxCount: 2},
						},
					},
				},
			},
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	// Record 2 calls for agent-a (hits per-agent limit of 2)
	logger.RecordCall("agent-a", "shared_tool")
	logger.RecordCall("agent-a", "shared_tool")

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "shared_tool",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Per-agent limit (2) is hit before global limit (5)
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny from per-agent limit, got %v", result.Verdict)
	}
}

func TestRateLimiter_GlobalLimitHitBeforePerAgent(t *testing.T) {
	// Global limit = 3, per-agent limit = 10
	// 3 calls from different agents → global limit hit
	cfg := &config.Config{
		Queue: map[string]config.QueueConfig{
			"backend-1": {
				Enabled:    true,
				MaxPending: 10,
				GlobalRateLimits: map[string]config.RateLimitConfig{
					"shared_tool": {Window: 1 * time.Minute, MaxCount: 3},
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"backend-1": {
						Allowed: true,
						RateLimits: map[string]config.RateLimitConfig{
							"shared_tool": {Window: 1 * time.Minute, MaxCount: 10},
						},
					},
				},
			},
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	logger := testLogger(t)
	rl := NewRateLimiter(mgr, logger)

	// 3 calls from different agents exhaust the global limit
	logger.RecordCall("agent-b", "shared_tool")
	logger.RecordCall("agent-c", "shared_tool")
	logger.RecordCall("agent-d", "shared_tool")

	req := &model.PipelineRequest{
		AgentID:   "agent-a",
		BackendID: "backend-1",
		ToolName:  "shared_tool",
	}
	result, err := rl.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != model.VerdictDeny {
		t.Errorf("expected deny from global limit, got %v", result.Verdict)
	}
}

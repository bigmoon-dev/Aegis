package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func pipelineConfig() *config.Config {
	return &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {Timeout: 5 * time.Second},
		},
		Queue: map[string]config.QueueConfig{
			"demo": {
				Enabled:    true,
				DelayMin:   1 * time.Millisecond,
				DelayMax:   2 * time.Millisecond,
				MaxPending: 10,
			},
		},
		Agents: map[string]config.AgentConfig{
			"agent-a": {
				Backends: map[string]config.AgentBackendConfig{
					"demo": {
						Allowed:      true,
						ToolDenylist: []string{"admin_reset"},
						RateLimits: map[string]config.RateLimitConfig{
							"get_weather": {Window: 1 * time.Minute, MaxCount: 2},
						},
					},
				},
			},
		},
	}
}

func pipelineLogger(t *testing.T) *audit.Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := audit.NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestExecutePipeline_AllAllow(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := pipelineLogger(t)

	acl := NewACL(mgr)
	rl := NewRateLimiter(mgr, logger)
	stages := []Stage{acl, rl}

	queue := NewFIFOQueue(mgr, echoForward)
	queue.Start()
	defer queue.Stop()

	var auditEntries []*model.AuditEntry
	auditFn := func(e *model.AuditEntry) {
		auditEntries = append(auditEntries, e)
	}
	recordFn := func(agent, tool string) {
		logger.RecordCall(agent, tool)
	}

	req := &model.PipelineRequest{
		RequestID: "req-1",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "echo",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err != nil {
		t.Fatalf("ExecutePipeline: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Error != nil {
		t.Errorf("expected no error, got %v", resp.Error)
	}
	if len(auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditEntries))
	}
	if auditEntries[0].ExecStatus != "success" {
		t.Errorf("expected exec_status=success, got %s", auditEntries[0].ExecStatus)
	}
	if auditEntries[0].ACLResult != "allowed" {
		t.Errorf("expected acl_result=allowed, got %s", auditEntries[0].ACLResult)
	}
}

func TestExecutePipeline_ACLDeny(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := pipelineLogger(t)

	acl := NewACL(mgr)
	rl := NewRateLimiter(mgr, logger)
	stages := []Stage{acl, rl}

	queue := NewFIFOQueue(mgr, echoForward)
	queue.Start()
	defer queue.Stop()

	var auditEntries []*model.AuditEntry
	auditFn := func(e *model.AuditEntry) { auditEntries = append(auditEntries, e) }
	recordFn := func(_, _ string) {}

	req := &model.PipelineRequest{
		RequestID: "req-2",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "admin_reset", // denylisted
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err != nil {
		t.Fatalf("ExecutePipeline: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Error == nil {
		t.Fatal("expected error response for ACL deny")
	}
	if resp.Error.Code != model.ErrCodeACLDenied {
		t.Errorf("expected error code %d, got %d", model.ErrCodeACLDenied, resp.Error.Code)
	}
	if len(auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditEntries))
	}
	if auditEntries[0].ACLResult != "denied" {
		t.Errorf("expected acl_result=denied, got %s", auditEntries[0].ACLResult)
	}
	if auditEntries[0].ExecStatus != "denied" {
		t.Errorf("expected exec_status=denied, got %s", auditEntries[0].ExecStatus)
	}
}

func TestExecutePipeline_RateLimitDeny(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := pipelineLogger(t)

	// Pre-fill rate limit
	logger.RecordCall("agent-a", "get_weather")
	logger.RecordCall("agent-a", "get_weather")

	acl := NewACL(mgr)
	rl := NewRateLimiter(mgr, logger)
	stages := []Stage{acl, rl}

	queue := NewFIFOQueue(mgr, echoForward)
	queue.Start()
	defer queue.Stop()

	var auditEntries []*model.AuditEntry
	auditFn := func(e *model.AuditEntry) { auditEntries = append(auditEntries, e) }
	recordFn := func(_, _ string) {}

	req := &model.PipelineRequest{
		RequestID: "req-3",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "get_weather",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err != nil {
		t.Fatalf("ExecutePipeline: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Error == nil {
		t.Fatal("expected error response for rate limit deny")
	}
	if resp.Error.Code != model.ErrCodeRateLimited {
		t.Errorf("expected error code %d, got %d", model.ErrCodeRateLimited, resp.Error.Code)
	}
	if len(auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditEntries))
	}
	if auditEntries[0].RateLimitResult != "denied" {
		t.Errorf("expected rate_limit_result=denied, got %s", auditEntries[0].RateLimitResult)
	}
}

func TestExecutePipeline_BackendErrorResponse(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := pipelineLogger(t)

	acl := NewACL(mgr)
	rl := NewRateLimiter(mgr, logger)
	stages := []Stage{acl, rl}

	// Forward returns a response with Error set (e.g., backend tool error)
	errorForward := func(_ context.Context, _ string, rpcReq *model.Request, _ string) (*model.Response, string, error) {
		return &model.Response{
			JSONRPC: "2.0",
			ID:      rpcReq.ID,
			Error:   &model.RPCError{Code: -32600, Message: "backend tool error"},
		}, "", nil
	}

	queue := NewFIFOQueue(mgr, errorForward)
	queue.Start()
	defer queue.Stop()

	var auditEntries []*model.AuditEntry
	auditFn := func(e *model.AuditEntry) { auditEntries = append(auditEntries, e) }
	var recorded []string
	recordFn := func(agent, tool string) { recorded = append(recorded, agent+"/"+tool) }

	req := &model.PipelineRequest{
		RequestID: "req-err",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "echo",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err != nil {
		t.Fatalf("ExecutePipeline: %v", err)
	}
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error response from backend")
	}
	if len(auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditEntries))
	}
	if auditEntries[0].ExecStatus != "error" {
		t.Errorf("expected exec_status=error, got %s", auditEntries[0].ExecStatus)
	}
	// Should NOT record calls for failed responses
	if len(recorded) != 0 {
		t.Errorf("expected 0 recorded calls on error response, got %d", len(recorded))
	}
}

func TestExecutePipeline_StageError(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)

	// A stage that returns a non-nil error
	errStage := &errorStage{err: fmt.Errorf("internal stage failure")}
	stages := []Stage{errStage}

	queue := NewFIFOQueue(mgr, echoForward)
	queue.Start()
	defer queue.Stop()

	var auditEntries []*model.AuditEntry
	auditFn := func(e *model.AuditEntry) { auditEntries = append(auditEntries, e) }
	recordFn := func(_, _ string) {}

	req := &model.PipelineRequest{
		RequestID: "req-stage-err",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "echo",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err == nil {
		t.Fatal("expected error from failing stage")
	}
	if resp != nil {
		t.Error("expected nil response on stage error")
	}
	if len(auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditEntries))
	}
	if auditEntries[0].ExecStatus != "error" {
		t.Errorf("expected exec_status=error, got %s", auditEntries[0].ExecStatus)
	}
}

// errorStage is a mock stage that always returns an error.
type errorStage struct {
	err error
}

func (e *errorStage) Name() string { return "error_stage" }
func (e *errorStage) Process(_ context.Context, _ *model.PipelineRequest) (*model.StageResult, error) {
	return nil, e.err
}

func TestExecutePipeline_RecordsOnSuccess(t *testing.T) {
	cfg := pipelineConfig()
	mgr := config.NewManagerFromConfig(cfg)
	logger := pipelineLogger(t)

	acl := NewACL(mgr)
	rl := NewRateLimiter(mgr, logger)
	stages := []Stage{acl, rl}

	queue := NewFIFOQueue(mgr, echoForward)
	queue.Start()
	defer queue.Stop()

	auditFn := func(e *model.AuditEntry) {}
	var recorded []string
	recordFn := func(agent, tool string) {
		recorded = append(recorded, agent+"/"+tool)
	}

	req := &model.PipelineRequest{
		RequestID: "req-4",
		AgentID:   "agent-a",
		BackendID: "demo",
		ToolName:  "get_weather",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	_, err := ExecutePipeline(context.Background(), req, stages, queue, auditFn, recordFn)
	if err != nil {
		t.Fatalf("ExecutePipeline: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(recorded))
	}
	if recorded[0] != "agent-a/get_weather" {
		t.Errorf("expected agent-a/get_weather, got %s", recorded[0])
	}
}

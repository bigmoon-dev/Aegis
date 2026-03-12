package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func queueConfig() *config.Config {
	return &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {Timeout: 5 * time.Second},
		},
		Queue: map[string]config.QueueConfig{
			"demo": {
				Enabled:     true,
				DelayMin:    10 * time.Millisecond,
				DelayMax:    20 * time.Millisecond,
				MaxPending:  5,
				BypassTools: []string{"list_posts"},
			},
		},
	}
}

func echoForward(_ context.Context, _ string, rpcReq *model.Request, _ string) (*model.Response, string, error) {
	return &model.Response{
		JSONRPC: "2.0",
		ID:      rpcReq.ID,
		Result:  json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
	}, "", nil
}

func TestQueue_Enqueue_Execute(t *testing.T) {
	cfg := queueConfig()
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()
	defer q.Stop()

	req := &model.PipelineRequest{
		AgentID:   "test-agent",
		BackendID: "demo",
		ToolName:  "echo",
		SessionID: "sess-1",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, pos, err := q.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if pos != 1 {
		t.Errorf("expected position 1, got %d", pos)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Error != nil {
		t.Errorf("expected no error in response, got %v", resp.Error)
	}
}

func TestQueue_BypassTool(t *testing.T) {
	cfg := queueConfig()
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()
	defer q.Stop()

	req := &model.PipelineRequest{
		AgentID:   "test-agent",
		BackendID: "demo",
		ToolName:  "list_posts", // bypass tool
		SessionID: "sess-1",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, pos, err := q.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if pos != 0 {
		t.Errorf("bypass tool should have position 0, got %d", pos)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestQueue_NoQueue_DirectExecute(t *testing.T) {
	cfg := &config.Config{
		Backends: map[string]config.BackendConfig{
			"demo": {Timeout: 5 * time.Second},
		},
		Queue: map[string]config.QueueConfig{}, // no queue
	}
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()
	defer q.Stop()

	req := &model.PipelineRequest{
		AgentID:   "test-agent",
		BackendID: "demo",
		ToolName:  "echo",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	resp, pos, err := q.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if pos != 0 {
		t.Errorf("no-queue should have position 0, got %d", pos)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestQueue_Full_Reject(t *testing.T) {
	cfg := queueConfig()
	cfg.Queue["demo"] = config.QueueConfig{
		Enabled:    true,
		DelayMin:   1 * time.Second,
		DelayMax:   2 * time.Second,
		MaxPending: 2,
	}
	mgr := config.NewManagerFromConfig(cfg)

	// Slow forward to fill the queue
	slowForward := func(ctx context.Context, _ string, rpcReq *model.Request, _ string) (*model.Response, string, error) {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
		return &model.Response{JSONRPC: "2.0", ID: rpcReq.ID}, "", nil
	}

	q := NewFIFOQueue(mgr, slowForward)
	q.Start()
	defer q.Stop()

	makeReq := func() *model.PipelineRequest {
		return &model.PipelineRequest{
			AgentID:   "test-agent",
			BackendID: "demo",
			ToolName:  "slow_tool",
			RPC: &model.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
			},
		}
	}

	// Fill the queue with 2 items
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _, _ = q.Enqueue(ctx, makeReq())
		}()
	}

	// Wait a bit for items to be enqueued
	time.Sleep(50 * time.Millisecond)

	// 3rd item should be rejected (max_pending=2)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, _, err := q.Enqueue(ctx, makeReq())
	if err == nil {
		t.Error("expected error for full queue")
	}

	// Cleanup: stop queue so goroutines unblock
	q.Stop()
	wg.Wait()
}

func TestQueue_Shutdown_UnblocksEnqueue(t *testing.T) {
	cfg := queueConfig()
	cfg.Queue["demo"] = config.QueueConfig{
		Enabled:    true,
		DelayMin:   10 * time.Second, // long delay
		DelayMax:   10 * time.Second,
		MaxPending: 10,
	}
	mgr := config.NewManagerFromConfig(cfg)

	slowForward := func(ctx context.Context, _ string, rpcReq *model.Request, _ string) (*model.Response, string, error) {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
		}
		return &model.Response{JSONRPC: "2.0", ID: rpcReq.ID}, "", ctx.Err()
	}

	q := NewFIFOQueue(mgr, slowForward)
	q.Start()

	done := make(chan error, 1)
	go func() {
		req := &model.PipelineRequest{
			AgentID:   "test-agent",
			BackendID: "demo",
			ToolName:  "slow",
			RPC: &model.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
			},
		}
		_, _, err := q.Enqueue(context.Background(), req)
		done <- err
	}()

	// Give time for enqueue
	time.Sleep(50 * time.Millisecond)

	// Stop should unblock the enqueue
	q.Stop()

	select {
	case <-done:
		// The first item may have completed or failed due to shutdown — both are fine.
	case <-time.After(5 * time.Second):
		t.Fatal("Enqueue not unblocked after Stop (race condition)")
	}
}

func TestQueue_ContextCancel(t *testing.T) {
	cfg := queueConfig()
	cfg.Queue["demo"] = config.QueueConfig{
		Enabled:    true,
		DelayMin:   10 * time.Second,
		DelayMax:   10 * time.Second,
		MaxPending: 10,
	}
	mgr := config.NewManagerFromConfig(cfg)

	// First item blocks the worker
	slowForward := func(ctx context.Context, _ string, rpcReq *model.Request, _ string) (*model.Response, string, error) {
		<-ctx.Done()
		return &model.Response{JSONRPC: "2.0", ID: rpcReq.ID}, "", ctx.Err()
	}

	q := NewFIFOQueue(mgr, slowForward)
	q.Start()
	defer q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := &model.PipelineRequest{
		AgentID:   "test-agent",
		BackendID: "demo",
		ToolName:  "slow",
		RPC: &model.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		},
	}

	_, _, err := q.Enqueue(ctx, req)
	if err == nil {
		// First item might complete before context timeout (executed directly)
		// That's also fine
		return
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestQueue_QueueStatus(t *testing.T) {
	cfg := queueConfig()
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()
	defer q.Stop()

	status := q.QueueStatus()
	if len(status) != 1 {
		t.Errorf("expected 1 backend in status, got %d", len(status))
	}
	if status["demo"] != 0 {
		t.Errorf("expected 0 pending for demo, got %d", status["demo"])
	}
}

func TestQueue_ConcurrentEnqueue(t *testing.T) {
	cfg := queueConfig()
	cfg.Queue["demo"] = config.QueueConfig{
		Enabled:    true,
		DelayMin:   1 * time.Millisecond,
		DelayMax:   2 * time.Millisecond,
		MaxPending: 20,
	}
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()
	defer q.Stop()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &model.PipelineRequest{
				AgentID:   "test-agent",
				BackendID: "demo",
				ToolName:  "echo",
				RPC: &model.Request{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Method:  "tools/call",
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _, err := q.Enqueue(ctx, req)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent enqueue error: %v", err)
	}
}

func TestQueue_DoubleStop(t *testing.T) {
	cfg := queueConfig()
	mgr := config.NewManagerFromConfig(cfg)
	q := NewFIFOQueue(mgr, echoForward)
	q.Start()

	// Calling Stop twice should not panic
	q.Stop()
	q.Stop()
}

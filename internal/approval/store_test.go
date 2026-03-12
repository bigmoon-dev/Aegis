package approval

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func testStore(notifier Notifier) *Store {
	cfg := &config.Config{
		Approval: config.ApprovalConfig{
			Timeout:         2 * time.Second,
			CallbackBaseURL: "http://localhost:18070",
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	return NewStore(mgr, notifier)
}

// waitForPending polls ListPending until the expected count is reached or timeout.
func waitForPending(t *testing.T, s *Store, expected int) []*PendingRequest {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		pending := s.ListPending()
		if len(pending) == expected {
			return pending
		}
		select {
		case <-deadline:
			t.Fatalf("expected %d pending, got %d", expected, len(s.ListPending()))
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestStore_GenerateAndValidateToken(t *testing.T) {
	s := testStore(nil)

	token := s.GenerateToken("req-123")
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	if !s.ValidateToken("req-123", token) {
		t.Error("expected valid token")
	}

	if s.ValidateToken("req-123", "wrong-token") {
		t.Error("expected invalid token for wrong value")
	}

	if s.ValidateToken("req-456", token) {
		t.Error("expected invalid token for different ID")
	}
}

func TestStore_GenerateToken_Deterministic(t *testing.T) {
	s := testStore(nil)
	t1 := s.GenerateToken("same-id")
	t2 := s.GenerateToken("same-id")
	if t1 != t2 {
		t.Error("same ID should produce same token")
	}
}

func TestStore_GenerateToken_UniquePerStore(t *testing.T) {
	s1 := testStore(nil)
	s2 := testStore(nil)
	t1 := s1.GenerateToken("same-id")
	t2 := s2.GenerateToken("same-id")
	if t1 == t2 {
		t.Error("different stores (different HMAC keys) should produce different tokens")
	}
}

func TestStore_RequestAndApprove(t *testing.T) {
	s := testStore(nil)

	done := make(chan bool, 1)
	go func() {
		approved, err := s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:   "agent-a",
			ToolName:  "publish",
			Arguments: `{"title":"test"}`,
		})
		if err != nil {
			t.Errorf("RequestApproval: %v", err)
		}
		done <- approved
	}()

	// Wait for the request to be registered
	pending := waitForPending(t, s, 1)

	ok := s.Resolve(pending[0].ID, true)
	if !ok {
		t.Error("Resolve returned false")
	}

	select {
	case approved := <-done:
		if !approved {
			t.Error("expected approved=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval result")
	}
}

func TestStore_RequestAndReject(t *testing.T) {
	s := testStore(nil)

	done := make(chan bool, 1)
	go func() {
		approved, err := s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
		if err != nil {
			t.Errorf("RequestApproval: %v", err)
		}
		done <- approved
	}()

	pending := waitForPending(t, s, 1)

	s.Resolve(pending[0].ID, false)

	select {
	case approved := <-done:
		if approved {
			t.Error("expected approved=false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestStore_RequestTimeout(t *testing.T) {
	cfg := &config.Config{
		Approval: config.ApprovalConfig{
			Timeout: 100 * time.Millisecond,
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	s := NewStore(mgr, nil)

	approved, err := s.RequestApproval(context.Background(), &model.PipelineRequest{
		AgentID:  "agent-a",
		ToolName: "publish",
	})
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if approved {
		t.Error("expected timeout to auto-reject (approved=false)")
	}

	// After timeout, pending should be cleaned up
	pending := s.ListPending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after timeout, got %d", len(pending))
	}
}

func TestStore_RequestContextCancel(t *testing.T) {
	s := testStore(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	approved, err := s.RequestApproval(ctx, &model.PipelineRequest{
		AgentID:  "agent-a",
		ToolName: "publish",
	})
	if err == nil {
		t.Error("expected context error")
	}
	if approved {
		t.Error("expected approved=false on context cancel")
	}
}

func TestStore_ResolveNonExistent(t *testing.T) {
	s := testStore(nil)
	ok := s.Resolve("non-existent-id", true)
	if ok {
		t.Error("expected false for non-existent request")
	}
}

func TestStore_ResolveAfterTimeout(t *testing.T) {
	cfg := &config.Config{
		Approval: config.ApprovalConfig{
			Timeout: 200 * time.Millisecond,
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	s := NewStore(mgr, nil)

	done := make(chan struct{})
	go func() {
		_, _ = s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
		close(done)
	}()

	// Poll until the request is registered
	var reqID string
	deadline := time.After(2 * time.Second)
	for {
		pending := s.ListPending()
		if len(pending) > 0 {
			reqID = pending[0].ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for request to be registered")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Wait for the approval timeout to expire
	<-done

	// Resolve after timeout should return false (already cleaned up)
	ok := s.Resolve(reqID, true)
	if ok {
		t.Error("expected false for already-timed-out request")
	}
}

func TestStore_ListPending_Empty(t *testing.T) {
	s := testStore(nil)
	pending := s.ListPending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestStore_ListPending_Multiple(t *testing.T) {
	s := testStore(nil)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.RequestApproval(context.Background(), &model.PipelineRequest{
				AgentID:  "agent-a",
				ToolName: "publish",
			})
		}()
	}

	// Poll until all 3 are registered
	deadline := time.After(2 * time.Second)
	for {
		pending := s.ListPending()
		if len(pending) == 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected 3 pending, got %d", len(s.ListPending()))
		case <-time.After(10 * time.Millisecond):
		}
	}

	pending := s.ListPending()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending, got %d", len(pending))
	}

	// Resolve all
	for _, p := range pending {
		s.Resolve(p.ID, false)
	}

	// Wait for all goroutines to finish
	wg.Wait()
}

// mockNotifier records calls for testing.
type mockNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	err   error
}

type notifyCall struct {
	reqID           string
	callbackBaseURL string
	token           string
}

func (m *mockNotifier) Notify(req *PendingRequest, callbackBaseURL string, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, notifyCall{
		reqID:           req.ID,
		callbackBaseURL: callbackBaseURL,
		token:           token,
	})
	return m.err
}

func (m *mockNotifier) getCalls() []notifyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]notifyCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

func TestStore_NotifiesDuringApproval(t *testing.T) {
	n := &mockNotifier{}
	s := testStore(n)

	go func() {
		_, _ = s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
	}()

	// Poll until notification is sent
	deadline := time.After(2 * time.Second)
	for {
		calls := n.getCalls()
		if len(calls) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected 1 notification call, got %d", len(n.getCalls()))
		case <-time.After(10 * time.Millisecond):
		}
	}

	calls := n.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification call, got %d", len(calls))
	}
	if calls[0].callbackBaseURL != "http://localhost:18070" {
		t.Errorf("unexpected callbackBaseURL: %s", calls[0].callbackBaseURL)
	}
	if calls[0].token == "" {
		t.Error("expected non-empty token in notification")
	}

	// Clean up
	pending := s.ListPending()
	for _, p := range pending {
		s.Resolve(p.ID, false)
	}
}

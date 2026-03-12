package approval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

func TestCallbackHandler_Approve(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	// Start an approval request in background
	done := make(chan bool, 1)
	go func() {
		approved, _ := s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
		done <- approved
	}()

	time.Sleep(50 * time.Millisecond)
	pending := s.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	reqID := pending[0].ID
	token := s.GenerateToken(reqID)

	req := httptest.NewRequest("GET", "/callback/approval?id="+reqID+"&action=approve&token="+token, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content-type: %s", ct)
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

func TestCallbackHandler_Reject(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	done := make(chan bool, 1)
	go func() {
		approved, _ := s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
		done <- approved
	}()

	time.Sleep(50 * time.Millisecond)
	pending := s.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	reqID := pending[0].ID
	token := s.GenerateToken(reqID)

	req := httptest.NewRequest("GET", "/callback/approval?id="+reqID+"&action=reject&token="+token, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	select {
	case approved := <-done:
		if approved {
			t.Error("expected approved=false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestCallbackHandler_MissingID(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	req := httptest.NewRequest("GET", "/callback/approval?action=approve&token=xxx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCallbackHandler_InvalidAction(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	req := httptest.NewRequest("GET", "/callback/approval?id=xxx&action=invalid&token=xxx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCallbackHandler_MissingAction(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	req := httptest.NewRequest("GET", "/callback/approval?id=xxx&token=xxx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCallbackHandler_InvalidToken(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	req := httptest.NewRequest("GET", "/callback/approval?id=req-1&action=approve&token=wrong", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCallbackHandler_MissingToken(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	req := httptest.NewRequest("GET", "/callback/approval?id=req-1&action=approve", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCallbackHandler_NonExistentRequest(t *testing.T) {
	s := testStore(nil)
	h := NewCallbackHandler(s)

	// Generate a valid token for this ID but don't create a pending request
	token := s.GenerateToken("no-such-request")

	req := httptest.NewRequest("GET", "/callback/approval?id=no-such-request&action=approve&token="+token, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCallbackHandler_AlreadyResolved(t *testing.T) {
	cfg := &config.Config{
		Approval: config.ApprovalConfig{
			Timeout:         2 * time.Second,
			CallbackBaseURL: "http://localhost:18070",
		},
	}
	mgr := config.NewManagerFromConfig(cfg)
	s := NewStore(mgr, nil)
	h := NewCallbackHandler(s)

	// Create and immediately resolve a request
	go func() {
		s.RequestApproval(context.Background(), &model.PipelineRequest{
			AgentID:  "agent-a",
			ToolName: "publish",
		})
	}()

	time.Sleep(50 * time.Millisecond)
	pending := s.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	reqID := pending[0].ID
	token := s.GenerateToken(reqID)

	// Resolve it first via store
	s.Resolve(reqID, true)
	time.Sleep(10 * time.Millisecond)

	// Now try callback — should return 404
	req := httptest.NewRequest("GET", "/callback/approval?id="+reqID+"&action=approve&token="+token, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for already-resolved request, got %d", w.Code)
	}
}

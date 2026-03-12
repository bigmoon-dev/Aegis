package approval

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- FeishuNotifier tests ---

func TestFeishuNotifier_EmptyURL_Skips(t *testing.T) {
	f := NewFeishuNotifier("")
	err := f.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err != nil {
		t.Errorf("expected nil error for empty URL, got %v", err)
	}
}

func TestFeishuNotifier_SendsCard(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFeishuNotifier(srv.URL)
	err := f.Notify(&PendingRequest{
		ID:        "req-1",
		AgentID:   "agent-a",
		ToolName:  "publish",
		Arguments: `{"title":"test"}`,
		CreatedAt: time.Now(),
	}, "http://localhost:18070", "test-token")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var card map[string]any
	if err := json.Unmarshal(receivedBody, &card); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}

	if card["msg_type"] != "interactive" {
		t.Errorf("expected msg_type=interactive, got %v", card["msg_type"])
	}

	// Verify callback URLs are in the body
	bodyStr := string(receivedBody)
	if !strings.Contains(bodyStr, "action=approve") {
		t.Error("expected approve URL in card body")
	}
	if !strings.Contains(bodyStr, "action=reject") {
		t.Error("expected reject URL in card body")
	}
	if !strings.Contains(bodyStr, "test-token") {
		t.Error("expected token in card body")
	}
}

func TestFeishuNotifier_TruncatesLongArgs(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFeishuNotifier(srv.URL)
	longArgs := strings.Repeat("x", 600)
	err := f.Notify(&PendingRequest{
		ID:        "req-1",
		AgentID:   "agent-a",
		ToolName:  "publish",
		Arguments: longArgs,
		CreatedAt: time.Now(),
	}, "http://localhost:18070", "tok")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bodyStr := string(receivedBody)
	// Should not contain the full 600 chars — truncated to 500 + "..."
	if strings.Contains(bodyStr, longArgs) {
		t.Error("expected arguments to be truncated")
	}
	if !strings.Contains(bodyStr, "...") {
		t.Error("expected truncation marker '...'")
	}
}

func TestFeishuNotifier_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewFeishuNotifier(srv.URL)
	err := f.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got: %v", err)
	}
}

func TestFeishuNotifier_ConnectionError(t *testing.T) {
	f := NewFeishuNotifier("http://127.0.0.1:1") // nothing listening
	err := f.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error for connection failure")
	}
}

// --- GenericWebhookNotifier tests ---

func TestGenericNotifier_EmptyURL_Skips(t *testing.T) {
	g := NewGenericWebhookNotifier("")
	err := g.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err != nil {
		t.Errorf("expected nil error for empty URL, got %v", err)
	}
}

func TestGenericNotifier_SendsPayload(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	g := NewGenericWebhookNotifier(srv.URL)
	err := g.Notify(&PendingRequest{
		ID:        "req-123",
		AgentID:   "agent-a",
		ToolName:  "publish",
		Arguments: `{"title":"test"}`,
		CreatedAt: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC),
	}, "http://localhost:18070", "test-token")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected application/json, got %s", receivedContentType)
	}

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload["event"] != "approval_request" {
		t.Errorf("expected event=approval_request, got %v", payload["event"])
	}
	if payload["id"] != "req-123" {
		t.Errorf("expected id=req-123, got %v", payload["id"])
	}
	if payload["agent_id"] != "agent-a" {
		t.Errorf("expected agent_id=agent-a, got %v", payload["agent_id"])
	}
	if payload["tool_name"] != "publish" {
		t.Errorf("expected tool_name=publish, got %v", payload["tool_name"])
	}

	approveURL, _ := payload["approve_url"].(string)
	if !strings.Contains(approveURL, "action=approve") || !strings.Contains(approveURL, "test-token") {
		t.Errorf("unexpected approve_url: %s", approveURL)
	}
	rejectURL, _ := payload["reject_url"].(string)
	if !strings.Contains(rejectURL, "action=reject") || !strings.Contains(rejectURL, "test-token") {
		t.Errorf("unexpected reject_url: %s", rejectURL)
	}
}

func TestGenericNotifier_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	g := NewGenericWebhookNotifier(srv.URL)
	err := g.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error for 503 response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention status 503, got: %v", err)
	}
}

func TestGenericNotifier_ConnectionError(t *testing.T) {
	g := NewGenericWebhookNotifier("http://127.0.0.1:1")
	err := g.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error for connection failure")
	}
}

// --- MultiNotifier tests ---

func TestMultiNotifier_AllSucceed(t *testing.T) {
	n1 := &mockNotifier{}
	n2 := &mockNotifier{}
	multi := NewMultiNotifier(n1, n2)

	err := multi.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	if len(n1.getCalls()) != 1 {
		t.Errorf("expected 1 call to n1, got %d", len(n1.getCalls()))
	}
	if len(n2.getCalls()) != 1 {
		t.Errorf("expected 1 call to n2, got %d", len(n2.getCalls()))
	}
}

func TestMultiNotifier_OneFailsStillCallsOthers(t *testing.T) {
	n1 := &mockNotifier{err: errors.New("feishu down")}
	n2 := &mockNotifier{}
	multi := NewMultiNotifier(n1, n2)

	err := multi.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error when one notifier fails")
	}
	if !strings.Contains(err.Error(), "feishu down") {
		t.Errorf("expected error message to contain 'feishu down', got: %v", err)
	}

	// n2 should still have been called
	if len(n2.getCalls()) != 1 {
		t.Errorf("expected n2 to be called despite n1 failure, got %d calls", len(n2.getCalls()))
	}
}

func TestMultiNotifier_AllFail(t *testing.T) {
	n1 := &mockNotifier{err: errors.New("err1")}
	n2 := &mockNotifier{err: errors.New("err2")}
	multi := NewMultiNotifier(n1, n2)

	err := multi.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err == nil {
		t.Error("expected error when all notifiers fail")
	}
	if !strings.Contains(err.Error(), "err1") || !strings.Contains(err.Error(), "err2") {
		t.Errorf("expected both error messages, got: %v", err)
	}
}

func TestMultiNotifier_Empty(t *testing.T) {
	multi := NewMultiNotifier()
	err := multi.Notify(&PendingRequest{
		ID:       "req-1",
		AgentID:  "agent-a",
		ToolName: "publish",
	}, "http://localhost:18070", "tok")

	if err != nil {
		t.Errorf("expected nil error for empty notifier list, got %v", err)
	}
}

package audit

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/model"
)

func tempLogger(t *testing.T) *Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestNewLogger_InvalidPath(t *testing.T) {
	_, err := NewLogger("/nonexistent/dir/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestLog_WriteAndQuery(t *testing.T) {
	l := tempLogger(t)

	l.Log(&model.AuditEntry{
		RequestID: "req-1",
		AgentID:   "agent-a",
		BackendID: "backend-b",
		ToolName:  "echo",
		ExecStatus: "success",
	})

	rows, err := l.Query(10, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].RequestID != "req-1" {
		t.Errorf("expected request_id=req-1, got %s", rows[0].RequestID)
	}
	if rows[0].AgentID != "agent-a" {
		t.Errorf("expected agent_id=agent-a, got %s", rows[0].AgentID)
	}
	if rows[0].ToolName != "echo" {
		t.Errorf("expected tool_name=echo, got %s", rows[0].ToolName)
	}
}

func TestRecordCall_And_CountCalls(t *testing.T) {
	l := tempLogger(t)

	l.RecordCall("agent-a", "get_weather")
	l.RecordCall("agent-a", "get_weather")
	l.RecordCall("agent-a", "echo")
	l.RecordCall("agent-b", "get_weather")

	count, err := l.CountCalls("agent-a", "get_weather", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("CountCalls: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 calls for agent-a/get_weather, got %d", count)
	}

	count, err = l.CountCalls("agent-a", "echo", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("CountCalls: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 call for agent-a/echo, got %d", count)
	}

	count, err = l.CountCalls("agent-b", "get_weather", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("CountCalls: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 call for agent-b/get_weather, got %d", count)
	}
}

func TestCountCalls_WindowFilter(t *testing.T) {
	l := tempLogger(t)

	l.RecordCall("agent-a", "tool-x")

	// Query with "since" in the future should return 0
	count, err := l.CountCalls("agent-a", "tool-x", time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("CountCalls: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 calls with future window, got %d", count)
	}
}

func TestCountCallsGlobal(t *testing.T) {
	l := tempLogger(t)

	l.RecordCall("agent-a", "get_weather")
	l.RecordCall("agent-b", "get_weather")
	l.RecordCall("agent-c", "get_weather")

	count, err := l.CountCallsGlobal("get_weather", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("CountCallsGlobal: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 global calls, got %d", count)
	}
}

func TestCountCalls_AfterClose_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	l.Close()

	_, err = l.CountCalls("agent-a", "tool", time.Now().Add(-1*time.Minute))
	if err == nil {
		t.Error("expected error after Close")
	}

	_, err = l.CountCallsGlobal("tool", time.Now().Add(-1*time.Minute))
	if err == nil {
		t.Error("expected error after Close for global")
	}
}

func TestPurge(t *testing.T) {
	l := tempLogger(t)

	l.Log(&model.AuditEntry{
		RequestID:  "req-1",
		AgentID:    "agent-a",
		ExecStatus: "success",
	})

	// Purge with 0 days: entries created at "now" are NOT strictly older than now,
	// so they should NOT be purged (correct behavior: < not <=).
	n, err := l.Purge(0)
	if err != nil {
		t.Fatalf("Purge(0): %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 purged (entry is not older than now), got %d", n)
	}

	rows, _ := l.Query(10, 0)
	if len(rows) != 1 {
		t.Errorf("expected 1 row after purge(0), got %d", len(rows))
	}

	// Purge with a large retention (365 days) should also not remove fresh entries
	n, err = l.Purge(365)
	if err != nil {
		t.Fatalf("Purge(365): %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 purged with 365d retention, got %d", n)
	}
}

func TestQuery_EmptyDB(t *testing.T) {
	l := tempLogger(t)

	rows, err := l.Query(10, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestQuery_Pagination(t *testing.T) {
	l := tempLogger(t)

	for i := 0; i < 5; i++ {
		l.Log(&model.AuditEntry{
			RequestID:  fmt.Sprintf("pag-%d", i),
			AgentID:    "agent-a",
			ExecStatus: "success",
		})
	}

	rows, err := l.Query(2, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows with limit=2, got %d", len(rows))
	}
}

package proxy

import (
	"testing"
)

func TestSessionManager_GetSet(t *testing.T) {
	sm := NewSessionManager()

	// Get on empty returns ""
	if got := sm.Get("agent-a"); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	sm.Set("agent-a", "session-1")
	if got := sm.Get("agent-a"); got != "session-1" {
		t.Errorf("expected session-1, got %q", got)
	}

	// Overwrite
	sm.Set("agent-a", "session-2")
	if got := sm.Get("agent-a"); got != "session-2" {
		t.Errorf("expected session-2, got %q", got)
	}

	// Different agent
	if got := sm.Get("agent-b"); got != "" {
		t.Errorf("expected empty string for agent-b, got %q", got)
	}
}

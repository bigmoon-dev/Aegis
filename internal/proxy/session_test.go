package proxy

import (
	"sync"
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

func TestSessionManager_Concurrent(t *testing.T) {
	sm := NewSessionManager()
	var wg sync.WaitGroup

	// Concurrent writes and reads should not race
	for i := 0; i < 50; i++ {
		wg.Add(2)
		agent := "agent-" + string(rune('a'+i%5))
		go func(id string) {
			defer wg.Done()
			sm.Set(id, "session-"+id)
		}(agent)
		go func(id string) {
			defer wg.Done()
			sm.Get(id)
		}(agent)
	}

	wg.Wait()
}

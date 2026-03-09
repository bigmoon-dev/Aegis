package proxy

import (
	"sync"
)

// SessionManager tracks MCP sessions (mcp-session-id) per agent.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]string // agentID -> backend session ID
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]string),
	}
}

func (sm *SessionManager) Get(agentID string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[agentID]
}

func (sm *SessionManager) Set(agentID, sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[agentID] = sessionID
}

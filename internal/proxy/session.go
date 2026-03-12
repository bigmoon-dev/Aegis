package proxy

import (
	"sync"
)

// SessionManager tracks MCP sessions (mcp-session-id) per agent.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]string // agentID -> backend session ID
}

// NewSessionManager creates a session manager for tracking MCP sessions.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]string),
	}
}

// Get returns the backend session ID for an agent, or empty string if none.
func (sm *SessionManager) Get(agentID string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[agentID]
}

// Set stores the backend session ID for an agent.
func (sm *SessionManager) Set(agentID, sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[agentID] = sessionID
}

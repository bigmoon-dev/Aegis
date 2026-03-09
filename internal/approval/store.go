package approval

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
	"github.com/google/uuid"
)

// PendingRequest represents a request awaiting human approval.
type PendingRequest struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	ToolName  string    `json:"tool_name"`
	Arguments string    `json:"arguments"`
	CreatedAt time.Time `json:"created_at"`
	done      chan bool
}

// Store manages pending approval requests.
type Store struct {
	cfgMgr   *config.Manager
	notifier Notifier
	hmacKey  []byte // random key for signing callback tokens

	mu       sync.Mutex
	pending  map[string]*PendingRequest
}

// Notifier sends approval notifications (e.g., Feishu webhook).
type Notifier interface {
	Notify(req *PendingRequest, callbackBaseURL string, token string) error
}

func NewStore(cfgMgr *config.Manager, notifier Notifier) *Store {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Fatalf("[approval] generate HMAC key: %v", err)
	}
	return &Store{
		cfgMgr:  cfgMgr,
		notifier: notifier,
		hmacKey: key,
		pending: make(map[string]*PendingRequest),
	}
}

// GenerateToken creates an HMAC-SHA256 token for a request ID.
func (s *Store) GenerateToken(id string) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateToken checks the HMAC token for a request ID.
func (s *Store) ValidateToken(id, token string) bool {
	expected := s.GenerateToken(id)
	return hmac.Equal([]byte(expected), []byte(token))
}

// RequestApproval creates a pending request, sends notification, and blocks until approved/rejected/timeout.
func (s *Store) RequestApproval(ctx context.Context, req *model.PipelineRequest) (bool, error) {
	cfg := s.cfgMgr.Get()

	pr := &PendingRequest{
		ID:        uuid.New().String(),
		AgentID:   req.AgentID,
		ToolName:  req.ToolName,
		Arguments: req.Arguments,
		CreatedAt: time.Now().UTC(),
		done:      make(chan bool, 1),
	}

	s.mu.Lock()
	s.pending[pr.ID] = pr
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, pr.ID)
		s.mu.Unlock()
	}()

	// Send notification with HMAC token
	token := s.GenerateToken(pr.ID)
	if s.notifier != nil {
		if err := s.notifier.Notify(pr, cfg.Approval.CallbackBaseURL, token); err != nil {
			log.Printf("[approval] notification error: %v", err)
			// Continue waiting — admin can use API to approve
		}
	}

	timeout := cfg.Approval.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case approved := <-pr.done:
		return approved, nil
	case <-timer.C:
		log.Printf("[approval] request %s timed out after %s", pr.ID, timeout)
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Resolve approves or rejects a pending request.
// Returns false if the request was not found or the requester already timed out.
func (s *Store) Resolve(id string, approved bool) bool {
	s.mu.Lock()
	pr, ok := s.pending[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.pending, id)
	s.mu.Unlock()

	// Non-blocking send: if the requester already timed out and stopped
	// listening, the channel buffer (cap 1) may be unread but we won't block.
	select {
	case pr.done <- approved:
		return true
	default:
		// Requester already gone (timed out or context cancelled)
		return false
	}
}

// ListPending returns all pending approval requests.
func (s *Store) ListPending() []*PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*PendingRequest, 0, len(s.pending))
	for _, pr := range s.pending {
		result = append(result, pr)
	}
	return result
}

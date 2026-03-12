package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

// ForwardFunc sends a JSON-RPC request to the backend and returns the response.
type ForwardFunc func(ctx context.Context, backendID string, rpcReq *model.Request, sessionID string) (*model.Response, string, error)

// queueItem wraps a pipeline request with a channel to deliver the result.
type queueItem struct {
	req    *model.PipelineRequest
	result chan queueResult
}

type queueResult struct {
	resp *model.Response
	err  error
}

// FIFOQueue serializes tool calls per backend with random delays.
type FIFOQueue struct {
	cfgMgr  *config.Manager
	forward ForwardFunc

	mu      sync.Mutex
	queues  map[string]chan *queueItem // per-backend channels
	lengths map[string]int             // current queue length per backend
	stopChs map[string]chan struct{}   // per-backend stop signals
	stopped bool                       // prevents double-close
	wg      sync.WaitGroup             // tracks active workers
}

func NewFIFOQueue(cfgMgr *config.Manager, forward ForwardFunc) *FIFOQueue {
	return &FIFOQueue{
		cfgMgr:  cfgMgr,
		forward: forward,
		queues:  make(map[string]chan *queueItem),
		lengths: make(map[string]int),
		stopChs: make(map[string]chan struct{}),
	}
}

// Start initializes queue workers for all configured backends.
func (q *FIFOQueue) Start() {
	cfg := q.cfgMgr.Get()
	for backendID, qCfg := range cfg.Queue {
		if !qCfg.Enabled {
			continue
		}
		ch := make(chan *queueItem, qCfg.MaxPending)
		stopCh := make(chan struct{})
		q.queues[backendID] = ch
		q.lengths[backendID] = 0
		q.stopChs[backendID] = stopCh
		q.wg.Add(1)
		go q.worker(backendID, ch, stopCh)
		log.Printf("[queue] started worker for backend %q (delay %s-%s)",
			backendID, qCfg.DelayMin, qCfg.DelayMax)
	}
}

// Enqueue adds a request to the backend's FIFO queue and blocks until execution.
// Returns the backend response or an error if the queue is full/context cancelled.
func (q *FIFOQueue) Enqueue(ctx context.Context, req *model.PipelineRequest) (*model.Response, int, error) {
	cfg := q.cfgMgr.Get()

	// Check if this tool bypasses the queue
	if qCfg, ok := cfg.Queue[req.BackendID]; ok {
		if slices.Contains(qCfg.BypassTools, req.ToolName) {
			log.Printf("[queue] %s bypasses queue, executing directly", req.ToolName)
			resp, _, err := q.forward(ctx, req.BackendID, req.RPC, req.SessionID)
			return resp, 0, err
		}
	}

	ch, ok := q.queues[req.BackendID]
	if !ok {
		// No queue configured for this backend, execute directly
		resp, _, err := q.forward(ctx, req.BackendID, req.RPC, req.SessionID)
		return resp, 0, err
	}

	stopCh := q.stopChs[req.BackendID]

	q.mu.Lock()
	pos := q.lengths[req.BackendID] + 1
	qCfg := cfg.Queue[req.BackendID]

	// Reject if queue is full
	if pos > qCfg.MaxPending {
		q.mu.Unlock()
		return nil, pos, fmt.Errorf("queue full (%d pending), please retry later", pos-1)
	}

	// Estimate wait time and reject if too long (> 5 minutes)
	estimatedWait := time.Duration(pos-1) * (qCfg.DelayMin + qCfg.DelayMax) / 2
	if estimatedWait > 5*time.Minute {
		q.mu.Unlock()
		return nil, pos, fmt.Errorf("queue position %d (est. wait %s), please retry later", pos, estimatedWait.Round(time.Second))
	}

	q.lengths[req.BackendID]++
	q.mu.Unlock()

	item := &queueItem{
		req:    req,
		result: make(chan queueResult, 1),
	}

	select {
	case ch <- item:
		log.Printf("[queue] enqueued %s for %s (position %d)", req.ToolName, req.BackendID, pos)
	case <-stopCh:
		q.mu.Lock()
		q.lengths[req.BackendID]--
		q.mu.Unlock()
		return nil, pos, fmt.Errorf("queue shutting down")
	case <-ctx.Done():
		q.mu.Lock()
		q.lengths[req.BackendID]--
		q.mu.Unlock()
		return nil, pos, ctx.Err()
	}

	select {
	case res := <-item.result:
		return res.resp, pos, res.err
	case <-stopCh:
		return nil, pos, fmt.Errorf("queue shutting down")
	case <-ctx.Done():
		return nil, pos, ctx.Err()
	}
}

// QueueStatus returns the current pending count per backend.
func (q *FIFOQueue) QueueStatus() map[string]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	status := make(map[string]int, len(q.lengths))
	for k, v := range q.lengths {
		status[k] = v
	}
	return status
}

func (q *FIFOQueue) worker(backendID string, ch chan *queueItem, stopCh chan struct{}) {
	defer q.wg.Done()
	var lastExecTime time.Time

	for {
		var item *queueItem
		select {
		case it, ok := <-ch:
			if !ok {
				return
			}
			item = it
		case <-stopCh:
			// Drain remaining items: return errors so callers unblock
			q.drainChannel(backendID, ch)
			return
		}

		// Delay only if we executed something recently.
		// If lastExecTime is zero (first item) or the queue was idle
		// longer than delay_max, skip the delay.
		if !lastExecTime.IsZero() {
			elapsed := time.Since(lastExecTime)
			delay := q.randomDelay(backendID)
			remaining := delay - elapsed
			if remaining > 0 {
				log.Printf("[queue] %s: waiting %s before next operation", backendID, remaining.Round(time.Second))
				delayTimer := time.NewTimer(remaining)
				select {
				case <-delayTimer.C:
				case <-stopCh:
					delayTimer.Stop()
					// Execute current item then drain remaining
					q.executeItem(backendID, item)
					q.drainChannel(backendID, ch)
					return
				}
				delayTimer.Stop()
			}
		}

		q.executeItem(backendID, item)
		lastExecTime = time.Now()
	}
}

func (q *FIFOQueue) executeItem(backendID string, item *queueItem) {
	log.Printf("[queue] %s: executing %s for agent %s",
		backendID, item.req.ToolName, item.req.AgentID)

	// Use a context with the backend's configured timeout (not Background)
	cfg := q.cfgMgr.Get()
	timeout := 120 * time.Second
	if bc, ok := cfg.Backends[backendID]; ok && bc.Timeout > 0 {
		timeout = bc.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	resp, _, err := q.forward(ctx, backendID, item.req.RPC, item.req.SessionID)
	cancel()

	q.mu.Lock()
	q.lengths[backendID]--
	q.mu.Unlock()

	item.result <- queueResult{resp: resp, err: err}
}

// drainChannel empties remaining items from the channel, returning errors to callers.
func (q *FIFOQueue) drainChannel(backendID string, ch chan *queueItem) {
	for {
		select {
		case item, ok := <-ch:
			if !ok {
				return
			}
			log.Printf("[queue] %s: draining %s (shutdown)", backendID, item.req.ToolName)
			q.mu.Lock()
			q.lengths[backendID]--
			q.mu.Unlock()
			item.result <- queueResult{err: fmt.Errorf("queue shutting down")}
		default:
			return
		}
	}
}

// Stop gracefully shuts down all queue workers and waits for them to finish.
func (q *FIFOQueue) Stop() {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return
	}
	q.stopped = true
	q.mu.Unlock()

	for backendID, stopCh := range q.stopChs {
		log.Printf("[queue] stopping worker for backend %q", backendID)
		close(stopCh)
	}
	q.wg.Wait()
	log.Printf("[queue] all workers stopped")
}

func (q *FIFOQueue) randomDelay(backendID string) time.Duration {
	cfg := q.cfgMgr.Get()
	qCfg, ok := cfg.Queue[backendID]
	if !ok {
		return 60 * time.Second
	}
	minMs := qCfg.DelayMin.Milliseconds()
	maxMs := qCfg.DelayMax.Milliseconds()
	if maxMs <= minMs {
		return qCfg.DelayMin
	}
	delayMs := minMs + rand.Int64N(maxMs-minMs)
	return time.Duration(delayMs) * time.Millisecond
}

// ExecutePipeline runs a tools/call request through ACL → RateLimit → Approval → Queue → Forward → Audit.
func ExecutePipeline(
	ctx context.Context,
	req *model.PipelineRequest,
	stages []Stage,
	queue *FIFOQueue,
	auditFn func(*model.AuditEntry),
	rateLimitRecordFn func(agentID, toolName string),
) (*model.Response, error) {
	entry := &model.AuditEntry{
		RequestID: req.RequestID,
		AgentID:   req.AgentID,
		BackendID: req.BackendID,
		ToolName:  req.ToolName,
		Arguments: req.Arguments,
	}
	start := time.Now()
	defer func() {
		entry.ExecDurationMs = time.Since(start).Milliseconds()
		auditFn(entry)
	}()

	// Run through stages: ACL, RateLimit, Approval
	for _, stage := range stages {
		result, err := stage.Process(ctx, req)
		if err != nil {
			entry.ExecStatus = "error"
			entry.ErrorMessage = err.Error()
			return nil, err
		}

		switch stage.Name() {
		case "acl":
			if result.Verdict == model.VerdictDeny {
				entry.ACLResult = "denied"
			} else {
				entry.ACLResult = "allowed"
			}
		case "rate_limiter":
			if result.Verdict == model.VerdictDeny {
				entry.RateLimitResult = "denied"
			} else {
				entry.RateLimitResult = "allowed"
			}
		case "approval":
			if result.Verdict == model.VerdictDeny {
				entry.ApprovalResult = "rejected"
			} else {
				entry.ApprovalResult = "approved"
			}
		}

		if result.Verdict == model.VerdictDeny {
			entry.ExecStatus = "denied"
			entry.ErrorMessage = result.ErrorMessage
			return model.NewErrorResponse(req.RPC.ID, result.ErrorCode, result.ErrorMessage), nil
		}
	}

	// Enqueue and execute
	queueStart := time.Now()
	resp, pos, err := queue.Enqueue(ctx, req)
	entry.QueuePosition = pos
	entry.QueueWaitMs = time.Since(queueStart).Milliseconds()

	if err != nil {
		entry.ExecStatus = "error"
		entry.ErrorMessage = err.Error()
		errResp := model.NewErrorResponse(req.RPC.ID, model.ErrCodeQueueFull, err.Error())
		return errResp, nil
	}

	if resp.Error != nil {
		entry.ExecStatus = "error"
		errMsg, _ := json.Marshal(resp.Error)
		entry.ErrorMessage = string(errMsg)
	} else {
		entry.ExecStatus = "success"
		// Only count successful calls against rate limits
		rateLimitRecordFn(req.AgentID, req.ToolName)
	}

	return resp, nil
}

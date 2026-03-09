package model

import "time"

// PipelineRequest carries all context through the pipeline stages.
type PipelineRequest struct {
	RequestID string
	AgentID   string
	BackendID string
	ToolName  string
	Arguments string // raw JSON string
	SessionID string // MCP session ID for backend
	RPC       *Request
	CreatedAt time.Time
}

// AuditEntry records the outcome of a pipeline execution.
type AuditEntry struct {
	RequestID       string
	AgentID         string
	BackendID       string
	ToolName        string
	Arguments       string
	ACLResult       string
	RateLimitResult string
	ApprovalResult  string
	QueuePosition   int
	QueueWaitMs     int64
	ExecStatus      string // success|error|denied|rate_limited|rejected
	ExecDurationMs  int64
	ErrorMessage    string
}

// Verdict represents the outcome of a pipeline stage.
type Verdict int

const (
	VerdictAllow    Verdict = iota // proceed to next stage
	VerdictDeny                    // reject the request
	VerdictPending                 // waiting for async action (approval)
)

// StageResult carries the verdict and optional message from a pipeline stage.
type StageResult struct {
	Verdict      Verdict
	ErrorCode    int
	ErrorMessage string
}

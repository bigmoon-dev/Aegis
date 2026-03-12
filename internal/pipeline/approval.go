package pipeline

import (
	"context"
	"log"
	"slices"

	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

// ApprovalGate blocks destructive operations until human approval.
type ApprovalGate struct {
	cfgMgr *config.Manager
	store  *approval.Store
}

// NewApprovalGate creates an approval gate stage.
func NewApprovalGate(cfgMgr *config.Manager, store *approval.Store) *ApprovalGate {
	return &ApprovalGate{cfgMgr: cfgMgr, store: store}
}

// Name returns the stage name for audit logging.
func (a *ApprovalGate) Name() string { return "approval" }

// Process blocks until the request is approved, rejected, or times out.
func (a *ApprovalGate) Process(ctx context.Context, req *model.PipelineRequest) (*model.StageResult, error) {
	cfg := a.cfgMgr.Get()

	agentCfg, ok := cfg.Agents[req.AgentID]
	if !ok {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}
	backendCfg, ok := agentCfg.Backends[req.BackendID]
	if !ok {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}

	if !slices.Contains(backendCfg.ApprovalRequired, req.ToolName) {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}

	log.Printf("[approval] agent %q requesting approval for %s", req.AgentID, req.ToolName)

	approved, err := a.store.RequestApproval(ctx, req)
	if err != nil {
		return nil, err
	}

	if !approved {
		log.Printf("[approval] agent %q: %s rejected/timed out", req.AgentID, req.ToolName)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeApprovalDeny,
			ErrorMessage: "approval denied or timed out for " + req.ToolName,
		}, nil
	}

	log.Printf("[approval] agent %q: %s approved", req.AgentID, req.ToolName)
	return &model.StageResult{Verdict: model.VerdictAllow}, nil
}

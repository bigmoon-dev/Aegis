package pipeline

import (
	"context"
	"log"
	"slices"

	"github.com/bigmoon-dev/agent-harness/internal/config"
	"github.com/bigmoon-dev/agent-harness/internal/model"
)

// ACL checks whether an agent is allowed to call a specific tool on a backend.
type ACL struct {
	cfgMgr *config.Manager
}

func NewACL(cfgMgr *config.Manager) *ACL {
	return &ACL{cfgMgr: cfgMgr}
}

func (a *ACL) Name() string { return "acl" }

func (a *ACL) Process(_ context.Context, req *model.PipelineRequest) (*model.StageResult, error) {
	cfg := a.cfgMgr.Get()

	agentCfg, ok := cfg.Agents[req.AgentID]
	if !ok {
		log.Printf("[acl] unknown agent %q, denying", req.AgentID)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeACLDenied,
			ErrorMessage: "unknown agent: " + req.AgentID,
		}, nil
	}

	backendCfg, ok := agentCfg.Backends[req.BackendID]
	if !ok || !backendCfg.Allowed {
		log.Printf("[acl] agent %q denied access to backend %q", req.AgentID, req.BackendID)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeACLDenied,
			ErrorMessage: "agent " + req.AgentID + " is not allowed to access backend " + req.BackendID,
		}, nil
	}

	if slices.Contains(backendCfg.ToolDenylist, req.ToolName) {
		log.Printf("[acl] agent %q denied tool %q (denylist)", req.AgentID, req.ToolName)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeACLDenied,
			ErrorMessage: "tool " + req.ToolName + " is denied for agent " + req.AgentID,
		}, nil
	}

	return &model.StageResult{Verdict: model.VerdictAllow}, nil
}

package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

// RateLimiter enforces per-agent per-tool and global per-tool sliding window rate limits.
type RateLimiter struct {
	cfgMgr *config.Manager
	logger *audit.Logger
}

// NewRateLimiter creates a rate limiter stage using audit log counts.
func NewRateLimiter(cfgMgr *config.Manager, logger *audit.Logger) *RateLimiter {
	return &RateLimiter{cfgMgr: cfgMgr, logger: logger}
}

// Name returns the stage name for audit logging.
func (r *RateLimiter) Name() string { return "rate_limiter" }

// Process checks global and per-agent sliding window rate limits.
func (r *RateLimiter) Process(_ context.Context, req *model.PipelineRequest) (*model.StageResult, error) {
	cfg := r.cfgMgr.Get()

	// 1. Check global rate limits (per-backend, across all agents)
	if qCfg, ok := cfg.Queue[req.BackendID]; ok {
		if gl, ok := qCfg.GlobalRateLimits[req.ToolName]; ok {
			since := time.Now().Add(-gl.Window)
			count, err := r.logger.CountCallsGlobal(req.ToolName, since)
			if err != nil {
				log.Printf("[rate_limiter] global: DB error (fail-closed): %v", err)
				return &model.StageResult{
					Verdict:      model.VerdictDeny,
					ErrorCode:    model.ErrCodeRateLimited,
					ErrorMessage: "rate limiter unavailable, please retry later",
				}, nil
			}
			if count >= gl.MaxCount {
				msg := fmt.Sprintf("global rate limit exceeded for %s: %d/%d calls in %s window",
					req.ToolName, count, gl.MaxCount, gl.Window)
				log.Printf("[rate_limiter] global: %s", msg)
				return &model.StageResult{
					Verdict:      model.VerdictDeny,
					ErrorCode:    model.ErrCodeRateLimited,
					ErrorMessage: msg,
				}, nil
			}
		}
	}

	// 2. Check per-agent rate limits
	agentCfg, ok := cfg.Agents[req.AgentID]
	if !ok {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}
	backendCfg, ok := agentCfg.Backends[req.BackendID]
	if !ok {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}

	rl, ok := backendCfg.RateLimits[req.ToolName]
	if !ok {
		return &model.StageResult{Verdict: model.VerdictAllow}, nil
	}

	since := time.Now().Add(-rl.Window)
	count, err := r.logger.CountCalls(req.AgentID, req.ToolName, since)
	if err != nil {
		log.Printf("[rate_limiter] agent %q: DB error (fail-closed): %v", req.AgentID, err)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeRateLimited,
			ErrorMessage: "rate limiter unavailable, please retry later",
		}, nil
	}

	if count >= rl.MaxCount {
		msg := fmt.Sprintf("rate limit exceeded for %s: %d/%d calls in %s window",
			req.ToolName, count, rl.MaxCount, rl.Window)
		log.Printf("[rate_limiter] agent %q: %s", req.AgentID, msg)
		return &model.StageResult{
			Verdict:      model.VerdictDeny,
			ErrorCode:    model.ErrCodeRateLimited,
			ErrorMessage: msg,
		}, nil
	}

	return &model.StageResult{Verdict: model.VerdictAllow}, nil
}

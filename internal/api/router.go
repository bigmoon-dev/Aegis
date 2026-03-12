package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bigmoon-dev/aegis/internal/approval"
	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
)

// Router handles management REST API requests.
type Router struct {
	cfgMgr        *config.Manager
	queue         *pipeline.FIFOQueue
	approvalStore *approval.Store
	auditLog      *audit.Logger
	mux           *http.ServeMux
}

func NewRouter(
	cfgMgr *config.Manager,
	queue *pipeline.FIFOQueue,
	approvalStore *approval.Store,
	auditLog *audit.Logger,
) *Router {
	r := &Router{
		cfgMgr:        cfgMgr,
		queue:         queue,
		approvalStore: approvalStore,
		auditLog:      auditLog,
		mux:           http.NewServeMux(),
	}
	r.mux.HandleFunc("/api/v1/queue/status", r.queueStatus)
	r.mux.HandleFunc("/api/v1/agents", r.listAgents)
	r.mux.HandleFunc("/api/v1/agents/", r.agentRateLimits)
	r.mux.HandleFunc("/api/v1/approvals/pending", r.pendingApprovals)
	r.mux.HandleFunc("/api/v1/approvals/", r.handleApprovalAction)
	r.mux.HandleFunc("/api/v1/audit/logs", r.auditLogs)
	r.mux.HandleFunc("/api/v1/config/reload", r.configReload)
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if token := r.cfgMgr.Get().Server.APIToken; token != "" {
		auth := req.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) ||
			subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	r.mux.ServeHTTP(w, req)
}

func (r *Router) queueStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.queue.QueueStatus())
}

func (r *Router) listAgents(w http.ResponseWriter, _ *http.Request) {
	cfg := r.cfgMgr.Get()
	type backendInfo struct {
		Allowed     bool     `json:"allowed"`
		DeniedTools []string `json:"denied_tools,omitempty"`
	}
	type agentInfo struct {
		ID          string                 `json:"id"`
		DisplayName string                 `json:"display_name"`
		Backends    map[string]backendInfo `json:"backends"`
	}

	agents := make([]agentInfo, 0)
	for id, ac := range cfg.Agents {
		info := agentInfo{
			ID:          id,
			DisplayName: ac.DisplayName,
			Backends:    make(map[string]backendInfo),
		}
		for bid, bc := range ac.Backends {
			info.Backends[bid] = backendInfo{
				Allowed:     bc.Allowed,
				DeniedTools: bc.ToolDenylist,
			}
		}
		agents = append(agents, info)
	}
	writeJSON(w, agents)
}

func (r *Router) agentRateLimits(w http.ResponseWriter, req *http.Request) {
	// /api/v1/agents/{id}/rate-limits
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/agents/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "rate-limits" {
		http.Error(w, "expected /api/v1/agents/{id}/rate-limits", http.StatusBadRequest)
		return
	}
	agentID := parts[0]

	cfg := r.cfgMgr.Get()
	agentCfg, ok := cfg.Agents[agentID]
	if !ok {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}

	type rlStatus struct {
		Tool     string `json:"tool"`
		Window   string `json:"window"`
		MaxCount int    `json:"max_count"`
		Current  int    `json:"current"`
		Scope    string `json:"scope"` // "agent" or "global"
	}

	result := make([]rlStatus, 0)

	// Per-agent rate limits
	for _, bc := range agentCfg.Backends {
		for toolName, rl := range bc.RateLimits {
			current, _ := r.auditLog.CountCalls(agentID, toolName, time.Now().Add(-rl.Window))
			result = append(result, rlStatus{
				Tool:     toolName,
				Window:   rl.Window.String(),
				MaxCount: rl.MaxCount,
				Current:  current,
				Scope:    "agent",
			})
		}
	}

	// Global rate limits
	for _, qCfg := range cfg.Queue {
		for toolName, gl := range qCfg.GlobalRateLimits {
			current, _ := r.auditLog.CountCallsGlobal(toolName, time.Now().Add(-gl.Window))
			result = append(result, rlStatus{
				Tool:     toolName,
				Window:   gl.Window.String(),
				MaxCount: gl.MaxCount,
				Current:  current,
				Scope:    "global",
			})
		}
	}

	writeJSON(w, result)
}

func (r *Router) pendingApprovals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.approvalStore.ListPending())
}

func (r *Router) handleApprovalAction(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// /api/v1/approvals/{id}/approve or /api/v1/approvals/{id}/reject
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/approvals/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.Error(w, "expected /api/v1/approvals/{id}/approve|reject", http.StatusBadRequest)
		return
	}

	id := parts[0]
	action := parts[1]

	if action != "approve" && action != "reject" {
		http.Error(w, "action must be 'approve' or 'reject'", http.StatusBadRequest)
		return
	}

	ok := r.approvalStore.Resolve(id, action == "approve")
	if !ok {
		http.Error(w, "not found or already resolved", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]string{"status": "ok", "action": action})
}

func (r *Router) auditLogs(w http.ResponseWriter, req *http.Request) {
	limit := 50
	offset := 0
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := req.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := r.auditLog.Query(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (r *Router) configReload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	if err := r.cfgMgr.Reload(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "ok", "message": "config reloaded"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

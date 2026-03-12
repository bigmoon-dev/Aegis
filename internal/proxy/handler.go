package proxy

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bigmoon-dev/aegis/internal/audit"
	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
	"github.com/bigmoon-dev/aegis/internal/pipeline"
	"github.com/google/uuid"
)

// Handler is the main MCP proxy HTTP handler.
// Routes: /agents/{agentID}/mcp
type Handler struct {
	cfgMgr    *config.Manager
	forwarder *Forwarder
	sessions  *SessionManager
	stages    []pipeline.Stage
	queue     *pipeline.FIFOQueue
	auditLog  *audit.Logger
}

// NewHandler creates the MCP proxy handler.
func NewHandler(
	cfgMgr *config.Manager,
	forwarder *Forwarder,
	sessions *SessionManager,
	stages []pipeline.Stage,
	queue *pipeline.FIFOQueue,
	auditLog *audit.Logger,
) *Handler {
	return &Handler{
		cfgMgr:    cfgMgr,
		forwarder: forwarder,
		sessions:  sessions,
		stages:    stages,
		queue:     queue,
		auditLog:  auditLog,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract agent ID from path: /agents/{agentID}/mcp
	agentID, backendID := h.parsePath(r.URL.Path)
	if agentID == "" {
		http.Error(w, "invalid path: expected /agents/{agentID}/mcp", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		// SSE not supported in proxy mode; return 405
		http.Error(w, "GET (SSE) not supported by proxy", http.StatusMethodNotAllowed)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse the JSON-RPC request
	var rpcReq model.Request
	if err := json.Unmarshal(body, &rpcReq); err != nil {
		h.writeJSONRPCError(w, nil, model.ErrCodeParseError, "invalid JSON-RPC request")
		return
	}

	log.Printf("[proxy] agent=%s backend=%s method=%s", agentID, backendID, rpcReq.Method)

	switch rpcReq.Method {
	case "tools/call":
		h.handleToolsCall(w, r, agentID, backendID, &rpcReq)
	case "tools/list":
		h.handleToolsList(w, r, agentID, backendID, &rpcReq)
	default:
		// Passthrough: initialize, notifications/initialized, ping, etc.
		h.handlePassthrough(w, r, agentID, backendID, body)
	}
}

func (h *Handler) handleToolsCall(w http.ResponseWriter, r *http.Request, agentID, backendID string, rpcReq *model.Request) {
	// Parse tool name and arguments
	var params model.ToolsCallParams
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
		h.writeJSONRPCError(w, rpcReq.ID, model.ErrCodeInvalidParams, "invalid tools/call params")
		return
	}

	pReq := &model.PipelineRequest{
		RequestID: uuid.New().String(),
		AgentID:   agentID,
		BackendID: backendID,
		ToolName:  params.Name,
		Arguments: string(params.Arguments),
		SessionID: h.sessions.Get(agentID),
		RPC:       rpcReq,
		CreatedAt: time.Now().UTC(),
	}

	resp, err := pipeline.ExecutePipeline(
		r.Context(),
		pReq,
		h.stages,
		h.queue,
		func(e *model.AuditEntry) { h.auditLog.Log(e) },
		func(agent, tool string) { h.auditLog.RecordCall(agent, tool) },
	)
	if err != nil {
		log.Printf("[proxy] pipeline error: %v", err)
		h.writeJSONRPCError(w, rpcReq.ID, model.ErrCodeInternal, err.Error())
		return
	}

	h.writeJSON(w, resp)
}

func (h *Handler) handleToolsList(w http.ResponseWriter, r *http.Request, agentID, backendID string, rpcReq *model.Request) {
	cfg := h.cfgMgr.Get()

	// Forward to backend with session ID
	sessionID := h.sessions.Get(agentID)
	resp, respSessionID, err := h.forwarder.Forward(r.Context(), backendID, rpcReq, sessionID)
	if err != nil {
		log.Printf("[proxy] tools/list forward error: %v", err)
		h.writeJSONRPCError(w, rpcReq.ID, model.ErrCodeInternal, err.Error())
		return
	}
	if respSessionID != "" {
		h.sessions.Set(agentID, respSessionID)
	}

	// Parse and enhance
	result, err := ParseToolsListResult(resp)
	if err != nil {
		log.Printf("[proxy] tools/list parse error: %v", err)
		h.writeJSON(w, resp) // return original on parse failure
		return
	}

	enhanced := EnhanceToolsList(cfg, agentID, backendID, result)

	// Re-serialize
	resultBytes, _ := json.Marshal(enhanced)
	resp.Result = resultBytes
	h.writeJSON(w, resp)
}

func (h *Handler) handlePassthrough(w http.ResponseWriter, r *http.Request, agentID, backendID string, body []byte) {
	sessionID := h.sessions.Get(agentID)
	respBody, statusCode, respSessionID, err := h.forwarder.ForwardRaw(r.Context(), backendID, body, sessionID)
	if err != nil {
		log.Printf("[proxy] passthrough error: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if respSessionID != "" {
		h.sessions.Set(agentID, respSessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(respBody)
}

func (h *Handler) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := model.NewErrorResponse(id, code, message)
	h.writeJSON(w, resp)
}

// parsePath extracts agentID and determines backendID from the URL path.
// Path format: /agents/{agentID}/mcp
// Returns empty strings if the path is invalid or agent is unknown.
func (h *Handler) parsePath(path string) (agentID, backendID string) {
	// Strip leading/trailing slashes and split
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")

	// Expected: ["agents", "{agentID}", "mcp"]
	if len(parts) != 3 || parts[0] != "agents" || parts[2] != "mcp" {
		return "", ""
	}

	agentID = parts[1]

	// Reject unknown agents
	cfg := h.cfgMgr.Get()
	agentCfg, ok := cfg.Agents[agentID]
	if !ok {
		log.Printf("[proxy] rejected unknown agent: %s", agentID)
		return "", ""
	}

	// Select the first allowed backend (sorted by name for determinism)
	bids := make([]string, 0, len(agentCfg.Backends))
	for bid := range agentCfg.Backends {
		bids = append(bids, bid)
	}
	sort.Strings(bids)

	for _, bid := range bids {
		bc := agentCfg.Backends[bid]
		if bc.Allowed {
			backendID = bid
			break
		}
	}

	if backendID == "" {
		log.Printf("[proxy] agent %s has no allowed backends", agentID)
		return "", ""
	}

	return agentID, backendID
}

// HealthCheck returns a handler that checks backend health.
func HealthCheck(cfgMgr *config.Manager) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := cfgMgr.Get()
		status := map[string]any{
			"status":  "ok",
			"service": "aegis",
		}

		backends := make(map[string]string)
		for name, b := range cfg.Backends {
			if b.HealthURL == "" {
				backends[name] = "no_health_url"
				continue
			}
			resp, err := client.Get(b.HealthURL)
			if err != nil {
				backends[name] = "unhealthy: " + err.Error()
				status["status"] = "degraded"
			} else {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					backends[name] = "healthy"
				} else {
					backends[name] = "unhealthy: status " + resp.Status
					status["status"] = "degraded"
				}
			}
		}
		status["backends"] = backends

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

// NewMux creates the main HTTP router.
func NewMux(
	handler *Handler,
	healthCheck http.HandlerFunc,
	approvalCallback http.Handler,
	apiRouter http.Handler,
) *http.ServeMux {
	mux := http.NewServeMux()

	// MCP proxy: /agents/{agentID}/mcp
	mux.Handle("/agents/", handler)

	// Health check
	mux.HandleFunc("/health", healthCheck)

	// Approval callbacks
	mux.Handle("/callback/approval", approvalCallback)

	// Management API
	mux.Handle("/api/v1/", apiRouter)

	return mux
}

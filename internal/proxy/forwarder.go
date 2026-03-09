package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

// Forwarder sends JSON-RPC requests to backend MCP servers.
type Forwarder struct {
	cfgMgr *config.Manager
	client *http.Client
}

func NewForwarder(cfgMgr *config.Manager) *Forwarder {
	return &Forwarder{
		cfgMgr: cfgMgr,
		client: &http.Client{},
	}
}

// Forward sends a JSON-RPC request to the specified backend and returns the response.
// sessionID is the MCP session ID to include in the request; respSessionID is the
// session ID returned by the backend (if any).
func (f *Forwarder) Forward(ctx context.Context, backendID string, rpcReq *model.Request, sessionID string) (*model.Response, string, error) {
	cfg := f.cfgMgr.Get()
	backend, ok := cfg.Backends[backendID]
	if !ok {
		return nil, "", fmt.Errorf("unknown backend: %s", backendID)
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	timeout := backend.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.URL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("forward to %s: %w", backendID, err)
	}
	defer resp.Body.Close()

	// Capture session ID from response
	respSessionID := resp.Header.Get("Mcp-Session-Id")

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
	if err != nil {
		return nil, "", fmt.Errorf("read response from %s: %w", backendID, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("backend %s returned status %d: %s", backendID, resp.StatusCode, string(respBody))
	}

	var rpcResp model.Response
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, "", fmt.Errorf("unmarshal response from %s: %w", backendID, err)
	}

	return &rpcResp, respSessionID, nil
}

// ForwardRaw sends raw bytes to the backend and returns raw response bytes.
// Used for passthrough of non-tools/call messages.
func (f *Forwarder) ForwardRaw(ctx context.Context, backendID string, body []byte, sessionID string) ([]byte, int, string, error) {
	cfg := f.cfgMgr.Get()
	backend, ok := cfg.Backends[backendID]
	if !ok {
		return nil, 0, "", fmt.Errorf("unknown backend: %s", backendID)
	}

	timeout := backend.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	respSessionID := resp.Header.Get("Mcp-Session-Id")

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
	if err != nil {
		return nil, resp.StatusCode, "", err
	}

	log.Printf("[forwarder] %s → %d (%d bytes)", backendID, resp.StatusCode, len(respBody))
	return respBody, resp.StatusCode, respSessionID, nil
}

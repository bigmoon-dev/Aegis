package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bigmoon-dev/aegis/internal/model"
)

// DiscoverTools connects to an MCP backend, performs the initialize handshake,
// and returns the list of available tools.
func DiscoverTools(backendURL string) ([]model.ToolInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Send initialize request
	initReq := model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {"name": "aegis-setup", "version": "1.0"}
		}`),
	}

	initResp, err := sendRPC(client, backendURL, &initReq)
	if err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}
	if initResp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s", initResp.Error.Message)
	}

	// Step 2: Send notifications/initialized notification
	notif := model.Notification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := sendNotification(client, backendURL, &notif); err != nil {
		return nil, fmt.Errorf("send initialized notification: %w", err)
	}

	// Step 3: Send tools/list request
	listReq := model.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
	}

	listResp, err := sendRPC(client, backendURL, &listReq)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}
	if listResp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", listResp.Error.Message)
	}

	var result model.ToolsListResult
	if err := json.Unmarshal(listResp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	return result.Tools, nil
}

func sendRPC(client *http.Client, url string, req *model.Request) (*model.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp model.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response (not a valid MCP server?): %w", err)
	}

	return &resp, nil
}

func sendNotification(client *http.Client, url string, notif *model.Notification) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer httpResp.Body.Close()
	io.ReadAll(httpResp.Body) // drain

	// Notifications may return 200 or 202, both are ok
	if httpResp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", httpResp.StatusCode)
	}

	return nil
}

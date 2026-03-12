package model

import "encoding/json"

// JSON-RPC 2.0 message types for MCP protocol

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // can be number, string, or null; must always be present
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ToolsCallParams represents the params for a tools/call request.
type ToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolInfo represents a single tool in the tools/list response.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolsListResult represents the result of tools/list.
type ToolsListResult struct {
	Tools []ToolInfo `json:"tools"`
}

// Predefined JSON-RPC error codes for Aegis.
const (
	ErrCodeACLDenied     = -32001
	ErrCodeRateLimited   = -32002
	ErrCodeQueueFull     = -32003
	ErrCodeApprovalDeny  = -32004
	ErrCodeParseError    = -32700 // JSON parse error per JSON-RPC 2.0 spec
	ErrCodeInvalidParams = -32602 // Invalid method parameters
	ErrCodeInternal      = -32603
)

// NewErrorResponse creates a JSON-RPC error response.
func NewErrorResponse(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// IsNotification returns true if the request has no ID (is a notification).
func (r *Request) IsNotification() bool {
	return r.ID == nil || string(r.ID) == "null"
}

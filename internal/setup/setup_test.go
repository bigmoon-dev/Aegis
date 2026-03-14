package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bigmoon-dev/aegis/internal/model"
)

// --- DiscoverTools tests ---

func TestDiscoverTools(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req model.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Might be a notification (no ID)
			w.WriteHeader(http.StatusOK)
			return
		}

		switch req.Method {
		case "initialize":
			callCount++
			_ = json.NewEncoder(w).Encode(model.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			})
		case "tools/list":
			callCount++
			_ = json.NewEncoder(w).Encode(model.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: json.RawMessage(`{"tools":[
					{"name":"search_papers","description":"Search ArXiv papers"},
					{"name":"list_papers","description":"List downloaded papers"}
				]}`),
			})
		default:
			// notifications/initialized — just accept
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	tools, err := DiscoverTools(server.URL)
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "search_papers" {
		t.Errorf("tool[0].Name = %q, want %q", tools[0].Name, "search_papers")
	}
	if tools[1].Name != "list_papers" {
		t.Errorf("tool[1].Name = %q, want %q", tools[1].Name, "list_papers")
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 RPC calls, got %d", callCount)
	}
}

func TestDiscoverToolsConnectionRefused(t *testing.T) {
	_, err := DiscoverTools("http://127.0.0.1:19999/mcp")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestDiscoverToolsNonMCP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html>Not an MCP server</html>")
	}))
	defer server.Close()

	_, err := DiscoverTools(server.URL)
	if err == nil {
		t.Fatal("expected error for non-MCP server")
	}
}

// --- inferDefaults tests ---

func TestInferDefaults(t *testing.T) {
	tests := []struct {
		name      string
		wantRate  string
		wantQueue bool
		wantDeny  bool
		wantAppr  bool
	}{
		{"list_papers", "unlimited", false, false, false},
		{"get_feed_detail", "unlimited", false, false, false},
		{"check_login_status", "unlimited", false, false, false},
		{"search_papers", "20/1h", false, false, false},
		{"query_users", "20/1h", false, false, false},
		{"download_paper", "5/1h", true, false, false},
		{"fetch_content", "5/1h", true, false, false},
		{"publish_content", "1/24h", true, false, true},
		{"send_message", "1/24h", true, false, true},
		{"delete_cookies", "unlimited", false, true, false},
		{"admin_reset", "unlimited", false, true, false},
		{"some_random_tool", "10/1h", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := inferDefaults(tt.name)
			if p.RateLimit != tt.wantRate {
				t.Errorf("RateLimit = %q, want %q", p.RateLimit, tt.wantRate)
			}
			if p.Queue != tt.wantQueue {
				t.Errorf("Queue = %v, want %v", p.Queue, tt.wantQueue)
			}
			if p.Deny != tt.wantDeny {
				t.Errorf("Deny = %v, want %v", p.Deny, tt.wantDeny)
			}
			if p.Approval != tt.wantAppr {
				t.Errorf("Approval = %v, want %v", p.Approval, tt.wantAppr)
			}
		})
	}
}

// --- GenerateConfig tests ---

func TestGenerateConfig(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "config", "test.yaml")

	backend := BackendInput{Name: "chatpaper", URL: "http://localhost:9200/mcp"}
	policies := []ToolPolicy{
		{Name: "search_papers", RateLimit: "20/1h", Queue: false},
		{Name: "download_paper", RateLimit: "5/1h", Queue: true, QueueDelay: "30s-60s"},
		{Name: "list_papers", RateLimit: "unlimited", Queue: false},
		{Name: "delete_all", RateLimit: "unlimited", Deny: true},
		{Name: "publish_result", RateLimit: "1/24h", Queue: true, QueueDelay: "30s-60s", Approval: true},
	}
	agent := AgentChoice{Adapter: &OpenClawAdapter{}, AgentID: "openclaw-chatpaper"}

	err := GenerateConfig(backend, policies, agent, outputPath)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	content := string(data)
	// Check key elements are present
	checks := []string{
		"chatpaper",
		"http://localhost:9200/mcp",
		"openclaw-chatpaper",
		"search_papers",
		"download_paper",
		"list_papers",
		"delete_all",    // should be in denylist
		"publish_result", // should be in approval_required
	}
	for _, c := range checks {
		if !strings.Contains(content, c) {
			t.Errorf("output missing %q", c)
		}
	}
}

func TestGenerateConfigParseable(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "aegis.yaml")

	backend := BackendInput{Name: "test", URL: "http://localhost:8000/mcp"}
	policies := []ToolPolicy{
		{Name: "tool_a", RateLimit: "10/1h", Queue: true, QueueDelay: "5s-15s"},
	}
	agent := AgentChoice{Adapter: &CustomAdapter{}, AgentID: "test-agent"}

	if err := GenerateConfig(backend, policies, agent, outputPath); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	// Verify it's valid YAML that can be loaded as config
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty output")
	}
}

// --- parseRateLimit tests ---

func TestParseRateLimit(t *testing.T) {
	tests := []struct {
		input string
		count int
		hours float64
		isNil bool
	}{
		{"5/1h", 5, 1, false},
		{"1/24h", 1, 24, false},
		{"20/1h", 20, 1, false},
		{"10/30m", 10, 0.5, false},
		{"unlimited", 0, 0, true},
		{"bad", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			rl := parseRateLimit(tt.input)
			if tt.isNil {
				if rl != nil {
					t.Errorf("expected nil for %q", tt.input)
				}
				return
			}
			if rl == nil {
				t.Fatalf("unexpected nil for %q", tt.input)
			}
			if rl.MaxCount != tt.count {
				t.Errorf("MaxCount = %d, want %d", rl.MaxCount, tt.count)
			}
			if rl.Window.Hours() != tt.hours {
				t.Errorf("Window = %v, want %vh", rl.Window, tt.hours)
			}
		})
	}
}

// --- Adapter tests ---

func TestInjectMCPServerNewFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp_servers.json")

	err := injectMCPServer(configPath, "test-server", "http://localhost:18070/agents/test/mcp", "url")
	if err != nil {
		t.Fatalf("injectMCPServer: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	servers, ok := parsed["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatal("mcpServers not found")
	}

	entry, ok := servers["test-server"].(map[string]interface{})
	if !ok {
		t.Fatal("test-server not found")
	}

	if entry["url"] != "http://localhost:18070/agents/test/mcp" {
		t.Errorf("url = %v, want http://localhost:18070/agents/test/mcp", entry["url"])
	}
}

func TestInjectMCPServerExistingFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcporter.json")

	// Write existing config
	existing := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"existing": map[string]interface{}{
				"baseUrl": "http://localhost:9000/mcp",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	err := injectMCPServer(configPath, "new-server", "http://localhost:18070/agents/new/mcp", "baseUrl")
	if err != nil {
		t.Fatalf("injectMCPServer: %v", err)
	}

	// Verify backup exists
	if _, err := os.Stat(configPath + ".bak"); os.IsNotExist(err) {
		t.Error("backup file not created")
	}

	// Verify both entries exist
	result, _ := os.ReadFile(configPath)
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}

	servers := parsed["mcpServers"].(map[string]interface{})
	if _, ok := servers["existing"]; !ok {
		t.Error("existing entry was removed")
	}
	if _, ok := servers["new-server"]; !ok {
		t.Error("new entry not added")
	}
}

func TestInjectMCPServerConflict(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	existing := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"myserver": map[string]interface{}{
				"url": "http://localhost:9000/mcp",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	err := injectMCPServer(configPath, "myserver", "http://localhost:18070/mcp", "url")
	if err == nil {
		t.Fatal("expected CONFLICT error")
	}
	if !strings.Contains(err.Error(), "CONFLICT") {
		t.Errorf("error = %q, want CONFLICT prefix", err.Error())
	}
}

func TestInjectMCPServerSkip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	existing := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"myserver": map[string]interface{}{
				"url": "http://localhost:18070/mcp",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	err := injectMCPServer(configPath, "myserver", "http://localhost:18070/mcp", "url")
	if err == nil {
		t.Fatal("expected SKIP error")
	}
	if !strings.Contains(err.Error(), "SKIP") {
		t.Errorf("error = %q, want SKIP prefix", err.Error())
	}
}

// --- resolveQueueDelays tests ---

func TestResolveQueueDelays(t *testing.T) {
	tests := []struct {
		name    string
		policies []ToolPolicy
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			"no queued tools",
			[]ToolPolicy{
				{Name: "a", Queue: false},
			},
			60 * time.Second, 600 * time.Second,
		},
		{
			"single queued tool",
			[]ToolPolicy{
				{Name: "a", Queue: true, QueueDelay: "5s-15s"},
			},
			5 * time.Second, 15 * time.Second,
		},
		{
			"multiple queued tools",
			[]ToolPolicy{
				{Name: "a", Queue: true, QueueDelay: "5s-15s"},
				{Name: "b", Queue: true, QueueDelay: "30s-60s"},
			},
			5 * time.Second, 60 * time.Second,
		},
		{
			"mixed queued and bypass",
			[]ToolPolicy{
				{Name: "a", Queue: false, RateLimit: "unlimited"},
				{Name: "b", Queue: true, QueueDelay: "30s-60s"},
			},
			30 * time.Second, 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, gotMax := resolveQueueDelays(tt.policies)
			if gotMin != tt.wantMin {
				t.Errorf("minDelay = %v, want %v", gotMin, tt.wantMin)
			}
			if gotMax != tt.wantMax {
				t.Errorf("maxDelay = %v, want %v", gotMax, tt.wantMax)
			}
		})
	}
}

// --- inferBackendName tests ---

func TestInferBackendName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"http://localhost:9200/mcp", "mcp-9200"},
		{"http://127.0.0.1:18060/mcp", "mcp-18060"},
		{"http://chatpaper.local/mcp", "chatpaper.local"},
		{"http://localhost:9200/chatpaper/mcp", "chatpaper"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := inferBackendName(tt.url)
			if got != tt.want {
				t.Errorf("inferBackendName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}


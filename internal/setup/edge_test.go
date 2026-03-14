package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests that simulate real-world mcporter.json scenarios from the RPi deployment.

func TestInjectRealMcporterSkipSameURL(t *testing.T) {
	// Real mcporter.json from RPi: chatpaper already configured with same URL
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcporter.json")

	existing := `{
  "mcpServers": {
    "chatpaper": {
      "baseUrl": "http://localhost:18070/agents/openclaw-chatpaper/mcp"
    }
  }
}`
	os.WriteFile(configPath, []byte(existing), 0644)

	err := injectMCPServer(configPath, "chatpaper",
		"http://localhost:18070/agents/openclaw-chatpaper/mcp", "baseUrl")

	if err == nil {
		t.Fatal("expected SKIP error")
	}
	if !strings.HasPrefix(err.Error(), "SKIP:") {
		t.Errorf("error = %q, want SKIP: prefix", err.Error())
	}
}

func TestInjectRealMcporterConflictDifferentURL(t *testing.T) {
	// chatpaper exists but pointing to different URL
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcporter.json")

	existing := `{
  "mcpServers": {
    "chatpaper": {
      "baseUrl": "http://localhost:18070/agents/openclaw-chatpaper/mcp"
    }
  }
}`
	os.WriteFile(configPath, []byte(existing), 0644)

	err := injectMCPServer(configPath, "chatpaper",
		"http://localhost:18070/agents/new-agent/mcp", "baseUrl")

	if err == nil {
		t.Fatal("expected CONFLICT error")
	}
	if !strings.HasPrefix(err.Error(), "CONFLICT:") {
		t.Errorf("error = %q, want CONFLICT: prefix", err.Error())
	}
}

func TestInjectRealMcporterAddNewServer(t *testing.T) {
	// Add a second server alongside existing chatpaper
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcporter.json")

	existing := `{
  "mcpServers": {
    "chatpaper": {
      "baseUrl": "http://localhost:18070/agents/openclaw-chatpaper/mcp"
    }
  }
}`
	os.WriteFile(configPath, []byte(existing), 0644)

	err := injectMCPServer(configPath, "xiaohongshu",
		"http://localhost:18070/agents/openclaw-xhs/mcp", "baseUrl")
	if err != nil {
		t.Fatalf("inject new server: %v", err)
	}

	// Verify backup
	bakData, err := os.ReadFile(configPath + ".bak")
	if err != nil {
		t.Fatal("backup not created")
	}
	if string(bakData) != existing {
		t.Error("backup content doesn't match original")
	}

	// Verify result has both servers
	data, _ := os.ReadFile(configPath)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	servers := parsed["mcpServers"].(map[string]interface{})

	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}

	// Original preserved
	cp := servers["chatpaper"].(map[string]interface{})
	if cp["baseUrl"] != "http://localhost:18070/agents/openclaw-chatpaper/mcp" {
		t.Error("original chatpaper entry was modified")
	}

	// New entry added
	xhs := servers["xiaohongshu"].(map[string]interface{})
	if xhs["baseUrl"] != "http://localhost:18070/agents/openclaw-xhs/mcp" {
		t.Error("new xiaohongshu entry incorrect")
	}
}

func TestInjectClaudeCodeFormat(t *testing.T) {
	// Claude Code uses "url" instead of "baseUrl"
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp_servers.json")

	existing := `{
  "mcpServers": {
    "other-server": {
      "url": "http://localhost:3000/mcp"
    }
  }
}`
	os.WriteFile(configPath, []byte(existing), 0644)

	err := injectMCPServer(configPath, "aegis-chatpaper",
		"http://localhost:18070/agents/claude-chatpaper/mcp", "url")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	servers := parsed["mcpServers"].(map[string]interface{})

	entry := servers["aegis-chatpaper"].(map[string]interface{})
	if entry["url"] != "http://localhost:18070/agents/claude-chatpaper/mcp" {
		t.Errorf("url = %v, want correct aegis URL", entry["url"])
	}

	// Original preserved
	other := servers["other-server"].(map[string]interface{})
	if other["url"] != "http://localhost:3000/mcp" {
		t.Error("original entry was modified")
	}
}

func TestConfigAlreadyExistsOverwrite(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "aegis.yaml")

	backend := BackendInput{Name: "test", URL: "http://localhost:8000/mcp"}
	policies := []ToolPolicy{
		{Name: "tool_a", RateLimit: "10/1h", Queue: true, QueueDelay: "5s-15s"},
	}
	agent := AgentChoice{Adapter: &CustomAdapter{}, AgentID: "test-agent"}

	// Write first time
	if err := GenerateConfig(backend, policies, agent, outputPath); err != nil {
		t.Fatalf("first write: %v", err)
	}
	data1, _ := os.ReadFile(outputPath)

	// Write again (overwrite with different rate limit)
	policies[0].RateLimit = "20/1h"
	if err := GenerateConfig(backend, policies, agent, outputPath); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	data2, _ := os.ReadFile(outputPath)

	// Content should differ
	if string(data1) == string(data2) {
		t.Error("file content was not updated on overwrite")
	}

	// Verify new content is correct
	if !strings.Contains(string(data2), "max_count: 20") {
		t.Error("overwritten config doesn't reflect new rate limit")
	}
	if strings.Contains(string(data2), "max_count: 10") {
		t.Error("overwritten config still has old rate limit")
	}
}

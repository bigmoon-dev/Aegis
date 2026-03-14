package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bigmoon-dev/aegis/internal/config"
)

// Integration test: requires chatpaper-mcp running at http://127.0.0.1:9200/mcp
// Run with: go test ./internal/setup/ -run TestIntegration -v -count=1
// Skip automatically if chatpaper-mcp is not reachable.

func TestIntegrationDiscoverChatpaper(t *testing.T) {
	const backendURL = "http://127.0.0.1:9200/mcp"

	tools, err := DiscoverTools(backendURL)
	if err != nil {
		t.Skipf("chatpaper-mcp not reachable (skipping integration test): %v", err)
	}

	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	expected := map[string]bool{
		"search_papers":   false,
		"download_paper":  false,
		"summarize_paper": false,
		"list_papers":     false,
	}
	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		expected[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}

	t.Logf("Discovered %d tools from chatpaper-mcp", len(tools))
}

func TestIntegrationGenerateAndLoadConfig(t *testing.T) {
	const backendURL = "http://127.0.0.1:9200/mcp"

	tools, err := DiscoverTools(backendURL)
	if err != nil {
		t.Skipf("chatpaper-mcp not reachable (skipping integration test): %v", err)
	}

	// Simulate wizard output: apply smart defaults
	policies := make([]ToolPolicy, 0, len(tools))
	for _, tool := range tools {
		policies = append(policies, inferDefaults(tool.Name))
	}

	backend := BackendInput{Name: "chatpaper", URL: backendURL}
	agent := AgentChoice{Adapter: &OpenClawAdapter{}, AgentID: "openclaw-chatpaper"}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "aegis.yaml")

	// Generate config
	if err := GenerateConfig(backend, policies, agent, outputPath); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	// Verify file exists and is non-empty
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("generated config is empty")
	}

	// Verify config can be loaded by config.NewManager
	mgr, err := config.NewManager(outputPath)
	if err != nil {
		t.Fatalf("config.NewManager failed to load generated config: %v", err)
	}
	cfg := mgr.Get()

	// Verify structure
	if _, ok := cfg.Backends["chatpaper"]; !ok {
		t.Error("missing backend 'chatpaper'")
	}
	if cfg.Backends["chatpaper"].URL != backendURL {
		t.Errorf("backend URL = %q, want %q", cfg.Backends["chatpaper"].URL, backendURL)
	}
	if _, ok := cfg.Agents["openclaw-chatpaper"]; !ok {
		t.Error("missing agent 'openclaw-chatpaper'")
	}
	agentCfg := cfg.Agents["openclaw-chatpaper"]
	if !agentCfg.Backends["chatpaper"].Allowed {
		t.Error("agent backend not allowed")
	}

	// Verify smart defaults were applied correctly
	bc := agentCfg.Backends["chatpaper"]

	// list_papers should be unlimited (no rate limit entry)
	if _, hasRL := bc.RateLimits["list_papers"]; hasRL {
		t.Error("list_papers should not have a rate limit (unlimited)")
	}

	// search_papers should have 20/1h
	if rl, ok := bc.RateLimits["search_papers"]; ok {
		if rl.MaxCount != 20 {
			t.Errorf("search_papers rate limit = %d, want 20", rl.MaxCount)
		}
	} else {
		t.Error("search_papers missing rate limit")
	}

	// download_paper should have 5/1h
	if rl, ok := bc.RateLimits["download_paper"]; ok {
		if rl.MaxCount != 5 {
			t.Errorf("download_paper rate limit = %d, want 5", rl.MaxCount)
		}
	} else {
		t.Error("download_paper missing rate limit")
	}

	// Queue should have list_papers in bypass_tools
	qCfg := cfg.Queue["chatpaper"]
	if !qCfg.Enabled {
		t.Error("queue should be enabled (download_paper and summarize_paper have queue)")
	}
	foundBypass := false
	for _, name := range qCfg.BypassTools {
		if name == "list_papers" {
			foundBypass = true
		}
	}
	if !foundBypass {
		t.Error("list_papers should be in bypass_tools")
	}

	// Global rate limits should be 2x agent limits
	if gl, ok := qCfg.GlobalRateLimits["search_papers"]; ok {
		if gl.MaxCount != 40 {
			t.Errorf("global search_papers = %d, want 40 (2x agent)", gl.MaxCount)
		}
	} else {
		t.Error("missing global rate limit for search_papers")
	}

	t.Logf("Generated config loaded successfully: %d backends, %d agents", len(cfg.Backends), len(cfg.Agents))

	// Print generated file for manual review
	data, _ := os.ReadFile(outputPath)
	t.Logf("Generated YAML:\n%s", string(data))
}

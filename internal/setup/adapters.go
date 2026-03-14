package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentAdapter knows how to detect and configure a specific agent framework.
type AgentAdapter interface {
	Name() string
	Verified() bool
	Detect() bool
	ConfigPath() string
	Inject(serverName, aegisURL string) error
	PostSetupHint() string
}

// AllAdapters returns the list of known agent adapters.
func AllAdapters() []AgentAdapter {
	return []AgentAdapter{
		&OpenClawAdapter{},
		&ClaudeCodeAdapter{},
		&CustomAdapter{},
	}
}

// --- OpenClaw Adapter ---

type OpenClawAdapter struct{}

func (a *OpenClawAdapter) Name() string     { return "OpenClaw" }
func (a *OpenClawAdapter) Verified() bool   { return true }
func (a *OpenClawAdapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openclaw", "workspace", "config", "mcporter.json")
}

func (a *OpenClawAdapter) Detect() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".openclaw", "workspace", "config"))
	return err == nil && info.IsDir()
}

func (a *OpenClawAdapter) Inject(serverName, aegisURL string) error {
	return injectMCPServer(a.ConfigPath(), serverName, aegisURL, "baseUrl")
}

func (a *OpenClawAdapter) PostSetupHint() string {
	return "Restart agent:  openclaw gateway restart"
}

// --- Claude Code Adapter ---

type ClaudeCodeAdapter struct{}

func (a *ClaudeCodeAdapter) Name() string     { return "Claude Code" }
func (a *ClaudeCodeAdapter) Verified() bool   { return true }
func (a *ClaudeCodeAdapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "mcp_servers.json")
}

func (a *ClaudeCodeAdapter) Detect() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil && info.IsDir()
}

func (a *ClaudeCodeAdapter) Inject(serverName, aegisURL string) error {
	return injectMCPServer(a.ConfigPath(), serverName, aegisURL, "url")
}

func (a *ClaudeCodeAdapter) PostSetupHint() string {
	return "Claude Code will auto-detect the new server on next start"
}

// --- Custom Adapter ---

type CustomAdapter struct{}

func (a *CustomAdapter) Name() string       { return "Custom" }
func (a *CustomAdapter) Verified() bool     { return false }
func (a *CustomAdapter) Detect() bool       { return true }
func (a *CustomAdapter) ConfigPath() string { return "" }

func (a *CustomAdapter) Inject(serverName, aegisURL string) error {
	// No-op: just print instructions
	return nil
}

func (a *CustomAdapter) PostSetupHint() string {
	return "Configure your agent to connect to the Aegis proxy URL shown above"
}

// --- shared helper ---

// injectMCPServer reads a JSON config file, adds/updates an MCP server entry,
// creates a .bak backup, and writes back the updated file.
func injectMCPServer(configPath, serverName, aegisURL, urlKey string) error {
	// Read existing config or start fresh
	data := map[string]interface{}{}
	existing, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(existing, &data); err != nil {
			return fmt.Errorf("parse %s: %w", configPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	// Ensure mcpServers map exists
	servers, ok := data["mcpServers"].(map[string]interface{})
	if !ok {
		servers = map[string]interface{}{}
		data["mcpServers"] = servers
	}

	// Check for existing entry
	if entry, exists := servers[serverName]; exists {
		if m, ok := entry.(map[string]interface{}); ok {
			if m[urlKey] == aegisURL {
				return fmt.Errorf("SKIP: %s already configured with same URL", serverName)
			}
			return fmt.Errorf("CONFLICT: %s exists with different URL (%v)", serverName, m[urlKey])
		}
	}

	// Add the new entry
	servers[serverName] = map[string]interface{}{
		urlKey: aegisURL,
	}

	// Create backup if original file exists
	if existing != nil {
		backupPath := configPath + ".bak"
		if err := os.WriteFile(backupPath, existing, 0644); err != nil {
			return fmt.Errorf("create backup: %w", err)
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Write updated config
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(configPath, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	// Verify written JSON is valid
	verify, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("verify read: %w", err)
	}
	var check map[string]interface{}
	if err := json.Unmarshal(verify, &check); err != nil {
		return fmt.Errorf("verify JSON invalid: %w", err)
	}

	return nil
}

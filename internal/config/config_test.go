package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidate_Defaults(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp"},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
		},
	}

	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Listen != ":18070" {
		t.Errorf("expected default listen :18070, got %s", cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeout != 300*time.Second {
		t.Errorf("expected default read_timeout 300s, got %s", cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout != 300*time.Second {
		t.Errorf("expected default write_timeout 300s, got %s", cfg.Server.WriteTimeout)
	}
	if cfg.Backends["demo"].Timeout != 120*time.Second {
		t.Errorf("expected default backend timeout 120s, got %s", cfg.Backends["demo"].Timeout)
	}
	if cfg.Audit.DBPath != "./data/audit.db" {
		t.Errorf("expected default db_path, got %s", cfg.Audit.DBPath)
	}
	if cfg.Audit.RetentionDays != 90 {
		t.Errorf("expected default retention 90, got %d", cfg.Audit.RetentionDays)
	}
	if cfg.Approval.Timeout != 600*time.Second {
		t.Errorf("expected default approval timeout 600s, got %s", cfg.Approval.Timeout)
	}
}

func TestValidate_NoBackends(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected error for no backends")
	}
}

func TestValidate_BackendNoURL(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected error for backend with no URL")
	}
}

func TestValidate_QueueNoMatchingBackend(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp"},
		},
		Queue: map[string]QueueConfig{
			"nonexistent": {Enabled: true},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends:    map[string]AgentBackendConfig{"demo": {Allowed: true}},
			},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected error for queue with no matching backend")
	}
}

func TestValidate_QueueDefaults(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp"},
		},
		Queue: map[string]QueueConfig{
			"demo": {Enabled: true},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends:    map[string]AgentBackendConfig{"demo": {Allowed: true}},
			},
		},
	}

	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := cfg.Queue["demo"]
	if q.MaxPending != 50 {
		t.Errorf("expected default max_pending 50, got %d", q.MaxPending)
	}
	if q.DelayMin != 60*time.Second {
		t.Errorf("expected default delay_min 60s, got %s", q.DelayMin)
	}
	if q.DelayMax != 600*time.Second {
		t.Errorf("expected default delay_max 600s, got %s", q.DelayMax)
	}
}

func TestValidate_AgentNoDisplayName(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp"},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				Backends: map[string]AgentBackendConfig{"demo": {Allowed: true}},
			},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected error for agent with no display_name")
	}
}

func TestValidate_AgentUnknownBackend(t *testing.T) {
	cfg := &Config{
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp"},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends:    map[string]AgentBackendConfig{"nonexistent": {Allowed: true}},
			},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected error for agent referencing unknown backend")
	}
}

func TestValidate_WriteTimeoutInflation(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			WriteTimeout: 120 * time.Second,
		},
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp", Timeout: 30 * time.Second},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]AgentBackendConfig{
					"demo": {
						Allowed:          true,
						ApprovalRequired: []string{"publish"},
					},
				},
			},
		},
		Approval: ApprovalConfig{
			Timeout: 120 * time.Second,
		},
	}

	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// minWriteTimeout = approval(120s) + backend(30s) + 30s = 180s
	// Original 120s < 180s, so should be inflated
	if cfg.Server.WriteTimeout < 180*time.Second {
		t.Errorf("expected write_timeout >= 180s, got %s", cfg.Server.WriteTimeout)
	}
}

func TestValidate_NoInflationWithoutApproval(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			WriteTimeout: 60 * time.Second,
		},
		Backends: map[string]BackendConfig{
			"demo": {URL: "http://localhost:9100/mcp", Timeout: 30 * time.Second},
		},
		Agents: map[string]AgentConfig{
			"agent-a": {
				DisplayName: "Agent A",
				Backends: map[string]AgentBackendConfig{
					"demo": {Allowed: true},
				},
			},
		},
	}

	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No approval required → no inflation
	if cfg.Server.WriteTimeout != 60*time.Second {
		t.Errorf("expected write_timeout to stay 60s without approval, got %s", cfg.Server.WriteTimeout)
	}
}

func TestNewManager_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `
server:
  listen: ":9999"
backends:
  demo:
    url: "http://localhost:9100/mcp"
agents:
  test:
    display_name: "Test Agent"
    backends:
      demo:
        allowed: true
audit:
  db_path: "./test.db"
`
	os.WriteFile(cfgPath, []byte(cfgYAML), 0644)

	mgr, err := NewManager(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Server.Listen != ":9999" {
		t.Errorf("expected :9999, got %s", cfg.Server.Listen)
	}
}

func TestNewManager_InvalidPath(t *testing.T) {
	_, err := NewManager("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestNewManager_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`invalid: yaml: [[[`), 0644)

	_, err := NewManager(cfgPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestNewManager_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Valid YAML but no backends → validation error
	os.WriteFile(cfgPath, []byte(`server:\n  listen: ":8080"\n`), 0644)

	_, err := NewManager(cfgPath)
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestManager_Reload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `
backends:
  demo:
    url: "http://localhost:9100/mcp"
agents:
  test:
    display_name: "Test"
    backends:
      demo:
        allowed: true
`
	os.WriteFile(cfgPath, []byte(cfgYAML), 0644)

	mgr, err := NewManager(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modify config file
	newYAML := `
server:
  listen: ":9999"
backends:
  demo:
    url: "http://localhost:9200/mcp"
agents:
  test:
    display_name: "Test"
    backends:
      demo:
        allowed: true
`
	os.WriteFile(cfgPath, []byte(newYAML), 0644)

	if err := mgr.Reload(); err != nil {
		t.Fatalf("reload error: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Server.Listen != ":9999" {
		t.Errorf("expected :9999 after reload, got %s", cfg.Server.Listen)
	}
	if cfg.Backends["demo"].URL != "http://localhost:9200/mcp" {
		t.Errorf("expected new URL after reload, got %s", cfg.Backends["demo"].URL)
	}
}

func TestNewManagerFromConfig(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Listen: ":9876"},
	}
	mgr := NewManagerFromConfig(cfg)
	if mgr.Get().Server.Listen != ":9876" {
		t.Error("expected config to be stored")
	}
}

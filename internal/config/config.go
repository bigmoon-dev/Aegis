package config

import "time"

// Config is the root configuration structure.
type Config struct {
	Server   ServerConfig              `yaml:"server"`
	Backends map[string]BackendConfig  `yaml:"backends"`
	Queue    map[string]QueueConfig    `yaml:"queue"`
	Agents   map[string]AgentConfig    `yaml:"agents"`
	Approval ApprovalConfig            `yaml:"approval"`
	Audit    AuditConfig               `yaml:"audit"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Listen       string        `yaml:"listen"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// BackendConfig defines an upstream MCP server.
type BackendConfig struct {
	URL       string        `yaml:"url"`
	HealthURL string        `yaml:"health_url"`
	Timeout   time.Duration `yaml:"timeout"`
}

// QueueConfig defines FIFO queue settings per backend.
type QueueConfig struct {
	Enabled          bool                       `yaml:"enabled"`
	DelayMin         time.Duration              `yaml:"delay_min"`
	DelayMax         time.Duration              `yaml:"delay_max"`
	MaxPending       int                        `yaml:"max_pending"`
	BypassTools      []string                   `yaml:"bypass_tools"`
	GlobalRateLimits map[string]RateLimitConfig `yaml:"global_rate_limits"`
}

// AgentConfig defines per-agent access rules.
type AgentConfig struct {
	DisplayName string                       `yaml:"display_name"`
	Backends    map[string]AgentBackendConfig `yaml:"backends"`
}

// AgentBackendConfig defines an agent's access to a specific backend.
type AgentBackendConfig struct {
	Allowed          bool                       `yaml:"allowed"`
	ToolDenylist     []string                   `yaml:"tool_denylist"`
	RateLimits       map[string]RateLimitConfig `yaml:"rate_limits"`
	ApprovalRequired []string                   `yaml:"approval_required"`
}

// RateLimitConfig defines a sliding window rate limit.
type RateLimitConfig struct {
	Window   time.Duration `yaml:"window"`
	MaxCount int           `yaml:"max_count"`
}

// ApprovalConfig holds approval system settings.
type ApprovalConfig struct {
	Feishu          FeishuConfig  `yaml:"feishu"`
	Timeout         time.Duration `yaml:"timeout"`
	CallbackBaseURL string        `yaml:"callback_base_url"`
}

// FeishuConfig holds Feishu webhook settings.
type FeishuConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

// AuditConfig holds audit logging settings.
type AuditConfig struct {
	DBPath        string `yaml:"db_path"`
	RetentionDays int    `yaml:"retention_days"`
}

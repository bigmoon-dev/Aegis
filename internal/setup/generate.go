package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bigmoon-dev/aegis/internal/config"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// GenerateConfig builds a config.Config from wizard results and writes it as YAML.
func GenerateConfig(
	backend BackendInput,
	policies []ToolPolicy,
	agent AgentChoice,
	outputPath string,
) error {
	cfg := buildConfig(backend, policies, agent)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func buildConfig(backend BackendInput, policies []ToolPolicy, agent AgentChoice) *config.Config {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Listen:       ":18070",
			ReadTimeout:  300 * time.Second,
			WriteTimeout: 300 * time.Second,
		},
		Backends: map[string]config.BackendConfig{
			backend.Name: {
				URL:     backend.URL,
				Timeout: 120 * time.Second,
			},
		},
		Queue:    map[string]config.QueueConfig{},
		Agents:   map[string]config.AgentConfig{},
		Approval: config.ApprovalConfig{
			Timeout: 600 * time.Second,
		},
		Audit: config.AuditConfig{
			DBPath:        "./data/audit.db",
			RetentionDays: 90,
		},
	}

	// Build queue config
	bypassTools := []string{}
	globalRateLimits := map[string]config.RateLimitConfig{}
	hasQueuedTool := false

	for _, p := range policies {
		if p.Deny {
			continue
		}
		if !p.Queue && p.RateLimit == "unlimited" {
			bypassTools = append(bypassTools, p.Name)
		}
		if p.Queue {
			hasQueuedTool = true
		}
		// Global rate limits = agent rate limit × 2 (headroom for multiple agents)
		if p.RateLimit != "unlimited" {
			rl := parseRateLimit(p.RateLimit)
			if rl != nil {
				globalRateLimits[p.Name] = config.RateLimitConfig{
					Window:   rl.Window,
					MaxCount: rl.MaxCount * 2,
				}
			}
		}
	}

	delayMin, delayMax := resolveQueueDelays(policies)
	cfg.Queue[backend.Name] = config.QueueConfig{
		Enabled:          hasQueuedTool,
		DelayMin:         delayMin,
		DelayMax:         delayMax,
		MaxPending:       50,
		BypassTools:      bypassTools,
		GlobalRateLimits: globalRateLimits,
	}

	// Build agent config
	rateLimits := map[string]config.RateLimitConfig{}
	denylist := []string{}
	approvalRequired := []string{}

	for _, p := range policies {
		if p.Deny {
			denylist = append(denylist, p.Name)
			continue
		}
		if p.RateLimit != "unlimited" {
			rl := parseRateLimit(p.RateLimit)
			if rl != nil {
				rateLimits[p.Name] = *rl
			}
		}
		if p.Approval {
			approvalRequired = append(approvalRequired, p.Name)
		}
	}

	cfg.Agents[agent.AgentID] = config.AgentConfig{
		DisplayName: fmt.Sprintf("%s %s", agent.Adapter.Name(), cases.Title(language.English).String(backend.Name)),
		Backends: map[string]config.AgentBackendConfig{
			backend.Name: {
				Allowed:          true,
				ToolDenylist:     denylist,
				RateLimits:       rateLimits,
				ApprovalRequired: approvalRequired,
			},
		},
	}

	return cfg
}

// parseRateLimit converts "5/1h" into a RateLimitConfig.
func parseRateLimit(s string) *config.RateLimitConfig {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	count, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	window, err := parseDurationShort(parts[1])
	if err != nil {
		return nil
	}

	return &config.RateLimitConfig{
		Window:   window,
		MaxCount: count,
	}
}

// parseDurationShort parses "1h", "24h", "30m", "1d" etc.
func parseDurationShort(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// resolveQueueDelays finds the smallest min and largest max delay across all queued tools.
// Falls back to 60s/600s if no queued tools are found.
func resolveQueueDelays(policies []ToolPolicy) (time.Duration, time.Duration) {
	var minDelay, maxDelay time.Duration
	found := false

	for _, p := range policies {
		if !p.Queue || p.QueueDelay == "" || p.QueueDelay == "none" {
			continue
		}
		parts := strings.SplitN(p.QueueDelay, "-", 2)
		if len(parts) != 2 {
			continue
		}
		lo, err1 := time.ParseDuration(parts[0])
		hi, err2 := time.ParseDuration(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		if !found {
			minDelay = lo
			maxDelay = hi
			found = true
		} else {
			if lo < minDelay {
				minDelay = lo
			}
			if hi > maxDelay {
				maxDelay = hi
			}
		}
	}

	if !found {
		return 60 * time.Second, 600 * time.Second
	}
	return minDelay, maxDelay
}

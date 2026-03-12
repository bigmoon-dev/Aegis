package config

import (
	"fmt"
	"log"
	"time"
)

func validate(cfg *Config) error {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":18070"
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 300 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 300 * time.Second
	}

	if len(cfg.Backends) == 0 {
		return fmt.Errorf("at least one backend must be configured")
	}
	for name, b := range cfg.Backends {
		if b.URL == "" {
			return fmt.Errorf("backend %q: url is required", name)
		}
		if b.Timeout == 0 {
			b.Timeout = 120 * time.Second
			cfg.Backends[name] = b
		}
	}

	for name, q := range cfg.Queue {
		if _, ok := cfg.Backends[name]; !ok {
			return fmt.Errorf("queue %q: no matching backend", name)
		}
		if q.MaxPending == 0 {
			q.MaxPending = 50
			cfg.Queue[name] = q
		}
		if q.DelayMin == 0 {
			q.DelayMin = 60 * time.Second
			cfg.Queue[name] = q
		}
		if q.DelayMax == 0 {
			q.DelayMax = 600 * time.Second
			cfg.Queue[name] = q
		}
		if q.DelayMin > q.DelayMax {
			return fmt.Errorf("queue %q: delay_min (%s) must not exceed delay_max (%s)", name, q.DelayMin, q.DelayMax)
		}
	}

	for agentID, ac := range cfg.Agents {
		if ac.DisplayName == "" {
			return fmt.Errorf("agent %q: display_name is required", agentID)
		}
		for backendID := range ac.Backends {
			if _, ok := cfg.Backends[backendID]; !ok {
				return fmt.Errorf("agent %q: unknown backend %q", agentID, backendID)
			}
		}
	}

	if cfg.Audit.DBPath == "" {
		cfg.Audit.DBPath = "./data/audit.db"
	}
	if cfg.Audit.RetentionDays == 0 {
		cfg.Audit.RetentionDays = 90
	}

	if cfg.Approval.Timeout == 0 {
		cfg.Approval.Timeout = 600 * time.Second
	}

	// Only inflate timeouts if at least one agent has approval_required tools
	hasApproval := false
	for _, ac := range cfg.Agents {
		for _, bc := range ac.Backends {
			if len(bc.ApprovalRequired) > 0 {
				hasApproval = true
				break
			}
		}
		if hasApproval {
			break
		}
	}

	if hasApproval {
		// Ensure write_timeout >= approval.timeout + max backend timeout
		// Otherwise HTTP connection is killed before approval can complete
		var maxBackendTimeout time.Duration
		for _, b := range cfg.Backends {
			if b.Timeout > maxBackendTimeout {
				maxBackendTimeout = b.Timeout
			}
		}
		minWriteTimeout := cfg.Approval.Timeout + maxBackendTimeout + 30*time.Second
		if cfg.Server.WriteTimeout < minWriteTimeout {
			log.Printf("[config] adjusting write_timeout from %s to %s (approval_timeout=%s + backend_timeout=%s + 30s buffer)",
				cfg.Server.WriteTimeout, minWriteTimeout, cfg.Approval.Timeout, maxBackendTimeout)
			cfg.Server.WriteTimeout = minWriteTimeout
		}
		if cfg.Server.ReadTimeout < minWriteTimeout {
			cfg.Server.ReadTimeout = minWriteTimeout
		}
	}

	return nil
}

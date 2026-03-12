package config

// NewManagerFromConfig creates a Manager from a Config struct directly.
// Useful for tests and programmatic configuration (e.g., demo mode).
func NewManagerFromConfig(cfg *Config) *Manager {
	return &Manager{cfg: cfg}
}

package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Manager handles configuration loading and provides thread-safe access.
type Manager struct {
	mu       sync.RWMutex
	cfg      *Config
	filePath string
}

// NewManager creates a Manager and loads the config from the given path.
func NewManager(path string) (*Manager, error) {
	m := &Manager{filePath: path}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Get returns the current config (read-safe).
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Reload re-reads the config file. Returns an error if the new config is invalid.
func (m *Manager) Reload() error {
	return m.load()
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return fmt.Errorf("read config %s: %w", m.filePath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	m.mu.Lock()
	m.cfg = &cfg
	m.mu.Unlock()
	return nil
}

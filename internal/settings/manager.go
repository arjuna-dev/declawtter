package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultProvider = "codex"

type Config struct {
	DefaultProvider string `json:"default_provider"`
}

type Manager struct {
	path string
}

func NewManager() (*Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(configDir, "declaw")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Manager{path: filepath.Join(root, "settings.json")}, nil
}

func (m *Manager) DefaultProvider() (string, error) {
	config, err := m.load()
	if err != nil {
		return "", err
	}
	return normalizeProvider(config.DefaultProvider), nil
}

func (m *Manager) SetDefaultProvider(value string) error {
	provider := normalizeProvider(value)
	if err := ValidateProvider(provider); err != nil {
		return err
	}
	config, err := m.load()
	if err != nil {
		return err
	}
	config.DefaultProvider = provider
	return m.save(config)
}

func (m *Manager) Path() string {
	return m.path
}

func ValidateProvider(value string) error {
	switch normalizeProvider(value) {
	case "codex", "claude":
		return nil
	default:
		return fmt.Errorf("provider must be codex or claude, got %q", value)
	}
}

func (m *Manager) load() (Config, error) {
	raw, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{DefaultProvider: DefaultProvider}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config.DefaultProvider = normalizeProvider(config.DefaultProvider)
	return config, nil
}

func (m *Manager) save(config Config) error {
	config.DefaultProvider = normalizeProvider(config.DefaultProvider)
	if err := ValidateProvider(config.DefaultProvider); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(m.path, data, 0o644)
}

func normalizeProvider(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultProvider
	}
	return value
}

package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"declaw/internal/activity"
)

const DefaultProvider = "codex"
const DefaultCodexReasoningMode = "default"
const DefaultCodexUI = "codex"
const DefaultClaudeUI = "claude"
const DefaultTerminal = "terminal"

type Config struct {
	DefaultHarness     string `json:"default_harness,omitempty"`
	DefaultProvider    string `json:"default_provider"`
	CodexReasoningMode string `json:"codex_reasoning_mode"`
	DefaultCodexUI     string `json:"default_codex_ui"`
	DefaultClaudeUI    string `json:"default_claude_ui"`
	DefaultTerminal    string `json:"default_terminal"`
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
	if strings.TrimSpace(config.DefaultHarness) != "" {
		return normalizeProvider(config.DefaultHarness), nil
	}
	return normalizeProvider(config.DefaultProvider), nil
}

// DefaultHarness is the new name for the global agent selection. Provider is
// retained as a compatibility alias for existing scripts and settings files.
func (m *Manager) DefaultHarness() (string, error) {
	return m.DefaultProvider()
}

func (m *Manager) SetDefaultHarness(value string) error {
	return m.SetDefaultProvider(value)
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
	config.DefaultHarness = provider
	return m.save(config)
}

func (m *Manager) CodexReasoningMode() (string, error) {
	config, err := m.load()
	if err != nil {
		return "", err
	}
	return normalizeCodexReasoningMode(config.CodexReasoningMode), nil
}

func (m *Manager) SetCodexReasoningMode(value string) error {
	mode := normalizeCodexReasoningMode(value)
	if err := ValidateCodexReasoningMode(mode); err != nil {
		return err
	}
	config, err := m.load()
	if err != nil {
		return err
	}
	config.CodexReasoningMode = mode
	return m.save(config)
}

func (m *Manager) DefaultCodexUI() (string, error) {
	config, err := m.load()
	if err != nil {
		return "", err
	}
	return normalizeCodexUI(config.DefaultCodexUI), nil
}

func (m *Manager) SetDefaultCodexUI(value string) error {
	ui := normalizeCodexUI(value)
	if err := ValidateCodexUI(ui); err != nil {
		return err
	}
	config, err := m.load()
	if err != nil {
		return err
	}
	config.DefaultCodexUI = ui
	return m.save(config)
}

func (m *Manager) DefaultClaudeUI() (string, error) {
	config, err := m.load()
	if err != nil {
		return "", err
	}
	return normalizeClaudeUI(config.DefaultClaudeUI), nil
}

func (m *Manager) SetDefaultClaudeUI(value string) error {
	ui := normalizeClaudeUI(value)
	if err := ValidateClaudeUI(ui); err != nil {
		return err
	}
	config, err := m.load()
	if err != nil {
		return err
	}
	config.DefaultClaudeUI = ui
	return m.save(config)
}

func (m *Manager) DefaultTerminal() (string, error) {
	config, err := m.load()
	if err != nil {
		return "", err
	}
	return normalizeTerminal(config.DefaultTerminal), nil
}

func (m *Manager) SetDefaultTerminal(value string) error {
	terminal := normalizeTerminal(value)
	if err := ValidateTerminal(terminal); err != nil {
		return err
	}
	config, err := m.load()
	if err != nil {
		return err
	}
	config.DefaultTerminal = terminal
	return m.save(config)
}

func (m *Manager) Path() string {
	return m.path
}

func ValidateProvider(value string) error {
	return ValidateHarness(value)
}

func ValidateHarness(value string) error {
	if _, err := activity.NormalizeHarness(value); err != nil {
		return fmt.Errorf("%w, got %q", err, value)
	}
	return nil
}

func ValidateCodexReasoningMode(value string) error {
	switch normalizeCodexReasoningMode(value) {
	case "default", "no_reasoning":
		return nil
	default:
		return fmt.Errorf("codex reasoning mode must be default or no_reasoning, got %q", value)
	}
}

func ValidateCodexUI(value string) error {
	switch normalizeCodexUI(value) {
	case "app-server", "declaw", "codex":
		return nil
	default:
		return fmt.Errorf("codex UI must be app-server, declaw, or codex, got %q", value)
	}
}

func ValidateClaudeUI(value string) error {
	switch normalizeClaudeUI(value) {
	case "claude", "declaw", "print":
		return nil
	default:
		return fmt.Errorf("claude UI must be claude, declaw, or print, got %q", value)
	}
}

func ValidateTerminal(value string) error {
	switch normalizeTerminal(value) {
	case "terminal", "ghostty":
		return nil
	default:
		return fmt.Errorf("terminal must be terminal or ghostty, got %q", value)
	}
}

func (m *Manager) load() (Config, error) {
	raw, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{
			DefaultHarness:     DefaultProvider,
			DefaultProvider:    DefaultProvider,
			CodexReasoningMode: DefaultCodexReasoningMode,
			DefaultCodexUI:     DefaultCodexUI,
			DefaultClaudeUI:    DefaultClaudeUI,
			DefaultTerminal:    DefaultTerminal,
		}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config.DefaultHarness = normalizeProvider(config.DefaultHarness)
	config.DefaultProvider = normalizeProvider(config.DefaultProvider)
	if strings.TrimSpace(config.DefaultHarness) == "" || config.DefaultHarness == DefaultProvider && strings.TrimSpace(config.DefaultProvider) != "" {
		config.DefaultHarness = config.DefaultProvider
	}
	if strings.TrimSpace(config.DefaultProvider) == "" {
		config.DefaultProvider = config.DefaultHarness
	}
	config.CodexReasoningMode = normalizeCodexReasoningMode(config.CodexReasoningMode)
	config.DefaultCodexUI = normalizeCodexUI(config.DefaultCodexUI)
	config.DefaultClaudeUI = normalizeClaudeUI(config.DefaultClaudeUI)
	config.DefaultTerminal = normalizeTerminal(config.DefaultTerminal)
	return config, nil
}

func (m *Manager) save(config Config) error {
	config.DefaultHarness = normalizeProvider(config.DefaultHarness)
	config.DefaultProvider = normalizeProvider(config.DefaultProvider)
	if strings.TrimSpace(config.DefaultHarness) == "" {
		config.DefaultHarness = config.DefaultProvider
	}
	if strings.TrimSpace(config.DefaultProvider) == "" {
		config.DefaultProvider = config.DefaultHarness
	}
	config.CodexReasoningMode = normalizeCodexReasoningMode(config.CodexReasoningMode)
	config.DefaultCodexUI = normalizeCodexUI(config.DefaultCodexUI)
	config.DefaultClaudeUI = normalizeClaudeUI(config.DefaultClaudeUI)
	config.DefaultTerminal = normalizeTerminal(config.DefaultTerminal)
	if err := ValidateProvider(config.DefaultHarness); err != nil {
		return err
	}
	if err := ValidateCodexReasoningMode(config.CodexReasoningMode); err != nil {
		return err
	}
	if err := ValidateCodexUI(config.DefaultCodexUI); err != nil {
		return err
	}
	if err := ValidateClaudeUI(config.DefaultClaudeUI); err != nil {
		return err
	}
	if err := ValidateTerminal(config.DefaultTerminal); err != nil {
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

func normalizeCodexReasoningMode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", "standard", "normal":
		return DefaultCodexReasoningMode
	case "none", "no-thinking", "no_thinking", "nothinking", "no-reasoning", "noreasoning":
		return "no_reasoning"
	default:
		return value
	}
}

func normalizeCodexUI(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultCodexUI
	}
	return value
}

func normalizeClaudeUI(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultClaudeUI
	}
	return value
}

func normalizeTerminal(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultTerminal
	}
	return value
}

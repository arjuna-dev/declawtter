package activity

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultSources() []Source {
	return []Source{
		newFilesystemSource(Pi),
		newSQLiteSource(Pi),
		newFilesystemSource(Hermes),
		newSQLiteSource(Hermes),
		newFilesystemSource(Codex),
		newSQLiteSource(Codex),
		newFilesystemSource(Claude),
		newSQLiteSource(Claude),
		newFilesystemSource(OpenCode),
		newSQLiteSource(OpenCode),
		newFilesystemSource(Antigravity),
		newSQLiteSource(Antigravity),
	}
}

func knownDataLocations(harness Harness) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if strings.TrimSpace(configHome) == "" {
		configHome = filepath.Join(home, ".config")
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if strings.TrimSpace(dataHome) == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if strings.TrimSpace(stateHome) == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	applicationSupport := filepath.Join(home, "Library", "Application Support")

	candidates := []string{}
	add := func(values ...string) {
		candidates = append(candidates, values...)
	}
	addEnv := func(names ...string) {
		for _, name := range names {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				add(value)
			}
		}
	}
	switch harness {
	case Pi:
		addEnv("DECLAW_PI_HOME", "PI_CODING_AGENT_DIR", "PI_HOME", "PI_CONFIG_DIR", "PI_DATA_DIR")
		piHome := filepath.Join(home, ".pi")
		add(
			filepath.Join(piHome, "agent", "sessions"),
			filepath.Join(piHome, "sessions"),
			filepath.Join(piHome, "history.jsonl"),
			filepath.Join(piHome, "history.json"),
			filepath.Join(configHome, "pi"),
			filepath.Join(dataHome, "pi", "agent", "sessions"),
			filepath.Join(applicationSupport, "Pi", "sessions"),
			filepath.Join(applicationSupport, "Pi", "Default", "Sessions"),
			filepath.Join(applicationSupport, "pi", "sessions"),
		)
	case Hermes:
		addEnv("DECLAW_HERMES_HOME", "HERMES_HOME", "HERMES_AGENT_HOME", "HERMES_CONFIG_DIR", "HERMES_DATA_DIR", "HERMES_STATE_DIR")
		hermesHome := filepath.Join(home, ".hermes")
		add(
			filepath.Join(hermesHome, "sessions"),
			filepath.Join(hermesHome, "terminal-sessions"),
			filepath.Join(hermesHome, "runtime"),
			filepath.Join(hermesHome, "state.db"),
			filepath.Join(hermesHome, "projects.db"),
			filepath.Join(hermesHome, "shared-state.db"),
			filepath.Join(hermesHome, ".hermes_history"),
			filepath.Join(configHome, "hermes", "sessions"),
			filepath.Join(dataHome, "hermes", "sessions"),
			filepath.Join(stateHome, "hermes"),
			filepath.Join(applicationSupport, "Hermes", "sessions"),
			filepath.Join(applicationSupport, "Hermes", "state"),
			filepath.Join(applicationSupport, "Hermes", "state.db"),
			filepath.Join(applicationSupport, "Hermes", "projects.db"),
			filepath.Join(applicationSupport, "Hermes", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Hermes Agent", "sessions"),
			filepath.Join(applicationSupport, "com.hermes.agent", "sessions"),
		)
	case Codex:
		addEnv("DECLAW_CODEX_HOME", "CODEX_HOME", "CODEX_CONFIG_DIR", "CODEX_DATA_DIR")
		codexHome := filepath.Join(home, ".codex")
		add(
			filepath.Join(codexHome, "sessions"),
			filepath.Join(codexHome, "archived_sessions"),
			filepath.Join(codexHome, "sqlite"),
			filepath.Join(codexHome, "history.json"),
			filepath.Join(codexHome, "history.jsonl"),
			filepath.Join(codexHome, "session_index.jsonl"),
			filepath.Join(configHome, "codex", "sessions"),
			filepath.Join(dataHome, "codex", "sessions"),
			filepath.Join(applicationSupport, "Codex", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Codex", "sessions"),
			filepath.Join(applicationSupport, "Codex", "Local State"),
			filepath.Join(applicationSupport, "Codex", "browser-sidebar-page-states.json"),
			filepath.Join(applicationSupport, "com.openai.codex", "Default", "Sessions"),
			filepath.Join(applicationSupport, "com.openai.codex.desktop", "Default", "Sessions"),
			filepath.Join(applicationSupport, "OpenAI", "Codex", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Codex App Base", "Default", "Sessions"),
		)
	case Claude:
		addEnv("DECLAW_CLAUDE_HOME", "CLAUDE_CONFIG_DIR", "CLAUDE_HOME", "CLAUDE_DATA_DIR", "CLAUDE_STATE_DIR")
		claudeHome := filepath.Join(home, ".claude")
		add(
			filepath.Join(claudeHome, "projects"),
			filepath.Join(claudeHome, "sessions"),
			filepath.Join(claudeHome, "history.jsonl"),
			filepath.Join(configHome, "claude", "projects"),
			filepath.Join(dataHome, "claude", "projects"),
			filepath.Join(stateHome, "claude"),
			filepath.Join(applicationSupport, "Claude", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Claude", "sessions"),
			filepath.Join(applicationSupport, "Claude", "Local State"),
			filepath.Join(applicationSupport, "Claude Desktop", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Claude Desktop", "sessions"),
			filepath.Join(applicationSupport, "Anthropic", "Claude", "Default", "Sessions"),
			filepath.Join(applicationSupport, "com.anthropic.claude", "Default", "Sessions"),
			filepath.Join(applicationSupport, "com.anthropic.claude.desktop", "Default", "Sessions"),
			filepath.Join(applicationSupport, "Claude Code", "projects"),
		)
	case OpenCode:
		addEnv("DECLAW_OPENCODE_HOME", "OPENCODE_HOME", "OPENCODE_CONFIG_DIR", "OPENCODE_DATA_DIR")
		add(
			filepath.Join(dataHome, "opencode", "opencode.db"),
			filepath.Join(dataHome, "opencode", "storage"),
			filepath.Join(home, ".opencode", "opencode.db"),
			filepath.Join(configHome, "opencode"),
			filepath.Join(applicationSupport, "opencode"),
		)
	case Antigravity:
		addEnv("DECLAW_ANTIGRAVITY_HOME", "ANTIGRAVITY_HOME", "ANTIGRAVITY_CONFIG_DIR", "GEMINI_HOME")
		antigravityCLI := filepath.Join(home, ".gemini", "antigravity-cli")
		add(
			filepath.Join(antigravityCLI, "conversation_summaries.db"),
			filepath.Join(antigravityCLI, "conversations"),
			filepath.Join(antigravityCLI, "cache", "conversation_metadata.json"),
			filepath.Join(antigravityCLI, "history.jsonl"),
			filepath.Join(home, ".gemini", "antigravity"),
			filepath.Join(applicationSupport, "Antigravity", "User", "globalStorage"),
		)
	}

	// A generic override is useful for test fixtures and unusual installations.
	// Harness-specific overrides take precedence in real installations and keep
	// one agent's private state from being interpreted as another agent's data.
	for _, raw := range strings.Split(os.Getenv("DECLAW_ACTIVITY_PATHS"), string(os.PathListSeparator)) {
		if strings.TrimSpace(raw) != "" {
			candidates = append(candidates, raw)
		}
	}
	harnessEnv := "DECLAW_" + strings.ToUpper(string(harness)) + "_ACTIVITY_PATHS"
	for _, raw := range strings.Split(os.Getenv(harnessEnv), string(os.PathListSeparator)) {
		if strings.TrimSpace(raw) != "" {
			candidates = append(candidates, raw)
		}
	}
	return uniquePaths(candidates)
}

// KnownDataLocations exposes the locations selected by the adapter without
// requiring callers to know each harness's environment variables.
func KnownDataLocations(harness string) []string {
	normalized, err := NormalizeHarness(harness)
	if err != nil || normalized == "" {
		return nil
	}
	return knownDataLocations(normalized)
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, strings.TrimPrefix(path, "~"+string(os.PathSeparator)))
			}
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = filepath.Clean(abs)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

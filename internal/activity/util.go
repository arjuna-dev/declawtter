package activity

import (
	"os"
	"path/filepath"
	"strings"
)

func normalizeDirectory(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "~"+string(os.PathSeparator)) {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, strings.TrimPrefix(value, "~"+string(os.PathSeparator)))
		}
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs, true
}

func shouldIgnoreActivityPath(harness Harness, path string) bool {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if filepath.Base(path) == ".git" {
		return true
	}
	if isInternalActivityPath(path) {
		return true
	}
	if harness != Codex {
		return false
	}
	return strings.Contains(path, "/.codex/visualizations/") || strings.Contains(path, "/.codex/attachments/")
}

// IsInternalActivityPath identifies agent-owned and declaw-owned workspaces
// that should not become automatically discovered user projects. Explicitly
// tracked projects are still retained by the project registry.
func IsInternalActivityPath(path string) bool {
	return isInternalActivityPath(filepath.ToSlash(filepath.Clean(strings.TrimSpace(path))))
}

func isInternalActivityPath(path string) bool {
	if hasPathComponent(path, ".git") || hasPathComponent(path, "node_modules") {
		return true
	}
	home, err := activityHomeDir()
	if err != nil {
		return false
	}
	applicationSupport := filepath.Join(home, "Library", "Application Support")
	internalRoots := []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".hermes"),
		filepath.Join(home, ".pi"),
		filepath.Join(applicationSupport, "Claude"),
		filepath.Join(applicationSupport, "Claude Desktop"),
		filepath.Join(applicationSupport, "Anthropic", "Claude"),
		filepath.Join(applicationSupport, "com.anthropic.claude"),
		filepath.Join(applicationSupport, "com.anthropic.claude.desktop"),
		filepath.Join(applicationSupport, "Hermes"),
		filepath.Join(applicationSupport, "Hermes Agent"),
		filepath.Join(applicationSupport, "com.hermes.agent"),
		filepath.Join(applicationSupport, "Pi"),
		filepath.Join(applicationSupport, "pi"),
		filepath.Join(applicationSupport, "declaw", "ai-agent"),
		filepath.Join(applicationSupport, "declaw", "support", "runs"),
	}
	for _, root := range internalRoots {
		if pathWithin(path, filepath.ToSlash(filepath.Clean(root))) {
			return true
		}
	}
	declawProjectsRoot := filepath.Join(applicationSupport, "declaw", "projects")
	if pathWithin(path, filepath.ToSlash(filepath.Clean(declawProjectsRoot))) && hasPathComponent(path, "WORKSPACE") {
		return true
	}
	return false
}

func hasPathComponent(path, component string) bool {
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		if part == component {
			return true
		}
	}
	return false
}

func activityHomeDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		home = filepath.Clean(home)
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = filepath.Clean(resolved)
		}
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = filepath.Clean(resolved)
	}
	return home, nil
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(filepath.FromSlash(path))
	root = filepath.Clean(filepath.FromSlash(root))
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

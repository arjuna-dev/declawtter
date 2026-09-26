package instructions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	agentsFile = "AGENTS.md"
	claudeFile = "CLAUDE.md"
)

// EnsureClaudeAlias keeps CLAUDE.md pointed at AGENTS.md without creating a
// second hand-maintained instruction source. If symlinks are unavailable, it
// falls back to a plain file copy.
func EnsureClaudeAlias(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}

	agentsPath := filepath.Join(root, agentsFile)
	claudePath := filepath.Join(root, claudeFile)

	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	info, err := os.Lstat(claudePath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(claudePath)
			if err != nil {
				return err
			}
			if target == agentsFile {
				return nil
			}
		} else {
			currentData, err := os.ReadFile(claudePath)
			if err != nil {
				return err
			}
			if string(currentData) != string(agentsData) {
				return nil
			}
		}
		if err := os.Remove(claudePath); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}

	if err := os.Symlink(agentsFile, claudePath); err == nil {
		return nil
	}
	return os.WriteFile(claudePath, agentsData, 0o644)
}

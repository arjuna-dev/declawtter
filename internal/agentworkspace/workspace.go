package agentworkspace

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"declaw/internal/instructions"
)

//go:embed all:template
var workspaceFS embed.FS

func Ensure(root string) error {
	if err := fs.WalkDir(workspaceFS, "template", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("template", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(root, 0o755)
		}

		targetPath := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := workspaceFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if shouldPreserveExistingWorkspaceFile(rel) {
			if _, err := os.Stat(targetPath); err == nil {
				return nil
			}
		}
		return os.WriteFile(targetPath, data, 0o644)
	}); err != nil {
		return err
	}
	return instructions.EnsureClaudeAlias(root)
}

func shouldPreserveExistingWorkspaceFile(rel string) bool {
	switch rel {
	case "AGENTS.md", "README.md", "CLAUDE.md":
		return false
	default:
		return true
	}
}

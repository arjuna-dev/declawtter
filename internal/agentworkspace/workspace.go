package agentworkspace

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:template
var workspaceFS embed.FS

func Ensure(root string) error {
	return fs.WalkDir(workspaceFS, "template", func(path string, entry fs.DirEntry, err error) error {
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

		if _, err := os.Stat(targetPath); err == nil {
			return nil
		}
		data, err := workspaceFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}

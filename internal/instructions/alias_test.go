package instructions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureClaudeAliasCreatesAlias(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, agentsFile)
	if err := os.WriteFile(agentsPath, []byte("# AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureClaudeAlias(root); err != nil {
		t.Fatal(err)
	}

	claudePath := filepath.Join(root, claudeFile)
	info, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(claudePath)
		if err != nil {
			t.Fatal(err)
		}
		if target != agentsFile {
			t.Fatalf("symlink target = %q, want %q", target, agentsFile)
		}
		return
	}

	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# AGENTS\n" {
		t.Fatalf("CLAUDE.md contents = %q", string(data))
	}
}

func TestEnsureClaudeAliasLeavesCustomClaudeFileAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, agentsFile), []byte("# AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, claudeFile)
	if err := os.WriteFile(claudePath, []byte("# custom claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureClaudeAlias(root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# custom claude\n" {
		t.Fatalf("CLAUDE.md contents = %q, want custom file to remain", string(data))
	}
}

func TestEnsureClaudeAliasReplacesIdenticalFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, agentsFile), []byte("# AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, claudeFile)
	if err := os.WriteFile(claudePath, []byte("# AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureClaudeAlias(root); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		data, err := os.ReadFile(claudePath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "# AGENTS\n" {
			t.Fatalf("CLAUDE.md contents = %q", string(data))
		}
		return
	}

	target, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if target != agentsFile {
		t.Fatalf("symlink target = %q, want %q", target, agentsFile)
	}
}

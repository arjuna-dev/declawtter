package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectProviderOverrideRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Track([]string{"demo", "--path", projectDir}); err != nil {
		t.Fatal(err)
	}

	if got, err := manager.Settings([]string{"demo", "provider"}); err != nil {
		t.Fatal(err)
	} else if got != "inherit" {
		t.Fatalf("initial provider = %q, want inherit", got)
	}

	if _, err := manager.Settings([]string{"demo", "provider", "claude"}); err != nil {
		t.Fatal(err)
	}
	project, err := manager.Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if project.Provider != "claude" {
		t.Fatalf("project.Provider = %q, want claude", project.Provider)
	}

	if _, err := manager.Settings([]string{"demo", "provider", "inherit"}); err != nil {
		t.Fatal(err)
	}
	project, err = manager.Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if project.Provider != "" {
		t.Fatalf("project.Provider = %q, want empty", project.Provider)
	}
}

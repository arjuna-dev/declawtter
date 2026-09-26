package app

import (
	"strings"
	"testing"
	"time"

	"declaw/internal/projects"
)

func TestProjectSettingsCommandsUseFriendlyLabelsAndCanonicalLines(t *testing.T) {
	project := projects.Project{
		Name:           "019fc67e-4526-7380-b508-87a5ea778130",
		Path:           "/Users/example/Customer-Portal",
		LastActivityAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC),
	}
	commands := projectSettingsCommandChildren([]projects.Project{project}, "codex")
	if len(commands) != 1 {
		t.Fatalf("project settings commands = %#v, want one project", commands)
	}
	if got, want := commands[0].Name, "/project-settings Customer-Portal"; got != want {
		t.Fatalf("project settings label = %q, want %q", got, want)
	}
	if got, want := commands[0].CommandLine, "/project-settings 019fc67e-4526-7380-b508-87a5ea778130"; got != want {
		t.Fatalf("project settings command line = %q, want %q", got, want)
	}
	if !strings.Contains(commands[0].Description, "canonical: "+project.Name) {
		t.Fatalf("project settings description = %q, want canonical name", commands[0].Description)
	}

	children := commands[0].Children
	if len(children) != 6 {
		t.Fatalf("project settings child count = %d, want 6", len(children))
	}
	if got, want := children[0].Name, "/project-settings Customer-Portal path"; got != want {
		t.Fatalf("path label = %q, want %q", got, want)
	}
	if got, want := children[0].CommandLine, "/project-settings 019fc67e-4526-7380-b508-87a5ea778130 path"; got != want {
		t.Fatalf("path command line = %q, want %q", got, want)
	}
	if got, want := children[3].CommandLine, "/project-settings 019fc67e-4526-7380-b508-87a5ea778130 pin on"; got != want {
		t.Fatalf("pin command line = %q, want %q", got, want)
	}
	if got, want := children[4].CommandLine, "/project-settings 019fc67e-4526-7380-b508-87a5ea778130 ignore on"; got != want {
		t.Fatalf("ignore command line = %q, want %q", got, want)
	}
}

func TestProjectSettingsCommandsShowActionsToRestorePinnedIgnoredProject(t *testing.T) {
	project := projects.Project{
		Name:    "repo",
		Alias:   "Customer Portal",
		Path:    "/Users/example/repo",
		Pinned:  true,
		Ignored: true,
	}
	commands := projectSettingsCommandChildren([]projects.Project{project}, "codex")
	if got, want := commands[0].Name, "/project-settings Customer Portal"; got != want {
		t.Fatalf("project settings label = %q, want %q", got, want)
	}
	if got, want := commands[0].CommandLine, "/project-settings repo"; got != want {
		t.Fatalf("project settings command line = %q, want %q", got, want)
	}
	if got, want := commands[0].Children[3].CommandLine, "/project-settings repo pin off"; got != want {
		t.Fatalf("unpin command line = %q, want %q", got, want)
	}
	if got, want := commands[0].Children[4].CommandLine, "/project-settings repo ignore off"; got != want {
		t.Fatalf("include command line = %q, want %q", got, want)
	}
	if !strings.Contains(commands[0].Children[3].Description, "Unpin") || !strings.Contains(commands[0].Children[4].Description, "Include") {
		t.Fatalf("restore descriptions = %q, %q", commands[0].Children[3].Description, commands[0].Children[4].Description)
	}
}

package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLauncherInputRowsGrowForNewlinesAndWrapping(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  int
	}{
		{name: "empty", value: "", width: 10, want: 1},
		{name: "single line fits", value: "hello", width: 10, want: 1},
		{name: "explicit newline", value: "hello\nworld", width: 10, want: 2},
		{name: "trailing newline", value: "hello\n", width: 10, want: 2},
		{name: "wraps", value: "hello world", width: 5, want: 3},
		{name: "narrow fallback", value: "abc", width: 0, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := launcherInputRows(tt.value, tt.width); got != tt.want {
				t.Fatalf("launcherInputRows(%q, %d) = %d, want %d", tt.value, tt.width, got, tt.want)
			}
		})
	}
}

func TestLauncherInputHeightUpdatesAfterTypingNewline(t *testing.T) {
	model := newLauncherModel(Commands())

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("hello")},
		{Type: tea.KeyCtrlJ},
		{Type: tea.KeyRunes, Runes: []rune("world")},
	} {
		updated, _ := model.Update(key)
		model = updated.(launcherModel)
	}

	if got := model.input.Height(); got != 3 {
		t.Fatalf("input height = %d, want 3", got)
	}
}

func TestLauncherInputHeightUpdatesAfterWrapping(t *testing.T) {
	model := newLauncherModel(Commands())
	model.input.SetWidth(8)
	message := strings.Repeat("x", 9)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(message)})
	model = updated.(launcherModel)

	if got := model.input.Height(); got != 3 {
		t.Fatalf("input height = %d, want 3", got)
	}
}

func TestLauncherInputViewKeepsEarlierLinesVisible(t *testing.T) {
	model := newLauncherModel(Commands())

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("hello")},
		{Type: tea.KeyCtrlJ},
		{Type: tea.KeyRunes, Runes: []rune("world")},
		{Type: tea.KeyCtrlJ},
		{Type: tea.KeyRunes, Runes: []rune("again")},
	} {
		updated, _ := model.Update(key)
		model = updated.(launcherModel)
	}

	view := model.input.View()
	for _, want := range []string{"hello", "world", "again"} {
		if !strings.Contains(view, want) {
			t.Fatalf("input view %q does not contain %q", view, want)
		}
	}
}

func TestCommandsPutCheckoutAndScheduleFirst(t *testing.T) {
	commands := Commands()
	if len(commands) < 2 {
		t.Fatalf("commands = %#v, want checkout and schedule", commands)
	}
	if commands[0].Name != "/checkout" || commands[1].Name != "/schedule" {
		t.Fatalf("first commands = %q, %q; want checkout, schedule", commands[0].Name, commands[1].Name)
	}
	for _, command := range commands {
		if command.Name == "/path" || command.Name == "/remove" {
			t.Fatalf("legacy top-level project command still visible: %s", command.Name)
		}
	}
}

func TestSettingsListContainsAllHarnesses(t *testing.T) {
	commands := SettingsCommands()
	for _, harness := range []string{"pi", "hermes", "codex", "claude"} {
		found := false
		for _, command := range commands {
			if command.Name == "/settings harness "+harness {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("settings commands = %#v, missing %s", commands, harness)
		}
	}
}

func TestLauncherRunsCanonicalCommandForFriendlyLabel(t *testing.T) {
	model := newLauncherModel([]Command{{
		Name:        "/project-settings Customer Portal path",
		CommandLine: "/project-settings project-id path",
	}})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(launcherModel)
	if got, want := model.commandLine, "/project-settings project-id path"; got != want {
		t.Fatalf("launcher command line = %q, want %q", got, want)
	}
}

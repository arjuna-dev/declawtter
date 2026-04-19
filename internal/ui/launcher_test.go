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

	if got := model.input.Height(); got != 2 {
		t.Fatalf("input height = %d, want 2", got)
	}
}

func TestLauncherInputHeightUpdatesAfterWrapping(t *testing.T) {
	model := newLauncherModel(Commands())
	model.input.SetWidth(8)
	message := strings.Repeat("x", 9)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(message)})
	model = updated.(launcherModel)

	if got := model.input.Height(); got != 2 {
		t.Fatalf("input height = %d, want 2", got)
	}
}

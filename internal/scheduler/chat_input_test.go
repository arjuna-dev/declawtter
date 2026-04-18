package scheduler

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLongPasteUsesMarkerButPreservesMessage(t *testing.T) {
	model := newDeclawChatInputModel()
	paste := strings.Repeat("x", longPasteRuneThreshold)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})
	model = updated.(declawChatInputModel)

	marker := "[pasted 80 characters]"
	if got := model.input.Value(); got != marker {
		t.Fatalf("visible input = %q, want %q", got, marker)
	}
	if got := model.expandPasteMarkers(model.input.Value()); got != paste {
		t.Fatalf("expanded message = %q, want pasted text", got)
	}
}

func TestExpandPasteMarkers(t *testing.T) {
	model := declawChatInputModel{
		replacements: []pasteReplacement{
			{marker: "[pasted 120 characters]", text: strings.Repeat("a", 120)},
			{marker: "[pasted 3 characters]", text: "xyz"},
		},
	}

	got := model.expandPasteMarkers("before [pasted 120 characters]\nafter [pasted 3 characters]")
	want := "before " + strings.Repeat("a", 120) + "\nafter xyz"
	if got != want {
		t.Fatalf("expandPasteMarkers() = %q, want %q", got, want)
	}
}

func TestChatInputWidth(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		want          int
	}{
		{name: "default", terminalWidth: 0, want: 90},
		{name: "minimum", terminalWidth: 20, want: 40},
		{name: "normal", terminalWidth: 82, want: 80},
		{name: "maximum", terminalWidth: 200, want: 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatInputWidth(tt.terminalWidth); got != tt.want {
				t.Fatalf("chatInputWidth(%d) = %d, want %d", tt.terminalWidth, got, tt.want)
			}
		})
	}
}

func TestChatInputStartsAtOneRow(t *testing.T) {
	model := newDeclawChatInputModel()

	if got := model.input.Height(); got != 1 {
		t.Fatalf("initial input height = %d, want 1", got)
	}
}

func TestChatInputRowsGrowForNewlinesAndWrapping(t *testing.T) {
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
			if got := chatInputRows(tt.value, tt.width); got != tt.want {
				t.Fatalf("chatInputRows(%q, %d) = %d, want %d", tt.value, tt.width, got, tt.want)
			}
		})
	}
}

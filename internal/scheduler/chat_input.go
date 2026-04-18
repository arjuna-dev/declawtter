package scheduler

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const longPasteRuneThreshold = 80

type declawChatInputResult struct {
	Message string
	Display string
}

type pasteReplacement struct {
	marker string
	text   string
}

type declawChatInputModel struct {
	input        textarea.Model
	submitted    bool
	cancelled    bool
	replacements []pasteReplacement
	width        int
}

func readDeclawChatInput() (declawChatInputResult, error) {
	if !stdinIsTerminal() {
		fmt.Print(colorize("\nYou > ", ansiBlue))
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				return declawChatInputResult{}, io.EOF
			}
			return declawChatInputResult{}, err
		}
		message := strings.TrimSpace(line)
		return declawChatInputResult{Message: message, Display: message}, nil
	}

	model := newDeclawChatInputModel()
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return declawChatInputResult{}, err
	}
	finalModel, ok := result.(declawChatInputModel)
	if !ok {
		return declawChatInputResult{}, errors.New("chat input returned unexpected model")
	}
	if finalModel.cancelled {
		return declawChatInputResult{}, io.EOF
	}
	display := strings.TrimSpace(finalModel.input.Value())
	message := strings.TrimSpace(finalModel.expandPasteMarkers(display))
	return declawChatInputResult{Message: message, Display: display}, nil
}

func newDeclawChatInputModel() declawChatInputModel {
	ti := textarea.New()
	ti.Focus()
	ti.Placeholder = "Message declaw..."
	ti.CharLimit = 20000
	ti.SetWidth(90)
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	ti.Prompt = colorize("You > ", ansiBlue)
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.KeyMap.InsertNewline.SetKeys("ctrl+j")
	return declawChatInputModel{input: ti, width: 90}
}

func (m declawChatInputModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m declawChatInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(chatInputWidth(msg.Width))
		m.updateInputHeight()
	case tea.KeyMsg:
		if key := tea.Key(msg); key.Paste && key.Type == tea.KeyRunes {
			text := string(key.Runes)
			if pasteRuneCount(text) >= longPasteRuneThreshold {
				marker := fmt.Sprintf("[pasted %d characters]", pasteRuneCount(text))
				m.input.InsertString(marker)
				m.updateInputHeight()
				m.replacements = append(m.replacements, pasteReplacement{
					marker: marker,
					text:   text,
				})
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if strings.TrimSpace(m.input.Value()) == "" {
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.updateInputHeight()
	return m, cmd
}

func (m declawChatInputModel) View() string {
	if m.submitted || m.cancelled {
		return ""
	}
	hint := colorize("Enter sends. Ctrl+J adds a line break. /raw opens the raw Codex session.", ansiDim)
	return "\n" + m.input.View() + "\n" + hint
}

func (m *declawChatInputModel) updateInputHeight() {
	m.input.SetHeight(chatInputRows(m.input.Value(), m.input.Width()))
}

func (m declawChatInputModel) expandPasteMarkers(display string) string {
	message := display
	for _, replacement := range m.replacements {
		message = strings.Replace(message, replacement.marker, replacement.text, 1)
	}
	return message
}

func chatInputWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		return 90
	}
	width := terminalWidth - 2
	if width < 40 {
		return 40
	}
	if width > 120 {
		return 120
	}
	return width
}

func chatInputRows(value string, width int) int {
	if width < 1 {
		width = 1
	}
	if value == "" {
		return 1
	}

	rows := 0
	for _, line := range strings.Split(value, "\n") {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			rows++
			continue
		}
		rows += (lineWidth + width - 1) / width
	}
	if rows < 1 {
		return 1
	}
	return rows
}

func pasteRuneCount(value string) int {
	return len([]rune(value))
}

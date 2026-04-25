package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Command struct {
	Name        string
	Description string
	Children    []Command
}

type Result struct {
	Line string
	Exit bool
}

const visibleCommandLimit = 12

func Commands() []Command {
	return []Command{
		{Name: "/create", Description: "Create a new declaw project/assistant"},
		{Name: "/track", Description: "Track an existing directory as a declaw project"},
		{Name: "/checkout", Description: "Open the configured agent in a tracked project"},
		{Name: "/settings", Description: "Show or change declaw settings", Children: SettingsCommands()},
		{Name: "/list", Description: "List tracked projects"},
		{Name: "/path", Description: "Print a tracked project path"},
		{Name: "/remove", Description: "Remove a tracked project"},
		{Name: "/schedule", Description: "Schedule commands and installed jobs", Children: ScheduleCommands()},
		{Name: "/exit", Description: "Exit declaw"},
	}
}

func SettingsCommands() []Command {
	return []Command{
		{Name: "/settings provider", Description: "Print the configured agent provider"},
		{Name: "/settings provider codex", Description: "Use Codex for launcher input and checkout"},
		{Name: "/settings provider claude", Description: "Use Claude Code for launcher input and checkout"},
	}
}

func ScheduleCommands() []Command {
	return []Command{
		{Name: "/schedule list", Description: "List installed scheduled jobs"},
		{Name: "/schedule status", Description: "Show launchctl status for a job"},
		{Name: "/schedule enable", Description: "Enable a scheduled job"},
		{Name: "/schedule disable", Description: "Disable a scheduled job"},
		{Name: "/schedule restart", Description: "Restart a scheduled job"},
		{Name: "/schedule run", Description: "Trigger a scheduled job immediately"},
		{Name: "/schedule remove", Description: "Remove a scheduled job"},
		{Name: "/schedule remove-all", Description: "Remove every installed scheduled job"},
		{Name: "/schedule prune-once", Description: "Clean up completed one-off job records"},
		{Name: "/schedule get-prompt", Description: "Print the stored prompt for an agent schedule"},
		{Name: "/schedule get-time", Description: "Print the stored time for a schedule"},
		{Name: "/schedule codex", Description: "Schedule a Codex run"},
		{Name: "/schedule claude", Description: "Schedule a Claude Code run"},
		{Name: "/schedule edit", Description: "Edit an existing scheduled job, including provider"},
	}
}

type Launcher struct {
	commands []Command
}

func NewLauncher(commands []Command) *Launcher {
	return &Launcher{
		commands: commands,
	}
}

func (l *Launcher) Run() (Result, error) {
	if input := os.Getenv("DECLAW_LAUNCHER_INPUT"); input != "" {
		_ = os.Unsetenv("DECLAW_LAUNCHER_INPUT")
		return Result{Line: input, Exit: strings.EqualFold(strings.TrimSpace(input), "/exit")}, nil
	}
	model := newLauncherModel(l.commands)
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return Result{}, err
	}

	finalModel, ok := result.(launcherModel)
	if !ok {
		return Result{}, fmt.Errorf("unexpected launcher model type")
	}
	return Result{Line: finalModel.commandLine, Exit: finalModel.exit}, nil
}

type launcherModel struct {
	input        textarea.Model
	commands     []Command
	commandStack [][]Command
	breadcrumbs  []string
	filtered     []Command
	selected     int
	offset       int
	commandLine  string
	exit         bool
}

func newLauncherModel(commands []Command) launcherModel {
	ti := textarea.New()
	ti.Focus()
	ti.Placeholder = "Ask declaw..."
	ti.CharLimit = 4000
	ti.SetWidth(80)
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	ti.Prompt = "> "
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.KeyMap.InsertNewline.SetKeys("ctrl+j")

	model := launcherModel{
		input:    ti,
		commands: commands,
	}
	model.updateInputHeight()
	model.filtered = model.filterCommands("")
	return model
}

func (m launcherModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m launcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if len(m.breadcrumbs) > 0 {
				m.commands = m.commandStack[len(m.commandStack)-1]
				m.commandStack = m.commandStack[:len(m.commandStack)-1]
				m.breadcrumbs = m.breadcrumbs[:len(m.breadcrumbs)-1]
				m.input.SetValue("")
				m.updateInputHeight()
				m.filtered = m.filterCommands("")
				m.selected = 0
				return m, nil
			}
			return m, tea.Quit
		case "backspace":
			if strings.TrimSpace(m.input.Value()) == "" && len(m.breadcrumbs) > 0 {
				m.commands = m.commandStack[len(m.commandStack)-1]
				m.commandStack = m.commandStack[:len(m.commandStack)-1]
				m.breadcrumbs = m.breadcrumbs[:len(m.breadcrumbs)-1]
				m.filtered = m.filterCommands("")
				m.selected = 0
				return m, nil
			}
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			m.adjustOffset()
			return m, nil
		case "down":
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			m.adjustOffset()
			return m, nil
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			if strings.EqualFold(line, "/exit") {
				m.exit = true
				return m, tea.Quit
			}
			if line == "" && len(m.filtered) > 0 {
				if len(m.filtered[m.selected].Children) > 0 {
					m.breadcrumbs = append(m.breadcrumbs, m.filtered[m.selected].Name)
					m.commandStack = append(m.commandStack, m.commands)
					m.commands = m.filtered[m.selected].Children
					m.input.SetValue("")
					m.updateInputHeight()
					m.filtered = m.filterCommands("")
					m.selected = 0
					return m, nil
				}
				m.input.SetValue(m.filtered[m.selected].Name + " ")
				m.updateInputHeight()
				m.filtered = m.filterCommands(m.input.Value())
				m.selected = 0
				return m, nil
			}
			if shouldAutocomplete(line, m.filtered, m.selected) {
				if len(m.filtered[m.selected].Children) > 0 {
					m.breadcrumbs = append(m.breadcrumbs, m.filtered[m.selected].Name)
					m.commandStack = append(m.commandStack, m.commands)
					m.commands = m.filtered[m.selected].Children
					m.input.SetValue("")
					m.updateInputHeight()
					m.filtered = m.filterCommands("")
					m.selected = 0
					return m, nil
				}
				m.input.SetValue(m.filtered[m.selected].Name + " ")
				m.updateInputHeight()
				m.filtered = m.filterCommands(m.input.Value())
				m.selected = 0
				return m, nil
			}
			m.commandLine = line
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.updateInputHeight()
	m.filtered = m.filterCommands(m.input.Value())
	if m.selected >= len(m.filtered) {
		m.selected = max(0, len(m.filtered)-1)
	}
	m.adjustOffset()
	return m, cmd
}

func (m *launcherModel) updateInputHeight() {
	m.input.SetHeight(launcherInputRows(m.input.Value(), m.input.Width()))
}

func launcherInputRows(value string, width int) int {
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

func (m *launcherModel) adjustOffset() {
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+visibleCommandLimit {
		m.offset = m.selected - visibleCommandLimit + 1
	}
	maxOffset := max(0, len(m.filtered)-visibleCommandLimit)
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func shouldAutocomplete(line string, filtered []Command, selected int) bool {
	if len(filtered) == 0 || selected >= len(filtered) {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "/") {
		return false
	}
	if strings.Contains(line, " ") {
		return false
	}
	return strings.TrimSpace(line) != filtered[selected].Name
}

func (m launcherModel) View() string {
	titleText := "declaw"
	if len(m.breadcrumbs) > 0 {
		titleText += " / " + strings.Join(m.breadcrumbs, " / ")
	}
	title := lipgloss.NewStyle().Bold(true).Render(titleText)
	hintText := "Type to ask declaw. Use / for commands. Enter sends, Ctrl+J newline, Esc quits."
	if len(m.breadcrumbs) > 0 {
		hintText = "Use arrows to move, Enter to select or run, Esc/Backspace to go back."
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(hintText)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2563EB")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	lines := []string{
		title,
		"",
		m.input.View(),
		"",
	}

	if m.hideCommands() {
		lines = append(lines, muted.Render("Press Enter to ask declaw. Ctrl+J adds a new line."))
	} else if len(m.filtered) == 0 {
		lines = append(lines, muted.Render("No matching commands"))
	} else {
		limit := min(len(m.filtered), visibleCommandLimit)
		start := min(m.offset, max(0, len(m.filtered)-limit))
		end := min(len(m.filtered), start+limit)
		if start > 0 {
			lines = append(lines, muted.Render(fmt.Sprintf("↑ %d more", start)))
		}
		for idx, command := range m.filtered[start:end] {
			absoluteIdx := start + idx
			line := fmt.Sprintf("%s  %s", command.Name, muted.Render(command.Description))
			if absoluteIdx == m.selected {
				lines = append(lines, selectedStyle.Render(line))
				continue
			}
			lines = append(lines, line)
		}
		if end < len(m.filtered) {
			lines = append(lines, muted.Render(fmt.Sprintf("↓ %d more", len(m.filtered)-end)))
		}
	}

	lines = append(lines, "", hint)
	return strings.Join(lines, "\n")
}

func (m launcherModel) filterCommands(input string) []Command {
	query := strings.ToLower(strings.TrimSpace(input))
	if query == "" {
		return append([]Command(nil), m.commands...)
	}
	if !strings.HasPrefix(query, "/") {
		return nil
	}

	firstToken := strings.Fields(query)
	needle := query
	if len(firstToken) > 0 {
		needle = firstToken[0]
		if strings.HasPrefix(query, "/schedule ") && len(firstToken) >= 2 {
			needle = strings.Join(firstToken[:2], " ")
		}
	}

	filtered := make([]Command, 0, len(m.commands))
	for _, command := range m.commands {
		name := strings.ToLower(command.Name)
		desc := strings.ToLower(command.Description)
		if strings.Contains(name, needle) || strings.Contains(desc, query) {
			filtered = append(filtered, command)
		}
	}
	return filtered
}

func (m launcherModel) hideCommands() bool {
	value := strings.TrimSpace(m.input.Value())
	return value != "" && !strings.HasPrefix(value, "/")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

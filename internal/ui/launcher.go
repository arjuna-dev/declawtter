package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Command struct {
	Name        string
	Description string
}

func Commands() []Command {
	return []Command{
		{Name: "create", Description: "Create a tracked copy of the workspace template"},
		{Name: "list", Description: "List tracked projects"},
		{Name: "path", Description: "Print a tracked project path"},
		{Name: "remove", Description: "Remove a tracked project"},
		{Name: "schedule list", Description: "List installed scheduled jobs"},
		{Name: "schedule status", Description: "Show launchctl status for a job"},
		{Name: "schedule enable", Description: "Enable a scheduled job"},
		{Name: "schedule disable", Description: "Disable a scheduled job"},
		{Name: "schedule restart", Description: "Restart a scheduled job"},
		{Name: "schedule run", Description: "Trigger a scheduled recurring job immediately"},
		{Name: "schedule remove", Description: "Remove a scheduled job"},
		{Name: "schedule remove-all", Description: "Remove every installed scheduled job"},
		{Name: "schedule prune-once", Description: "Clean up completed one-off job records"},
		{Name: "schedule get-prompt", Description: "Print the stored prompt for a Codex schedule"},
		{Name: "schedule get-time", Description: "Print the stored time for a schedule"},
		{Name: "schedule codex", Description: "Schedule a Codex run"},
		{Name: "schedule reminder", Description: "Schedule a macOS notification reminder"},
		{Name: "schedule edit", Description: "Edit an existing scheduled job"},
	}
}

type Executor func(string) (string, error)

type Launcher struct {
	commands []Command
	exec     Executor
}

func NewLauncher(commands []Command, exec Executor) *Launcher {
	return &Launcher{
		commands: commands,
		exec:     exec,
	}
}

func (l *Launcher) Run() (string, error) {
	model := newLauncherModel(l.commands, l.exec)
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return "", err
	}

	finalModel, ok := result.(launcherModel)
	if !ok {
		return "", fmt.Errorf("unexpected launcher model type")
	}
	if finalModel.err != nil {
		return "", finalModel.err
	}
	return finalModel.output, nil
}

type launcherModel struct {
	input     textinput.Model
	commands  []Command
	filtered  []Command
	selected  int
	executing bool
	output    string
	err       error
	exec      Executor
}

type commandResultMsg struct {
	output string
	err    error
}

func newLauncherModel(commands []Command, exec Executor) launcherModel {
	ti := textinput.New()
	ti.Focus()
	ti.Placeholder = "Type a declaw command"
	ti.CharLimit = 512
	ti.Width = 80

	model := launcherModel{
		input:    ti,
		commands: commands,
		exec:     exec,
	}
	model.filtered = model.filterCommands("")
	return model
}

func (m launcherModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m launcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandResultMsg:
		m.executing = false
		m.output = msg.output
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			if line == "" && len(m.filtered) > 0 {
				m.input.SetValue(m.filtered[m.selected].Name + " ")
				m.filtered = m.filterCommands(m.input.Value())
				m.selected = 0
				return m, nil
			}
			if shouldAutocomplete(line, m.filtered, m.selected) {
				m.input.SetValue(m.filtered[m.selected].Name + " ")
				m.filtered = m.filterCommands(m.input.Value())
				m.selected = 0
				return m, nil
			}
			m.executing = true
			commandLine := line
			return m, func() tea.Msg {
				output, err := m.exec(commandLine)
				return commandResultMsg{output: output, err: err}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filtered = m.filterCommands(m.input.Value())
	if m.selected >= len(m.filtered) {
		m.selected = max(0, len(m.filtered)-1)
	}
	return m, cmd
}

func shouldAutocomplete(line string, filtered []Command, selected int) bool {
	if len(filtered) == 0 || selected >= len(filtered) {
		return false
	}
	if strings.Contains(line, " ") {
		return false
	}
	return strings.TrimSpace(line) != filtered[selected].Name
}

func (m launcherModel) View() string {
	title := lipgloss.NewStyle().Bold(true).Render("declaw")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Use arrows to move, Enter to select or run, Esc to quit.")
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2563EB")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	lines := []string{
		title,
		"",
		m.input.View(),
		"",
	}

	if len(m.filtered) == 0 {
		lines = append(lines, muted.Render("No matching commands"))
	} else {
		for idx, command := range m.filtered {
			line := fmt.Sprintf("%s  %s", command.Name, muted.Render(command.Description))
			if idx == m.selected {
				lines = append(lines, selectedStyle.Render(line))
				continue
			}
			lines = append(lines, line)
		}
	}

	lines = append(lines, "", hint)
	if m.executing {
		lines = append(lines, "", muted.Render("Running command..."))
	}

	return strings.Join(lines, "\n")
}

func (m launcherModel) filterCommands(input string) []Command {
	query := strings.ToLower(strings.TrimSpace(input))
	if query == "" {
		return append([]Command(nil), m.commands...)
	}

	firstToken := strings.Fields(query)
	needle := query
	if len(firstToken) > 0 {
		needle = firstToken[0]
		if strings.HasPrefix(query, "schedule ") && len(firstToken) >= 2 {
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

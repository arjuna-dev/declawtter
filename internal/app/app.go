package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"declaw/internal/agentworkspace"
	"declaw/internal/projects"
	"declaw/internal/scheduler"
	"declaw/internal/ui"
)

type App struct {
	projects *projects.Manager
	schedule *scheduler.Manager
}

func New() (*App, error) {
	projectManager, err := projects.NewManager()
	if err != nil {
		return nil, err
	}

	scheduleManager, err := scheduler.NewManager(projectManager)
	if err != nil {
		return nil, err
	}

	return &App{
		projects: projectManager,
		schedule: scheduleManager,
	}, nil
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		return a.runInteractive()
	}

	output, err := a.execute(args)
	if output != "" {
		fmt.Fprintln(os.Stdout, output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func (a *App) runInteractive() int {
	for {
		commands, err := a.interactiveCommands()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		launcher := ui.NewLauncher(commands)
		result, err := launcher.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		line := strings.TrimSpace(result.Line)
		fields, err := parseCommandLine(line)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if result.Exit || len(fields) == 0 || strings.EqualFold(line, "/exit") {
			return 0
		}
		if !strings.HasPrefix(fields[0], "/") {
			output, err := a.aiAgent([]string{line})
			if output != "" {
				fmt.Fprintln(os.Stdout, output)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		}
		fields[0] = strings.TrimPrefix(fields[0], "/")

		output, err := a.execute(fields)
		if output != "" {
			fmt.Fprintln(os.Stdout, output)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			if shouldExitAfterInteractiveCommand(fields) {
				return 1
			}
		}
		if shouldExitAfterInteractiveCommand(fields) {
			return 0
		}
		if os.Getenv("DECLAW_LAUNCHER_ONCE") != "" {
			return 0
		}
		fmt.Fprintln(os.Stdout)
	}
}

func shouldExitAfterInteractiveCommand(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "checkout", "create", "track", "remove":
		return true
	case "schedule":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "remove", "remove-all", "codex", "edit":
			return true
		}
	}
	return false
}

func parseCommandLine(line string) ([]string, error) {
	if strings.TrimSpace(line) == "" {
		return nil, nil
	}

	var args []string
	var current strings.Builder
	inQuote := rune(0)
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		current.WriteRune('\\')
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote in command input")
	}
	flush()
	return args, nil
}

func (a *App) execute(args []string) (string, error) {
	if len(args) == 0 {
		return a.help(), nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		return a.help(), nil
	case "create":
		return a.projects.Create(args[1:])
	case "track":
		return a.projects.Track(args[1:])
	case "checkout":
		return a.checkout(args[1:])
	case "ai-agent":
		return a.aiAgent(args[1:])
	case "list":
		return a.projects.List(args[1:])
	case "path":
		return a.projects.Path(args[1:])
	case "remove":
		return a.projects.Remove(args[1:])
	case "schedule":
		return a.schedule.Execute(args[1:])
	default:
		return "", fmt.Errorf("unknown command %q\n\n%s", args[0], a.help())
	}
}

func (a *App) aiAgent(args []string) (string, error) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return "usage: declaw ai-agent [prompt]\n\nOpen Codex in declaw's embedded management-agent workspace. If prompt is provided, it is passed to Codex as the starting task.", nil
	}
	workspace, err := declawAgentWorkspace()
	if err != nil {
		return "", err
	}
	if err := agentworkspace.Ensure(workspace); err != nil {
		return "", err
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return "", fmt.Errorf("codex command not found in PATH")
	}

	cmdArgs := []string{}
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt != "" {
		cmdArgs = append(cmdArgs, prompt)
	}
	cmd := exec.Command("codex", cmdArgs...)
	cmd.Dir = workspace
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return "", cmd.Run()
}

func declawAgentWorkspace() (string, error) {
	dataDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "declaw", "ai-agent"), nil
}

func (a *App) interactiveCommands() ([]ui.Command, error) {
	commands := ui.Commands()

	projects, err := a.projects.Projects()
	if err != nil {
		return nil, err
	}
	projectChildren := make([]ui.Command, 0, len(projects))
	for _, project := range projects {
		projectChildren = append(projectChildren, ui.Command{
			Name:        project.Name,
			Description: project.Path,
		})
	}

	jobs, err := a.schedule.Jobs()
	if err != nil {
		return nil, err
	}
	scheduleChildren := ui.ScheduleCommands()
	for _, job := range jobs {
		scheduleChildren = append(scheduleChildren, ui.Command{
			Name:        job.Name,
			Description: fmt.Sprintf("%s %s", job.Type, job.Config.Kind),
			Children: []ui.Command{
				{Name: "/schedule run " + job.Name, Description: "Trigger job immediately"},
				{Name: "/schedule status " + job.Name, Description: "Show launchctl status"},
				{Name: "/schedule enable " + job.Name, Description: "Enable scheduled job"},
				{Name: "/schedule disable " + job.Name, Description: "Disable scheduled job"},
				{Name: "/schedule restart " + job.Name, Description: "Restart scheduled job"},
				{Name: "/schedule get-prompt " + job.Name, Description: "Print stored prompt"},
				{Name: "/schedule get-time " + job.Name, Description: "Print stored time"},
				{Name: "/schedule remove " + job.Name, Description: "Remove scheduled job"},
			},
		})
	}
	jobChildren := make([]ui.Command, 0, len(jobs))
	for _, job := range jobs {
		jobChildren = append(jobChildren, ui.Command{
			Name:        job.Name,
			Description: fmt.Sprintf("%s %s", job.Type, job.Config.Kind),
		})
	}
	scheduleJobActions := map[string]string{
		"/schedule status":     "status",
		"/schedule enable":     "enable",
		"/schedule disable":    "disable",
		"/schedule restart":    "restart",
		"/schedule run":        "run",
		"/schedule remove":     "remove",
		"/schedule get-prompt": "get-prompt",
		"/schedule get-time":   "get-time",
	}
	for idx := range scheduleChildren {
		action, ok := scheduleJobActions[scheduleChildren[idx].Name]
		if !ok {
			continue
		}
		scheduleChildren[idx].Children = scheduleJobCommandChildren(action, jobChildren)
		if len(jobChildren) == 0 {
			scheduleChildren[idx].Description = "No installed jobs"
		}
	}

	for idx := range commands {
		switch commands[idx].Name {
		case "/checkout":
			commands[idx].Children = projectCommandChildren("checkout", projectChildren)
			if len(projectChildren) == 0 {
				commands[idx].Description = "No tracked projects"
			}
		case "/path":
			commands[idx].Children = projectCommandChildren("path", projectChildren)
			if len(projectChildren) == 0 {
				commands[idx].Description = "No tracked projects"
			}
		case "/remove":
			commands[idx].Children = projectCommandChildren("remove", projectChildren)
			if len(projectChildren) == 0 {
				commands[idx].Description = "No tracked projects"
			}
		case "/schedule":
			commands[idx].Children = scheduleChildren
		}
	}

	return commands, nil
}

func projectCommandChildren(action string, projects []ui.Command) []ui.Command {
	children := make([]ui.Command, 0, len(projects))
	for _, project := range projects {
		children = append(children, ui.Command{
			Name:        "/" + action + " " + project.Name,
			Description: project.Description,
		})
	}
	return children
}

func scheduleJobCommandChildren(action string, jobs []ui.Command) []ui.Command {
	children := make([]ui.Command, 0, len(jobs))
	for _, job := range jobs {
		children = append(children, ui.Command{
			Name:        "/schedule " + action + " " + job.Name,
			Description: job.Description,
		})
	}
	return children
}

func (a *App) checkout(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: declaw checkout <project>")
	}
	project, err := a.projects.Get(args[0])
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return "", fmt.Errorf("codex command not found in PATH")
	}

	cmd := exec.Command("codex")
	cmd.Dir = project.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return "", cmd.Run()
}

func (a *App) help() string {
	return strings.TrimSpace(`
declaw

Commands:
  create <name> [--into <dir>] [--source <dir>]
  track <name> --path <dir>
  checkout <name>
  ai-agent [prompt]
  list
  path <name>
  remove <name>
  schedule list
  schedule status <job>
  schedule enable <job>
  schedule disable <job>
  schedule restart <job>
  schedule run <job>
  schedule remove <job>
  schedule remove-all
  schedule prune-once
  schedule get-prompt <job>
  schedule get-time <job>
  schedule codex <job> --prompt <text> --project <name> [recurring schedule flags]
  schedule codex <job> --prompt <text> [--project <name> | --workspace <path>] --at "YYYY-MM-DD HH:MM"
  schedule edit <job> [schedule flags] [--prompt <text>] [--project <name>]

Schedule flags:
  --daily HH:MM
  --weekdays HH:MM
  --weekly mon@09:30
  --at "YYYY-MM-DD HH:MM"        One-off schedule at an exact future time.
  --time HH:MM
  --once                         Make explicit --year/--month/--day fields one-off.
  --year YYYY --month M --day D --hour H --minute M [--weekday mon]
  --cwd <dir> --stdout <path> --stderr <path> --env KEY=VALUE
  --ui app-server|declaw|codex   app-server is the default clean chat UI; declaw uses the legacy codex exec UI; codex opens the raw Codex TUI.
  --no-recurring-fallback

Agent workflow:
  1. For a recurring Codex job, first choose a declaw project.
  2. If the target directory already exists, run declaw track <name> --path <dir>, then use --project <name>.
  3. If a fresh assistant workspace is needed, run declaw create <name> --into <parent-dir>, then use --project <name>.
  4. For one-off Codex jobs only, --project/--workspace may be omitted; declaw uses ~/.local/share/declaw/workspaces/one-off.
  5. Do not create recurring Codex schedules without --project; the CLI rejects that because recurring jobs need managed project context.

Examples:
  declaw create pm-workspace --into ~/Documents/dev
  declaw track product-repo --path ~/Documents/dev/my-repo
  declaw checkout pm-workspace
  declaw ai-agent "Create a recurring Codex schedule for my PM review."
  declaw schedule codex pm-deadline-review --project pm-workspace --weekdays 09:00 --prompt "Review the workspace, update the relevant CLI-managed directory, inspect the codebase context, and propose PM deadline follow-ups."
  declaw schedule codex repo-review --project product-repo --daily 10:00 --prompt "Review this repo for PM risks, deadlines, and next actions."
`)
}

package app

import (
	"fmt"
	"os"
	"strings"

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
	launcher := ui.NewLauncher(ui.Commands(), func(line string) (string, error) {
		fields, err := parseCommandLine(strings.TrimSpace(line))
		if err != nil {
			return "", err
		}
		if len(fields) == 0 {
			return "", nil
		}
		return a.execute(fields)
	})

	output, err := launcher.Run()
	if output != "" {
		fmt.Fprintln(os.Stdout, output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
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

func (a *App) help() string {
	return strings.TrimSpace(`
declaw

Commands:
  create <name> [--into <dir>] [--source <dir>]
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
  schedule codex <job> --prompt <text> (--project <name> | --workspace <path> | --no-workspace) [schedule flags]
  schedule reminder <job> --title <text> --body <text> [schedule flags]
  schedule edit <job> [schedule flags] [--prompt <text>] [--project <name> | --workspace <path> | --no-workspace] [--title <text>] [--body <text>]

Schedule flags:
  --daily HH:MM
  --weekdays HH:MM
  --weekly mon@09:30
  --at "YYYY-MM-DD HH:MM"
  --time HH:MM
  --once
  --year YYYY --month M --day D --hour H --minute M [--weekday mon]
  --cwd <dir> --stdout <path> --stderr <path> --env KEY=VALUE
  --no-recurring-fallback
`)
}

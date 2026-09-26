package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"declaw/internal/activity"
	"declaw/internal/agentworkspace"
	"declaw/internal/paths"
	"declaw/internal/projects"
	"declaw/internal/scheduler"
	"declaw/internal/settings"
	"declaw/internal/ui"
)

type App struct {
	projects       *projects.Manager
	schedule       *scheduler.Manager
	settings       *settings.Manager
	activityDetect *activity.Detector
}

func New() (*App, error) {
	projectManager, err := projects.NewManager()
	if err != nil {
		return nil, err
	}
	settingsManager, err := settings.NewManager()
	if err != nil {
		return nil, err
	}
	scheduleManager, err := scheduler.NewManager(projectManager, settingsManager)
	if err != nil {
		return nil, err
	}

	application := &App{
		projects:       projectManager,
		schedule:       scheduleManager,
		settings:       settingsManager,
		activityDetect: activity.DefaultDetector(),
	}
	return application, nil
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
		if result.Exit || line == "" || strings.EqualFold(line, "/exit") {
			return 0
		}
		if !strings.HasPrefix(line, "/") {
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
		fields, err := parseCommandLine(line)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(fields) == 0 {
			return 0
		}
		fields[0] = strings.TrimPrefix(fields[0], "/")

		if fields[0] == "create" && (len(fields) == 1 || len(fields) >= 2 && len(fields) <= 3 && !strings.HasPrefix(fields[1], "-")) {
			currentDirectory, err := os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			projectName := filepath.Base(filepath.Clean(currentDirectory))
			createArgs := []string{"create", projectName, "--path", currentDirectory}
			if len(fields) >= 2 {
				projectName = fields[1]
				createArgs[1] = projectName
			}
			if len(fields) == 3 {
				createArgs = append(createArgs, "--harness", fields[2])
			} else if len(fields) == 1 {
				defaultHarness, err := a.settings.DefaultHarness()
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				createArgs = append(createArgs, "--harness", defaultHarness)
			}
			fields = createArgs
		}

		if fields[0] == "track" && len(fields) <= 2 {
			accepted, err := confirmCurrentDirectoryTracking()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if !accepted {
				fmt.Fprintln(os.Stdout, "aborted")
				if os.Getenv("DECLAW_LAUNCHER_ONCE") != "" {
					return 0
				}
				continue
			}
			currentDirectory, err := os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			projectName := filepath.Base(filepath.Clean(currentDirectory))
			if len(fields) == 2 && strings.TrimSpace(fields[1]) != "" {
				projectName = fields[1]
			}
			fields = []string{"track", projectName, "--path", currentDirectory}
		}

		if fields[0] == "project-settings" && len(fields) == 4 && strings.EqualFold(fields[2], "give") && strings.EqualFold(fields[3], "alias") {
			alias, err := readInteractiveValue("give alias: ")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fields = append(fields, alias)
		}

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
		if err == nil && len(fields) > 0 && fields[0] == "create" {
			projectName := createdProjectName(output)
			if projectName == "" {
				fmt.Fprintln(os.Stderr, "created project but could not determine its name for checkout")
				return 1
			}
			if _, err := a.checkout([]string{projectName, "continue"}); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
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
	case "checkout":
		return true
	}
	return false
}

func createdProjectName(output string) string {
	firstLine := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	parts := strings.Fields(firstLine)
	if len(parts) == 2 && parts[0] == "created" {
		return parts[1]
	}
	return ""
}

func confirmCurrentDirectoryTracking() (bool, error) {
	fmt.Fprint(os.Stdout, "track this directory? Y/n ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}

func readInteractiveValue(prompt string) (string, error) {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
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
	case "project-settings":
		return a.projects.Settings(args[1:])
	case "project":
		return a.projectCommand(args[1:])
	case "ai-agent":
		return a.aiAgent(args[1:])
	case "settings", "config":
		return a.settingsCommand(args[1:])
	case "list":
		if err := a.refreshActivity(); err != nil {
			return "", err
		}
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

func (a *App) projectCommand(args []string) (string, error) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return "usage: declaw project-settings <name> harness [pi|hermes|codex|claude|inherit]\n       declaw project-settings <name> give alias <display name>\n       declaw project-settings <name> pin [on|off]\n       declaw project-settings <name> ignore [on|off]\n       declaw project-settings <name> path\n       declaw project-settings <name> remove\n\nManage per-project harness, display-name, pin, ignore, and filesystem settings.", nil
	}
	switch args[0] {
	case "settings":
		return a.projects.Settings(args[1:])
	default:
		return "", fmt.Errorf("unknown project subcommand %q\n\nusage: declaw project-settings <name> harness [pi|hermes|codex|claude|inherit]", args[0])
	}
}

func (a *App) aiAgent(args []string) (string, error) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return "usage: declaw ai-agent [prompt]\n\nOpen the configured agent provider in declaw's embedded management-agent workspace. If prompt is provided, it is passed to the agent as the starting task.", nil
	}
	workspace, err := declawAgentWorkspace()
	if err != nil {
		return "", err
	}
	if err := agentworkspace.Ensure(workspace); err != nil {
		return "", err
	}
	provider, err := a.settings.DefaultProvider()
	if err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(strings.Join(args, " "))
	codexReasoningMode, err := a.effectiveCodexReasoningMode()
	if err != nil {
		return "", err
	}
	program, cmdArgs, err := agentCommand(provider, prompt, codexReasoningMode)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(program); err != nil {
		return "", fmt.Errorf("%s command not found in PATH", program)
	}
	cmd := exec.Command(program, cmdArgs...)
	cmd.Dir = workspace
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return "", cmd.Run()
}

func declawAgentWorkspace() (string, error) {
	return paths.AgentWorkspaceDir()
}

func (a *App) refreshActivity() error {
	if a.activityDetect == nil {
		a.activityDetect = activity.DefaultDetector()
	}
	records, err := a.activityDetect.Discover()
	if mergeErr := a.projects.MergeActivities(records); mergeErr != nil {
		return mergeErr
	}
	if err != nil {
		// A source can be unavailable while other adapters still provide useful
		// activity. The detector returns partial results, so keep the CLI useful.
		return nil
	}
	return nil
}

func (a *App) settingsCommand(args []string) (string, error) {
	if len(args) == 0 {
		provider, err := a.settings.DefaultProvider()
		if err != nil {
			return "", err
		}
		codexReasoningMode, err := a.settings.CodexReasoningMode()
		if err != nil {
			return "", err
		}
		codexUI, err := a.settings.DefaultCodexUI()
		if err != nil {
			return "", err
		}
		claudeUI, err := a.settings.DefaultClaudeUI()
		if err != nil {
			return "", err
		}
		terminal, err := a.settings.DefaultTerminal()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("default harness: %s\ndefault provider: %s\ncodex reasoning mode: %s\ndefault codex ui: %s\ndefault claude ui: %s\ndefault terminal: %s\nsettings: %s", provider, provider, codexReasoningMode, codexUI, claudeUI, terminal, a.settings.Path()), nil
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return "usage: declaw settings harness [pi|hermes|codex|claude]\n       declaw settings provider [pi|hermes|codex|claude]\n       declaw settings codex-reasoning [default|no_reasoning]\n       declaw settings terminal [terminal|ghostty]\n\nChoose the default agent harness and scheduled terminal. The provider spelling remains available as a compatibility alias.", nil
	}
	switch args[0] {
	case "harness", "provider":
		if len(args) == 1 {
			provider, err := a.settings.DefaultProvider()
			if err != nil {
				return "", err
			}
			return provider, nil
		}
		if len(args) != 2 {
			return "", errors.New("usage: declaw settings harness [pi|hermes|codex|claude]")
		}
		if err := a.settings.SetDefaultProvider(args[1]); err != nil {
			return "", err
		}
		provider, err := a.settings.DefaultProvider()
		if err != nil {
			return "", err
		}
		if args[0] == "harness" {
			return fmt.Sprintf("default harness: %s", provider), nil
		}
		return fmt.Sprintf("default provider: %s", provider), nil
	case "codex-reasoning", "reasoning":
		if len(args) == 1 {
			mode, err := a.settings.CodexReasoningMode()
			if err != nil {
				return "", err
			}
			return mode, nil
		}
		if len(args) != 2 {
			return "", errors.New("usage: declaw settings codex-reasoning [default|no_reasoning]")
		}
		if err := a.settings.SetCodexReasoningMode(args[1]); err != nil {
			return "", err
		}
		mode, err := a.settings.CodexReasoningMode()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("codex reasoning mode: %s", mode), nil
	case "codex-ui":
		if len(args) == 1 {
			ui, err := a.settings.DefaultCodexUI()
			if err != nil {
				return "", err
			}
			return ui, nil
		}
		if len(args) != 2 {
			return "", errors.New("usage: declaw settings codex-ui [app-server|declaw|codex]")
		}
		if err := a.settings.SetDefaultCodexUI(args[1]); err != nil {
			return "", err
		}
		ui, err := a.settings.DefaultCodexUI()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("default codex ui: %s", ui), nil
	case "claude-ui":
		if len(args) == 1 {
			ui, err := a.settings.DefaultClaudeUI()
			if err != nil {
				return "", err
			}
			return ui, nil
		}
		if len(args) != 2 {
			return "", errors.New("usage: declaw settings claude-ui [claude|declaw|print]")
		}
		if err := a.settings.SetDefaultClaudeUI(args[1]); err != nil {
			return "", err
		}
		ui, err := a.settings.DefaultClaudeUI()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("default claude ui: %s", ui), nil
	case "terminal":
		if len(args) == 1 {
			terminal, err := a.settings.DefaultTerminal()
			if err != nil {
				return "", err
			}
			return terminal, nil
		}
		if len(args) != 2 {
			return "", errors.New("usage: declaw settings terminal [terminal|ghostty]")
		}
		if err := a.settings.SetDefaultTerminal(args[1]); err != nil {
			return "", err
		}
		terminal, err := a.settings.DefaultTerminal()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("default terminal: %s", terminal), nil
	default:
		return "", fmt.Errorf("unknown settings key %q\n\nusage: declaw settings harness [pi|hermes|codex|claude]\n       declaw settings provider [pi|hermes|codex|claude]\n       declaw settings codex-reasoning [default|no_reasoning]\n       declaw settings terminal [terminal|ghostty]", args[0])
	}
}

func (a *App) interactiveCommands() ([]ui.Command, error) {
	if err := a.refreshActivity(); err != nil {
		return nil, err
	}
	commands := ui.Commands()

	projectSettingsProjects, err := a.projects.RecentProjects()
	if err != nil {
		return nil, err
	}
	recentProjects, err := a.projects.CheckoutProjects()
	if err != nil {
		return nil, err
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
				{Name: "/schedule pause " + job.Name, Description: "Pause scheduled job"},
				{Name: "/schedule resume " + job.Name, Description: "Resume paused job"},
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
		"/schedule pause":      "pause",
		"/schedule resume":     "resume",
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
		case "/create":
			commands[idx].Children = createCommandChildren()
		case "/track":
			commands[idx].Children = trackCommandChildren()
		case "/checkout":
			defaultHarness, err := a.settings.DefaultHarness()
			if err != nil {
				return nil, err
			}
			commands[idx].Children = checkoutCommandChildren(recentProjects, defaultHarness)
			if len(recentProjects) == 0 {
				commands[idx].Description = "No discovered projects"
			}
		case "/project-settings":
			defaultHarness, err := a.settings.DefaultHarness()
			if err != nil {
				return nil, err
			}
			commands[idx].Children = projectSettingsCommandChildren(projectSettingsProjects, defaultHarness)
			if len(projectSettingsProjects) == 0 {
				commands[idx].Description = "No discovered projects"
			}
		case "/schedule":
			commands[idx].Children = scheduleChildren
		}
	}

	return commands, nil
}

func currentDirectoryName() (string, string) {
	directory, err := os.Getwd()
	if err != nil {
		return "project", ""
	}
	directory = filepath.Clean(directory)
	name := filepath.Base(directory)
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		name = "project"
	}
	return projects.SanitizeName(name), directory
}

func createCommandChildren() []ui.Command {
	name, directory := currentDirectoryName()
	if name == "" {
		name = "project"
	}
	return []ui.Command{{
		Name:        "/create " + name,
		Description: "Create an empty project in " + directory,
		Children:    harnessCommandChildren("/create "+name, "Choose a harness"),
	}}
}

func trackCommandChildren() []ui.Command {
	name, directory := currentDirectoryName()
	if name == "" {
		name = "project"
	}
	return []ui.Command{{
		Name:        "/track " + name,
		Description: "Track " + directory,
	}}
}

func projectCommandChildren(action string, projectList []ui.Command) []ui.Command {
	children := make([]ui.Command, 0, len(projectList))
	for _, project := range projectList {
		children = append(children, ui.Command{
			Name:        "/" + action + " " + project.Name,
			Description: project.Description,
		})
	}
	return children
}

func checkoutCommandChildren(projectList []projects.Project, fallback string) []ui.Command {
	children := make([]ui.Command, 0, len(projectList))
	for _, project := range projectList {
		harness := lastProjectHarness(project, fallback)
		lastUsed := "never used"
		if !project.LastActivityAt.IsZero() {
			lastUsed = project.LastActivityAt.Local().Format("2006-01-02 15:04") + " via " + displayHarness(harness)
		}
		displayPrefix := "/checkout " + project.DisplayName()
		commandPrefix := "/checkout " + project.Name
		children = append(children, ui.Command{
			Name:        displayPrefix,
			CommandLine: commandPrefix,
			Description: fmt.Sprintf("%s (%s%s)", project.Path, lastUsed, projectStatusSuffix(project)),
			Children: []ui.Command{
				{Name: displayPrefix + " continue", CommandLine: commandPrefix + " continue", Description: "Continue with " + displayHarness(harness)},
				{
					Name:        displayPrefix + " change-harness",
					CommandLine: commandPrefix + " change-harness",
					Description: "Change harness",
					Children:    harnessCommandChildrenWithPrefixes(displayPrefix+" change-harness", commandPrefix+" change-harness", "Choose a harness"),
				},
			},
		})
	}
	return children
}

func projectSettingsCommandChildren(projects []projects.Project, fallback string) []ui.Command {
	children := make([]ui.Command, 0, len(projects))
	for _, project := range projects {
		harness := effectiveProjectHarness(project, fallback)
		displayPrefix := "/project-settings " + project.DisplayName()
		commandPrefix := "/project-settings " + project.Name
		lastUsed := "never used"
		if !project.LastActivityAt.IsZero() {
			lastUsed = project.LastActivityAt.Local().Format("2006-01-02 15:04")
		}
		settingsDescription := fmt.Sprintf("%s (last used %s via %s%s)", project.Path, lastUsed, displayHarness(harness), projectStatusSuffix(project))
		if project.DisplayName() != project.Name {
			settingsDescription += "; canonical: " + project.Name
		}
		pinValue := "on"
		pinDescription := "Pin project to the top"
		if project.Pinned {
			pinValue = "off"
			pinDescription = "Unpin project from the top"
		}
		ignoreValue := "on"
		ignoreDescription := "Ignore project in checkout"
		if project.Ignored {
			ignoreValue = "off"
			ignoreDescription = "Include project in checkout"
		}
		children = append(children, ui.Command{
			Name:        displayPrefix,
			CommandLine: commandPrefix,
			Description: settingsDescription,
			Children: []ui.Command{
				{Name: displayPrefix + " path", CommandLine: commandPrefix + " path", Description: "Show the canonical filesystem path"},
				{Name: displayPrefix + " harness", CommandLine: commandPrefix + " harness", Description: "Choose the default harness", Children: harnessCommandChildrenWithPrefixes(displayPrefix+" harness", commandPrefix+" harness", "Choose a harness")},
				{Name: displayPrefix + " give alias", CommandLine: commandPrefix + " give alias", Description: "Set a custom display name"},
				{Name: displayPrefix + " pin", CommandLine: commandPrefix + " pin " + pinValue, Description: pinDescription},
				{Name: displayPrefix + " ignore", CommandLine: commandPrefix + " ignore " + ignoreValue, Description: ignoreDescription},
				{Name: displayPrefix + " remove", CommandLine: commandPrefix + " remove", Description: "Remove or untrack this project"},
			},
		})
	}
	return children
}

func harnessCommandChildren(prefix, description string) []ui.Command {
	return harnessCommandChildrenWithPrefixes(prefix, prefix, description)
}

func harnessCommandChildrenWithPrefixes(displayPrefix, commandPrefix, description string) []ui.Command {
	harnesses := activity.SupportedHarnessValues()
	children := make([]ui.Command, 0, len(harnesses))
	for _, harness := range harnesses {
		status := "not installed"
		if activity.IsInstalled(harness) {
			status = "installed"
		}
		children = append(children, ui.Command{
			Name:        displayPrefix + " " + string(harness),
			CommandLine: commandPrefix + " " + string(harness),
			Description: fmt.Sprintf("%s (%s)", description, status),
		})
	}
	return children
}

func projectStatusSuffix(project projects.Project) string {
	var statuses []string
	if project.Pinned {
		statuses = append(statuses, "pinned")
	}
	if project.Ignored {
		statuses = append(statuses, "ignored")
	}
	if len(statuses) == 0 {
		return ""
	}
	return "; " + strings.Join(statuses, ", ")
}

func effectiveProjectHarness(project projects.Project, fallback string) string {
	for _, value := range []string{project.Harness, project.LastHarness, project.Provider, fallback} {
		if strings.TrimSpace(value) != "" {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return fallback
}

func lastProjectHarness(project projects.Project, fallback string) string {
	for _, value := range []string{project.LastHarness, project.Harness, project.Provider, fallback} {
		if strings.TrimSpace(value) != "" {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return fallback
}

func displayHarness(value string) string {
	if strings.TrimSpace(value) == "" {
		return "inherit"
	}
	return value
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
	if len(args) == 0 {
		if err := a.refreshActivity(); err != nil {
			return "", err
		}
		recent, err := a.projects.CheckoutProjects()
		if err != nil {
			return "", err
		}
		defaultHarness, err := a.settings.DefaultHarness()
		if err != nil {
			return "", err
		}
		if len(recent) == 0 {
			return "no available projects", nil
		}
		lines := make([]string, 0, len(recent))
		for index, project := range recent {
			harness := lastProjectHarness(project, defaultHarness)
			lastUsed := "never used"
			if !project.LastActivityAt.IsZero() {
				lastUsed = project.LastActivityAt.Local().Format("2006-01-02 15:04") + " via " + displayHarness(harness)
			}
			lines = append(lines, fmt.Sprintf("%d. %s - %s (%s)", index+1, project.DisplayName(), project.Path, lastUsed))
		}
		return strings.Join(lines, "\n"), nil
	}
	if len(args) > 3 {
		return "", fmt.Errorf("usage: declaw checkout <project> [continue|change-harness <harness>]")
	}
	if err := a.refreshActivity(); err != nil {
		return "", err
	}
	project, err := a.projects.Get(args[0])
	if err != nil {
		return "", err
	}
	if project.Ignored {
		return "", fmt.Errorf("project %s is ignored; use declaw project-settings %s ignore off to include it", project.DisplayName(), project.Name)
	}
	harness, err := a.settings.DefaultHarness()
	if err != nil {
		return "", err
	}
	harness = lastProjectHarness(project, harness)
	harnessChanged := false
	if len(args) >= 2 {
		switch strings.ToLower(strings.TrimSpace(args[1])) {
		case "continue":
		case "change-harness", "harness":
			if len(args) != 3 {
				return harnessListOutput("usage: declaw checkout <project> change-harness <harness>"), nil
			}
			selected, err := activity.NormalizeHarness(args[2])
			if err != nil {
				return "", err
			}
			if selected == "" {
				return "", errors.New("checkout harness must be pi, hermes, codex, or claude")
			}
			project, err = a.projects.SetHarness(project.Name, string(selected))
			if err != nil {
				return "", err
			}
			harness = string(selected)
			harnessChanged = true
		default:
			return "", fmt.Errorf("unknown checkout action %q; use continue or change-harness", args[1])
		}
	}
	selectedHarness, err := activity.NormalizeHarness(harness)
	if err != nil {
		return "", err
	}
	if selectedHarness == "" {
		return "", errors.New("no checkout harness selected")
	}
	if harnessChanged {
		project.Harness = string(selectedHarness)
	}
	reasoningMode, err := a.effectiveCodexReasoningMode()
	if err != nil {
		return "", err
	}
	program, cmdArgs, err := agentCommand(string(selectedHarness), "", reasoningMode)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(program); err != nil {
		return "", fmt.Errorf("%s command not found in PATH", program)
	}
	if err := a.projects.RecordActivity(project.Name, string(selectedHarness), time.Now().UTC(), "declaw checkout"); err != nil {
		return "", err
	}

	cmd := exec.Command(program, cmdArgs...)
	cmd.Dir = project.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return "", cmd.Run()
}

func harnessListOutput(prefix string) string {
	lines := []string{prefix}
	for _, harness := range activity.SupportedHarnesses() {
		status := "not installed"
		if activity.IsInstalled(activity.Harness(harness)) {
			status = "installed"
		}
		lines = append(lines, fmt.Sprintf("  %s (%s)", harness, status))
	}
	return strings.Join(lines, "\n")
}

func agentCommand(provider, prompt, codexReasoningMode string) (string, []string, error) {
	harness, err := activity.NormalizeHarness(provider)
	if err != nil {
		return "", nil, err
	}
	prompt = strings.TrimSpace(prompt)
	switch harness {
	case activity.Codex:
		args := codexFullPermissionArgs(codexReasoningMode)
		if prompt != "" {
			args = append(args, prompt)
		}
		return "codex", args, nil
	case activity.Claude:
		args := []string{"--dangerously-skip-permissions", "--permission-mode", "bypassPermissions"}
		if prompt != "" {
			args = append(args, prompt)
		}
		return "claude", args, nil
	case activity.Pi:
		args := []string{}
		if prompt != "" {
			args = append(args, prompt)
		}
		return "pi", args, nil
	case activity.Hermes:
		program := "hermes"
		if _, err := exec.LookPath(program); err != nil {
			program = "hermes-agent"
		}
		args := []string{}
		if prompt != "" {
			args = append(args, prompt)
		}
		return program, args, nil
	default:
		return "", nil, fmt.Errorf("unsupported harness %q", provider)
	}
}

func (a *App) effectiveCodexReasoningMode() (string, error) {
	if filepath.Base(os.Args[0]) == "declaw0" {
		return "no_reasoning", nil
	}
	return a.settings.CodexReasoningMode()
}

func codexFullPermissionArgs(reasoningMode string) []string {
	args := []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}
	mode := strings.TrimSpace(strings.ToLower(reasoningMode))
	if settings.ValidateCodexReasoningMode(reasoningMode) == nil && (mode == "no_reasoning" || mode == "no-thinking" || mode == "no_thinking" || mode == "no-reasoning") {
		args = append(args, "-m", "gpt-5.5", "-p", "no_reasoning", "--disable", "image_generation")
	}
	return args
}

func (a *App) help() string {
	return strings.TrimSpace(`
declaw

Commands:
  create <name> [--into <dir> | --path <dir>] [--harness <name>]
  track <name> --path <dir>
  checkout [<name> [continue|change-harness <harness>]]
  project-settings <name> harness [pi|hermes|codex|claude|inherit]
  project-settings <name> give alias <display name>
  project-settings <name> pin [on|off]
  project-settings <name> ignore [on|off]
  project-settings <name> path
  project-settings <name> remove
  ai-agent [prompt]
  settings harness [pi|hermes|codex|claude]
  settings codex-reasoning [default|no_reasoning]
  settings terminal [terminal|ghostty]
  list
  schedule list
  schedule status <job>
  schedule enable <job>
  schedule disable <job>
  schedule pause <job>
  schedule resume <job>
  schedule restart <job>
  schedule run <job>
  schedule ready
  schedule remove <job>
  schedule remove-all
  schedule prune-once
  schedule get-prompt <job>
  schedule get-time <job>
  schedule pi <job> --prompt <text> --project <name> [recurring schedule flags]
  schedule pi <job> --prompt <text> [--project <name> | --workspace <path>] --at "YYYY-MM-DD HH:MM"
  schedule hermes <job> --prompt <text> --project <name> [recurring schedule flags]
  schedule hermes <job> --prompt <text> [--project <name> | --workspace <path>] --at "YYYY-MM-DD HH:MM"
  schedule codex <job> --prompt <text> --project <name> [recurring schedule flags]
  schedule codex <job> --prompt <text> [--project <name> | --workspace <path>] --at "YYYY-MM-DD HH:MM"
  schedule claude <job> --prompt <text> --project <name> [recurring schedule flags]
  schedule claude <job> --prompt <text> [--project <name> | --workspace <path>] --at "YYYY-MM-DD HH:MM"
  schedule create <job> --provider [pi|hermes|codex|claude|default] --prompt <text> [schedule flags]
  schedule edit <job> [schedule flags] [--prompt <text>] [--project <name>] [--provider <harness>]

Schedule flags:
  --daily HH:MM
  --weekdays HH:MM
  --weekly mon@09:30
  --at "YYYY-MM-DD HH:MM"        One-off schedule at an exact future time.
  --time HH:MM
  --once                         Make explicit --year/--month/--day fields one-off.
  --year YYYY --month M --day D --hour H --minute M [--weekday mon]
  --cwd <dir> --stdout <path> --stderr <path> --env KEY=VALUE
  --no-recurring-fallback

Codex and Claude schedules may use their native command modes. Pi and Hermes schedules always use their native command line interfaces.

Settings:
  declaw settings harness codex      Use Codex for free-text launcher input, checkout, and ai-agent.
  declaw settings harness claude     Use Claude Code for free-text launcher input, checkout, and ai-agent.
  declaw settings harness pi         Use Pi for free-text launcher input, checkout, and ai-agent.
  declaw settings harness hermes     Use Hermes for free-text launcher input, checkout, and ai-agent.
  declaw settings terminal ghostty    Open scheduled interactive jobs in Ghostty instead of Terminal.
  declaw settings codex-reasoning no_reasoning
                                      Run Codex with GPT-5.5, no_reasoning, and image generation disabled.
  declaw settings codex-reasoning default
                                      Use Codex's default reasoning behavior.
  declaw project-settings my-repo harness claude
                                      Override one tracked project to prefer Claude for checkout and schedules.
  declaw project-settings my-repo give alias "Customer Portal"
                                      Set a display name without changing the canonical project path.

Scheduled chat follow-ups:
  Enter sends. Ctrl+J inserts a line break. Long bracketed pastes are summarized in the visible input as [pasted N characters] while the full pasted text is still sent.

Agent workflow:
  1. For a recurring agent job, first choose a declaw project.
  2. If the target directory already exists, run declaw track <name> --path <dir>, then use --project <name>.
  3. If a fresh empty project directory is needed, run declaw create <name> --into <parent-dir>, then use --project <name>.
  4. For one-off agent jobs only, --project/--workspace may be omitted; declaw uses ~/Library/Application Support/declaw/support/workspaces/one-off.
  5. Do not create recurring schedules without --project; the CLI rejects that because recurring jobs need managed project context.

Examples:
  declaw create pm-workspace --into ~/Documents/dev --harness codex
  declaw track product-repo --path ~/Documents/dev/my-repo --harness claude
  declaw settings harness claude
	declaw project-settings product-repo harness codex
	declaw project-settings product-repo give alias "Product Repository"
	declaw project-settings product-repo pin on
	declaw project-settings product-repo ignore on
	declaw checkout
  declaw schedule create repo-review --provider default --project product-repo --daily 10:00 --prompt "Review this repo with the preferred harness."
  declaw schedule pi pm-deadline-review --project pm-workspace --weekdays 09:00 --prompt "Review the project and propose deadline follow-ups."
  declaw schedule codex repo-review --project product-repo --daily 10:00 --prompt "Review this repo for risks, deadlines, and next actions."
  declaw schedule claude repo-review --project product-repo --daily 10:00 --prompt "Review this repo with Claude and summarize risks and next actions."
`)
}

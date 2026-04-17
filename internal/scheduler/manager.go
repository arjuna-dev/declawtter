package scheduler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"declaw/internal/cliargs"
	"declaw/internal/projects"
)

const (
	labelPrefix   = "com.declaw"
	promptEnvKey  = "DECLAW_SCHEDULE_PROMPT"
	launchctlPath = "/bin/launchctl"
)

const scheduledCodexPromptIntro = "You are running as a scheduled Codex job."

const workspaceCodexPromptPrefix = "Use the current working directory as the workspace root.\nBefore doing the main task, read `AGENTS.md` from the workspace root if it exists and follow the workspace-local instructions and conventions there. Also pay attention to relevant workspace files before acting.\nDeclaw records the visible chat transcript automatically. Do not create or update `SESSIONS/` files unless the user explicitly asks you to."

const recurringCodexPromptPrefix = "Before starting the main task, follow the workspace `AGENTS.md` memory convention. If a required prior-day memory entry is missing, create a concise distilled note in `MEMORY/` using the workspace's date format. Do not copy raw transcripts, tool logs, IDs, or long paths into memory unless that exact detail is necessary."

const scheduledCodexChatHandoff = "When the initial scheduled work is done, answer the user directly in this chat. Write like you are texting a colleague who did not see your prompt, tool calls, or hidden reasoning. Start with enough context for the user to understand why this message exists, then give the useful result. Do not make the user open files to understand the result. Mention files only as backup or trace. If the task asks for an artifact, include the artifact itself in the chat. Keep process narration brief and focus on what the user can actually use next."

const scheduledCodexLocalToolPrefix = "For scheduled non-interactive local CLI checks, prefer command flags over prompts. If using `gog` and `GOG_ACCOUNT` is set, pass `--account \"$GOG_ACCOUNT\"` explicitly; `gog` does not use that environment variable as the default account selector. Also pass `--no-input` for read-only scheduled checks so failures are explicit instead of waiting for terminal input."

var declawSpinnerFrames = []string{"|", "/", "-", "\\"}

const declawChatHistoryLimit = 3

type Manager struct {
	projects        *projects.Manager
	supportDir      string
	runsDir         string
	jobsDir         string
	jobsPath        string
	launchAgentsDir string
	domain          string
	executablePath  string
}

type JobStore struct {
	Jobs map[string]JobRecord `json:"jobs"`
}

func NewManager(projectsManager *projects.Manager) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	supportDir := filepath.Join(home, ".local", "share", "declaw")
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	for _, path := range []string{supportDir, filepath.Join(supportDir, "runs"), filepath.Join(supportDir, "logs"), filepath.Join(supportDir, "jobs"), launchAgentsDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	}

	return &Manager{
		projects:        projectsManager,
		supportDir:      supportDir,
		runsDir:         filepath.Join(supportDir, "runs"),
		jobsDir:         filepath.Join(supportDir, "jobs"),
		jobsPath:        filepath.Join(supportDir, "jobs.json"),
		launchAgentsDir: launchAgentsDir,
		domain:          fmt.Sprintf("gui/%s", currentUser.Uid),
		executablePath:  executablePath,
	}, nil
}

func (m *Manager) Execute(args []string) (string, error) {
	if len(args) == 0 {
		return m.help(), nil
	}
	if isHelpArg(args[0]) {
		return m.help(), nil
	}

	switch args[0] {
	case "list":
		return m.list(args[1:])
	case "status":
		return m.status(args[1:])
	case "enable":
		return m.enable(args[1:])
	case "disable":
		return m.disable(args[1:])
	case "restart":
		return m.restart(args[1:])
	case "run":
		return m.runJob(args[1:])
	case "remove":
		return m.remove(args[1:])
	case "remove-all":
		return m.removeAll(args[1:])
	case "prune-once":
		return m.pruneOnce(args[1:])
	case "get-prompt":
		return m.getPrompt(args[1:])
	case "get-time":
		return m.getTime(args[1:])
	case "codex":
		return m.scheduleCodex(args[1:])
	case "edit":
		return m.edit(args[1:])
	case "__internal":
		return m.internal(args[1:])
	default:
		return "", fmt.Errorf("unknown schedule subcommand %q", args[0])
	}
}

func (m *Manager) internal(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("missing internal subcommand")
	}

	switch args[0] {
	case "run-recurring":
		return "", m.runRecurringInternal(args[1:])
	case "run-once":
		return "", m.runOnceInternal(args[1:])
	case "run-codex":
		return "", m.runCodexInternal(args[1:])
	default:
		return "", fmt.Errorf("unknown internal schedule command %q", args[0])
	}
}

func (m *Manager) list(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("usage: declaw schedule list")
	}
	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}
	if len(store.Jobs) == 0 {
		return "no jobs installed", nil
	}

	names := make([]string, 0, len(store.Jobs))
	for name := range store.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		job := store.Jobs[name]
		kind := "recurring"
		label := job.PrimaryLabel
		if job.Config.Kind == "once" {
			kind = "once"
			label = job.OnceLabel
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", job.Name, job.Type, kind, label))
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) Jobs() ([]JobRecord, error) {
	store, err := m.loadJobs()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(store.Jobs))
	for name := range store.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]JobRecord, 0, len(names))
	for _, name := range names {
		out = append(out, store.Jobs[name])
	}
	return out, nil
}

func (m *Manager) status(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule status <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	label := job.PrimaryLabel
	if job.Config.Kind == "once" {
		label = job.OnceLabel
	}
	proc := m.runLaunchctl(true, "print", fmt.Sprintf("%s/%s", m.domain, label))
	output := strings.TrimSpace(proc.Stdout + proc.Stderr)
	if proc.Err != nil {
		return output, proc.Err
	}
	return output, nil
}

func (m *Manager) enable(args []string) (string, error) {
	return m.simpleLabelAction(args, "enabled", func(label string) error {
		_, err := m.runLaunchctl(true, "enable", fmt.Sprintf("%s/%s", m.domain, label)).Output()
		return err
	})
}

func (m *Manager) disable(args []string) (string, error) {
	return m.simpleLabelAction(args, "disabled", func(label string) error {
		_, err := m.runLaunchctl(false, "disable", fmt.Sprintf("%s/%s", m.domain, label)).Output()
		return err
	})
}

func (m *Manager) restart(args []string) (string, error) {
	return m.simpleLabelAction(args, "restarted", func(label string) error {
		_ = m.bootout(label)
		if err := m.bootstrap(label); err != nil {
			return err
		}
		return m.enableLabel(label)
	})
}

func (m *Manager) runJob(args []string) (string, error) {
	if hasHelpArg(args) {
		return "usage: declaw schedule run <job>\n\nTrigger an installed scheduled job immediately.", nil
	}
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule run <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	if job.Config.Kind == "recurring" {
		if err := m.runRecurringManual(job); err != nil {
			return "", err
		}
		return fmt.Sprintf("triggered %s", job.Name), nil
	}
	label := job.PrimaryLabel
	label = job.OnceLabel
	if _, err := m.runLaunchctl(true, "kickstart", "-k", fmt.Sprintf("%s/%s", m.domain, label)).Output(); err != nil {
		return "", err
	}
	return fmt.Sprintf("triggered %s", label), nil
}

func (m *Manager) runRecurringManual(job JobRecord) error {
	if job.Type != "codex" {
		return fmt.Errorf("unsupported schedule type %q", job.Type)
	}
	prompt, err := resolveRuntimePrompt("", m.promptPath(job.Name))
	if err != nil {
		return err
	}
	now := time.Now().In(time.Local)
	runDir := filepath.Join(m.runsDir, "recurring", sanitizeName(job.Name), now.Format("2006-01-02"), "manual-"+now.Format("150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	command := m.codexLaunchCommand(prompt, job.Workspace, job.Name, effectiveCodexUI(job.UI))
	payload := map[string]any{
		"job":            sanitizeName(job.Name),
		"trigger_kind":   "manual",
		"scheduled_time": job.Config.ScheduledTime,
		"slot_key":       filepath.Base(runDir),
		"fired_at":       now.Format(time.RFC3339),
		"pid":            os.Getpid(),
		"command":        command,
	}
	if err := writeJSON(filepath.Join(runDir, "trigger.json"), payload); err != nil {
		return err
	}
	runMD := filepath.Join(runDir, "run.md")
	if err := os.WriteFile(runMD, []byte(strings.Join([]string{
		fmt.Sprintf("# %s Manual Run", sanitizeName(job.Name)),
		"",
		fmt.Sprintf("- Date: %s", now.Format("2006-01-02")),
		fmt.Sprintf("- Fired at: %s", now.Format("2006-01-02 15:04:05 MST")),
		fmt.Sprintf("- Command: %s", strings.Join(command, " ")),
		"",
		"## Result",
		"",
	}, "\n")), 0o644); err != nil {
		return err
	}
	exitCode := m.runRuntimeCommand(job.Type, job.Name, prompt, job.Workspace, effectiveCodexUI(job.UI), m.runtimeEnvForJob(job, "manual", runDir, job.Config.ScheduledTime))
	finishedAt := time.Now().In(time.Local)
	return appendLines(runMD,
		fmt.Sprintf("- Exit code: %d", exitCode),
		fmt.Sprintf("- Finished at: %s", finishedAt.Format("2006-01-02 15:04:05 MST")),
	)
}

func (m *Manager) remove(args []string) (string, error) {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))
	asLabel := fs.Bool("label", false, "treat value as exact label")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{})); err != nil {
		return "", err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw schedule remove <job> [--label]")
	}

	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}

	var labels []string
	var name string
	if *asLabel {
		labels = []string{rest[0]}
		for key, job := range store.Jobs {
			if containsLabel(job, rest[0]) {
				name = key
				break
			}
		}
	} else {
		job, err := m.getJob(rest[0])
		if err != nil {
			return "", err
		}
		name = job.Name
		labels = labelsForJob(job)
	}

	lines := []string{}
	for _, label := range labels {
		if label == "" {
			continue
		}
		if err := m.uninstallLabel(label); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("removed %s", label))
	}

	if name != "" {
		m.removePromptFile(name)
		delete(store.Jobs, sanitizeName(name))
		if err := m.saveJobs(store); err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) removeAll(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("usage: declaw schedule remove-all")
	}
	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}
	if len(store.Jobs) == 0 {
		return "no jobs installed", nil
	}

	lines := []string{}
	for name, job := range store.Jobs {
		for _, label := range labelsForJob(job) {
			if label == "" {
				continue
			}
			if err := m.uninstallLabel(label); err != nil {
				return "", err
			}
			lines = append(lines, fmt.Sprintf("removed %s", label))
		}
		m.removePromptFile(name)
		delete(store.Jobs, name)
	}
	if err := m.saveJobs(store); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) pruneOnce(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("usage: declaw schedule prune-once")
	}
	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}
	lines := []string{}
	changed := false
	for name, job := range store.Jobs {
		if job.Config.Kind != "once" {
			continue
		}
		plist := m.installedPlistPath(job.OnceLabel)
		if _, err := os.Stat(plist); errors.Is(err, os.ErrNotExist) {
			delete(store.Jobs, name)
			lines = append(lines, fmt.Sprintf("pruned %s", job.OnceLabel))
			changed = true
		}
	}
	if !changed {
		return "no stale one-off jobs found", nil
	}
	if err := m.saveJobs(store); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) getPrompt(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule get-prompt <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	if job.Type != "codex" || job.Prompt == "" {
		return "", fmt.Errorf("job %q does not have a stored Codex prompt", args[0])
	}
	return job.Prompt, nil
}

func (m *Manager) getTime(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule get-time <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	return formatConfig(job.Config), nil
}

func (m *Manager) scheduleCodex(args []string) (string, error) {
	if hasHelpArg(args) {
		return m.codexHelp(), nil
	}

	fs := flag.NewFlagSet("codex", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	prompt := fs.String("prompt", "", "prompt")
	projectName := fs.String("project", "", "tracked project")
	workspace := fs.String("workspace", "", "workspace path")
	noWorkspace := fs.Bool("no-workspace", false, "unsupported; recurring Codex schedules require --project")
	daily := fs.String("daily", "", "daily time")
	timeValue := fs.String("time", "", "daily time")
	weekdays := fs.String("weekdays", "", "weekday time")
	weekly := fs.String("weekly", "", "weekly")
	at := fs.String("at", "", "one-off time")
	once := fs.Bool("once", false, "one-off explicit schedule")
	year := fs.Int("year", -1, "year")
	month := fs.Int("month", -1, "month")
	day := fs.Int("day", -1, "day")
	hour := fs.Int("hour", -1, "hour")
	minute := fs.Int("minute", -1, "minute")
	cwd := fs.String("cwd", "", "cwd")
	stdout := fs.String("stdout", "", "stdout")
	stderr := fs.String("stderr", "", "stderr")
	ui := fs.String("ui", "app-server", "scheduled Codex UI: app-server, declaw, or codex")
	noRecurringFallback := fs.Bool("no-recurring-fallback", false, "disable fallback")
	weekday := stringListFlag{}
	env := stringListFlag{}
	fs.Var(&weekday, "weekday", "weekday")
	fs.Var(&env, "env", "env")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, scheduleValueFlags())); err != nil {
		return "", err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw schedule codex <job> --prompt <text> --project <name> [recurring flags] OR declaw schedule codex <job> --prompt <text> [--project <name> | --workspace <path>] --at \"YYYY-MM-DD HH:MM\"")
	}
	if strings.TrimSpace(*prompt) == "" {
		return "", errors.New("--prompt is required")
	}
	if err := validateCodexUI(*ui); err != nil {
		return "", err
	}

	config, err := resolveScheduleConfig(scheduleShapeInput{
		Daily:    *daily,
		Time:     *timeValue,
		Weekdays: *weekdays,
		Weekly:   *weekly,
		At:       *at,
		Once:     *once,
		Year:     *year,
		Month:    *month,
		Day:      *day,
		Hour:     *hour,
		Minute:   *minute,
		Weekday:  []string(weekday),
	})
	if err != nil {
		return "", err
	}

	resolvedWorkspace, workspaceBootstrap, err := m.resolveWorkspace(*projectName, *workspace, *noWorkspace, config.Kind)
	if err != nil {
		return "", err
	}

	jobName := sanitizeName(rest[0])
	effectivePrompt := buildCodexPrompt(*prompt, config.Kind == "recurring", workspaceBootstrap)
	record := JobRecord{
		Name:               jobName,
		Type:               "codex",
		CreatedAt:          time.Now().UTC(),
		Config:             config,
		Prompt:             effectivePrompt,
		Workspace:          resolvedWorkspace,
		UI:                 normalizeCodexUI(*ui),
		Cwd:                *cwd,
		Stdout:             *stdout,
		Stderr:             *stderr,
		Env:                []string(env),
		HasRecovery:        config.Kind == "recurring" && !*noRecurringFallback,
		PrimaryLabel:       primaryLabel(jobName),
		RecoveryLabel:      recoveryLabel(jobName),
		OnceLabel:          onceLabel(jobName),
		WorkspaceBootstrap: workspaceBootstrap,
	}
	if config.Kind == "once" {
		record.HasRecovery = false
	}
	return m.installAndStore(record)
}

func (m *Manager) edit(args []string) (string, error) {
	if hasHelpArg(args) {
		return m.editHelp(), nil
	}

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	prompt := fs.String("prompt", "", "prompt")
	projectName := fs.String("project", "", "project")
	workspace := fs.String("workspace", "", "workspace")
	noWorkspace := fs.Bool("no-workspace", false, "unsupported; recurring Codex schedules require --project")
	ui := fs.String("ui", "", "scheduled Codex UI: app-server, declaw, or codex")
	daily := fs.String("daily", "", "daily")
	timeValue := fs.String("time", "", "time")
	weekdays := fs.String("weekdays", "", "weekdays")
	weekly := fs.String("weekly", "", "weekly")
	at := fs.String("at", "", "at")
	once := fs.Bool("once", false, "once")
	year := fs.Int("year", -1, "year")
	month := fs.Int("month", -1, "month")
	day := fs.Int("day", -1, "day")
	hour := fs.Int("hour", -1, "hour")
	minute := fs.Int("minute", -1, "minute")
	weekday := stringListFlag{}
	fs.Var(&weekday, "weekday", "weekday")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, scheduleValueFlags())); err != nil {
		return "", err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw schedule edit <job> [flags]")
	}

	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}
	record, ok := store.Jobs[sanitizeName(rest[0])]
	if !ok {
		return "", fmt.Errorf("unknown job %q", rest[0])
	}

	if hasScheduleShape(scheduleShapeInput{
		Daily:    *daily,
		Time:     *timeValue,
		Weekdays: *weekdays,
		Weekly:   *weekly,
		At:       *at,
		Once:     *once,
		Year:     *year,
		Month:    *month,
		Day:      *day,
		Hour:     *hour,
		Minute:   *minute,
		Weekday:  []string(weekday),
	}) {
		config, err := resolveScheduleConfig(scheduleShapeInput{
			Daily:    *daily,
			Time:     *timeValue,
			Weekdays: *weekdays,
			Weekly:   *weekly,
			At:       *at,
			Once:     *once,
			Year:     *year,
			Month:    *month,
			Day:      *day,
			Hour:     *hour,
			Minute:   *minute,
			Weekday:  []string(weekday),
		})
		if err != nil {
			return "", err
		}
		if config.Kind != record.Config.Kind {
			return "", errors.New("edit cannot change a job between recurring and one-off")
		}
		record.Config = config
	}

	switch record.Type {
	case "codex":
		if *ui != "" {
			if err := validateCodexUI(*ui); err != nil {
				return "", err
			}
			record.UI = normalizeCodexUI(*ui)
		}
		resolvedWorkspace := record.Workspace
		workspaceBootstrap := record.WorkspaceBootstrap
		if *projectName != "" || *workspace != "" || *noWorkspace {
			var err error
			resolvedWorkspace, workspaceBootstrap, err = m.resolveWorkspace(*projectName, *workspace, *noWorkspace, record.Config.Kind)
			if err != nil {
				return "", err
			}
			record.Workspace = resolvedWorkspace
			record.WorkspaceBootstrap = workspaceBootstrap
		}
		if *prompt != "" || *projectName != "" || *workspace != "" || *noWorkspace {
			taskPrompt := extractTaskPrompt(record.Prompt)
			if *prompt != "" {
				taskPrompt = *prompt
			}
			record.Prompt = buildCodexPrompt(taskPrompt, record.Config.Kind == "recurring", workspaceBootstrap)
		}
	default:
		return "", fmt.Errorf("unsupported schedule type %q", record.Type)
	}

	for _, label := range labelsForJob(record) {
		if label == "" {
			continue
		}
		_ = m.uninstallLabel(label)
	}

	store.Jobs[record.Name] = record
	if err := m.installRecord(record); err != nil {
		return "", err
	}
	if err := m.saveJobs(store); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated %s", record.Name), nil
}

func (m *Manager) resolveWorkspace(projectName, workspace string, noWorkspace bool, scheduleKind string) (string, bool, error) {
	if noWorkspace {
		return "", false, errors.New("--no-workspace is not supported; recurring Codex schedules require --project")
	}
	if strings.TrimSpace(projectName) != "" && strings.TrimSpace(workspace) != "" {
		return "", false, errors.New("--project and --workspace cannot be combined")
	}
	if strings.TrimSpace(projectName) != "" {
		project, err := m.projects.Get(projectName)
		if err != nil {
			return "", false, err
		}
		if err := validateWorkspaceDir(project.Path); err != nil {
			return "", false, err
		}
		return project.Path, true, nil
	}
	if scheduleKind == "recurring" {
		if strings.TrimSpace(workspace) != "" {
			return "", false, errors.New("recurring Codex schedules require --project; run declaw track <name> --path <dir> first for an existing directory")
		}
		return "", false, errors.New("recurring Codex schedules require --project")
	}
	if strings.TrimSpace(workspace) != "" {
		abs, err := filepath.Abs(workspace)
		if err != nil {
			return "", false, err
		}
		if err := validateWorkspaceDir(abs); err != nil {
			return "", false, err
		}
		return abs, true, nil
	}
	if scheduleKind == "once" {
		return m.defaultOnceWorkspace()
	}
	return "", false, errors.New("codex schedules require --project unless this is a one-off schedule")
}

func (m *Manager) defaultOnceWorkspace() (string, bool, error) {
	path := filepath.Join(m.supportDir, "workspaces", "one-off")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", false, err
	}
	readmePath := filepath.Join(path, "README.md")
	if _, err := os.Stat(readmePath); errors.Is(err, os.ErrNotExist) {
		content := strings.Join([]string{
			"# declaw one-off workspace",
			"",
			"This directory is the default workspace for one-off scheduled Codex jobs created without `--project` or `--workspace`.",
			"",
			"Use an explicit `--project` or `--workspace` when the job needs repo-specific context.",
			"",
		}, "\n")
		if err := os.WriteFile(readmePath, []byte(content), 0o644); err != nil {
			return "", false, err
		}
	}
	return path, false, nil
}

func (m *Manager) help() string {
	return strings.TrimSpace(`
declaw schedule

Manage native macOS launchd schedules for Codex runs.

Commands:
  declaw schedule list
  declaw schedule status <job>
  declaw schedule enable <job>
  declaw schedule disable <job>
  declaw schedule restart <job>
  declaw schedule run <job>
  declaw schedule remove <job>
  declaw schedule remove-all
  declaw schedule prune-once
  declaw schedule get-prompt <job>
  declaw schedule get-time <job>
  declaw schedule codex <job> --prompt <text> --project <name> [recurring schedule flags]
  declaw schedule codex <job> --prompt <text> [--project <name> | --workspace <dir>] --at "YYYY-MM-DD HH:MM"
  declaw schedule edit <job> [schedule flags] [--prompt <text>] [--project <name>]

Codex schedule context:
  Recurring Codex schedules require a declaw project. Use declaw track <name> --path <dir> for an existing directory, or declaw create <name> for a fresh workspace. One-off schedules may use --project, --workspace, or omit both to use declaw's default one-off workspace.

Schedule flags:
  --daily HH:MM
  --weekdays HH:MM
  --weekly mon@09:30
  --at "YYYY-MM-DD HH:MM"        One-off schedule at an exact future time.
  --time HH:MM
  --once                         Make explicit --year/--month/--day fields one-off.
  --year YYYY --month M --day D --hour H --minute M [--weekday mon]
  --cwd <dir> --stdout <path> --stderr <path> --env KEY=VALUE
  --ui app-server|declaw|codex       app-server is the default clean chat UI; declaw uses the legacy codex exec UI; codex opens the raw Codex TUI.
  --no-recurring-fallback
`)
}

func (m *Manager) codexHelp() string {
	return strings.TrimSpace(`
declaw schedule codex

Create a scheduled Codex run. Recurring jobs require a declaw project so Codex gets durable managed context. One-off jobs may omit it and use declaw's default one-off workspace.

Usage:
  declaw schedule codex <job> --prompt <text> --project <name> [recurring schedule flags]
  declaw schedule codex <job> --prompt <text> [--project <name> | --workspace <dir>] --at "YYYY-MM-DD HH:MM"

Choose the target:
  --project <name>      Use a declaw-tracked project from declaw list.
  --workspace <dir>     Use an existing directory directly. Allowed only for one-off jobs.
  omitted               Allowed only for one-off jobs; uses ~/.local/share/declaw/workspaces/one-off.

If no project exists yet:
  declaw create <name> --into <parent-dir>
  declaw track <name> --path <existing-dir>
  declaw schedule codex <job> --project <name> --daily HH:MM --prompt "<task>"

Examples:
  declaw schedule codex pm-review --project pm-workspace --weekdays 09:00 --prompt "Review PM deadlines, project risks, and codebase signals."
  declaw track product-repo --path ~/Documents/dev/my-repo
  declaw schedule codex repo-review --project product-repo --daily 10:00 --prompt "Inspect this repo and summarize PM risks and next actions."

Schedule flags:
  --daily HH:MM
  --weekdays HH:MM
  --weekly mon@09:30
  --at "YYYY-MM-DD HH:MM"        One-off schedule at an exact future time.
  --time HH:MM
  --once                         Make explicit --year/--month/--day fields one-off.
  --year YYYY --month M --day D --hour H --minute M [--weekday mon]
  --cwd <dir> --stdout <path> --stderr <path> --env KEY=VALUE
  --ui app-server|declaw|codex       app-server is the default clean chat UI; declaw uses the legacy codex exec UI; codex opens the raw Codex TUI.
  --no-recurring-fallback
`)
}

func (m *Manager) editHelp() string {
	return strings.TrimSpace(`
declaw schedule edit

Edit an existing schedule while preserving unspecified fields.

Usage:
  declaw schedule edit <job> [schedule flags] [--prompt <text>] [--project <name>]

For recurring Codex jobs, switching context requires --project. For one-off Codex jobs, --workspace is also allowed.
`)
}

func isHelpArg(value string) bool {
	return value == "-h" || value == "--help" || value == "help"
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func normalizeCodexUI(value string) string {
	if strings.TrimSpace(value) == "" {
		return "app-server"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func effectiveCodexUI(value string) string {
	return normalizeCodexUI(value)
}

func validateCodexUI(value string) error {
	switch normalizeCodexUI(value) {
	case "app-server", "declaw", "codex":
		return nil
	default:
		return fmt.Errorf("--ui must be app-server, declaw, or codex, got %q", value)
	}
}

func validateWorkspaceDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("workspace directory is not accessible: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", path)
	}
	return nil
}

func (m *Manager) installAndStore(record JobRecord) (string, error) {
	store, err := m.loadJobs()
	if err != nil {
		return "", err
	}
	store.Jobs[record.Name] = record
	if err := m.installRecord(record); err != nil {
		return "", err
	}
	if err := m.saveJobs(store); err != nil {
		return "", err
	}
	return fmt.Sprintf("installed %s", record.Name), nil
}

func (m *Manager) installRecord(record JobRecord) error {
	if record.Type != "codex" {
		return fmt.Errorf("unsupported schedule type %q", record.Type)
	}
	if record.Type == "codex" {
		if err := m.writePromptFile(record); err != nil {
			return err
		}
	}

	if record.Config.Kind == "once" {
		payload := m.buildPlist(record.OnceLabel, m.onceProgramArguments(record), record, nil)
		return m.installJob(record.OnceLabel, payload)
	}

	primaryPayload := m.buildPlist(record.PrimaryLabel, m.recurringProgramArguments(record, "scheduled"), record, nil)
	if err := m.installJob(record.PrimaryLabel, primaryPayload); err != nil {
		return err
	}
	if !record.HasRecovery {
		return nil
	}
	recoveryPayload := m.buildPlist(record.RecoveryLabel, m.recurringProgramArguments(record, "recovery"), record, recoveryCalendarEntries(record.Config))
	return m.installJob(record.RecoveryLabel, recoveryPayload)
}

func (m *Manager) installJob(label string, payload map[string]any) error {
	if err := os.WriteFile(m.installedPlistPath(label), buildPlistXML(payload), 0o644); err != nil {
		return err
	}
	_ = m.bootout(label)
	_ = m.enableLabel(label)
	if err := m.bootstrap(label); err != nil {
		return err
	}
	return m.enableLabel(label)
}

func (m *Manager) recurringProgramArguments(record JobRecord, trigger string) []string {
	args := []string{
		m.executablePath,
		"schedule",
		"__internal",
		"run-recurring",
		"--job", record.Name,
		"--trigger-kind", trigger,
		"--scheduled-time", record.Config.ScheduledTime,
		"--type", record.Type,
	}
	if record.Config.Day != 0 {
		args = append(args, "--day", strconv.Itoa(record.Config.Day))
	}
	if record.Config.Month != 0 {
		args = append(args, "--month", strconv.Itoa(record.Config.Month))
	}
	for _, weekday := range record.Config.Weekdays {
		args = append(args, "--weekday", strconv.Itoa(weekday))
	}
	args = append(args, m.jobPayloadArgs(record)...)
	return args
}

func (m *Manager) onceProgramArguments(record JobRecord) []string {
	args := []string{
		m.executablePath,
		"schedule",
		"__internal",
		"run-once",
		"--job", record.Name,
		"--cleanup-label", record.OnceLabel,
		"--type", record.Type,
	}
	args = append(args, m.jobPayloadArgs(record)...)
	return args
}

func (m *Manager) jobPayloadArgs(record JobRecord) []string {
	args := []string{}
	switch record.Type {
	case "codex":
		args = append(args, "--prompt-file", m.promptPath(record.Name))
		if record.Workspace != "" {
			args = append(args, "--workspace", record.Workspace)
		}
		args = append(args, "--ui", effectiveCodexUI(record.UI))
	}
	return args
}

func (m *Manager) runRecurringInternal(args []string) error {
	fs := flag.NewFlagSet("run-recurring", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	jobName := fs.String("job", "", "job")
	triggerKind := fs.String("trigger-kind", "", "trigger")
	scheduledTime := fs.String("scheduled-time", "", "time")
	jobType := fs.String("type", "", "type")
	day := fs.Int("day", 0, "day")
	month := fs.Int("month", 0, "month")
	prompt := fs.String("prompt", "", "prompt")
	promptFile := fs.String("prompt-file", "", "prompt file")
	workspace := fs.String("workspace", "", "workspace")
	ui := fs.String("ui", "app-server", "ui")
	weekday := stringListFlag{}
	fs.Var(&weekday, "weekday", "weekday")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, internalRecurringValueFlags())); err != nil {
		return err
	}
	if *jobType == "codex" {
		resolvedPrompt, err := resolveRuntimePrompt(*prompt, *promptFile)
		if err != nil {
			return err
		}
		*prompt = resolvedPrompt
	}

	now := time.Now().In(time.Local)
	if shouldSkipWeekday(now, weekdayInts([]string(weekday))) {
		return nil
	}
	if shouldSkipCalendarDay(now, *day, *month) {
		return nil
	}
	if *triggerKind == "recovery" {
		hour, minute, err := parseDailyTime(*scheduledTime)
		if err != nil {
			return err
		}
		if shouldSkipRecovery(now, hour, minute) {
			return nil
		}
	}

	slot := slotKey(*scheduledTime)
	runDir := filepath.Join(m.runsDir, "recurring", sanitizeName(*jobName), now.Format("2006-01-02"), slot)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	markerPath := filepath.Join(runDir, "trigger.json")
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	}

	lockDir := filepath.Join(runDir, ".trigger-lock")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer os.Remove(lockDir)

	if _, err := os.Stat(markerPath); err == nil {
		return nil
	}

	firedAt := time.Now().In(time.Local)
	command := []string{}
	switch *jobType {
	case "codex":
		command = m.codexLaunchCommand(*prompt, *workspace, *jobName, *ui)
	default:
		return fmt.Errorf("unknown recurring job type %q", *jobType)
	}

	payload := map[string]any{
		"job":            sanitizeName(*jobName),
		"trigger_kind":   *triggerKind,
		"scheduled_time": *scheduledTime,
		"slot_key":       slot,
		"fired_at":       firedAt.Format(time.RFC3339),
		"pid":            os.Getpid(),
		"command":        command,
	}
	if err := writeJSON(markerPath, payload); err != nil {
		return err
	}

	runMD := filepath.Join(runDir, "run.md")
	if err := os.WriteFile(runMD, []byte(strings.Join([]string{
		fmt.Sprintf("# %s Run", sanitizeName(*jobName)),
		"",
		fmt.Sprintf("- Date: %s", firedAt.Format("2006-01-02")),
		fmt.Sprintf("- Trigger kind: %s", *triggerKind),
		fmt.Sprintf("- Fired at: %s", firedAt.Format("2006-01-02 15:04:05 MST")),
		fmt.Sprintf("- Scheduled time: %s", *scheduledTime),
		fmt.Sprintf("- Slot key: %s", slot),
		fmt.Sprintf("- Command: %s", strings.Join(command, " ")),
		"",
		"## Result",
		"",
	}, "\n")), 0o644); err != nil {
		return err
	}

	exitCode := m.runRuntimeCommand(*jobType, *jobName, *prompt, *workspace, *ui, runtimeEnv(*jobName, *triggerKind, runDir, *scheduledTime))
	finishedAt := time.Now().In(time.Local)
	return appendLines(runMD,
		fmt.Sprintf("- Exit code: %d", exitCode),
		fmt.Sprintf("- Finished at: %s", finishedAt.Format("2006-01-02 15:04:05 MST")),
	)
}

func (m *Manager) runOnceInternal(args []string) error {
	fs := flag.NewFlagSet("run-once", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	jobName := fs.String("job", "", "job")
	cleanupLabel := fs.String("cleanup-label", "", "label")
	jobType := fs.String("type", "", "type")
	prompt := fs.String("prompt", "", "prompt")
	promptFile := fs.String("prompt-file", "", "prompt file")
	workspace := fs.String("workspace", "", "workspace")
	ui := fs.String("ui", "app-server", "ui")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, internalOnceValueFlags())); err != nil {
		return err
	}
	if *jobType == "codex" {
		resolvedPrompt, err := resolveRuntimePrompt(*prompt, *promptFile)
		if err != nil {
			return err
		}
		*prompt = resolvedPrompt
	}

	now := time.Now().In(time.Local)
	runDir := filepath.Join(m.runsDir, "once", sanitizeName(*jobName), now.Format("2006-01-02-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	command := []string{}
	switch *jobType {
	case "codex":
		command = m.codexLaunchCommand(*prompt, *workspace, *jobName, *ui)
	default:
		return fmt.Errorf("unknown one-off job type %q", *jobType)
	}

	runMD := filepath.Join(runDir, "run.md")
	if err := os.WriteFile(runMD, []byte(strings.Join([]string{
		fmt.Sprintf("# %s One-Off Run", sanitizeName(*jobName)),
		"",
		fmt.Sprintf("- Fired at: %s", now.Format("2006-01-02 15:04:05 MST")),
		fmt.Sprintf("- Command: %s", strings.Join(command, " ")),
		"",
		"## Result",
		"",
	}, "\n")), 0o644); err != nil {
		return err
	}

	env := runtimeEnv(*jobName, "one-off", runDir, "")
	if m.isDefaultOnceWorkspace(*workspace) {
		env["DECLAW_CODEX_STATELESS"] = "1"
	}
	exitCode := m.runRuntimeCommand(*jobType, *jobName, *prompt, *workspace, *ui, env)
	finishedAt := time.Now().In(time.Local)
	if err := appendLines(runMD,
		fmt.Sprintf("- Exit code: %d", exitCode),
		fmt.Sprintf("- Finished at: %s", finishedAt.Format("2006-01-02 15:04:05 MST")),
		"",
		"## Cleanup",
	); err != nil {
		return err
	}

	if err := m.cleanupOnceJob(*cleanupLabel); err != nil {
		_ = appendLines(runMD, fmt.Sprintf("- Cleanup error: %s", err))
		return err
	}
	return appendLines(runMD, fmt.Sprintf("- Removed label: %s", *cleanupLabel))
}

func (m *Manager) cleanupOnceJob(label string) error {
	store, err := m.loadJobs()
	if err != nil {
		return err
	}
	delete(store.Jobs, sanitizeName(labelName(label)))
	m.removePromptFile(labelName(label))
	if err := m.saveJobs(store); err != nil {
		return err
	}
	if err := m.uninstallLabel(label); err != nil {
		return err
	}
	return nil
}

func (m *Manager) runCodexInternal(args []string) error {
	fs := flag.NewFlagSet("run-codex", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	promptFile := fs.String("prompt-file", "", "prompt file")
	workspace := fs.String("workspace", "", "workspace")
	jobName := fs.String("job-name", "", "job")
	ui := fs.String("ui", "app-server", "ui")
	deletePromptFile := fs.Bool("delete-prompt-file", false, "delete prompt file")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{
		"prompt-file": true,
		"workspace":   true,
		"job-name":    true,
		"ui":          true,
	})); err != nil {
		return err
	}

	if strings.TrimSpace(*promptFile) == "" {
		return errors.New("missing --prompt-file")
	}
	promptBytes, err := os.ReadFile(*promptFile)
	if err != nil {
		return err
	}
	if *deletePromptFile {
		_ = os.Remove(*promptFile)
	}

	return runCodexWithUI(string(promptBytes), *workspace, *jobName, *ui, map[string]string{})
}

func (m *Manager) runRuntimeCommand(jobType, jobName, prompt, workspace, ui string, env map[string]string) int {
	switch jobType {
	case "codex":
		if err := m.launchCodex(prompt, workspace, jobName, ui, env); err != nil {
			fmt.Fprintf(os.Stderr, "codex run failed: %s\n", err)
			return 1
		}
		return 0
	default:
		return 1
	}
}

func (m *Manager) launchCodexInteractive(prompt, workspace, jobName string, extraEnv map[string]string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("missing Codex prompt")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("codex command not found in PATH")
	}

	runDir := extraEnv["DECLAW_RUN_DIR"]
	if strings.TrimSpace(runDir) == "" {
		var err error
		runDir, err = os.MkdirTemp(m.runsDir, "codex-")
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	promptPath := filepath.Join(runDir, "codex-prompt.txt")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return err
	}

	scriptPath := filepath.Join(runDir, "run-codex.command")
	script := m.codexTerminalScript(promptPath, workspace, jobName, extraEnv)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	if jobName != "" {
		fmt.Printf("Opening scheduled Codex job in Terminal: %s\n", jobName)
	}
	if workspace != "" {
		fmt.Printf("Workspace: %s\n\n", workspace)
	}

	cmd := exec.Command("/usr/bin/open", "-a", "Terminal", scriptPath)
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) codexTerminalScript(promptPath, workspace, jobName string, extraEnv map[string]string) string {
	var b strings.Builder
	b.WriteString("#!/bin/zsh\n")
	b.WriteString("set -e\n")
	for key, value := range extraEnv {
		if isSafeEnvName(key) {
			b.WriteString("export ")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(shellQuote(value))
			b.WriteString("\n")
		}
	}
	if workspace != "" {
		b.WriteString("cd ")
		b.WriteString(shellQuote(workspace))
		b.WriteString("\n")
	}
	b.WriteString("exec ")
	b.WriteString(shellQuote(m.executablePath))
	b.WriteString(" schedule __internal run-codex --prompt-file ")
	b.WriteString(shellQuote(promptPath))
	b.WriteString(" --delete-prompt-file --job-name ")
	b.WriteString(shellQuote(jobName))
	if workspace != "" {
		b.WriteString(" --workspace ")
		b.WriteString(shellQuote(workspace))
	}
	b.WriteString(" --ui codex")
	b.WriteString("\n")
	return b.String()
}

func runCodexWithUI(prompt, workspace, jobName, ui string, extraEnv map[string]string) error {
	switch effectiveCodexUI(ui) {
	case "codex":
		return runCodexDirect(prompt, workspace, jobName, extraEnv)
	case "declaw":
		return runDeclawCodexChat(prompt, workspace, jobName, extraEnv)
	case "app-server":
		return runCodexAppServerChat(prompt, workspace, jobName, extraEnv)
	default:
		return fmt.Errorf("unknown Codex UI %q", ui)
	}
}

func runCodexDirect(prompt, workspace, jobName string, extraEnv map[string]string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("missing Codex prompt")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("codex command not found in PATH")
	}

	program, args := codexCommand(prompt, workspace)

	if jobName != "" {
		fmt.Printf("Launching scheduled Codex job: %s\n", jobName)
	}
	if workspace != "" {
		fmt.Printf("Workspace: %s\n\n", workspace)
	}

	cmd := exec.Command(program, args...)
	if workspace != "" {
		cmd.Dir = workspace
	}
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type codexExecEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     codexExecItem   `json:"item"`
	Raw      json.RawMessage `json:"-"`
}

type codexExecItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type declawChatMessage struct {
	Role string
	Text string
}

func runDeclawCodexChat(prompt, workspace, jobName string, extraEnv map[string]string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("missing Codex prompt")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("codex command not found in PATH")
	}
	logPath := filepath.Join(os.TempDir(), "declaw-codex-chat.err.log")
	if runDir := envValue(extraEnv, "DECLAW_RUN_DIR"); strings.TrimSpace(runDir) != "" {
		logPath = filepath.Join(runDir, "codex-exec.err.log")
	}

	fmt.Println("declaw")
	fmt.Println()

	agentName := declawAgentName(workspace)
	printRecentDeclawChatHistory(workspace, agentName, declawChatHistoryLimit)

	transcriptPath, err := startDeclawChatTranscript(workspace, jobName, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create visible chat transcript: %s\n", err)
	}

	threadID, displayMessage, err := runCodexExecTurn("", prompt, workspace, extraEnv, logPath, agentName)
	if err != nil {
		return err
	}
	if transcriptPath != "" && displayMessage != "" {
		_ = appendDeclawChatMessage(transcriptPath, agentName, displayMessage)
	}
	if threadID == "" {
		return errors.New("codex exec did not return a thread id")
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(colorize("\nYou > ", ansiBlue))
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
		message := strings.TrimSpace(line)
		if message == "" {
			continue
		}
		redrawUserInput(message)
		switch strings.ToLower(message) {
		case "q", "quit", "exit":
			fmt.Println("bye")
			return nil
		case "/raw":
			fmt.Printf("Raw Codex session: codex resume %s\n", threadID)
			fmt.Printf("Raw log: %s\n", logPath)
			continue
		case "/info":
			if jobName != "" {
				fmt.Printf("Job: %s\n", jobName)
			}
			if workspace != "" {
				fmt.Printf("Workspace: %s\n", workspace)
			}
			fmt.Printf("Raw log: %s\n", logPath)
			if transcriptPath != "" {
				fmt.Printf("Visible chat transcript: %s\n", transcriptPath)
			}
			continue
		}

		if transcriptPath != "" {
			_ = appendDeclawChatMessage(transcriptPath, "User", message)
		}
		nextThreadID, displayMessage, err := runCodexExecTurn(threadID, message, workspace, extraEnv, logPath, agentName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "follow-up failed: %s\n", err)
			fmt.Fprintf(os.Stderr, "You can try again, or open the raw session with /raw.\n")
			continue
		}
		if transcriptPath != "" && displayMessage != "" {
			_ = appendDeclawChatMessage(transcriptPath, agentName, displayMessage)
		}
		if nextThreadID != "" {
			threadID = nextThreadID
		}
	}
}

func runCodexExecTurn(threadID, prompt, workspace string, extraEnv map[string]string, stderrPath, agentName string) (string, string, error) {
	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume", "--json", "--skip-git-repo-check")
		args = append(args, threadID, prompt)
	} else {
		args = append(args, "--json", "--skip-git-repo-check")
		if workspace != "" {
			args = append(args, "-C", workspace)
		}
		args = append(args, prompt)
	}

	cmd := exec.Command("codex", args...)
	if workspace != "" {
		cmd.Dir = workspace
	}
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", "", err
	}
	defer stderrFile.Close()
	cmd.Stderr = stderrFile

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	seenThreadID := threadID
	messages := []string{}
	showSpinner := stdoutIsTerminal()
	done := make(chan struct{})
	if showSpinner {
		go runDeclawSpinner(done)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event codexExecEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Type {
		case "thread.started":
			if event.ThreadID != "" {
				seenThreadID = event.ThreadID
			}
		case "item.completed":
			if event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) != "" {
				messages = append(messages, strings.TrimSpace(event.Item.Text))
			}
		}
	}
	if showSpinner {
		close(done)
		clearDeclawSpinner()
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return seenThreadID, "", err
	}
	if err := cmd.Wait(); err != nil {
		return seenThreadID, "", err
	}
	displayMessage := selectDeclawDisplayMessage(messages)
	if displayMessage != "" {
		printDeclawChatMessage(agentName, displayMessage)
	}
	return seenThreadID, displayMessage, nil
}

func startDeclawChatTranscript(workspace, jobName, prompt string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", nil
	}
	sessionsDir := filepath.Join(workspace, "SESSIONS")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return "", err
	}
	now := time.Now().In(time.Local)
	name := now.Format("2006-01-02T15-04-05") + "-declaw-chat.md"
	path := filepath.Join(sessionsDir, name)
	title := "# Declaw Chat " + now.Format("2006-01-02 15:04:05 MST")
	parts := []string{title, ""}
	if strings.TrimSpace(jobName) != "" {
		parts = append(parts, fmt.Sprintf("- Job: `%s`", jobName))
	}
	parts = append(parts, fmt.Sprintf("- Workspace: `%s`", workspace), "")
	if task := extractTaskPrompt(prompt); task != "" {
		parts = append(parts, "## User", "", task, "")
	}
	return path, os.WriteFile(path, []byte(strings.Join(parts, "\n")), 0o644)
}

func appendDeclawChatMessage(path, role, message string) error {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(message) == "" {
		return nil
	}
	return appendLines(path, fmt.Sprintf("## %s", role), "", strings.TrimSpace(message), "")
}

func printRecentDeclawChatHistory(workspace, agentName string, limit int) {
	if strings.TrimSpace(workspace) == "" || limit <= 0 {
		return
	}
	files, err := filepath.Glob(filepath.Join(workspace, "SESSIONS", "*-declaw-chat.md"))
	if err != nil || len(files) == 0 {
		return
	}
	sort.Strings(files)
	if len(files) > limit {
		files = files[len(files)-limit:]
	}
	fmt.Println(colorize("Recent chat", ansiDim))
	for _, path := range files {
		messages, err := parseDeclawChatTranscript(path)
		if err != nil || len(messages) == 0 {
			continue
		}
		fmt.Println(colorize(filepath.Base(path), ansiDim))
		for _, message := range messages {
			printDeclawChatMessage(normalizeTranscriptRole(message.Role, agentName), message.Text)
		}
	}
	fmt.Println(colorize("End recent chat", ansiDim))
	fmt.Println()
}

func parseDeclawChatTranscript(path string) ([]declawChatMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	messages := []declawChatMessage{}
	currentRole := ""
	currentLines := []string{}
	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if currentRole != "" && text != "" {
			messages = append(messages, declawChatMessage{Role: currentRole, Text: text})
		}
		currentLines = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentRole = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if currentRole != "" {
			currentLines = append(currentLines, line)
		}
	}
	flush()
	return messages, nil
}

func normalizeTranscriptRole(role, agentName string) string {
	if strings.EqualFold(role, "Assistant") {
		return agentName
	}
	if strings.EqualFold(role, "User") {
		return "You"
	}
	return role
}

func printDeclawChatMessage(role, message string) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "Declaw"
	}
	color := ansiGreen
	if strings.EqualFold(role, "You") || strings.EqualFold(role, "User") {
		color = ansiBlue
		role = "You"
	}
	label := colorize(role+" >", color)
	text := colorize(strings.TrimSpace(message), color)
	fmt.Printf("\n%s %s\n", label, text)
}

func redrawUserInput(message string) {
	if stdinIsTerminal() && stdoutIsTerminal() {
		fmt.Print("\x1b[1A\r\x1b[2K")
	}
	fmt.Printf("%s %s\n", colorize("You >", ansiBlue), colorize(message, ansiBlue))
}

func declawAgentName(workspace string) string {
	if strings.TrimSpace(workspace) == "" {
		return "Declaw"
	}
	data, err := os.ReadFile(filepath.Join(workspace, "WORKSPACE", "IDENTITY.md"))
	if err != nil {
		return "Declaw"
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- Name:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "- Name:"))
		if name != "" {
			return name
		}
	}
	return "Declaw"
}

const (
	ansiReset    = "\x1b[0m"
	ansiGreen    = "\x1b[32m"
	ansiGreenDim = "\x1b[32m\x1b[2m" // Green + dim
	ansiBlue     = "\x1b[36m"
	ansiDim      = "\x1b[2m"
)

func colorize(value, color string) string {
	if color == "" || !stdoutIsTerminal() {
		return value
	}
	return color + value + ansiReset
}

func runDeclawSpinner(done <-chan struct{}) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	idx := 0
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fmt.Fprintf(os.Stdout, "\r%s working...", declawSpinnerFrames[idx%len(declawSpinnerFrames)])
			idx++
		}
	}
}

func clearDeclawSpinner() {
	fmt.Fprint(os.Stdout, "\r              \r")
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func selectDeclawDisplayMessage(messages []string) string {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		message := strings.TrimSpace(messages[idx])
		if message == "" {
			continue
		}
		if looksLikeDeclawProcessNarration(message) && idx > 0 {
			continue
		}
		return message
	}
	return ""
}

func looksLikeDeclawProcessNarration(message string) bool {
	lowered := strings.ToLower(strings.TrimSpace(message))
	prefixes := []string{
		"i'll ",
		"i will ",
		"i’m ",
		"i am ",
		"startup context",
		"memory hygiene",
		"the core workspace files",
		"yesterday’s distilled memory",
		"yesterday's distilled memory",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return strings.Contains(lowered, "session log") && strings.Contains(lowered, "workspace")
}

func (m *Manager) launchCodex(prompt, workspace, jobName, ui string, extraEnv map[string]string) error {
	switch effectiveCodexUI(ui) {
	case "codex":
		return m.launchCodexInteractive(prompt, workspace, jobName, extraEnv)
	case "declaw":
		return m.launchDeclawCodexChat(prompt, workspace, jobName, extraEnv)
	case "app-server":
		return m.launchAppServerCodexChat(prompt, workspace, jobName, extraEnv)
	default:
		return fmt.Errorf("unknown Codex UI %q", ui)
	}
}

func (m *Manager) launchDeclawCodexChat(prompt, workspace, jobName string, extraEnv map[string]string) error {
	return m.launchCodexChatTerminal(prompt, workspace, jobName, "declaw", extraEnv)
}

func (m *Manager) launchAppServerCodexChat(prompt, workspace, jobName string, extraEnv map[string]string) error {
	return m.launchCodexChatTerminal(prompt, workspace, jobName, "app-server", extraEnv)
}

func (m *Manager) launchCodexChatTerminal(prompt, workspace, jobName, ui string, extraEnv map[string]string) error {
	runDir := extraEnv["DECLAW_RUN_DIR"]
	if strings.TrimSpace(runDir) == "" {
		var err error
		runDir, err = os.MkdirTemp(m.runsDir, "codex-")
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	promptPath := filepath.Join(runDir, "codex-prompt.txt")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return err
	}

	scriptPath := filepath.Join(runDir, "run-"+effectiveCodexUI(ui)+"-chat.command")
	script := m.declawChatTerminalScript(promptPath, workspace, jobName, effectiveCodexUI(ui), extraEnv)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	if jobName != "" {
		fmt.Printf("Opening %s chat for scheduled Codex job: %s\n", effectiveCodexUI(ui), jobName)
	}
	if workspace != "" {
		fmt.Printf("Workspace: %s\n\n", workspace)
	}

	cmd := exec.Command("/usr/bin/open", "-a", "Terminal", scriptPath)
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) declawChatTerminalScript(promptPath, workspace, jobName, ui string, extraEnv map[string]string) string {
	var b strings.Builder
	b.WriteString("#!/bin/zsh\n")
	b.WriteString("set -e\n")
	for key, value := range extraEnv {
		if isSafeEnvName(key) {
			b.WriteString("export ")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(shellQuote(value))
			b.WriteString("\n")
		}
	}
	if workspace != "" {
		b.WriteString("cd ")
		b.WriteString(shellQuote(workspace))
		b.WriteString("\n")
	}
	b.WriteString("exec ")
	b.WriteString(shellQuote(m.executablePath))
	b.WriteString(" schedule __internal run-codex --prompt-file ")
	b.WriteString(shellQuote(promptPath))
	b.WriteString(" --delete-prompt-file --job-name ")
	b.WriteString(shellQuote(jobName))
	if workspace != "" {
		b.WriteString(" --workspace ")
		b.WriteString(shellQuote(workspace))
	}
	b.WriteString(" --ui ")
	b.WriteString(shellQuote(effectiveCodexUI(ui)))
	b.WriteString("\n")
	return b.String()
}

func codexCommand(prompt, workspace string) (string, []string) {
	codexArgs := []string{"--no-alt-screen"}
	if workspace != "" {
		codexArgs = append(codexArgs, "-C", workspace)
	}
	codexArgs = append(codexArgs, prompt)
	return "codex", codexArgs
}

func codexCommandForUI(prompt, workspace, ui string) (string, []string) {
	switch effectiveCodexUI(ui) {
	case "declaw":
		args := []string{"exec", "--json", "--skip-git-repo-check"}
		if workspace != "" {
			args = append(args, "-C", workspace)
		}
		args = append(args, prompt)
		return "codex", args
	case "app-server":
		args := []string{"app-server", "--listen", "stdio://", prompt}
		return "codex", args
	}
	return codexCommand(prompt, workspace)
}

func (m *Manager) buildPlist(label string, argv []string, record JobRecord, calendarEntries []map[string]int) map[string]any {
	payload := map[string]any{
		"Label":                label,
		"ProgramArguments":     argv,
		"RunAtLoad":            false,
		"EnvironmentVariables": buildStandardEnv(record.Env),
	}

	if calendarEntries != nil {
		payload["StartCalendarInterval"] = calendarEntries
	} else {
		payload["StartCalendarInterval"] = configCalendar(record.Config)
	}

	if record.Cwd != "" {
		payload["WorkingDirectory"] = record.Cwd
	}
	if record.Stdout != "" {
		payload["StandardOutPath"] = record.Stdout
	} else {
		payload["StandardOutPath"] = filepath.Join(m.supportDir, "logs", label+".out.log")
	}
	if record.Stderr != "" {
		payload["StandardErrorPath"] = record.Stderr
	} else {
		payload["StandardErrorPath"] = filepath.Join(m.supportDir, "logs", label+".err.log")
	}
	return payload
}

func configCalendar(config ScheduleConfig) any {
	makeEntry := func(weekday int) map[string]int {
		entry := map[string]int{
			"Hour":   config.Hour,
			"Minute": config.Minute,
		}
		if config.Month != 0 {
			entry["Month"] = config.Month
		}
		if config.Day != 0 {
			entry["Day"] = config.Day
		}
		if weekday != 0 {
			entry["Weekday"] = weekday
		}
		return entry
	}

	if len(config.Weekdays) == 0 {
		return makeEntry(0)
	}
	values := make([]map[string]int, 0, len(config.Weekdays))
	for _, weekday := range config.Weekdays {
		values = append(values, makeEntry(weekday))
	}
	return values
}

func recoveryCalendarEntries(config ScheduleConfig) []map[string]int {
	weekdays := config.Weekdays
	if len(weekdays) == 0 {
		weekdays = []int{0}
	}
	entries := []map[string]int{}
	for _, weekday := range weekdays {
		for _, minute := range []int{0, 30} {
			entry := map[string]int{"Minute": minute}
			if weekday != 0 {
				entry["Weekday"] = weekday
			}
			if config.Day != 0 {
				entry["Day"] = config.Day
			}
			if config.Month != 0 {
				entry["Month"] = config.Month
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func buildStandardEnv(extra []string) map[string]string {
	env := map[string]string{
		"HOME": os.Getenv("HOME"),
		"PATH": os.Getenv("PATH"),
	}
	if env["PATH"] == "" {
		env["PATH"] = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	addPassthroughToolEnv(env)
	for _, item := range extra {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

func buildPlistXML(payload map[string]any) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	writePlistValue(&b, payload, 1)
	b.WriteString("\n</plist>\n")
	return []byte(b.String())
}

func writePlistValue(b *strings.Builder, value any, depth int) {
	indent := strings.Repeat("  ", depth)
	switch typed := value.(type) {
	case map[string]any:
		b.WriteString(indent + "<dict>\n")
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteString(indent + "  <key>")
			escapeXMLText(b, key)
			b.WriteString("</key>\n")
			writePlistValue(b, typed[key], depth+1)
			b.WriteString("\n")
		}
		b.WriteString(indent + "</dict>")
	case map[string]string:
		data := map[string]any{}
		for key, value := range typed {
			data[key] = value
		}
		writePlistValue(b, data, depth)
	case map[string]int:
		data := map[string]any{}
		for key, value := range typed {
			data[key] = value
		}
		writePlistValue(b, data, depth)
	case []string:
		b.WriteString(indent + "<array>\n")
		for _, item := range typed {
			writePlistValue(b, item, depth+1)
			b.WriteString("\n")
		}
		b.WriteString(indent + "</array>")
	case []map[string]int:
		b.WriteString(indent + "<array>\n")
		for _, item := range typed {
			writePlistValue(b, item, depth+1)
			b.WriteString("\n")
		}
		b.WriteString(indent + "</array>")
	case []any:
		b.WriteString(indent + "<array>\n")
		for _, item := range typed {
			writePlistValue(b, item, depth+1)
			b.WriteString("\n")
		}
		b.WriteString(indent + "</array>")
	case string:
		b.WriteString(indent + "<string>")
		escapeXMLText(b, typed)
		b.WriteString("</string>")
	case int:
		b.WriteString(indent + "<integer>" + strconv.Itoa(typed) + "</integer>")
	case bool:
		if typed {
			b.WriteString(indent + "<true/>")
		} else {
			b.WriteString(indent + "<false/>")
		}
	default:
		b.WriteString(indent + "<string>")
		escapeXMLText(b, fmt.Sprint(value))
		b.WriteString("</string>")
	}
}

func escapeXMLText(b *strings.Builder, value string) {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	b.Write(escaped.Bytes())
}

func (m *Manager) installedPlistPath(label string) string {
	return filepath.Join(m.launchAgentsDir, label+".plist")
}

func primaryLabel(name string) string {
	return labelPrefix + ".recurring." + sanitizeName(name)
}

func recoveryLabel(name string) string {
	return primaryLabel(name) + ".recovery"
}

func onceLabel(name string) string {
	return labelPrefix + ".once." + sanitizeName(name)
}

func labelsForJob(job JobRecord) []string {
	if job.Config.Kind == "once" {
		return []string{job.OnceLabel}
	}
	labels := []string{job.PrimaryLabel}
	if job.HasRecovery {
		labels = append(labels, job.RecoveryLabel)
	}
	return labels
}

func containsLabel(job JobRecord, label string) bool {
	for _, candidate := range labelsForJob(job) {
		if candidate == label {
			return true
		}
	}
	return false
}

func labelName(label string) string {
	if strings.HasPrefix(label, labelPrefix+".once.") {
		return strings.TrimPrefix(label, labelPrefix+".once.")
	}
	if strings.HasPrefix(label, labelPrefix+".recurring.") {
		value := strings.TrimPrefix(label, labelPrefix+".recurring.")
		return strings.TrimSuffix(value, ".recovery")
	}
	return sanitizeName(label)
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "job"
	}
	return out
}

func buildCodexPrompt(prompt string, recurring bool, workspaceRoot bool) string {
	parts := []string{scheduledCodexPromptIntro}
	if workspaceRoot {
		parts = append(parts, workspaceCodexPromptPrefix)
	}
	if workspaceRoot && recurring {
		parts = append(parts, recurringCodexPromptPrefix)
	}
	parts = append(parts, scheduledCodexLocalToolPrefix)
	parts = append(parts, scheduledCodexChatHandoff)
	return strings.Join(parts, "\n\n") + "\n\nTask:\n" + strings.TrimSpace(prompt)
}

func extractTaskPrompt(prompt string) string {
	marker := "\n\nTask:\n"
	if idx := strings.Index(prompt, marker); idx >= 0 {
		return strings.TrimSpace(prompt[idx+len(marker):])
	}
	return strings.TrimSpace(prompt)
}

func formatConfig(config ScheduleConfig) string {
	if config.Kind == "once" {
		return fmt.Sprintf("once %02d-%02d %02d:%02d", config.Month, config.Day, config.Hour, config.Minute)
	}
	if len(config.Weekdays) == 5 && joinWeekdays(config.Weekdays) == "1,2,3,4,5" {
		return "weekdays " + config.ScheduledTime
	}
	if len(config.Weekdays) == 1 {
		return fmt.Sprintf("weekly %d@%s", config.Weekdays[0], config.ScheduledTime)
	}
	if config.Day != 0 || config.Month != 0 {
		parts := []string{fmt.Sprintf("time %02d:%02d", config.Hour, config.Minute)}
		if config.Month != 0 {
			parts = append(parts, fmt.Sprintf("month %d", config.Month))
		}
		if config.Day != 0 {
			parts = append(parts, fmt.Sprintf("day %d", config.Day))
		}
		return strings.Join(parts, " ")
	}
	return "daily " + config.ScheduledTime
}

func joinWeekdays(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

type launchctlResult struct {
	Stdout string
	Stderr string
	Err    error
}

func (m *Manager) runLaunchctl(check bool, args ...string) *launchctlResult {
	cmd := exec.Command(launchctlPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && !check {
		err = nil
	}
	return &launchctlResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
}

func (r *launchctlResult) Output() (string, error) {
	output := strings.TrimSpace(r.Stdout + r.Stderr)
	if r.Err != nil && output != "" {
		return output, fmt.Errorf("%w: %s", r.Err, output)
	}
	return output, r.Err
}

func (m *Manager) bootout(label string) error {
	_, err := m.runLaunchctl(false, "bootout", m.domain, m.installedPlistPath(label)).Output()
	return err
}

func (m *Manager) bootstrap(label string) error {
	_, err := m.runLaunchctl(true, "bootstrap", m.domain, m.installedPlistPath(label)).Output()
	return err
}

func (m *Manager) enableLabel(label string) error {
	_, err := m.runLaunchctl(true, "enable", fmt.Sprintf("%s/%s", m.domain, label)).Output()
	return err
}

func (m *Manager) uninstallLabel(label string) error {
	_ = m.bootout(label)
	if err := os.Remove(m.installedPlistPath(label)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *Manager) promptPath(jobName string) string {
	return filepath.Join(m.jobsDir, sanitizeName(jobName)+".prompt.txt")
}

func (m *Manager) writePromptFile(record JobRecord) error {
	if strings.TrimSpace(record.Prompt) == "" {
		return errors.New("missing Codex prompt")
	}
	if err := os.MkdirAll(m.jobsDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.promptPath(record.Name), []byte(record.Prompt), 0o600)
}

func (m *Manager) removePromptFile(jobName string) {
	_ = os.Remove(m.promptPath(jobName))
}

func (m *Manager) simpleLabelAction(args []string, pastTense string, action func(string) error) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule <enable|disable|restart> <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	lines := []string{}
	for _, label := range labelsForJob(job) {
		if err := action(label); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("%s %s", pastTense, label))
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) loadJobs() (JobStore, error) {
	raw, err := os.ReadFile(m.jobsPath)
	if errors.Is(err, os.ErrNotExist) {
		return JobStore{Jobs: map[string]JobRecord{}}, nil
	}
	if err != nil {
		return JobStore{}, err
	}

	var store JobStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return JobStore{}, err
	}
	if store.Jobs == nil {
		store.Jobs = map[string]JobRecord{}
	}
	return store, nil
}

func (m *Manager) saveJobs(store JobStore) error {
	if store.Jobs == nil {
		store.Jobs = map[string]JobRecord{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(m.jobsPath, data, 0o644)
}

func (m *Manager) getJob(name string) (JobRecord, error) {
	store, err := m.loadJobs()
	if err != nil {
		return JobRecord{}, err
	}
	job, ok := store.Jobs[sanitizeName(name)]
	if !ok {
		return JobRecord{}, fmt.Errorf("unknown job %q", name)
	}
	return job, nil
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func scheduleValueFlags() map[string]bool {
	return map[string]bool{
		"prompt":    true,
		"project":   true,
		"workspace": true,
		"daily":     true,
		"time":      true,
		"weekdays":  true,
		"weekly":    true,
		"at":        true,
		"year":      true,
		"month":     true,
		"day":       true,
		"hour":      true,
		"minute":    true,
		"cwd":       true,
		"stdout":    true,
		"stderr":    true,
		"weekday":   true,
		"env":       true,
		"ui":        true,
	}
}

func internalRecurringValueFlags() map[string]bool {
	return map[string]bool{
		"job":            true,
		"trigger-kind":   true,
		"scheduled-time": true,
		"type":           true,
		"day":            true,
		"month":          true,
		"prompt":         true,
		"prompt-file":    true,
		"workspace":      true,
		"ui":             true,
		"weekday":        true,
	}
}

func internalOnceValueFlags() map[string]bool {
	return map[string]bool{
		"job":           true,
		"cleanup-label": true,
		"type":          true,
		"prompt":        true,
		"prompt-file":   true,
		"workspace":     true,
		"ui":            true,
	}
}

type scheduleShapeInput struct {
	Daily    string
	Time     string
	Weekdays string
	Weekly   string
	At       string
	Once     bool
	Year     int
	Month    int
	Day      int
	Hour     int
	Minute   int
	Weekday  []string
}

func hasScheduleShape(input scheduleShapeInput) bool {
	return strings.TrimSpace(input.Daily) != "" ||
		strings.TrimSpace(input.Time) != "" ||
		strings.TrimSpace(input.Weekdays) != "" ||
		strings.TrimSpace(input.Weekly) != "" ||
		strings.TrimSpace(input.At) != "" ||
		input.Once ||
		input.Year != -1 ||
		input.Month != -1 ||
		input.Day != -1 ||
		input.Hour != -1 ||
		input.Minute != -1 ||
		len(input.Weekday) > 0
}

func resolveScheduleConfig(input scheduleShapeInput) (ScheduleConfig, error) {
	explicitFieldsUsed := input.Year != -1 || input.Minute != -1 || input.Hour != -1 || input.Day != -1 || input.Month != -1 || len(input.Weekday) > 0
	convenienceCount := 0
	for _, value := range []string{input.Time, input.Daily, input.Weekdays, input.Weekly, input.At} {
		if strings.TrimSpace(value) != "" {
			convenienceCount++
		}
	}
	if convenienceCount > 1 {
		return ScheduleConfig{}, errors.New("choose exactly one of --time, --daily, --weekdays, --weekly, or --at")
	}
	if strings.TrimSpace(input.At) != "" && explicitFieldsUsed {
		return ScheduleConfig{}, errors.New("--at cannot be combined with explicit calendar fields")
	}
	if strings.TrimSpace(input.At) != "" {
		dt, err := parseOnceTime(input.At)
		if err != nil {
			return ScheduleConfig{}, err
		}
		return ScheduleConfig{
			Kind:   "once",
			Year:   dt.Year(),
			Month:  int(dt.Month()),
			Day:    dt.Day(),
			Hour:   dt.Hour(),
			Minute: dt.Minute(),
		}, nil
	}
	if strings.TrimSpace(input.Weekly) != "" {
		weekday, timeText, err := parseWeeklySpec(input.Weekly)
		if err != nil {
			return ScheduleConfig{}, err
		}
		hour, minute, err := parseDailyTime(timeText)
		if err != nil {
			return ScheduleConfig{}, err
		}
		return ScheduleConfig{Kind: "recurring", Hour: hour, Minute: minute, Weekdays: []int{weekday}, ScheduledTime: timeText}, nil
	}
	if strings.TrimSpace(input.Weekdays) != "" {
		hour, minute, err := parseDailyTime(input.Weekdays)
		if err != nil {
			return ScheduleConfig{}, err
		}
		return ScheduleConfig{Kind: "recurring", Hour: hour, Minute: minute, Weekdays: []int{1, 2, 3, 4, 5}, ScheduledTime: input.Weekdays}, nil
	}
	if strings.TrimSpace(input.Daily) != "" || strings.TrimSpace(input.Time) != "" {
		value := strings.TrimSpace(input.Daily)
		if value == "" {
			value = strings.TrimSpace(input.Time)
		}
		hour, minute, err := parseDailyTime(value)
		if err != nil {
			return ScheduleConfig{}, err
		}
		return ScheduleConfig{Kind: "recurring", Hour: hour, Minute: minute, ScheduledTime: value}, nil
	}
	if explicitFieldsUsed {
		if input.Hour == -1 || input.Minute == -1 {
			return ScheduleConfig{}, errors.New("--hour and --minute are required when using explicit calendar fields")
		}
		if input.Hour < 0 || input.Hour > 23 {
			return ScheduleConfig{}, errors.New("--hour must be between 0 and 23")
		}
		if input.Minute < 0 || input.Minute > 59 {
			return ScheduleConfig{}, errors.New("--minute must be between 0 and 59")
		}
		weekdays, err := normalizeWeekdays(input.Weekday)
		if err != nil {
			return ScheduleConfig{}, err
		}
		if input.Once {
			if input.Year == -1 || input.Month == -1 || input.Day == -1 {
				return ScheduleConfig{}, errors.New("--once with explicit calendar fields requires --year, --month, and --day")
			}
			dt := time.Date(input.Year, time.Month(input.Month), input.Day, input.Hour, input.Minute, 0, 0, time.Local)
			if dt.Before(time.Now()) {
				return ScheduleConfig{}, errors.New("explicit one-off schedule must be in the future")
			}
			return ScheduleConfig{
				Kind:   "once",
				Year:   input.Year,
				Month:  input.Month,
				Day:    input.Day,
				Hour:   input.Hour,
				Minute: input.Minute,
			}, nil
		}
		return ScheduleConfig{
			Kind:          "recurring",
			Month:         zeroIfUnset(input.Month),
			Day:           zeroIfUnset(input.Day),
			Hour:          input.Hour,
			Minute:        input.Minute,
			Weekdays:      weekdays,
			ScheduledTime: fmt.Sprintf("%02d:%02d", input.Hour, input.Minute),
		}, nil
	}
	return ScheduleConfig{}, errors.New("missing schedule. use --time/--daily/--weekdays/--weekly/--at or explicit calendar fields")
}

func zeroIfUnset(value int) int {
	if value == -1 {
		return 0
	}
	return value
}

func parseDailyTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid time %q. expected HH:MM", value)
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func parseOnceTime(value string) (time.Time, error) {
	formats := []string{"2006-01-02 15:04", "2006-01-02T15:04"}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, value, time.Local); err == nil {
			if !parsed.After(time.Now()) {
				return time.Time{}, errors.New("--at must be in the future")
			}
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid datetime %q. expected YYYY-MM-DD HH:MM", value)
}

func parseWeeklySpec(value string) (int, string, error) {
	parts := strings.SplitN(value, "@", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid weekly spec %q. expected WEEKDAY@HH:MM", value)
	}
	weekday, err := parseWeekday(parts[0])
	if err != nil {
		return 0, "", err
	}
	return weekday, parts[1], nil
}

func parseWeekday(value string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "mon", "monday":
		return 1, nil
	case "2", "tue", "tues", "tuesday":
		return 2, nil
	case "3", "wed", "wednesday":
		return 3, nil
	case "4", "thu", "thur", "thurs", "thursday":
		return 4, nil
	case "5", "fri", "friday":
		return 5, nil
	case "6", "sat", "saturday":
		return 6, nil
	case "7", "sun", "sunday":
		return 7, nil
	default:
		return 0, fmt.Errorf("invalid weekday %q", value)
	}
}

func normalizeWeekdays(values []string) ([]int, error) {
	if len(values) == 0 {
		return nil, nil
	}
	set := map[int]struct{}{}
	for _, value := range values {
		weekday, err := parseWeekday(value)
		if err != nil {
			return nil, err
		}
		set[weekday] = struct{}{}
	}
	out := make([]int, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Ints(out)
	return out, nil
}

func weekdayInts(values []string) []int {
	out := []int{}
	for _, value := range values {
		if weekday, err := strconv.Atoi(value); err == nil {
			out = append(out, weekday)
		}
	}
	return out
}

func shouldSkipWeekday(now time.Time, weekdays []int) bool {
	if len(weekdays) == 0 {
		return false
	}
	current := int(now.Weekday())
	if current == 0 {
		current = 7
	}
	for _, weekday := range weekdays {
		if weekday == current {
			return false
		}
	}
	return true
}

func shouldSkipCalendarDay(now time.Time, day, month int) bool {
	if month != 0 && int(now.Month()) != month {
		return true
	}
	if day != 0 && now.Day() != day {
		return true
	}
	return false
}

func shouldSkipRecovery(now time.Time, scheduledHour, scheduledMinute int) bool {
	return now.Hour()*60+now.Minute() < scheduledHour*60+scheduledMinute
}

func slotKey(scheduledTime string) string {
	hour, minute, _ := parseDailyTime(scheduledTime)
	return fmt.Sprintf("%02d%02d", hour, minute)
}

func writeJSON(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func appendLines(path string, lines ...string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(strings.Join(lines, "\n") + "\n")
	return err
}

func runtimeEnv(job, triggerKind, runDir, scheduledTime string) map[string]string {
	env := map[string]string{
		"DECLAW_SCHEDULE_JOB":          sanitizeName(job),
		"DECLAW_TRIGGER_KIND":          triggerKind,
		"DECLAW_RUN_DIR":               runDir,
		"AGENT_SCHEDULER_JOB":          sanitizeName(job),
		"AGENT_SCHEDULER_TRIGGER_KIND": triggerKind,
		"AGENT_SCHEDULER_RUN_DIR":      runDir,
	}
	if scheduledTime != "" {
		env["DECLAW_SCHEDULED_TIME"] = scheduledTime
		env["AGENT_SCHEDULER_SCHEDULED_TIME"] = scheduledTime
	}
	addPassthroughToolEnv(env)
	return env
}

func addPassthroughToolEnv(env map[string]string) {
	for _, key := range []string{
		"GOG_ACCOUNT",
		"GOG_KEYRING_PASSWORD",
		"GOG_ACCESS_TOKEN",
		"NOTION_TOKEN",
		"NOTION_API_KEY",
		"SSH_AUTH_SOCK",
	} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
}

func (m *Manager) runtimeEnvForJob(job JobRecord, triggerKind, runDir, scheduledTime string) map[string]string {
	env := runtimeEnv(job.Name, triggerKind, runDir, scheduledTime)
	if job.Config.Kind == "once" && !job.WorkspaceBootstrap {
		env["DECLAW_CODEX_STATELESS"] = "1"
	}
	return env
}

func (m *Manager) isDefaultOnceWorkspace(workspace string) bool {
	if strings.TrimSpace(workspace) == "" {
		return false
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	return filepath.Clean(abs) == filepath.Join(m.supportDir, "workspaces", "one-off")
}

func resolveRuntimePrompt(argPrompt string, promptFile string) (string, error) {
	if strings.TrimSpace(promptFile) != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if envPrompt := os.Getenv(promptEnvKey); envPrompt != "" {
		return envPrompt, nil
	}
	return argPrompt, nil
}

func mergeEnv(base []string, extra map[string]string) []string {
	values := map[string]string{}
	for _, item := range base {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	for key, value := range extra {
		values[key] = value
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func envValue(extra map[string]string, key string) string {
	if value := extra[key]; value != "" {
		return value
	}
	return os.Getenv(key)
}

func isSafeEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (m *Manager) codexLaunchCommand(prompt, workspace, jobName, ui string) []string {
	program, args := codexCommandForUI(prompt, workspace, ui)
	_ = jobName
	if len(args) > 0 {
		args = append([]string(nil), args...)
		args[len(args)-1] = "<prompt from file>"
	}
	return append([]string{program}, args...)
}

func upsertEnv(items []string, key, value string) []string {
	filtered := []string{}
	for _, item := range items {
		if strings.HasPrefix(item, key+"=") {
			continue
		}
		filtered = append(filtered, item)
	}
	return append(filtered, key+"="+value)
}

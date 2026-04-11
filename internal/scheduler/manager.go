package scheduler

import (
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
	"syscall"
	"time"

	"declaw/internal/cliargs"
	"declaw/internal/projects"
)

const (
	labelPrefix   = "com.declaw"
	promptEnvKey  = "DECLAW_SCHEDULE_PROMPT"
	launchctlPath = "/bin/launchctl"
	osascriptPath = "/usr/bin/osascript"
)

const scheduledCodexPromptIntro = "You are running as a scheduled Codex job."

const workspaceCodexPromptPrefix = "Use the current working directory as the workspace root.\nBefore doing the main task, read `AGENTS.md` from the workspace root if it exists and follow the workspace-local instructions and conventions there. Also pay attention to relevant workspace files before acting.\nBefore finishing this run, ensure `SESSIONS/` exists under the workspace root and save the whole conversation appending as you go with each message to a markdown file in `SESSIONS/` using a timestamped filename such as `YYYY-MM-DDTHH-MM-SS.md`."

const recurringCodexPromptPrefix = "Before starting the main task, inspect the existing session files and identify the most recent prior date that has session history. If there is not already a distilled memory markdown file for that date, ensure `MEMORY/` exists under the workspace root and create `MEMORY/YYYY-MM-DD.md` for that date with a distilled summary of that date's sessions."

type Manager struct {
	projects        *projects.Manager
	supportDir      string
	runsDir         string
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
	for _, path := range []string{supportDir, filepath.Join(supportDir, "runs"), launchAgentsDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	}

	return &Manager{
		projects:        projectsManager,
		supportDir:      supportDir,
		runsDir:         filepath.Join(supportDir, "runs"),
		jobsPath:        filepath.Join(supportDir, "jobs.json"),
		launchAgentsDir: launchAgentsDir,
		domain:          fmt.Sprintf("gui/%s", currentUser.Uid),
		executablePath:  executablePath,
	}, nil
}

func (m *Manager) Execute(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("usage: declaw schedule <subcommand>")
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
	case "reminder":
		return m.scheduleReminder(args[1:])
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
	if len(args) != 1 {
		return "", errors.New("usage: declaw schedule run <job>")
	}
	job, err := m.getJob(args[0])
	if err != nil {
		return "", err
	}
	label := job.PrimaryLabel
	if job.Config.Kind == "once" {
		label = job.OnceLabel
	}
	if _, err := m.runLaunchctl(true, "kickstart", "-k", fmt.Sprintf("%s/%s", m.domain, label)).Output(); err != nil {
		return "", err
	}
	return fmt.Sprintf("triggered %s", label), nil
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
	fs := flag.NewFlagSet("codex", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	prompt := fs.String("prompt", "", "prompt")
	projectName := fs.String("project", "", "tracked project")
	workspace := fs.String("workspace", "", "workspace path")
	noWorkspace := fs.Bool("no-workspace", false, "disable workspace bootstrap")
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
		return "", errors.New("usage: declaw schedule codex <job> --prompt <text> (--project <name> | --workspace <path> | --no-workspace) [schedule flags]")
	}
	if strings.TrimSpace(*prompt) == "" {
		return "", errors.New("--prompt is required")
	}

	resolvedWorkspace, workspaceBootstrap, err := m.resolveWorkspace(*projectName, *workspace, *noWorkspace)
	if err != nil {
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

	jobName := sanitizeName(rest[0])
	effectivePrompt := buildCodexPrompt(*prompt, config.Kind == "recurring", workspaceBootstrap)
	record := JobRecord{
		Name:               jobName,
		Type:               "codex",
		CreatedAt:          time.Now().UTC(),
		Config:             config,
		Prompt:             effectivePrompt,
		Workspace:          resolvedWorkspace,
		Cwd:                *cwd,
		Stdout:             *stdout,
		Stderr:             *stderr,
		Env:                upsertEnv([]string(env), promptEnvKey, effectivePrompt),
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

func (m *Manager) scheduleReminder(args []string) (string, error) {
	fs := flag.NewFlagSet("reminder", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	title := fs.String("title", "", "title")
	body := fs.String("body", "", "body")
	daily := fs.String("daily", "", "daily")
	timeValue := fs.String("time", "", "time")
	weekdays := fs.String("weekdays", "", "weekdays")
	weekly := fs.String("weekly", "", "weekly")
	at := fs.String("at", "", "one-off")
	once := fs.Bool("once", false, "once")
	year := fs.Int("year", -1, "year")
	month := fs.Int("month", -1, "month")
	day := fs.Int("day", -1, "day")
	hour := fs.Int("hour", -1, "hour")
	minute := fs.Int("minute", -1, "minute")
	cwd := fs.String("cwd", "", "cwd")
	stdout := fs.String("stdout", "", "stdout")
	stderr := fs.String("stderr", "", "stderr")
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
		return "", errors.New("usage: declaw schedule reminder <job> --title <text> --body <text> [schedule flags]")
	}
	if strings.TrimSpace(*title) == "" || strings.TrimSpace(*body) == "" {
		return "", errors.New("--title and --body are required")
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

	jobName := sanitizeName(rest[0])
	record := JobRecord{
		Name:          jobName,
		Type:          "reminder",
		CreatedAt:     time.Now().UTC(),
		Config:        config,
		Title:         *title,
		Body:          *body,
		Cwd:           *cwd,
		Stdout:        *stdout,
		Stderr:        *stderr,
		Env:           env,
		HasRecovery:   config.Kind == "recurring" && !*noRecurringFallback,
		PrimaryLabel:  primaryLabel(jobName),
		RecoveryLabel: recoveryLabel(jobName),
		OnceLabel:     onceLabel(jobName),
	}
	if config.Kind == "once" {
		record.HasRecovery = false
	}
	return m.installAndStore(record)
}

func (m *Manager) edit(args []string) (string, error) {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(bytes.NewBuffer(nil))

	prompt := fs.String("prompt", "", "prompt")
	projectName := fs.String("project", "", "project")
	workspace := fs.String("workspace", "", "workspace")
	noWorkspace := fs.Bool("no-workspace", false, "disable workspace")
	title := fs.String("title", "", "title")
	body := fs.String("body", "", "body")
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
		resolvedWorkspace := record.Workspace
		workspaceBootstrap := record.WorkspaceBootstrap
		if *projectName != "" || *workspace != "" || *noWorkspace {
			var err error
			resolvedWorkspace, workspaceBootstrap, err = m.resolveWorkspace(*projectName, *workspace, *noWorkspace)
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
			record.Env = upsertEnv(record.Env, promptEnvKey, record.Prompt)
		}
	case "reminder":
		if *title != "" {
			record.Title = *title
		}
		if *body != "" {
			record.Body = *body
		}
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

func (m *Manager) resolveWorkspace(projectName, workspace string, noWorkspace bool) (string, bool, error) {
	if noWorkspace && (strings.TrimSpace(projectName) != "" || strings.TrimSpace(workspace) != "") {
		return "", false, errors.New("--no-workspace cannot be combined with --project or --workspace")
	}
	if noWorkspace {
		return "", false, nil
	}
	if strings.TrimSpace(projectName) != "" && strings.TrimSpace(workspace) != "" {
		return "", false, errors.New("--project and --workspace cannot be combined")
	}
	if strings.TrimSpace(projectName) != "" {
		project, err := m.projects.Get(projectName)
		if err != nil {
			return "", false, err
		}
		return project.Path, true, nil
	}
	if strings.TrimSpace(workspace) != "" {
		abs, err := filepath.Abs(workspace)
		if err != nil {
			return "", false, err
		}
		return abs, true, nil
	}
	return "", false, errors.New("codex schedules require --project, --workspace, or --no-workspace")
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
		args = append(args, "--prompt", record.Prompt)
		if record.Workspace != "" {
			args = append(args, "--workspace", record.Workspace)
		}
	case "reminder":
		args = append(args, "--title", record.Title, "--body", record.Body)
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
	workspace := fs.String("workspace", "", "workspace")
	title := fs.String("title", "", "title")
	body := fs.String("body", "", "body")
	weekday := stringListFlag{}
	fs.Var(&weekday, "weekday", "weekday")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, internalRecurringValueFlags())); err != nil {
		return err
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
		command = m.codexLaunchCommand(*prompt, *workspace, *jobName)
	case "reminder":
		command = []string{osascriptPath, "-e", notificationScript(*title, *body)}
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

	exitCode := m.runRuntimeCommand(*jobType, *jobName, *prompt, *workspace, *title, *body, runtimeEnv(*jobName, *triggerKind, runDir, *scheduledTime))
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
	workspace := fs.String("workspace", "", "workspace")
	title := fs.String("title", "", "title")
	body := fs.String("body", "", "body")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, internalOnceValueFlags())); err != nil {
		return err
	}

	now := time.Now().In(time.Local)
	runDir := filepath.Join(m.runsDir, "once", sanitizeName(*jobName), now.Format("2006-01-02-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	command := []string{}
	switch *jobType {
	case "codex":
		command = m.codexLaunchCommand(*prompt, *workspace, *jobName)
	case "reminder":
		command = []string{osascriptPath, "-e", notificationScript(*title, *body)}
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

	exitCode := m.runRuntimeCommand(*jobType, *jobName, *prompt, *workspace, *title, *body, runtimeEnv(*jobName, "one-off", runDir, ""))
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
	deletePromptFile := fs.Bool("delete-prompt-file", false, "delete prompt file")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{
		"prompt-file": true,
		"workspace":   true,
		"job-name":    true,
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

	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("codex command not found in PATH")
	}

	if *workspace != "" {
		if err := os.Chdir(*workspace); err != nil {
			return err
		}
	}

	argsForExec := []string{"codex", "--no-alt-screen"}
	if *workspace != "" {
		argsForExec = append(argsForExec, "-C", *workspace)
	}
	argsForExec = append(argsForExec, string(promptBytes))

	if *jobName != "" {
		fmt.Printf("Launching scheduled Codex job: %s\n", *jobName)
	}
	if *workspace != "" {
		fmt.Printf("Workspace: %s\n\n", *workspace)
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return err
	}
	return syscall.Exec(path, argsForExec, os.Environ())
}

func (m *Manager) runRuntimeCommand(jobType, jobName, prompt, workspace, title, body string, env map[string]string) int {
	switch jobType {
	case "codex":
		if err := m.launchCodexInTerminal(prompt, workspace, jobName); err != nil {
			return 1
		}
		return 0
	case "reminder":
		cmd := exec.Command(osascriptPath, "-e", notificationScript(title, body))
		cmd.Env = mergeEnv(os.Environ(), env)
		if err := cmd.Run(); err != nil {
			return 1
		}
		return 0
	default:
		return 1
	}
}

func (m *Manager) launchCodexInTerminal(prompt, workspace, jobName string) error {
	tmpFile, err := os.CreateTemp("", "declaw-prompt.*.txt")
	if err != nil {
		return err
	}
	if _, err := tmpFile.WriteString(prompt); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	commandParts := []string{
		shellQuote(m.executablePath),
		"schedule",
		"__internal",
		"run-codex",
		"--prompt-file",
		shellQuote(tmpFile.Name()),
		"--delete-prompt-file",
	}
	if jobName != "" {
		commandParts = append(commandParts, "--job-name", shellQuote(jobName))
	}
	if workspace != "" {
		commandParts = append(commandParts, "--workspace", shellQuote(workspace))
	}
	terminalCommand := strings.Join(commandParts, " ")

	script := fmt.Sprintf(`tell application "Terminal"
  activate
  do script %q
end tell`, terminalCommand)
	return exec.Command(osascriptPath, "-e", script).Run()
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
	}
	if record.Stderr != "" {
		payload["StandardErrorPath"] = record.Stderr
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
	return strings.TrimSpace(r.Stdout + r.Stderr), r.Err
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
	_, _ = m.runLaunchctl(false, "disable", fmt.Sprintf("%s/%s", m.domain, label)).Output()
	if err := os.Remove(m.installedPlistPath(label)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
		"title":     true,
		"body":      true,
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
		"workspace":      true,
		"title":          true,
		"body":           true,
		"weekday":        true,
	}
}

func internalOnceValueFlags() map[string]bool {
	return map[string]bool{
		"job":           true,
		"cleanup-label": true,
		"type":          true,
		"prompt":        true,
		"workspace":     true,
		"title":         true,
		"body":          true,
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
	return env
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

func notificationScript(title, body string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return fmt.Sprintf(`display notification "%s" with title "%s"`, replacer.Replace(body), replacer.Replace(title))
}

func (m *Manager) codexLaunchCommand(prompt, workspace, jobName string) []string {
	_ = prompt
	args := []string{m.executablePath, "schedule", "__internal", "run-codex"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	if jobName != "" {
		args = append(args, "--job-name", jobName)
	}
	return args
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
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

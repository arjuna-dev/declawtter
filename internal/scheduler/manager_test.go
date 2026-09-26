package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"declaw/internal/activity"
	"declaw/internal/projects"
	"declaw/internal/settings"
)

func TestScheduleValueFlagsIncludesEditProviderFlags(t *testing.T) {
	flags := scheduleValueFlags()
	for _, name := range []string{"provider", "type", "agent-color"} {
		if !flags[name] {
			t.Fatalf("scheduleValueFlags()[%q] = false, want true", name)
		}
	}
}

func TestScheduledTerminalCommandDefaultsToTerminal(t *testing.T) {
	program, args, err := scheduledTerminalCommand("", "/tmp/run.command")
	if err != nil {
		t.Fatal(err)
	}
	if program != "/usr/bin/open" {
		t.Fatalf("program = %q, want /usr/bin/open", program)
	}
	want := []string{"-a", "Terminal", "/tmp/run.command"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestScheduledTerminalCommandUsesGhostty(t *testing.T) {
	program, args, err := scheduledTerminalCommand("ghostty", "/tmp/run.command")
	if err != nil {
		t.Fatal(err)
	}
	if program != "/usr/bin/open" {
		t.Fatalf("program = %q, want /usr/bin/open", program)
	}
	want := []string{"-na", "Ghostty", "--args", "-e", "/bin/zsh", "/tmp/run.command"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestScheduledTerminalCommandReadsGlobalSetting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	settingsManager, err := settings.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := settingsManager.SetDefaultTerminal("ghostty"); err != nil {
		t.Fatal(err)
	}

	manager := &Manager{settings: settingsManager}
	terminal, program, args, err := manager.scheduledTerminalCommand("/tmp/run.command")
	if err != nil {
		t.Fatal(err)
	}
	if terminal != "ghostty" {
		t.Fatalf("terminal = %q, want ghostty", terminal)
	}
	if program != "/usr/bin/open" {
		t.Fatalf("program = %q, want /usr/bin/open", program)
	}
	if len(args) < 2 || args[1] != "Ghostty" {
		t.Fatalf("args = %#v, want Ghostty app", args)
	}
}

func TestDeclawAgentColorValidation(t *testing.T) {
	for _, value := range []string{"green", "cyan", " Magenta "} {
		if err := validateDeclawAgentColor(value); err != nil {
			t.Fatalf("validateDeclawAgentColor(%q) returned error: %v", value, err)
		}
	}
	if err := validateDeclawAgentColor("orange"); err == nil {
		t.Fatal("validateDeclawAgentColor(\"orange\") returned nil, want error")
	}
}

func TestValidateAgentColorFlagRequiresDeclawUI(t *testing.T) {
	if err := validateAgentColorFlag("declaw", "cyan"); err != nil {
		t.Fatalf("validateAgentColorFlag returned error: %v", err)
	}
	err := validateAgentColorFlag("codex", "cyan")
	if err == nil {
		t.Fatal("validateAgentColorFlag returned nil, want error")
	}
	if got := err.Error(); got != "--agent-color only applies to --ui declaw" {
		t.Fatalf("validateAgentColorFlag error = %q", got)
	}
}

func TestScheduleSupportsAllHarnesses(t *testing.T) {
	for _, harness := range []string{"pi", "hermes", "codex", "claude"} {
		if err := validateProvider(harness); err != nil {
			t.Fatalf("validateProvider(%q) returned error: %v", harness, err)
		}
	}
	if err := validateCreateProvider("default"); err != nil {
		t.Fatalf("validateCreateProvider(default) returned error: %v", err)
	}
	if !scheduleValueFlags()["harness"] {
		t.Fatal("schedule flags do not recognize --harness")
	}
}

func TestNativeScheduledPromptDoesNotRequireMemoryFiles(t *testing.T) {
	prompt := buildNativePrompt(activity.Hermes, "review the repo", true, true)
	if strings.Contains(prompt, "MEMORY/") || strings.Contains(prompt, "SESSIONS/") {
		t.Fatalf("native scheduled prompt asks for generated workspace files: %q", prompt)
	}
}

func TestDeclawColorsHelpListsDefault(t *testing.T) {
	help := declawColorsHelp()
	if !strings.Contains(help, "green (default)") {
		t.Fatalf("declawColorsHelp() = %q, want green default", help)
	}
	if !strings.Contains(help, "--agent-color <name>") {
		t.Fatalf("declawColorsHelp() = %q, want usage example", help)
	}
}

func TestJobPayloadArgsIncludeAgentColor(t *testing.T) {
	manager := &Manager{}
	record := JobRecord{
		Name:       "demo",
		Type:       "codex",
		UI:         "declaw",
		AgentColor: "cyan",
	}
	args := manager.jobPayloadArgs(record)
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--agent-color cyan") {
		t.Fatalf("jobPayloadArgs() = %q, want agent color flag", got)
	}
}

func TestProviderValidation(t *testing.T) {
	for _, value := range []string{"codex", "claude", " Codex "} {
		if err := validateProvider(value); err != nil {
			t.Fatalf("validateProvider(%q) returned error: %v", value, err)
		}
	}
	if err := validateProvider("app-server"); err == nil {
		t.Fatal("validateProvider(\"app-server\") returned nil, want error")
	}
}

func TestClaudeUIValidationIncludesDeclaw(t *testing.T) {
	for _, value := range []string{"claude", "declaw", "print", " Declaw "} {
		if err := validateClaudeUI(value); err != nil {
			t.Fatalf("validateClaudeUI(%q) returned error: %v", value, err)
		}
	}
	if err := validateClaudeUI("app-server"); err == nil {
		t.Fatal("validateClaudeUI(\"app-server\") returned nil, want error")
	} else if got := err.Error(); got != `invalid --ui "app-server" for Claude schedules; "app-server" belongs to `+"`declaw schedule codex`"+`. Valid Claude values: claude, declaw, print. See `+"`declaw schedule claude -h`" {
		t.Fatalf("validateClaudeUI error = %q", got)
	}
}

func TestCodexUIValidationErrorHint(t *testing.T) {
	err := validateCodexUI("claude")
	if err == nil {
		t.Fatal("validateCodexUI(\"claude\") returned nil, want error")
	}
	want := `invalid --ui "claude" for Codex schedules; "claude" belongs to ` + "`declaw schedule claude`" + `. Valid Codex values: app-server, declaw, codex. See ` + "`declaw schedule codex -h`"
	if err.Error() != want {
		t.Fatalf("validateCodexUI error = %q, want %q", err.Error(), want)
	}
}

func TestParseClaudeStreamJSONLineResult(t *testing.T) {
	sessionID, message := parseClaudeStreamJSONLine([]byte(`{"type":"result","session_id":"abc-123","result":"Done"}`))
	if sessionID != "abc-123" {
		t.Fatalf("sessionID = %q, want abc-123", sessionID)
	}
	if message != "Done" {
		t.Fatalf("message = %q, want Done", message)
	}
}

func TestParseClaudeStreamJSONLineAssistantContent(t *testing.T) {
	line := []byte(`{"type":"assistant","session_id":"abc-123","message":{"content":[{"type":"text","text":"First"},{"type":"tool_use","name":"Read"},{"type":"text","text":"Second"}]}}`)
	sessionID, message := parseClaudeStreamJSONLine(line)
	if sessionID != "abc-123" {
		t.Fatalf("sessionID = %q, want abc-123", sessionID)
	}
	if message != "First\n\nSecond" {
		t.Fatalf("message = %q, want joined text", message)
	}
}

func TestClaudeCommandForDeclawUI(t *testing.T) {
	program, args := claudeCommandForUI("hello", "/tmp/workspace", "declaw")
	if program != "claude" {
		t.Fatalf("program = %q, want claude", program)
	}
	want := []string{"-p", "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "--output-format", "stream-json", "hello"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("args[%d] = %q, want %q", idx, args[idx], want[idx])
		}
	}
}

func TestCodexExecArgsExcludeInteractiveApprovalFlags(t *testing.T) {
	args := append([]string{"exec"}, codexExecArgs("default")...)
	got := strings.Join(args, " ")
	if strings.Contains(got, "--ask-for-approval") {
		t.Fatalf("codex exec args = %q, want no interactive approval flag", got)
	}
	if strings.Contains(got, "--sandbox") {
		t.Fatalf("codex exec args = %q, want no top-level sandbox flag", got)
	}
}

func TestCodexExecArgsNoReasoningUsesConfigOverride(t *testing.T) {
	args := codexExecArgs("no_reasoning")
	got := strings.Join(args, " ")
	if strings.Contains(got, "-p no_reasoning") {
		t.Fatalf("codexExecArgs(no_reasoning) = %q, want config override instead of profile flag", got)
	}
	if !strings.Contains(got, `model_reasoning_effort="none"`) {
		t.Fatalf("codexExecArgs(no_reasoning) = %q, want model_reasoning_effort override", got)
	}
}

func TestRawUIPromptsIncludeSessionTranscriptInstruction(t *testing.T) {
	codexPrompt := buildCodexPrompt("do work", true, true, "codex")
	if !strings.Contains(codexPrompt, rawUISessionTranscriptInstruction) {
		t.Fatalf("Codex raw UI prompt did not include transcript instruction: %q", codexPrompt)
	}

	claudePrompt := buildClaudePrompt("do work", true, true, "claude")
	if !strings.Contains(claudePrompt, rawUISessionTranscriptInstruction) {
		t.Fatalf("Claude raw UI prompt did not include transcript instruction: %q", claudePrompt)
	}
}

func TestRawUIScheduledRunOnlyAppliesToNativeTerminals(t *testing.T) {
	env := map[string]string{"DECLAW_RUN_DIR": t.TempDir()}
	for _, test := range []struct {
		jobType string
		ui      string
		want    bool
	}{
		{jobType: "codex", ui: "codex", want: true},
		{jobType: "claude", ui: "claude", want: true},
		{jobType: "codex", ui: "app-server", want: false},
		{jobType: "claude", ui: "print", want: false},
	} {
		if got := rawUIScheduledRun(test.jobType, test.ui, env); got != test.want {
			t.Errorf("rawUIScheduledRun(%q, %q) = %t, want %t", test.jobType, test.ui, got, test.want)
		}
	}
	if rawUIScheduledRun("codex", "codex", nil) {
		t.Fatal("rawUIScheduledRun without a scheduled run directory = true, want false")
	}
}

func TestScheduleReadyWritesReadinessMarker(t *testing.T) {
	runDir := t.TempDir()
	t.Setenv("DECLAW_RUN_DIR", runDir)
	t.Setenv("DECLAW_SCHEDULE_JOB", "Daily Review")
	t.Setenv("DECLAW_TRIGGER_KIND", "scheduled")

	got, err := (&Manager{}).ready(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "scheduled agent readiness recorded" {
		t.Fatalf("ready() = %q", got)
	}
	if !runReady(runDir) {
		t.Fatal("readiness marker was not written")
	}
	data, err := os.ReadFile(readinessPath(runDir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"job": "daily-review"`) {
		t.Fatalf("readiness marker = %s, want sanitized job name", data)
	}
}

func TestReadinessCommandUsesHeadlessProviderCommands(t *testing.T) {
	program, args := readinessCommand("codex", "probe", "/tmp/workspace")
	if program != "codex" || !strings.Contains(strings.Join(args, " "), "exec --json --skip-git-repo-check -C /tmp/workspace probe") {
		t.Fatalf("Codex readiness command = %s %q", program, args)
	}
	program, args = readinessCommand("claude", "probe", "/tmp/workspace")
	if program != "claude" || !strings.HasPrefix(strings.Join(args, " "), "-p --dangerously-skip-permissions") {
		t.Fatalf("Claude readiness command = %s %q", program, args)
	}
}

func TestValidateResumeJobAllowsRecurringAndFutureOneOff(t *testing.T) {
	recurring := JobRecord{Config: ScheduleConfig{Kind: "recurring"}}
	if err := validateResumeJob(recurring); err != nil {
		t.Fatalf("validateResumeJob(recurring) returned error: %v", err)
	}

	future := time.Now().In(time.Local).Add(2 * time.Hour)
	oneOff := JobRecord{Config: ScheduleConfig{
		Kind:          "once",
		Year:          future.Year(),
		Month:         int(future.Month()),
		Day:           future.Day(),
		Hour:          future.Hour(),
		Minute:        future.Minute(),
		ScheduledTime: future.Format("2006-01-02 15:04"),
	}}
	if err := validateResumeJob(oneOff); err != nil {
		t.Fatalf("validateResumeJob(future one-off) returned error: %v", err)
	}
}

func TestValidateResumeJobRejectsExpiredOneOff(t *testing.T) {
	past := time.Now().In(time.Local).Add(-2 * time.Hour)
	job := JobRecord{Config: ScheduleConfig{
		Kind:          "once",
		Year:          past.Year(),
		Month:         int(past.Month()),
		Day:           past.Day(),
		Hour:          past.Hour(),
		Minute:        past.Minute(),
		ScheduledTime: past.Format("2006-01-02 15:04"),
	}}
	err := validateResumeJob(job)
	if err == nil {
		t.Fatal("validateResumeJob(expired one-off) returned nil, want error")
	}
	if got := err.Error(); got != "cannot resume one-off job after its scheduled time; edit --at or create a new schedule" {
		t.Fatalf("validateResumeJob error = %q", got)
	}
}

func TestNonRawUIPromptsDoNotIncludeSessionTranscriptInstruction(t *testing.T) {
	for name, prompt := range map[string]string{
		"codex declaw":  buildCodexPrompt("do work", true, true, "declaw"),
		"codex app":     buildCodexPrompt("do work", true, true, "app-server"),
		"claude declaw": buildClaudePrompt("do work", true, true, "declaw"),
		"claude print":  buildClaudePrompt("do work", true, true, "print"),
	} {
		if strings.Contains(prompt, rawUISessionTranscriptInstruction) {
			t.Fatalf("%s prompt included transcript instruction: %q", name, prompt)
		}
	}
}

func TestResolveCreateProviderPrefersProjectOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectManager, err := projects.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	settingsManager, err := settings.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := settingsManager.SetDefaultProvider("codex"); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projectManager.Track([]string{"demo", "--path", projectDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := projectManager.Settings([]string{"demo", "provider", "claude"}); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(projectManager, settingsManager)
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.resolveCreateProvider("default", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "claude" {
		t.Fatalf("resolveCreateProvider(default, demo) = %q, want claude", got)
	}
}

func TestResolveCreateProviderFallsBackToGlobalDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectManager, err := projects.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	settingsManager, err := settings.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := settingsManager.SetDefaultProvider("claude"); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(projectManager, settingsManager)
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.resolveCreateProvider("default", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "claude" {
		t.Fatalf("resolveCreateProvider(default, \"\") = %q, want claude", got)
	}
}

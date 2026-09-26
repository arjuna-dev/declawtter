package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"declaw/internal/paths"
	"declaw/internal/scheduler"
)

type cliHarness struct {
	home         string
	agentLogPath string
}

func newCLIHarness(t *testing.T) *cliHarness {
	t.Helper()

	home := t.TempDir()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	agentLogPath := filepath.Join(t.TempDir(), "agent.log")
	script := "#!/bin/sh\n" +
		"printf '%s|%s|%s\\n' \"$(basename \"$0\")\" \"$(pwd)\" \"$*\" >> \"$DECLAW_FAKE_AGENT_LOG\"\n" +
		"printf 'fake-%s response\\n' \"$(basename \"$0\")\"\n"
	for _, name := range []string{"codex", "claude"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DECLAW_FAKE_AGENT_LOG", agentLogPath)

	return &cliHarness{
		home:         home,
		agentLogPath: agentLogPath,
	}
}

func (h *cliHarness) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	application, err := New()
	if err != nil {
		t.Fatal(err)
	}

	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutR.Close()
	defer stderrR.Close()

	os.Stdout = stdoutW
	os.Stderr = stderrW
	code := application.Run(args)
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := io.Copy(&stdout, stdoutR); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&stderr, stderrR); err != nil {
		t.Fatal(err)
	}
	return code, stdout.String(), stderr.String()
}

func (h *cliHarness) supportDir(t *testing.T) string {
	t.Helper()
	dir, err := paths.SupportDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func (h *cliHarness) writeJobs(t *testing.T, jobs map[string]scheduler.JobRecord) {
	t.Helper()

	supportDir := h.supportDir(t)
	store := scheduler.JobStore{Jobs: jobs}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(supportDir, "jobs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *cliHarness) readAgentLog(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(h.agentLogPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func normalizeTestPath(value string) string {
	value = filepath.Clean(value)
	return strings.ReplaceAll(value, "/private/var/", "/var/")
}

func TestLocalCLIRegressionSuite(t *testing.T) {
	h := newCLIHarness(t)

	projectsRoot := filepath.Join(t.TempDir(), "projects")
	linkedDir := filepath.Join(t.TempDir(), "linked-project")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	assert := func(args []string, wantCode int, stdoutContains ...string) string {
		t.Helper()
		code, stdout, stderr := h.run(t, args...)
		if code != wantCode {
			t.Fatalf("%v exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", args, code, wantCode, stdout, stderr)
		}
		if stderr != "" {
			t.Fatalf("%v stderr = %q, want empty", args, stderr)
		}
		for _, fragment := range stdoutContains {
			if !strings.Contains(stdout, fragment) {
				t.Fatalf("%v stdout = %q, want substring %q", args, stdout, fragment)
			}
		}
		return stdout
	}

	assert([]string{"help"}, 0, "Commands:", "schedule create <job>", "settings terminal [terminal|ghostty]")
	assert([]string{"settings"}, 0, "default provider: codex", "default codex ui: codex", "default claude ui: claude", "default terminal: terminal")
	assert([]string{"settings", "codex-ui", "codex"}, 0, "default codex ui: codex")
	assert([]string{"settings", "claude-ui", "claude"}, 0, "default claude ui: claude")
	assert([]string{"settings", "terminal", "ghostty"}, 0, "default terminal: ghostty")
	assert([]string{"settings", "terminal"}, 0, "ghostty")
	assert([]string{"settings", "terminal", "terminal"}, 0, "default terminal: terminal")
	assert([]string{"settings", "codex-reasoning", "no_reasoning"}, 0, "codex reasoning mode: no_reasoning")
	assert([]string{"settings", "provider", "claude"}, 0, "default provider: claude")
	assert([]string{"config", "provider"}, 0, "claude")

	created := assert([]string{"create", "demo", "--into", projectsRoot}, 0, "created demo")
	createdPath := strings.TrimSpace(strings.Split(created, "\n")[1])
	assert([]string{"list"}, 0, "demo\t"+createdPath)
	assert([]string{"path", "demo"}, 0, createdPath)
	assert([]string{"project", "settings", "demo", "provider"}, 0, "inherit")
	assert([]string{"project-settings", "demo", "provider", "codex"}, 0, "project demo provider: codex")
	assert([]string{"project-settings", "demo", "pin", "on"}, 0, "project demo pinned: yes")
	assert([]string{"project-settings", "demo", "ignore", "on"}, 0, "project demo ignored: yes")
	assert([]string{"checkout"}, 0, "no available projects")
	assert([]string{"project-settings", "demo", "ignore", "off"}, 0, "project demo ignored: no")
	assert([]string{"project", "settings", "demo", "codex-ui", "codex"}, 0, "project demo codex-ui: codex")
	assert([]string{"project", "settings", "demo", "claude-ui", "claude"}, 0, "project demo claude-ui: claude")
	assert([]string{"track", "linked", "--path", linkedDir}, 0, "tracked linked")
	assert([]string{"schedule"}, 0, "declaw schedule", "Manage native macOS launchd schedules", "schedule pause <job>", "schedule resume <job>")
	assert([]string{"schedule", "codex", "-h"}, 0, "Create a scheduled Codex run")
	assert([]string{"schedule", "claude", "-h"}, 0, "Create a scheduled Claude Code run")
	assert([]string{"schedule", "create", "-h"}, 0, "Create a scheduled agent run")
	assert([]string{"schedule", "edit", "-h"}, 0, "Edit an existing schedule")
	assert([]string{"schedule", "list"}, 0, "no jobs installed")
	assert([]string{"schedule", "colors"}, 0, "green (default)")

	assert([]string{"ai-agent", "Write a smoke test response."}, 0, "fake-claude response")
	agentWorkspace, err := paths.AgentWorkspaceDir()
	if err != nil {
		t.Fatal(err)
	}
	log := h.readAgentLog(t)
	if !strings.Contains(normalizeTestPath(log), "claude|"+normalizeTestPath(agentWorkspace)+"|--dangerously-skip-permissions --permission-mode bypassPermissions Write a smoke test response.") {
		t.Fatalf("agent log = %q, want claude ai-agent invocation in %s", log, agentWorkspace)
	}

	assert([]string{"checkout", "demo"}, 0, "fake-codex response")
	log = h.readAgentLog(t)
	if !strings.Contains(normalizeTestPath(log), "codex|"+normalizeTestPath(createdPath)+"|--sandbox danger-full-access --ask-for-approval never -m gpt-5.5 -p no_reasoning --disable image_generation") {
		t.Fatalf("agent log = %q, want codex checkout invocation in %s", log, createdPath)
	}

	assert([]string{"remove", "--yes", "linked"}, 0, "untracked linked")
	if _, err := os.Stat(linkedDir); err != nil {
		t.Fatalf("linked project dir removed unexpectedly: %v", err)
	}
	assert([]string{"remove", "--yes", "demo"}, 0, "removed demo")
	if _, err := os.Stat(createdPath); !os.IsNotExist(err) {
		t.Fatalf("created project dir still exists after remove: %v", err)
	}
	assert([]string{"list"}, 0, "no tracked projects")
}

func TestSyntheticScheduleCommandRegressionSuite(t *testing.T) {
	h := newCLIHarness(t)

	h.writeJobs(t, map[string]scheduler.JobRecord{
		"daily-review": {
			Name:         "daily-review",
			Type:         "codex",
			Prompt:       "Review the repo.",
			PrimaryLabel: "com.declaw.daily-review",
			Config: scheduler.ScheduleConfig{
				Kind:          "recurring",
				Hour:          9,
				Minute:        30,
				ScheduledTime: "09:30",
			},
		},
		"one-shot": {
			Name:      "one-shot",
			Type:      "claude",
			Prompt:    "Reply once.",
			OnceLabel: "com.declaw.one-shot",
			Config: scheduler.ScheduleConfig{
				Kind:          "once",
				Month:         5,
				Day:           4,
				Hour:          8,
				Minute:        15,
				ScheduledTime: "2026-05-04 08:15",
			},
		},
	})

	assert := func(args []string, wantCode int, stdoutContains ...string) string {
		t.Helper()
		code, stdout, stderr := h.run(t, args...)
		if code != wantCode {
			t.Fatalf("%v exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", args, code, wantCode, stdout, stderr)
		}
		if stderr != "" {
			t.Fatalf("%v stderr = %q, want empty", args, stderr)
		}
		for _, fragment := range stdoutContains {
			if !strings.Contains(stdout, fragment) {
				t.Fatalf("%v stdout = %q, want substring %q", args, stdout, fragment)
			}
		}
		return stdout
	}

	assert([]string{"schedule", "list"}, 0, "daily-review\tcodex\trecurring\tcom.declaw.daily-review", "one-shot\tclaude\tonce\tcom.declaw.one-shot")
	assert([]string{"schedule", "get-prompt", "daily-review"}, 0, "Review the repo.")
	assert([]string{"schedule", "get-time", "daily-review"}, 0, "daily 09:30")
	assert([]string{"schedule", "get-time", "one-shot"}, 0, "once 05-04 08:15")
	assert([]string{"schedule", "prune-once"}, 0, "pruned one-shot")
	assert([]string{"schedule", "remove", "daily-review"}, 0, "removed com.declaw.daily-review")
	assert([]string{"schedule", "remove-all"}, 0, "no jobs installed")

	h.writeJobs(t, map[string]scheduler.JobRecord{
		"daily-review": {
			Name:         "daily-review",
			Type:         "codex",
			Prompt:       "Review the repo.",
			PrimaryLabel: "com.declaw.daily-review",
			Config: scheduler.ScheduleConfig{
				Kind:          "recurring",
				Hour:          9,
				Minute:        30,
				ScheduledTime: "09:30",
			},
		},
		"weekly-review": {
			Name:         "weekly-review",
			Type:         "claude",
			Prompt:       "Weekly review.",
			PrimaryLabel: "com.declaw.weekly-review",
			Config: scheduler.ScheduleConfig{
				Kind:          "recurring",
				Hour:          10,
				Minute:        0,
				ScheduledTime: "10:00",
				Weekdays:      []int{1},
			},
		},
	})
	assert([]string{"schedule", "remove-all"}, 0, "removed com.declaw.daily-review", "removed com.declaw.weekly-review")
}

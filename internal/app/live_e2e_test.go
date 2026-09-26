package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLiveAIAgentResponseE2E(t *testing.T) {
	if os.Getenv("DECLAW_RUN_LIVE_E2E") == "" {
		t.Skip("set DECLAW_RUN_LIVE_E2E=1 to run live provider tests")
	}

	provider := strings.TrimSpace(strings.ToLower(os.Getenv("DECLAW_LIVE_PROVIDER")))
	if provider == "" {
		provider = "codex"
	}

	ui := map[string]string{
		"codex":  "codex",
		"claude": "claude",
	}[provider]
	if ui == "" {
		t.Fatalf("DECLAW_LIVE_PROVIDER must be codex or claude, got %q", provider)
	}
	if _, err := exec.LookPath(provider); err != nil {
		t.Skipf("%s not found in PATH: %v", provider, err)
	}

	bin := buildDeclawBinary(t)
	home := t.TempDir()
	token := "DECLAW-LIVE-AI-AGENT-OK"

	runDeclaw(t, home, bin, "settings", "provider", provider)
	if provider == "codex" {
		runDeclaw(t, home, bin, "settings", "codex-ui", ui)
	} else {
		runDeclaw(t, home, bin, "settings", "claude-ui", ui)
	}

	stdout, stderr := runDeclaw(t, home, bin, "ai-agent", "Reply with exactly "+token)
	combined := stdout + "\n" + stderr
	if !strings.Contains(combined, token) {
		t.Fatalf("live ai-agent output did not contain %q\nstdout:\n%s\nstderr:\n%s", token, stdout, stderr)
	}
}

func TestLiveClaudeSchedulePrintE2E(t *testing.T) {
	if os.Getenv("DECLAW_RUN_LIVE_SCHEDULE_E2E") == "" {
		t.Skip("set DECLAW_RUN_LIVE_SCHEDULE_E2E=1 to run the live schedule test")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("launchd schedule test only runs on macOS")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude not found in PATH: %v", err)
	}

	bin := buildDeclawBinary(t)
	home := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# live schedule test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobName := "live-schedule-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	token := "DECLAW-LIVE-SCHEDULE-OK"
	stdoutLog := filepath.Join(t.TempDir(), "schedule.stdout.log")
	stderrLog := filepath.Join(t.TempDir(), "schedule.stderr.log")
	scheduledAt := time.Now().Add(2 * time.Minute).In(time.Local).Format("2006-01-02 15:04")

	runDeclaw(t, home, bin, "schedule", "claude", jobName,
		"--workspace", workspace,
		"--ui", "print",
		"--at", scheduledAt,
		"--prompt", "Reply with exactly "+token+" and run declaw schedule complete --summary "+token+" exactly once before your final answer.",
		"--stdout", stdoutLog,
		"--stderr", stderrLog,
		"--env", "HOME="+home,
		"--env", "PATH="+os.Getenv("PATH"),
	)
	defer func() {
		_, _, _ = runDeclawNoFail(home, bin, "schedule", "remove", jobName)
	}()

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(stdoutLog); err == nil && strings.Contains(string(raw), token) {
			return
		}
		time.Sleep(5 * time.Second)
	}

	stdout, _ := os.ReadFile(stdoutLog)
	stderr, _ := os.ReadFile(stderrLog)
	t.Fatalf("scheduled run did not produce token %q before timeout\nstdout:\n%s\nstderr:\n%s", token, stdout, stderr)
}

func buildDeclawBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "declaw")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/declaw")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, output)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func runDeclaw(t *testing.T, home, bin string, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, err := runDeclawNoFail(home, bin, args...)
	if err != nil {
		t.Fatalf("%s %s failed: %v\nstdout:\n%s\nstderr:\n%s", bin, strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout, stderr
}

func runDeclawNoFail(home, bin string, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

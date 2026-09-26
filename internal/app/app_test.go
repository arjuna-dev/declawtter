package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInteractiveFreeTextWithQuoteBypassesCommandParser(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(t.TempDir(), "prompt.txt")
	fakeCodex := filepath.Join(binDir, "codex")
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\nfor arg in \"$@\"; do last=\"$arg\"; done\nprintf '%s\\n' \"$last\" > \"$DECLAW_FAKE_CODEX_PROMPT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DECLAW_FAKE_CODEX_PROMPT", capturePath)
	t.Setenv("DECLAW_LAUNCHER_INPUT", "Tell the agent it's okay")

	settingsPath := filepath.Join(home, "Library", "Application Support", "declaw", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"default_provider\":\"codex\",\"codex_reasoning_mode\":\"default\",\"default_codex_ui\":\"codex\",\"default_claude_ui\":\"claude\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	application, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if code := application.Run(nil); code != 0 {
		t.Fatalf("Run(nil) exit code = %d, want 0", code)
	}

	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), "Tell the agent it's okay\n"; got != want {
		t.Fatalf("captured prompt = %q, want %q", got, want)
	}
}

func TestAgentCommandForClaude(t *testing.T) {
	program, args, err := agentCommand("claude", "hello", "default")
	if err != nil {
		t.Fatal(err)
	}
	if program != "claude" {
		t.Fatalf("program = %q, want claude", program)
	}
	want := []string{"--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "hello"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("args[%d] = %q, want %q", idx, args[idx], want[idx])
		}
	}
}

func TestAgentCommandForCodexUsesFullPermissions(t *testing.T) {
	program, args, err := agentCommand("codex", "hello", "default")
	if err != nil {
		t.Fatal(err)
	}
	if program != "codex" {
		t.Fatalf("program = %q, want codex", program)
	}
	want := []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never", "hello"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("args[%d] = %q, want %q", idx, args[idx], want[idx])
		}
	}
}

func TestAgentCommandForCodexNoReasoning(t *testing.T) {
	program, args, err := agentCommand("codex", "hello", "no_reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if program != "codex" {
		t.Fatalf("program = %q, want codex", program)
	}
	want := []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never", "-m", "gpt-5.5", "-p", "no_reasoning", "--disable", "image_generation", "hello"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("args[%d] = %q, want %q", idx, args[idx], want[idx])
		}
	}
}

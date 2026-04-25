package scheduler

import "testing"

func TestScheduleValueFlagsIncludesEditProviderFlags(t *testing.T) {
	flags := scheduleValueFlags()
	for _, name := range []string{"provider", "type"} {
		if !flags[name] {
			t.Fatalf("scheduleValueFlags()[%q] = false, want true", name)
		}
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
	want := []string{"-p", "--dangerously-skip-permissions", "--output-format", "stream-json", "hello"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("args[%d] = %q, want %q", idx, args[idx], want[idx])
		}
	}
}

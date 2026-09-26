package settings

import "testing"

func TestDefaultProviderDefaultsToCodex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := manager.DefaultProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != "codex" {
		t.Fatalf("provider = %q, want codex", provider)
	}
}

func TestSetDefaultProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetDefaultProvider(" Claude "); err != nil {
		t.Fatal(err)
	}
	provider, err := manager.DefaultProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != "claude" {
		t.Fatalf("provider = %q, want claude", provider)
	}
}

func TestCodexReasoningModeDefaultsToDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	mode, err := manager.CodexReasoningMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != "default" {
		t.Fatalf("mode = %q, want default", mode)
	}
}

func TestSetCodexReasoningModeNormalizesAliases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetCodexReasoningMode(" no-thinking "); err != nil {
		t.Fatal(err)
	}
	mode, err := manager.CodexReasoningMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != "no_reasoning" {
		t.Fatalf("mode = %q, want no_reasoning", mode)
	}
}

func TestDefaultInteractiveUIDefaultsToProviderNativeModes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	codexUI, err := manager.DefaultCodexUI()
	if err != nil {
		t.Fatal(err)
	}
	claudeUI, err := manager.DefaultClaudeUI()
	if err != nil {
		t.Fatal(err)
	}
	if codexUI != "codex" {
		t.Fatalf("codexUI = %q, want codex", codexUI)
	}
	if claudeUI != "claude" {
		t.Fatalf("claudeUI = %q, want claude", claudeUI)
	}
}

func TestSetDefaultInteractiveUI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetDefaultCodexUI(" app-server "); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetDefaultClaudeUI(" print "); err != nil {
		t.Fatal(err)
	}
	codexUI, err := manager.DefaultCodexUI()
	if err != nil {
		t.Fatal(err)
	}
	claudeUI, err := manager.DefaultClaudeUI()
	if err != nil {
		t.Fatal(err)
	}
	if codexUI != "app-server" {
		t.Fatalf("codexUI = %q, want app-server", codexUI)
	}
	if claudeUI != "print" {
		t.Fatalf("claudeUI = %q, want print", claudeUI)
	}
}

func TestDefaultTerminalDefaultsToTerminal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := manager.DefaultTerminal()
	if err != nil {
		t.Fatal(err)
	}
	if terminal != "terminal" {
		t.Fatalf("terminal = %q, want terminal", terminal)
	}
}

func TestSetDefaultTerminalNormalizesGhostty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetDefaultTerminal(" Ghostty "); err != nil {
		t.Fatal(err)
	}
	terminal, err := manager.DefaultTerminal()
	if err != nil {
		t.Fatal(err)
	}
	if terminal != "ghostty" {
		t.Fatalf("terminal = %q, want ghostty", terminal)
	}
}

func TestSetDefaultTerminalRejectsUnknownTerminal(t *testing.T) {
	if err := ValidateTerminal("iterm"); err == nil {
		t.Fatal("ValidateTerminal(\"iterm\") returned nil, want error")
	}
}

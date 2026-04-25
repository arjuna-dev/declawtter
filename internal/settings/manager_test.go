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

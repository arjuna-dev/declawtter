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

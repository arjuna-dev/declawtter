package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectorNormalizesAndDeduplicatesRecords(t *testing.T) {
	projectDir := t.TempDir()
	first := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	latest := first.Add(time.Hour)
	detector := NewDetector(
		SourceFunc{
			SourceName: "older",
			DiscoverFn: func() ([]Record, error) {
				return []Record{{Path: projectDir, Harness: Codex, LastUsedAt: first, Source: "older"}}, nil
			},
		},
		SourceFunc{
			SourceName: "newer",
			DiscoverFn: func() ([]Record, error) {
				return []Record{
					{Path: projectDir, Harness: Codex, LastUsedAt: latest, Source: "newer"},
					{Path: projectDir, Harness: Hermes, LastUsedAt: first, Source: "newer"},
					{Path: filepath.Join(projectDir, "missing"), Harness: Pi, LastUsedAt: latest, Source: "ignored"},
				}, nil
			},
		},
	)

	records, err := detector.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v, want one record per harness", records)
	}
	if records[0].Harness != Codex || !records[0].LastUsedAt.Equal(latest) {
		t.Fatalf("latest record = %#v, want Codex at %s", records[0], latest)
	}
	if records[1].Harness != Hermes {
		t.Fatalf("second record = %#v, want Hermes", records[1])
	}
}

func TestDefaultDetectorFindsPiSessionFromCustomHome(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(t.TempDir(), "pi-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(home, ".pi", "agent", "sessions", "project")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDir, "session-2026-09-17.jsonl")
	content := `{"type":"session","id":"pi-session","timestamp":"2026-09-17T10:00:00Z","cwd":"` + projectDir + `"}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("DECLAW_ACTIVITY_PATHS", "")
	t.Setenv("DECLAW_PI_ACTIVITY_PATHS", "")

	records, err := DefaultDetector().Discover()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Harness == Pi && record.Path == canonicalProjectDir && record.SessionID == "pi-session" {
			return
		}
	}
	t.Fatalf("Pi session not found in records: %#v", records)
}

func TestKnownDataLocationsHonorHarnessSpecificOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "pi-data")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DECLAW_ACTIVITY_PATHS", "")
	t.Setenv("DECLAW_PI_ACTIVITY_PATHS", custom)
	locations := KnownDataLocations("pi")
	for _, location := range locations {
		if location == custom {
			return
		}
	}
	t.Fatalf("Pi locations = %#v, want %q", locations, custom)
}

func TestDetectorIgnoresAgentInternalCodexPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	internalDir := filepath.Join(home, ".codex", "visualizations", "2026", "09", "session")
	if err := os.MkdirAll(internalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	records, err := NewDetector(SourceFunc{
		SourceName: "fixture",
		DiscoverFn: func() ([]Record, error) {
			return []Record{{Path: internalDir, Harness: Codex, LastUsedAt: time.Now(), Source: "fixture"}}, nil
		},
	}).Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want internal Codex path filtered", records)
	}
}

func TestDetectorIgnoresAgentAndDeclawInternalPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := []string{
		filepath.Join(home, ".claude", "projects", "memory"),
		filepath.Join(home, "Library", "Application Support", "declaw", "support", "runs", "job"),
		filepath.Join(home, "Library", "Application Support", "declaw", "projects", "demo", "WORKSPACE"),
		filepath.Join(t.TempDir(), "repo", ".git"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	detector := NewDetector(SourceFunc{
		SourceName: "fixture",
		DiscoverFn: func() ([]Record, error) {
			records := make([]Record, 0, len(paths))
			for _, path := range paths {
				records = append(records, Record{Path: path, Harness: Claude, LastUsedAt: time.Now(), Source: "fixture"})
			}
			return records, nil
		},
	})
	records, err := detector.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want internal paths filtered", records)
	}
}

package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONActivityUsesRecordTimestampInsteadOfFileModTime(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "history.jsonl")
	eventTime := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	content, err := json.Marshal(map[string]any{
		"project":   projectDir,
		"sessionId": "session-1",
		"timestamp": eventTime.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	fileModTime := eventTime.Add(24 * time.Hour)
	records, err := recordsFromJSONFile(filePath, Claude, "claude local files", fileModTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one record", records)
	}
	if !records[0].LastUsedAt.Equal(eventTime) {
		t.Fatalf("LastUsedAt = %s, want %s", records[0].LastUsedAt, eventTime)
	}
	if records[0].SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want session-1", records[0].SessionID)
	}
}

func TestClaudeTranscriptIgnoresGenericPathFields(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	otherDir := filepath.Join(t.TempDir(), "other")
	for _, directory := range []string{projectDir, otherDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	filePath := filepath.Join(t.TempDir(), "session.jsonl")
	content, err := json.Marshal(map[string]any{
		"cwd":       projectDir,
		"path":      otherDir,
		"project":   otherDir,
		"timestamp": "2026-09-17T10:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := recordsFromJSONFile(filePath, Claude, "claude local files", time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Path != projectDir {
		t.Fatalf("records = %#v, want only cwd %q", records, projectDir)
	}
}

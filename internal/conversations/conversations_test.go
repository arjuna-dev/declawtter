package conversations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CleanTitle only normalizes whitespace and length; preamble stripping is
// TitleCandidate's job.
func TestCleanTitleNormalizesWhitespace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Fix the login bug", "Fix the login bug"},
		{"collapses whitespace", "Fix   the\n\nlogin bug", "Fix the login bug"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := CleanTitle(testCase.in); got != testCase.want {
				t.Fatalf("CleanTitle() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestTitleCandidateStripsHarnessPreamble(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"xml preamble", "<environment_context>cwd=/tmp</environment_context>\nFix the login bug", "Fix the login bug"},
		{"markdown preamble", "# Files mentioned by the user:\n## a.png: /tmp/a.png\n## My request:\nFix the login bug", "Fix the login bug"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := TitleCandidate(testCase.in); got != testCase.want {
				t.Fatalf("TitleCandidate() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A turn that is nothing but scaffolding must yield no title so callers fall
// through to the next user turn instead of showing machine noise.
func TestTitleCandidateRejectsPureScaffolding(t *testing.T) {
	if got := TitleCandidate("<environment_context>cwd=/tmp</environment_context>"); got != "" {
		t.Fatalf("TitleCandidate() = %q, want empty", got)
	}
	if got := TitleCandidate("Real question"); got != "Real question" {
		t.Fatalf("TitleCandidate() = %q, want %q", got, "Real question")
	}
}

func TestCleanTitleTruncatesLongInput(t *testing.T) {
	got := CleanTitle(strings.Repeat("a", maxTitleLength+50))
	if len([]rune(got)) > maxTitleLength+1 {
		t.Fatalf("title too long: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestCodexReaderParsesRolloutAndDetectsCompaction(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessions, "rollout-2026-01-02T10-00-00-abc12345-0000-0000-0000-000000000000.jsonl")
	content := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"abc12345-0000-0000-0000-000000000000","timestamp":"2026-01-02T10:00:00Z","cwd":"` + dir + `"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>noise</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Ship the feature"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"On it"}]}}`,
		`{"type":"compacted","payload":{"message":"Summary of earlier work"}}`,
	}, "\n")
	if err := os.WriteFile(rollout, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := &codexReader{roots: []string{filepath.Join(dir, "sessions")}}
	list, err := reader.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d conversations, want 1", len(list))
	}
	got := list[0]
	if got.Title != "Ship the feature" {
		t.Fatalf("Title = %q, want %q (preamble turn should be skipped)", got.Title, "Ship the feature")
	}
	if !got.Compacted {
		t.Fatal("expected the conversation to be marked compacted")
	}
	if got.Path != dir {
		t.Fatalf("Path = %q, want %q", got.Path, dir)
	}

	transcript, err := reader.Load(got)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	// When the origin harness compacted, its own summary replaces the raw turns
	// so the handoff carries what the harness itself considered essential.
	if !transcript.Compacted {
		t.Fatal("expected a compacted transcript")
	}
	if len(transcript.Messages) != 1 || !strings.Contains(transcript.Messages[0].Content, "Summary of earlier work") {
		t.Fatalf("expected the compaction summary to be the transcript body, got %+v", transcript.Messages)
	}
}

// A compaction marker can sit deep inside a multi-megabyte rollout, far past
// any cheap prefix or tail window. Listing must still flag it.
func TestCodexDetectsCompactionDeepInLargeFile(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessions, "rollout-2026-01-02T10-00-00-deadbeef-0000-0000-0000-000000000000.jsonl")

	var builder strings.Builder
	builder.WriteString(`{"type":"session_meta","payload":{"id":"deadbeef-0000-0000-0000-000000000000","timestamp":"2026-01-02T10:00:00Z","cwd":"` + dir + `"}}` + "\n")
	builder.WriteString(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Ship it"}]}}` + "\n")
	// Push the marker well beyond any prefix budget or tail probe.
	filler := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + strings.Repeat("y", 4000) + `"}]}}` + "\n"
	for builder.Len() < 3*1024*1024 {
		builder.WriteString(filler)
	}
	builder.WriteString(`{"type":"compacted","payload":{"message":"Condensed"}}` + "\n")
	for i := 0; i < 200; i++ {
		builder.WriteString(filler)
	}
	if err := os.WriteFile(rollout, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := &codexReader{roots: []string{filepath.Join(dir, "sessions")}}
	list, err := reader.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d conversations, want 1", len(list))
	}
	if !list[0].Compacted {
		t.Fatal("compaction marker deep in the file was not detected")
	}
	if list[0].Title != "Ship it" {
		t.Fatalf("Title = %q, want %q", list[0].Title, "Ship it")
	}
}

func TestCodexResumeCommandUsesNativeResume(t *testing.T) {
	reader := &codexReader{}
	program, args, err := reader.ResumeCommand(Conversation{ID: "abc", Harness: "codex"})
	if err != nil {
		t.Fatalf("ResumeCommand() error: %v", err)
	}
	if program != "codex" {
		t.Fatalf("program = %q, want codex", program)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "resume") || !strings.Contains(joined, "abc") {
		t.Fatalf("args = %v, want a resume invocation carrying the session id", args)
	}
}

func TestHandoffPromptFramesTranscriptAsContext(t *testing.T) {
	transcript := Transcript{
		Conversation: Conversation{Harness: "codex", Path: "/tmp/demo", UpdatedAt: time.Now()},
		Messages: []Message{
			{Role: "user", Content: "Fix the bug"},
			{Role: "assistant", Content: "Fixed"},
		},
	}
	prompt := HandoffPrompt(transcript, "claude")
	for _, want := range []string{"Origin harness: codex", "Continuing in: claude", "Fix the bug", "BEGIN TRANSCRIPT"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
	// The model must not answer the replayed history as if it were new input.
	if !strings.Contains(prompt, "wait for the user's next instruction") {
		t.Fatal("prompt should tell the agent not to act on the replayed transcript")
	}
}

func TestHandoffPromptNotesCompaction(t *testing.T) {
	transcript := Transcript{
		Conversation: Conversation{Harness: "codex", Compacted: true},
		Messages:     []Message{{Role: "assistant", Content: "Summary"}},
		Compacted:    true,
	}
	if !strings.Contains(HandoffPrompt(transcript, "claude"), "compacted this conversation") {
		t.Fatal("expected the prompt to disclose that the transcript is a compaction summary")
	}
}

// Long conversations must be trimmed from the middle so the newest turns, which
// matter most for continuing, always survive the budget.
func TestRenderTranscriptKeepsMostRecentTurns(t *testing.T) {
	messages := make([]Message, 0, 400)
	for i := 0; i < 400; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 200)})
	}
	messages = append(messages, Message{Role: "user", Content: "FINAL TURN MARKER"})
	rendered := Render(Transcript{Messages: messages})
	if !strings.Contains(rendered, "FINAL TURN MARKER") {
		t.Fatal("the most recent turn must be preserved")
	}
	if len(rendered) > maxPromptCharacters*2 {
		t.Fatalf("rendered transcript %d chars, far above the %d budget", len(rendered), maxPromptCharacters)
	}
}

func TestStorePinIgnoreAndAliasRoundTrip(t *testing.T) {
	t.Setenv("DECLAW_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	key := "codex:abc"
	if _, err := store.SetPinned(key, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAlias(key, "My chat"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetIgnored(key, true); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	all, err := reloaded.All()
	if err != nil {
		t.Fatal(err)
	}
	settings, ok := all[key]
	if !ok {
		t.Fatalf("settings for %q not persisted", key)
	}
	if !settings.Pinned || !settings.Ignored || settings.Alias != "My chat" {
		t.Fatalf("round trip lost data: %+v", settings)
	}
}

func TestDecorateSortsPinnedFirstThenRecency(t *testing.T) {
	now := time.Now()
	list := []Conversation{
		{Harness: "codex", ID: "old", UpdatedAt: now.Add(-48 * time.Hour)},
		{Harness: "codex", ID: "new", UpdatedAt: now},
		{Harness: "codex", ID: "pinned", UpdatedAt: now.Add(-72 * time.Hour)},
	}
	stored := map[string]Settings{"codex:pinned": {Pinned: true}}
	decorated := Decorate(list, stored)
	if decorated[0].ID != "pinned" {
		t.Fatalf("pinned conversation should sort first, got %q", decorated[0].ID)
	}
	if decorated[1].ID != "new" || decorated[2].ID != "old" {
		t.Fatalf("unpinned conversations should sort newest first, got %q then %q", decorated[1].ID, decorated[2].ID)
	}
}

func TestVisibleHidesIgnoredConversations(t *testing.T) {
	decorated := []Decorated{
		{Conversation: Conversation{ID: "a"}, Settings: Settings{}},
		{Conversation: Conversation{ID: "b"}, Settings: Settings{Ignored: true}},
	}
	visible := Visible(decorated)
	if len(visible) != 1 || visible[0].ID != "a" {
		t.Fatalf("Visible() = %+v, want only the unignored conversation", visible)
	}
}

func TestDisplayTitlePrefersAlias(t *testing.T) {
	item := Decorated{
		Conversation: Conversation{ID: "abc", Title: "Original"},
		Settings:     Settings{Alias: "Renamed"},
	}
	if got := item.DisplayTitle(); got != "Renamed" {
		t.Fatalf("DisplayTitle() = %q, want %q", got, "Renamed")
	}
	// With neither alias nor title the id keeps the entry addressable.
	bare := Decorated{Conversation: Conversation{ID: "abc"}}
	if got := bare.DisplayTitle(); got == "" {
		t.Fatal("DisplayTitle() must never be empty")
	}
}

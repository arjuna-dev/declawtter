package conversations

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"declaw/internal/activity"
)

// codexReader reads Codex rollout transcripts.
//
// Codex writes one JSONL file per session under sessions/YYYY/MM/DD. Line 0 is
// a session_meta record carrying the session id and cwd, so listing only needs
// the first line of each file. Compaction appears as a `compacted` record whose
// payload.replacement_history holds the condensed history that Codex itself
// would replay, which is exactly what we want to forward to another harness.
type codexReader struct {
	roots []string
}

func newCodexReader() Reader {
	return &codexReader{roots: activity.KnownDataLocations(string(activity.Codex))}
}

func (r *codexReader) Harness() activity.Harness {
	return activity.Codex
}

// codexSessionFileLimit bounds how many rollout files are inspected. Codex
// keeps years of transcripts and checkout only ever shows the recent slice.
const codexSessionFileLimit = 400

func (r *codexReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, root := range r.roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		base := strings.ToLower(filepath.Base(filepath.Clean(root)))
		if base != "sessions" && base != "archived_sessions" {
			continue
		}
		for _, path := range newestFilesUnder(root, ".jsonl", codexSessionFileLimit) {
			conversation, ok := r.summarize(path)
			if !ok || seen[conversation.ID] {
				continue
			}
			seen[conversation.ID] = true
			out = append(out, conversation)
		}
	}
	return out, nil
}

func (r *codexReader) summarize(path string) (Conversation, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Conversation{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	if !scanner.Scan() {
		return Conversation{}, false
	}
	var meta struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
			Timestamp string `json:"timestamp"`
			Cwd       string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil || meta.Type != "session_meta" {
		return Conversation{}, false
	}
	id := strings.TrimSpace(meta.Payload.SessionID)
	if id == "" {
		id = strings.TrimSpace(meta.Payload.ID)
	}
	if id == "" {
		return Conversation{}, false
	}

	conversation := Conversation{
		ID:        id,
		Harness:   activity.Codex,
		Path:      meta.Payload.Cwd,
		Location:  path,
		Source:    "codex rollout",
		UpdatedAt: fileModTime(path),
	}
	if stamp, ok := parseTimestamp(meta.Payload.Timestamp, meta.Timestamp); ok {
		conversation.UpdatedAt = stamp
	}

	// Scan the remainder for the first user turn (the title) and for any
	// compaction marker. Both are cheap to detect and make the listing useful.
	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"type"`) {
			continue
		}
		var record codexRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		switch record.Type {
		case "compacted":
			conversation.Compacted = true
		case "response_item":
			if record.Payload.Type != "message" {
				continue
			}
			conversation.MessageCount++
			if conversation.Title == "" && record.Payload.Role == "user" {
				conversation.Title = TitleCandidate(codexText(record.Payload.Content))
			}
		}
	}
	return conversation, true
}

type codexRecord struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content []codexContent  `json:"content"`
		Message string          `json:"message"`
		History json.RawMessage `json:"replacement_history"`
	} `json:"payload"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func codexText(parts []codexContent) string {
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func (r *codexReader) Load(conversation Conversation) (Transcript, error) {
	transcript := Transcript{Conversation: conversation}
	file, err := os.Open(conversation.Location)
	if err != nil {
		return transcript, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	var messages []Message
	for scanner.Scan() {
		var record codexRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		switch record.Type {
		case "compacted":
			// Codex already condensed everything before this point. Replacing
			// the accumulated messages with the compaction payload mirrors what
			// Codex replays on resume and keeps long chats inside a sane size.
			replacement := codexReplacementMessages(record)
			if len(replacement) > 0 {
				messages = replacement
				transcript.Compacted = true
			}
		case "response_item":
			if record.Payload.Type != "message" {
				continue
			}
			content := codexText(record.Payload.Content)
			if strings.TrimSpace(content) == "" {
				continue
			}
			at, _ := parseTimestamp(record.Timestamp)
			messages = append(messages, Message{
				Role:    normalizeRole(record.Payload.Role),
				Content: content,
				At:      at,
			})
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return transcript, err
	}
	transcript.Messages = messages
	transcript.Conversation.Compacted = transcript.Compacted
	return transcript, nil
}

func codexReplacementMessages(record codexRecord) []Message {
	if len(record.Payload.History) > 0 {
		var history []struct {
			Type    string         `json:"type"`
			Role    string         `json:"role"`
			Content []codexContent `json:"content"`
		}
		if err := json.Unmarshal(record.Payload.History, &history); err == nil {
			var messages []Message
			for _, item := range history {
				if item.Type != "message" {
					continue
				}
				content := codexText(item.Content)
				if strings.TrimSpace(content) == "" {
					continue
				}
				messages = append(messages, Message{
					Role:    normalizeRole(item.Role),
					Content: content,
				})
			}
			if len(messages) > 0 {
				return messages
			}
		}
	}
	if summary := strings.TrimSpace(record.Payload.Message); summary != "" {
		return []Message{{Role: "system", Content: summary}}
	}
	return nil
}

// ResumeCommand uses Codex's native resume so same-harness checkout keeps full
// fidelity instead of round-tripping through a rendered transcript.
func (r *codexReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	return "codex", []string{
		"resume", conversation.ID,
		"--sandbox", "danger-full-access",
		"--ask-for-approval", "never",
	}, nil
}

// newestFilesUnder walks a tree newest-name-first and returns up to limit files
// with the given extension. Session trees are date-partitioned, so reverse
// lexical order is a good proxy for recency and avoids statting everything.
func newestFilesUnder(root, extension string, limit int) []string {
	var found []string
	var visit func(string)
	visit = func(directory string) {
		if len(found) >= limit {
			return
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
		for _, entry := range entries {
			if len(found) >= limit {
				return
			}
			path := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				visit(path)
				continue
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), extension) {
				found = append(found, path)
			}
		}
	}
	visit(root)
	return found
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

func parseTimestamp(values ...string) (time.Time, bool) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05"} {
			if stamp, err := time.Parse(layout, value); err == nil {
				return stamp.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func normalizeRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	switch role {
	case "user", "human":
		return "user"
	case "assistant", "model", "ai":
		return "assistant"
	case "system", "developer":
		return "system"
	case "":
		return "assistant"
	default:
		return role
	}
}

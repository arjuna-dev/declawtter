package conversations

import (
	"bufio"
	"bytes"
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

// codexSummaryByteBudget caps how much of a rollout is read to build a list
// entry. The first user turn is near the top, so a small prefix suffices.
const codexSummaryByteBudget = 256 * 1024

// codexProbeChunkBytes is the read size used when scanning a rollout for a
// compaction marker. Raw scanning stops at the first hit, so this only bounds
// memory, not total work.
const codexProbeChunkBytes = 1024 * 1024

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

	// Scan a bounded prefix for the first user turn (the title).
	//
	// Listing must stay fast: a heavy Codex history is gigabytes across
	// hundreds of rollouts, and reading every line made the launcher take
	// tens of seconds to appear. Load() still reads the whole file, so the
	// transcript that is actually handed over remains complete; only this
	// list preview is approximate.
	scanned := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		scanned += len(line) + 1
		if bytes.Contains(line, []byte(`"type"`)) {
			var record codexRecord
			if err := json.Unmarshal(line, &record); err == nil {
				if record.Type == "compacted" {
					conversation.Compacted = true
				}
				if record.Type == "response_item" && record.Payload.Type == "message" && record.Payload.Role == "user" && conversation.Title == "" {
					conversation.Title = TitleCandidate(codexText(record.Payload.Content))
				}
			}
		}
		if conversation.Title != "" || scanned >= codexSummaryByteBudget {
			break
		}
	}
	// Compaction is recorded wherever it happened, which for a long session is
	// the middle of a multi-megabyte file. Probing bounded windows keeps the
	// flag accurate without reading everything; a miss only costs the list
	// badge, since Load() reads the full file and handles compaction properly.
	if !conversation.Compacted && codexFileHasCompaction(file) {
		conversation.Compacted = true
	}
	return conversation, true
}

// codexFileHasCompaction reports whether a rollout contains a compaction
// record. It scans raw bytes without JSON parsing and stops at the first hit,
// which keeps it fast even across gigabytes: parsing every line was what made
// listing slow, not reading the bytes.
func codexFileHasCompaction(file *os.File) bool {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false
	}
	marker := []byte(`"type":"compacted"`)
	spaced := []byte(`"type": "compacted"`)
	buffer := make([]byte, codexProbeChunkBytes)
	// Carry the tail of each chunk so a marker split across a chunk boundary
	// is still matched.
	overlap := len(spaced)
	var carry []byte
	for {
		count, err := file.Read(buffer)
		if count > 0 {
			chunk := append(carry, buffer[:count]...)
			if bytes.Contains(chunk, marker) || bytes.Contains(chunk, spaced) {
				return true
			}
			if len(chunk) > overlap {
				carry = append([]byte(nil), chunk[len(chunk)-overlap:]...)
			} else {
				carry = append([]byte(nil), chunk...)
			}
		}
		if err != nil {
			return false
		}
	}
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

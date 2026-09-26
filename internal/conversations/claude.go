package conversations

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"declaw/internal/activity"
)

// claudeReader reads Claude Code transcripts.
//
// Claude stores one JSONL file per session under projects/<slugified-cwd>/. The
// lines form a DAG (each carries parentUuid), but for a portable transcript the
// file order is a faithful linearization. Compaction shows up as a summary line
// plus an isCompactSummary user turn; when present we forward the summary.
type claudeReader struct {
	roots []string
}

func newClaudeReader() Reader {
	return &claudeReader{roots: activity.KnownDataLocations(string(activity.Claude))}
}

func (r *claudeReader) Harness() activity.Harness {
	return activity.Claude
}

const claudeSessionFileLimit = 400

func (r *claudeReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, root := range r.roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(filepath.Clean(root)), "projects") {
			continue
		}
		for _, path := range newestFilesUnder(root, ".jsonl", claudeSessionFileLimit) {
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

type claudeRecord struct {
	Type             string          `json:"type"`
	SessionID        string          `json:"sessionId"`
	UUID             string          `json:"uuid"`
	Cwd              string          `json:"cwd"`
	Timestamp        string          `json:"timestamp"`
	IsMeta           bool            `json:"isMeta"`
	IsSidechain      bool            `json:"isSidechain"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Summary          string          `json:"summary"`
	Title            string          `json:"title"`
	Message          json.RawMessage `json:"message"`
}

// claudeMessage covers both shapes Claude uses: a plain string content for
// simple user turns, and a content-block array for assistant turns.
type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func claudeMessageText(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var message claudeMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return "", ""
	}
	var text string
	if err := json.Unmarshal(message.Content, &text); err == nil {
		return message.Role, text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(message.Content, &blocks); err != nil {
		return message.Role, ""
	}
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type != "text" || strings.TrimSpace(block.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(block.Text)
	}
	return message.Role, builder.String()
}

func (r *claudeReader) summarize(path string) (Conversation, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Conversation{}, false
	}
	defer file.Close()

	conversation := Conversation{
		Harness:   activity.Claude,
		Location:  path,
		Source:    "claude projects",
		UpdatedAt: fileModTime(path),
		// The directory name is a slugified cwd; the records carry the real
		// path, so prefer those and fall back to the file name only if absent.
		ID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	var aiTitle string
	for scanner.Scan() {
		var record claudeRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.SessionID != "" {
			conversation.ID = record.SessionID
		}
		if record.Cwd != "" && conversation.Path == "" {
			conversation.Path = record.Cwd
		}
		if stamp, ok := parseTimestamp(record.Timestamp); ok && stamp.After(conversation.UpdatedAt) {
			conversation.UpdatedAt = stamp
		}
		switch record.Type {
		case "ai-title":
			if strings.TrimSpace(record.Title) != "" {
				aiTitle = record.Title
			}
		case "summary":
			if strings.TrimSpace(record.Summary) != "" {
				conversation.Compacted = true
			}
		case "user", "assistant":
			if record.IsSidechain || record.IsMeta {
				continue
			}
			if record.IsCompactSummary {
				conversation.Compacted = true
			}
			conversation.MessageCount++
			if conversation.Title == "" && record.Type == "user" {
				_, text := claudeMessageText(record.Message)
				conversation.Title = TitleCandidate(text)
			}
		}
	}
	if aiTitle != "" {
		conversation.Title = CleanTitle(aiTitle)
	}
	if conversation.MessageCount == 0 {
		return Conversation{}, false
	}
	return conversation, true
}

func (r *claudeReader) Load(conversation Conversation) (Transcript, error) {
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
		var record claudeRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.Type == "summary" && strings.TrimSpace(record.Summary) != "" {
			// Claude's compaction summary supersedes everything before it.
			messages = []Message{{Role: "system", Content: strings.TrimSpace(record.Summary)}}
			transcript.Compacted = true
			continue
		}
		if record.Type != "user" && record.Type != "assistant" {
			continue
		}
		if record.IsSidechain || record.IsMeta {
			continue
		}
		role, text := claudeMessageText(record.Message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		if record.IsCompactSummary {
			messages = []Message{{Role: "system", Content: strings.TrimSpace(text)}}
			transcript.Compacted = true
			continue
		}
		at, _ := parseTimestamp(record.Timestamp)
		if role == "" {
			role = record.Type
		}
		messages = append(messages, Message{Role: normalizeRole(role), Content: text, At: at})
	}
	transcript.Messages = messages
	transcript.Conversation.Compacted = transcript.Compacted
	return transcript, nil
}

func (r *claudeReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	return "claude", []string{
		"--resume", conversation.ID,
		"--dangerously-skip-permissions",
		"--permission-mode", "bypassPermissions",
	}, nil
}

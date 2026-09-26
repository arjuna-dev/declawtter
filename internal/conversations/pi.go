package conversations

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"declaw/internal/activity"
)

// piReader reads Pi coding agent sessions.
//
// Pi stores one JSONL file per session under agent/sessions/<slugified-cwd>/,
// with a leading {"type":"session"} header carrying the id and cwd, followed by
// {"type":"message"} records holding role/content blocks.
type piReader struct {
	roots []string
}

func newPiReader() Reader {
	return &piReader{roots: activity.KnownDataLocations(string(activity.Pi))}
}

func (r *piReader) Harness() activity.Harness {
	return activity.Pi
}

const piSessionFileLimit = 400

func (r *piReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, root := range r.roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(filepath.Clean(root)), "sessions") {
			continue
		}
		for _, path := range newestFilesUnder(root, ".jsonl", piSessionFileLimit) {
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

type piRecord struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Message   struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func (p piRecord) text() string {
	var builder strings.Builder
	for _, part := range p.Message.Content {
		if part.Type != "text" || strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func (r *piReader) summarize(path string) (Conversation, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Conversation{}, false
	}
	defer file.Close()

	conversation := Conversation{
		Harness:   activity.Pi,
		Location:  path,
		Source:    "pi sessions",
		UpdatedAt: fileModTime(path),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for scanner.Scan() {
		var record piRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		switch record.Type {
		case "session":
			conversation.ID = record.ID
			conversation.Path = record.Cwd
			if strings.TrimSpace(record.Name) != "" {
				conversation.Title = CleanTitle(record.Name)
			}
			if stamp, ok := parseTimestamp(record.Timestamp); ok {
				conversation.UpdatedAt = stamp
			}
		case "compaction", "compacted", "summary":
			conversation.Compacted = true
		case "message":
			conversation.MessageCount++
			if stamp, ok := parseTimestamp(record.Timestamp); ok && stamp.After(conversation.UpdatedAt) {
				conversation.UpdatedAt = stamp
			}
			if conversation.Title == "" && normalizeRole(record.Message.Role) == "user" {
				conversation.Title = TitleCandidate(record.text())
			}
		}
	}
	if conversation.ID == "" || conversation.MessageCount == 0 {
		return Conversation{}, false
	}
	return conversation, true
}

func (r *piReader) Load(conversation Conversation) (Transcript, error) {
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
		var record piRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		switch record.Type {
		case "compaction", "compacted", "summary":
			if summary := strings.TrimSpace(record.Summary); summary != "" {
				messages = []Message{{Role: "system", Content: summary}}
				transcript.Compacted = true
			}
		case "message":
			text := record.text()
			if strings.TrimSpace(text) == "" {
				continue
			}
			at, _ := parseTimestamp(record.Timestamp)
			messages = append(messages, Message{
				Role:    normalizeRole(record.Message.Role),
				Content: text,
				At:      at,
			})
		}
	}
	transcript.Messages = messages
	transcript.Conversation.Compacted = transcript.Compacted
	return transcript, nil
}

func (r *piReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	return "pi", []string{"--session", conversation.ID}, nil
}

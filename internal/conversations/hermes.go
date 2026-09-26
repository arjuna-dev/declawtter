package conversations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"declaw/internal/activity"
)

// hermesReader reads Hermes Agent sessions.
//
// Hermes writes one JSON document per session (session_<stamp>_<id>.json) that
// contains the whole message list, unlike the JSONL harnesses. Request dumps
// live beside them and are skipped by the file-name prefix.
type hermesReader struct {
	roots []string
}

func newHermesReader() Reader {
	return &hermesReader{roots: activity.KnownDataLocations(string(activity.Hermes))}
}

func (r *hermesReader) Harness() activity.Harness {
	return activity.Hermes
}

const hermesSessionFileLimit = 400

type hermesSession struct {
	SessionID    string          `json:"session_id"`
	Model        string          `json:"model"`
	Platform     string          `json:"platform"`
	SessionStart string          `json:"session_start"`
	LastUpdated  string          `json:"last_updated"`
	Cwd          string          `json:"cwd"`
	WorkingDir   string          `json:"working_directory"`
	MessageCount int             `json:"message_count"`
	Messages     []hermesMessage `json:"messages"`
}

// hermesMessage content is a string for plain turns and a block array when the
// turn carries tool results or images.
type hermesMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp"`
}

func (m hermesMessage) text() string {
	if len(m.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(block.Text)
	}
	return builder.String()
}

func (r *hermesReader) load(path string) (hermesSession, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hermesSession{}, false
	}
	var session hermesSession
	if err := json.Unmarshal(data, &session); err != nil {
		return hermesSession{}, false
	}
	if strings.TrimSpace(session.SessionID) == "" {
		return hermesSession{}, false
	}
	return session, true
}

func (r *hermesReader) conversation(path string, session hermesSession) Conversation {
	conversation := Conversation{
		ID:           session.SessionID,
		Harness:      activity.Hermes,
		Path:         firstNonEmptyString(session.Cwd, session.WorkingDir),
		Location:     path,
		Source:       "hermes sessions",
		UpdatedAt:    fileModTime(path),
		MessageCount: len(session.Messages),
	}
	if conversation.MessageCount == 0 {
		conversation.MessageCount = session.MessageCount
	}
	if stamp, ok := parseTimestamp(session.LastUpdated, session.SessionStart); ok {
		conversation.UpdatedAt = stamp
	}
	for _, message := range session.Messages {
		if normalizeRole(message.Role) != "user" {
			continue
		}
		if title := TitleCandidate(message.text()); title != "" {
			conversation.Title = title
			break
		}
	}
	return conversation
}

func (r *hermesReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, root := range r.roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, path := range newestFilesUnder(root, ".json", hermesSessionFileLimit) {
			if !strings.HasPrefix(filepath.Base(path), "session_") {
				continue
			}
			session, ok := r.load(path)
			if !ok || seen[session.SessionID] {
				continue
			}
			seen[session.SessionID] = true
			out = append(out, r.conversation(path, session))
		}
	}
	return out, nil
}

func (r *hermesReader) Load(conversation Conversation) (Transcript, error) {
	transcript := Transcript{Conversation: conversation}
	session, ok := r.load(conversation.Location)
	if !ok {
		return transcript, os.ErrNotExist
	}
	for _, message := range session.Messages {
		text := message.text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		at, _ := parseTimestamp(message.Timestamp)
		transcript.Messages = append(transcript.Messages, Message{
			Role:    normalizeRole(message.Role),
			Content: text,
			At:      at,
		})
	}
	return transcript, nil
}

// ResumeCommand: Hermes resumes by session id via its --resume flag.
func (r *hermesReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	program := "hermes"
	if !commandExists(program) {
		program = "hermes-agent"
	}
	return program, []string{"--resume", conversation.ID}, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

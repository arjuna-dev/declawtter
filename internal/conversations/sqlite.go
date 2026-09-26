package conversations

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"declaw/internal/activity"
)

// The SQLite-backed readers shell out to sqlite3 in readonly mode, matching the
// approach already used by internal/activity. This keeps declaw from linking a
// database driver into an agent's private store, and readonly mode guarantees
// declaw can never corrupt real conversation history.

func commandExists(program string) bool {
	_, err := exec.LookPath(program)
	return err == nil
}

// sqliteQuery runs one query against a database copy-free, readonly. The unit
// separator is used as the column delimiter because conversation text routinely
// contains pipes, tabs and newlines.
func sqliteQuery(database, query string) ([][]string, error) {
	if !commandExists("sqlite3") {
		return nil, nil
	}
	command := exec.Command("sqlite3", "-noheader", "-separator", "\x1f", "file:"+database+"?mode=ro&immutable=1", query)
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\x1f"))
	}
	return rows, nil
}

func sqliteTableExists(database, table string) bool {
	rows, err := sqliteQuery(database, "SELECT name FROM sqlite_master WHERE type='table' AND name='"+table+"' LIMIT 1")
	return err == nil && len(rows) > 0
}

// parseEpoch accepts the second- and millisecond-precision integer timestamps
// used by the SQLite-backed harnesses.
func parseEpoch(value string) time.Time {
	raw, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || raw <= 0 {
		return time.Time{}
	}
	if raw > 1e12 {
		return time.UnixMilli(raw).UTC()
	}
	return time.Unix(raw, 0).UTC()
}

// openCodeReader reads OpenCode sessions from its single opencode.db.
//
// Sessions live in session_v2 (id, directory, title, time_updated) and turns in
// session_message, whose `data` column holds the message JSON. Compaction is
// recorded on the session as time_compacting.
type openCodeReader struct {
	databases []string
}

func newOpenCodeReader() Reader {
	var databases []string
	for _, root := range activity.KnownDataLocations(string(activity.OpenCode)) {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if info.IsDir() {
			candidate := filepath.Join(root, "opencode.db")
			if _, err := os.Stat(candidate); err == nil {
				databases = append(databases, candidate)
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(root), ".db") {
			databases = append(databases, root)
		}
	}
	return &openCodeReader{databases: databases}
}

func (r *openCodeReader) Harness() activity.Harness {
	return activity.OpenCode
}

func (r *openCodeReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, database := range r.databases {
		if !sqliteTableExists(database, "session_v2") {
			continue
		}
		rows, err := sqliteQuery(database, `
			SELECT id,
			       COALESCE(directory, ''),
			       COALESCE(title, ''),
			       COALESCE(time_updated, time_created, 0),
			       COALESCE(time_compacting, 0),
			       (SELECT COUNT(*) FROM session_message WHERE session_message.session_id = session_v2.id)
			FROM session_v2
			WHERE time_archived IS NULL
			ORDER BY COALESCE(time_updated, time_created, 0) DESC
			LIMIT 400`)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if len(row) < 6 || seen[row[0]] {
				continue
			}
			seen[row[0]] = true
			count, _ := strconv.Atoi(row[5])
			out = append(out, Conversation{
				ID:           row[0],
				Harness:      activity.OpenCode,
				Path:         row[1],
				Title:        CleanTitle(row[2]),
				UpdatedAt:    parseEpoch(row[3]),
				MessageCount: count,
				Location:     database,
				Source:       "opencode database",
				Compacted:    parseEpoch(row[4]).IsZero() == false,
			})
		}
	}
	return out, nil
}

func (r *openCodeReader) Load(conversation Conversation) (Transcript, error) {
	transcript := Transcript{Conversation: conversation, Compacted: conversation.Compacted}
	rows, err := sqliteQuery(conversation.Location,
		"SELECT type, COALESCE(data,''), COALESCE(time_created,0) FROM session_message WHERE session_id='"+
			escapeSQLiteLiteral(conversation.ID)+"' ORDER BY seq ASC LIMIT 5000")
	if err != nil {
		return transcript, err
	}
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}
		role, text := openCodeMessageText(row[1])
		if strings.TrimSpace(text) == "" {
			continue
		}
		if role == "" {
			role = row[0]
		}
		transcript.Messages = append(transcript.Messages, Message{
			Role:    normalizeRole(role),
			Content: text,
			At:      parseEpoch(row[2]),
		})
	}
	return transcript, nil
}

// openCodeMessageText pulls readable text out of a session_message data blob.
// The blob shape varies by message type, so only known text carriers are read
// and anything else is skipped rather than guessed at.
func openCodeMessageText(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	var payload struct {
		Role  string `json:"role"`
		Text  string `json:"text"`
		Type  string `json:"type"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", ""
	}
	if strings.TrimSpace(payload.Text) != "" {
		return payload.Role, payload.Text
	}
	var builder strings.Builder
	for _, part := range payload.Parts {
		if part.Type != "text" || strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	if builder.Len() > 0 {
		return payload.Role, builder.String()
	}
	var text string
	if err := json.Unmarshal(payload.Content, &text); err == nil {
		return payload.Role, text
	}
	return payload.Role, ""
}

func (r *openCodeReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	return "opencode", []string{"--session", conversation.ID, "--auto"}, nil
}

// antigravityReader reads the Antigravity CLI conversation index.
//
// Antigravity keeps a summary row per conversation in conversation_summaries.db
// and the full step history in conversations/<id>.db as protobuf blobs. Declaw
// reads the summary index (which is plain SQL) and uses the stored preview for
// cross-harness handoff; the protobuf step payloads are deliberately not parsed
// because that format is private and unstable.
type antigravityReader struct {
	summaryDatabases []string
}

func newAntigravityReader() Reader {
	var databases []string
	for _, root := range activity.KnownDataLocations(string(activity.Antigravity)) {
		if strings.EqualFold(filepath.Base(root), "conversation_summaries.db") {
			if _, err := os.Stat(root); err == nil {
				databases = append(databases, root)
			}
		}
	}
	return &antigravityReader{summaryDatabases: databases}
}

func (r *antigravityReader) Harness() activity.Harness {
	return activity.Antigravity
}

func (r *antigravityReader) List() ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, database := range r.summaryDatabases {
		if !sqliteTableExists(database, "conversation_summaries") {
			continue
		}
		rows, err := sqliteQuery(database, `
			SELECT conversation_id,
			       COALESCE(title, ''),
			       COALESCE(preview, ''),
			       COALESCE(step_count, 0),
			       COALESCE(last_modified_time, ''),
			       COALESCE(workspace_uris, '')
			FROM conversation_summaries
			WHERE killed = 0
			ORDER BY last_modified_time DESC
			LIMIT 400`)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if len(row) < 6 || seen[row[0]] {
				continue
			}
			seen[row[0]] = true
			count, _ := strconv.Atoi(row[3])
			updatedAt, ok := parseTimestamp(row[4])
			if !ok {
				updatedAt = parseEpoch(row[4])
			}
			title := row[1]
			if strings.TrimSpace(title) == "" {
				title = row[2]
			}
			out = append(out, Conversation{
				ID:           row[0],
				Harness:      activity.Antigravity,
				Path:         antigravityWorkspacePath(row[5]),
				Title:        CleanTitle(title),
				UpdatedAt:    updatedAt,
				MessageCount: count,
				Location:     database,
				Source:       "antigravity summaries",
			})
		}
	}
	return out, nil
}

// antigravityWorkspacePath extracts the first local directory from the stored
// workspace_uris value, which is a JSON array of file:// URIs.
func antigravityWorkspacePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		uris = []string{raw}
	}
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		return strings.TrimPrefix(uri, "file://")
	}
	return ""
}

// Load returns the stored preview only. The full step history is protobuf in a
// private schema, so declaw does not attempt to decode it; same-harness resume
// still works natively via ResumeCommand, and cross-harness handoff carries the
// preview plus the working directory.
func (r *antigravityReader) Load(conversation Conversation) (Transcript, error) {
	transcript := Transcript{Conversation: conversation}
	if strings.TrimSpace(conversation.Title) != "" {
		transcript.Messages = append(transcript.Messages, Message{
			Role:    "system",
			Content: "Summary of the prior Antigravity conversation: " + conversation.Title,
			At:      conversation.UpdatedAt,
		})
	}
	return transcript, nil
}

func (r *antigravityReader) ResumeCommand(conversation Conversation) (string, []string, error) {
	program := "agy"
	if !commandExists(program) {
		program = "antigravity"
	}
	return program, []string{"--conversation", conversation.ID}, nil
}

// escapeSQLiteLiteral makes a value safe inside a single-quoted SQL literal.
func escapeSQLiteLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func defaultReaders() []Reader {
	return []Reader{
		newCodexReader(),
		newClaudeReader(),
		newPiReader(),
		newHermesReader(),
		newOpenCodeReader(),
		newAntigravityReader(),
	}
}

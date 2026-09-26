// Package conversations discovers individual agent chat sessions across every
// supported harness and renders them into a portable transcript.
//
// Declaw never writes into an agent's own session storage. Readers open agent
// data read-only, and any state declaw needs (pins, ignores, harness overrides)
// lives in declaw's own registry. This keeps each harness free to change its
// private on-disk format without declaw corrupting real history.
package conversations

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"declaw/internal/activity"
)

// Conversation is the normalized summary of a single chat session. It is what
// `declaw checkout chat` lists, and it carries just enough to locate the full
// transcript later without holding every message in memory.
type Conversation struct {
	// ID is the harness-native session identifier.
	ID string
	// Harness is the runtime that owns this conversation.
	Harness activity.Harness
	// Path is the working directory the conversation ran in.
	Path string
	// Title is a short human label, usually derived from the first user turn.
	Title string
	// UpdatedAt is the most recent activity timestamp.
	UpdatedAt time.Time
	// MessageCount is a best-effort turn count; zero when unknown.
	MessageCount int
	// Location is the file or database the conversation was read from.
	Location string
	// Source names the adapter that produced the record.
	Source string
	// Compacted reports whether the harness compacted this conversation. When
	// true the transcript is rendered from the compacted replacement history.
	Compacted bool
}

// Key is the stable identity of a conversation across declaw runs. Session IDs
// are only unique within a harness, so the harness is part of the key.
func (c Conversation) Key() string {
	return string(c.Harness) + ":" + c.ID
}

// Message is one turn in a portable transcript.
type Message struct {
	Role    string
	Content string
	At      time.Time
}

// Transcript is the harness-independent rendering of a conversation. It is the
// bridge format: every Reader produces one, and cross-harness checkout injects
// it as a prompt into the target harness.
type Transcript struct {
	Conversation Conversation
	Messages     []Message
	// Compacted mirrors Conversation.Compacted and reports that Messages came
	// from the harness's own compaction summary rather than the full history.
	Compacted bool
}

// Reader discovers and loads conversations for a single harness. Adapters are
// independently replaceable, mirroring activity.Source.
type Reader interface {
	// Harness identifies the runtime this reader serves.
	Harness() activity.Harness
	// List returns conversation summaries without loading full transcripts.
	List() ([]Conversation, error)
	// Load returns the full portable transcript for one conversation.
	Load(conversation Conversation) (Transcript, error)
}

// Resumer describes how to re-enter a conversation in its own harness. Native
// resume is always preferred over transcript injection because it preserves
// full fidelity, including tool calls declaw cannot represent.
type Resumer interface {
	// ResumeCommand returns the program and arguments that reopen the given
	// conversation natively. An error means the harness cannot resume by ID.
	ResumeCommand(conversation Conversation) (string, []string, error)
}

// Registry holds the reader for every harness declaw knows about.
type Registry struct {
	readers []Reader
}

func NewRegistry(readers ...Reader) *Registry {
	return &Registry{readers: append([]Reader(nil), readers...)}
}

// DefaultRegistry wires the per-harness adapters.
func DefaultRegistry() *Registry {
	return NewRegistry(defaultReaders()...)
}

// ReaderFor returns the adapter serving a harness.
func (r *Registry) ReaderFor(harness activity.Harness) (Reader, bool) {
	if r == nil {
		return nil, false
	}
	for _, reader := range r.readers {
		if reader != nil && reader.Harness() == harness {
			return reader, true
		}
	}
	return nil, false
}

// List gathers conversations from every reader. Adapters run concurrently
// because several of them touch slow on-disk trees, and one failing harness
// must never hide the others: errors are collected and returned alongside the
// records that did load.
func (r *Registry) List() ([]Conversation, error) {
	if r == nil {
		return nil, nil
	}
	type readerResult struct {
		conversations []Conversation
		err           error
	}
	results := make([]readerResult, len(r.readers))
	var waitGroup sync.WaitGroup
	for index, reader := range r.readers {
		if reader == nil {
			continue
		}
		waitGroup.Add(1)
		go func(index int, reader Reader) {
			defer waitGroup.Done()
			results[index].conversations, results[index].err = reader.List()
		}(index, reader)
	}
	waitGroup.Wait()

	var out []Conversation
	var readerErrors []string
	seen := map[string]bool{}
	for index, reader := range r.readers {
		if reader == nil {
			continue
		}
		if err := results[index].err; err != nil {
			readerErrors = append(readerErrors, string(reader.Harness())+": "+err.Error())
		}
		for _, conversation := range results[index].conversations {
			normalized, ok := normalize(conversation)
			if !ok || seen[normalized.Key()] {
				continue
			}
			seen[normalized.Key()] = true
			out = append(out, normalized)
		}
	}
	SortByRecency(out)
	if len(readerErrors) > 0 {
		return out, errors.New(strings.Join(readerErrors, "; "))
	}
	return out, nil
}

// Load resolves the transcript for a conversation via its harness reader.
func (r *Registry) Load(conversation Conversation) (Transcript, error) {
	reader, ok := r.ReaderFor(conversation.Harness)
	if !ok {
		return Transcript{}, errors.New("no conversation reader for harness " + string(conversation.Harness))
	}
	return reader.Load(conversation)
}

// SortByRecency orders conversations newest first with a stable tiebreak.
func SortByRecency(conversations []Conversation) {
	sort.SliceStable(conversations, func(i, j int) bool {
		if !conversations[i].UpdatedAt.Equal(conversations[j].UpdatedAt) {
			return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
		}
		if conversations[i].Harness != conversations[j].Harness {
			return conversations[i].Harness < conversations[j].Harness
		}
		return conversations[i].ID < conversations[j].ID
	})
}

func normalize(conversation Conversation) (Conversation, bool) {
	conversation.ID = strings.TrimSpace(conversation.ID)
	if conversation.ID == "" {
		return Conversation{}, false
	}
	harness, err := activity.NormalizeHarness(string(conversation.Harness))
	if err != nil || harness == "" {
		return Conversation{}, false
	}
	conversation.Harness = harness
	conversation.Title = CleanTitle(conversation.Title)
	if path, ok := activity.NormalizeDirectory(conversation.Path); ok {
		conversation.Path = path
	}
	conversation.UpdatedAt = conversation.UpdatedAt.UTC()
	return conversation, true
}

const maxTitleLength = 72

// harnessPreambleTags are wrapper elements that harnesses inject into the first
// user turn (environment info, plugin lists, instruction files). They are not
// what the user typed, so titles derived from a first turn strip them.
var harnessPreambleTags = []string{
	"environment_context",
	"recommended_plugins",
	"user_instructions",
	"instructions",
	"system_reminder",
	"system-reminder",
	"project_context",
	"ide_context",
	"command-message",
	"command-name",
	"local-command-stdout",
}

// harnessPreamblePrefixes mark a user turn that the harness generated rather
// than the user typing it. Such turns never make a useful title.
var harnessPreamblePrefixes = []string{
	"# agents.md instructions",
	"# claude.md instructions",
	"you are running as a scheduled",
	"caveat: the messages below",
	"<user_instructions>",
	"this session is being continued from",
}

// LooksLikePreamble reports whether a user turn is harness scaffolding rather
// than something the user actually wrote.
func LooksLikePreamble(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	if trimmed == "" {
		return true
	}
	for _, prefix := range harnessPreamblePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// userVisibleText removes injected preamble blocks so a title reflects the
// user's actual words rather than harness scaffolding.
func userVisibleText(raw string) string {
	text := raw
	for _, tag := range harnessPreambleTags {
		for {
			start := strings.Index(strings.ToLower(text), "<"+tag)
			if start < 0 {
				break
			}
			closing := "</" + tag + ">"
			end := strings.Index(strings.ToLower(text[start:]), closing)
			if end < 0 {
				// Unterminated block: drop the remainder, it is all preamble.
				text = text[:start]
				break
			}
			text = text[:start] + text[start+end+len(closing):]
		}
	}
	// A leading standalone tag with no close leaves stray angle content; trim
	// any remaining leading tag-only lines.
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(kept) == 0 && (trimmed == "" || (strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">"))) {
			continue
		}
		kept = append(kept, line)
	}
	if result := strings.TrimSpace(strings.Join(kept, "\n")); result != "" {
		return result
	}
	// Everything was preamble. Return empty so callers move on to a later turn
	// rather than titling the conversation with harness scaffolding.
	return ""
}

// TitleCandidate returns a usable title for a user turn, or "" when the turn is
// harness scaffolding and the caller should keep looking at later turns.
func TitleCandidate(raw string) string {
	cleaned := userVisibleText(raw)
	if LooksLikePreamble(cleaned) {
		return ""
	}
	return CleanTitle(cleaned)
}

// CleanTitle collapses a raw first-turn excerpt into a single readable line.
func CleanTitle(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return ""
	}
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > maxTitleLength {
		trimmed := title[:maxTitleLength]
		if index := strings.LastIndex(trimmed, " "); index > maxTitleLength/2 {
			trimmed = trimmed[:index]
		}
		title = strings.TrimSpace(trimmed) + "…"
	}
	return title
}

package conversations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"declaw/internal/paths"
)

// Settings is declaw's own per-conversation state. Agent stores are read-only
// to declaw, so pins, ignores and aliases live here instead, keyed by
// harness:session-id.
type Settings struct {
	Alias   string `json:"alias,omitempty"`
	Pinned  bool   `json:"pinned,omitempty"`
	Ignored bool   `json:"ignored,omitempty"`
	// LastCheckoutAt records the last time declaw opened this conversation.
	LastCheckoutAt time.Time `json:"last_checkout_at,omitempty"`
	// LastHarness is the harness declaw most recently opened it with, which can
	// differ from the origin harness after a cross-harness checkout.
	LastHarness string `json:"last_harness,omitempty"`
}

type settingsRegistry struct {
	Conversations map[string]Settings `json:"conversations"`
}

// Store persists per-conversation declaw settings.
type Store struct {
	path string
}

func NewStore() (*Store, error) {
	root, err := paths.RootDir()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(root, "conversations.json")}, nil
}

func (s *Store) load() (settingsRegistry, error) {
	registry := settingsRegistry{Conversations: map[string]Settings{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return registry, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, err
	}
	if registry.Conversations == nil {
		registry.Conversations = map[string]Settings{}
	}
	return registry, nil
}

func (s *Store) save(registry settingsRegistry) error {
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	// Write through a temporary file so an interrupted save cannot leave the
	// registry truncated.
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

// Get returns the stored settings for a conversation key.
func (s *Store) Get(key string) (Settings, error) {
	registry, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	return registry.Conversations[key], nil
}

// All returns every stored settings entry.
func (s *Store) All() (map[string]Settings, error) {
	registry, err := s.load()
	if err != nil {
		return nil, err
	}
	return registry.Conversations, nil
}

func (s *Store) update(key string, mutate func(*Settings)) (Settings, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Settings{}, errors.New("conversation key is required")
	}
	registry, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	settings := registry.Conversations[key]
	mutate(&settings)
	// Drop entries that carry no information so the registry does not grow a
	// row for every conversation the user merely opened.
	if settings == (Settings{}) {
		delete(registry.Conversations, key)
	} else {
		registry.Conversations[key] = settings
	}
	return settings, s.save(registry)
}

func (s *Store) SetPinned(key string, pinned bool) (Settings, error) {
	return s.update(key, func(settings *Settings) { settings.Pinned = pinned })
}

func (s *Store) SetIgnored(key string, ignored bool) (Settings, error) {
	return s.update(key, func(settings *Settings) { settings.Ignored = ignored })
}

func (s *Store) SetAlias(key, alias string) (Settings, error) {
	return s.update(key, func(settings *Settings) { settings.Alias = strings.TrimSpace(alias) })
}

// RecordCheckout notes that declaw opened a conversation, and with which
// harness. This is what lets a cross-harness chat keep showing up under the
// harness the user actually continued it in.
func (s *Store) RecordCheckout(key, harness string, at time.Time) error {
	_, err := s.update(key, func(settings *Settings) {
		settings.LastCheckoutAt = at.UTC()
		if strings.TrimSpace(harness) != "" {
			settings.LastHarness = strings.TrimSpace(harness)
		}
	})
	return err
}

// Decorated is a conversation joined with declaw's stored settings.
type Decorated struct {
	Conversation
	Settings Settings
}

// DisplayTitle prefers a user-set alias over the harness-derived title.
func (d Decorated) DisplayTitle() string {
	if strings.TrimSpace(d.Settings.Alias) != "" {
		return d.Settings.Alias
	}
	if strings.TrimSpace(d.Title) != "" {
		return d.Title
	}
	return "(untitled)"
}

// Decorate joins conversations with their stored settings and orders them the
// way checkout lists them: pinned first, then most recently active.
func Decorate(conversations []Conversation, stored map[string]Settings) []Decorated {
	out := make([]Decorated, 0, len(conversations))
	for _, conversation := range conversations {
		out = append(out, Decorated{
			Conversation: conversation,
			Settings:     stored[conversation.Key()],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Settings.Pinned != out[j].Settings.Pinned {
			return out[i].Settings.Pinned
		}
		left, right := out[i].UpdatedAt, out[j].UpdatedAt
		if out[i].Settings.LastCheckoutAt.After(left) {
			left = out[i].Settings.LastCheckoutAt
		}
		if out[j].Settings.LastCheckoutAt.After(right) {
			right = out[j].Settings.LastCheckoutAt
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// Visible drops conversations the user has ignored.
func Visible(decorated []Decorated) []Decorated {
	out := decorated[:0]
	for _, item := range decorated {
		if item.Settings.Ignored {
			continue
		}
		out = append(out, item)
	}
	return out
}

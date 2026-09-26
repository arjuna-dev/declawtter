package activity

import (
	"errors"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Harness is the normalized name used by declaw for an agent runtime.
type Harness string

const (
	Pi          Harness = "pi"
	Hermes      Harness = "hermes"
	Codex       Harness = "codex"
	Claude      Harness = "claude"
	OpenCode    Harness = "opencode"
	Antigravity Harness = "antigravity"
)

var supportedHarnesses = []Harness{Pi, Hermes, Codex, Claude, OpenCode, Antigravity}

// harnessAliases maps user-typed spellings onto canonical harness names.
var harnessAliases = map[string]Harness{
	"open-code": OpenCode,
	"open_code": OpenCode,
	"agy":       Antigravity,
	"anti":      Antigravity,
	"gravity":   Antigravity,
}

// Record is the normalized activity model shared by every source adapter.
// Adapters only read agent-owned data. Declaw stores the resulting summary in
// its own registry and never writes back to the source location.
type Record struct {
	Path       string
	Harness    Harness
	LastUsedAt time.Time
	Source     string
	SessionID  string
}

// Source is an independently replaceable activity adapter.
type Source interface {
	Name() string
	Discover() ([]Record, error)
}

// SourceFunc is useful for small adapters and tests.
type SourceFunc struct {
	SourceName string
	DiscoverFn func() ([]Record, error)
}

func (s SourceFunc) Name() string {
	return s.SourceName
}

func (s SourceFunc) Discover() ([]Record, error) {
	if s.DiscoverFn == nil {
		return nil, nil
	}
	return s.DiscoverFn()
}

// Detector runs all source adapters and folds duplicate observations into one
// latest observation per path and harness.
type Detector struct {
	sources []Source
}

func NewDetector(sources ...Source) *Detector {
	return &Detector{sources: append([]Source(nil), sources...)}
}

func DefaultDetector() *Detector {
	return NewDetector(defaultSources()...)
}

func SupportedHarnesses() []string {
	out := make([]string, 0, len(supportedHarnesses))
	for _, harness := range supportedHarnesses {
		out = append(out, string(harness))
	}
	return out
}

func SupportedHarnessValues() []Harness {
	return append([]Harness(nil), supportedHarnesses...)
}

func NormalizeHarness(value string) (Harness, error) {
	harness := Harness(strings.TrimSpace(strings.ToLower(value)))
	if harness == "" || harness == "inherit" || harness == "default" {
		return "", nil
	}
	if canonical, ok := harnessAliases[string(harness)]; ok {
		harness = canonical
	}
	for _, supported := range supportedHarnesses {
		if harness == supported {
			return supported, nil
		}
	}
	return "", errors.New("harness must be one of " + strings.Join(SupportedHarnesses(), ", "))
}

func IsSupportedHarness(value string) bool {
	_, err := NormalizeHarness(value)
	return err == nil && strings.TrimSpace(value) != "" && strings.ToLower(strings.TrimSpace(value)) != "inherit" && strings.ToLower(strings.TrimSpace(value)) != "default"
}

// InstalledHarnesses reports the supported runtimes currently resolvable from
// PATH. The returned order is stable and matches SupportedHarnesses.
func InstalledHarnesses() []string {
	installed := make([]string, 0, len(supportedHarnesses))
	for _, harness := range supportedHarnesses {
		if _, _, err := CommandForHarness(harness, ""); err == nil {
			installed = append(installed, string(harness))
		}
	}
	return installed
}

func IsInstalled(harness Harness) bool {
	_, _, err := CommandForHarness(harness, "")
	return err == nil
}

// CommandForHarness returns the native command used to open a harness. Pi and
// Hermes have multiple commonly installed command names, so the first command
// present on PATH wins.
func CommandForHarness(harness Harness, prompt string) (string, []string, error) {
	harness, err := NormalizeHarness(string(harness))
	if err != nil {
		return "", nil, err
	}
	if harness == "" {
		return "", nil, errors.New("harness is required")
	}

	candidates := map[Harness][]string{
		Pi:          {"pi"},
		Hermes:      {"hermes", "hermes-agent"},
		Codex:       {"codex"},
		Claude:      {"claude"},
		OpenCode:    {"opencode"},
		Antigravity: {"agy", "antigravity"},
	}
	var program string
	for _, candidate := range candidates[harness] {
		if _, err := exec.LookPath(candidate); err == nil {
			program = candidate
			break
		}
	}
	if program == "" {
		return "", nil, errors.New(string(harness) + " command not found in PATH")
	}
	args := []string{}
	switch harness {
	case Codex:
		args = []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}
	case Claude:
		args = []string{"--dangerously-skip-permissions", "--permission-mode", "bypassPermissions"}
	case OpenCode:
		args = []string{"--auto"}
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, strings.TrimSpace(prompt))
	}
	return program, args, nil
}

func (d *Detector) Discover() ([]Record, error) {
	if d == nil {
		return nil, nil
	}
	type sourceResult struct {
		records []Record
		err     error
	}
	results := make([]sourceResult, len(d.sources))
	var waitGroup sync.WaitGroup
	for index, source := range d.sources {
		if source == nil {
			continue
		}
		waitGroup.Add(1)
		go func(index int, source Source) {
			defer waitGroup.Done()
			results[index].records, results[index].err = source.Discover()
		}(index, source)
	}
	waitGroup.Wait()

	merged := map[string]Record{}
	var sourceErrors []string
	for index, source := range d.sources {
		if source == nil {
			continue
		}
		records, err := results[index].records, results[index].err
		if err != nil {
			sourceErrors = append(sourceErrors, source.Name()+": "+err.Error())
		}
		for _, record := range records {
			normalized, ok := normalizeRecord(record)
			if !ok {
				continue
			}
			key := string(normalized.Harness) + "\x00" + normalized.Path
			if previous, exists := merged[key]; !exists || normalized.LastUsedAt.After(previous.LastUsedAt) {
				merged[key] = normalized
			}
		}
	}

	out := make([]Record, 0, len(merged))
	for _, record := range merged {
		out = append(out, record)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastUsedAt.Equal(out[j].LastUsedAt) {
			if out[i].Path == out[j].Path {
				return out[i].Harness < out[j].Harness
			}
			return out[i].Path < out[j].Path
		}
		return out[i].LastUsedAt.After(out[j].LastUsedAt)
	})
	if len(sourceErrors) > 0 {
		return out, errors.New(strings.Join(sourceErrors, "; "))
	}
	return out, nil
}

func normalizeRecord(record Record) (Record, bool) {
	harness, err := NormalizeHarness(string(record.Harness))
	if err != nil || harness == "" {
		return Record{}, false
	}
	path, ok := normalizeDirectory(record.Path)
	if !ok {
		return Record{}, false
	}
	if shouldIgnoreActivityPath(harness, path) {
		return Record{}, false
	}
	if record.LastUsedAt.IsZero() {
		record.LastUsedAt = time.Unix(0, 0).UTC()
	}
	record.Path = path
	record.Harness = harness
	record.LastUsedAt = record.LastUsedAt.UTC()
	return record, true
}

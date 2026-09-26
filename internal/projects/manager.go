package projects

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"declaw/internal/activity"
	"declaw/internal/cliargs"
	"declaw/internal/paths"
	"declaw/internal/settings"
)

type Project struct {
	Name              string    `json:"name"`
	Alias             string    `json:"alias,omitempty"`
	Path              string    `json:"path"`
	Pinned            bool      `json:"pinned,omitempty"`
	Ignored           bool      `json:"ignored,omitempty"`
	Source            string    `json:"source,omitempty"`
	Harness           string    `json:"harness,omitempty"`
	LastHarness       string    `json:"last_harness,omitempty"`
	LastActivityAt    time.Time `json:"last_activity_at,omitempty"`
	ActivitySource    string    `json:"activity_source,omitempty"`
	ActivitySessionID string    `json:"activity_session_id,omitempty"`
	Provider          string    `json:"provider,omitempty"`
	CodexUI           string    `json:"codex_ui,omitempty"`
	ClaudeUI          string    `json:"claude_ui,omitempty"`
	Linked            bool      `json:"linked,omitempty"`
	Discovered        bool      `json:"discovered,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type Registry struct {
	Projects map[string]Project `json:"projects"`
}

type Manager struct {
	registryPath     string
	defaultParentDir string
}

func NewManager() (*Manager, error) {
	root, err := paths.RootDir()
	if err != nil {
		return nil, err
	}
	projectsRoot, err := paths.ProjectsDir()
	if err != nil {
		return nil, err
	}

	return &Manager{
		registryPath:     filepath.Join(root, "projects.json"),
		defaultParentDir: projectsRoot,
	}, nil
}

func (m *Manager) Create(args []string) (string, error) {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	into := fs.String("into", "", "target directory")
	path := fs.String("path", "", "register an existing project directory")
	harness := fs.String("harness", "", "default harness for the project")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{
		"into":    true,
		"path":    true,
		"harness": true,
	})); err != nil {
		return "", err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw create <name> [--into <dir> | --path <dir>] [--harness <name>]")
	}

	name := sanitizeName(rest[0])
	if name == "" {
		return "", errors.New("project name must contain at least one alphanumeric character")
	}
	selectedHarness, err := activity.NormalizeHarness(*harness)
	if err != nil {
		return "", err
	}
	intoValue := strings.TrimSpace(*into)
	pathValue := strings.TrimSpace(*path)
	if intoValue != "" && pathValue != "" {
		return "", errors.New("--into and --path cannot be used together")
	}

	target := ""
	linked := false
	if pathValue != "" {
		target, err = canonicalDirectory(pathValue)
		if err != nil {
			return "", err
		}
		linked = true
	} else {
		parent := intoValue
		if parent == "" {
			parent = m.defaultParentDir
		}
		parent, err = filepath.Abs(parent)
		if err != nil {
			return "", err
		}
		target = filepath.Join(parent, name)
		if _, err := os.Stat(target); err == nil {
			return "", fmt.Errorf("target already exists: %s", target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}

	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}
	if _, exists := registry.Projects[name]; exists {
		return "", fmt.Errorf("project %q already exists in registry", name)
	}

	if !linked {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", err
		}
	}

	project := Project{
		Name:      name,
		Path:      target,
		Source:    "directory",
		Harness:   string(selectedHarness),
		Provider:  string(selectedHarness),
		Linked:    linked,
		CreatedAt: time.Now().UTC(),
	}
	registry.Projects[name] = project
	if err := m.saveRegistry(registry); err != nil {
		return "", err
	}

	return fmt.Sprintf("created %s\n%s\nharness: %s", name, target, displayHarness(string(selectedHarness))), nil
}

func (m *Manager) Template(args []string) (string, error) {
	fs := flag.NewFlagSet("template", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return "", err
	}

	if len(fs.Args()) != 0 {
		return "", errors.New("usage: declaw template")
	}
	return "project creation makes or registers an empty directory; declaw does not copy a workspace template.", nil
}

func (m *Manager) Track(args []string) (string, error) {
	fs := flag.NewFlagSet("track", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	path := fs.String("path", "", "existing project directory")
	harness := fs.String("harness", "", "default harness for the project")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{
		"path":    true,
		"harness": true,
	})); err != nil {
		return "", err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw track <name> --path <dir>")
	}
	if strings.TrimSpace(*path) == "" {
		return "", errors.New("--path is required")
	}
	selectedHarness, err := activity.NormalizeHarness(*harness)
	if err != nil {
		return "", err
	}

	name := sanitizeName(rest[0])
	if name == "" {
		return "", errors.New("project name must contain at least one alphanumeric character")
	}

	absPath, err := canonicalDirectory(*path)
	if err != nil {
		return "", err
	}

	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}
	if _, exists := registry.Projects[name]; exists {
		return "", fmt.Errorf("project %q already exists in registry", name)
	}

	project := Project{
		Name:      name,
		Path:      absPath,
		Source:    "tracked",
		Harness:   string(selectedHarness),
		Provider:  string(selectedHarness),
		Linked:    true,
		CreatedAt: time.Now().UTC(),
	}
	registry.Projects[name] = project
	if err := m.saveRegistry(registry); err != nil {
		return "", err
	}

	return fmt.Sprintf("tracked %s\n%s\nharness: %s", name, absPath, displayHarness(string(selectedHarness))), nil
}

func (m *Manager) List(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("usage: declaw list")
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}
	if len(registry.Projects) == 0 {
		return "no tracked projects", nil
	}

	names := make([]string, 0, len(registry.Projects))
	for name := range registry.Projects {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		project := registry.Projects[name]
		lines = append(lines, fmt.Sprintf("%s\t%s", project.DisplayName(), project.Path))
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) Projects() ([]Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(registry.Projects))
	for name := range registry.Projects {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Project, 0, len(names))
	for _, name := range names {
		out = append(out, registry.Projects[name])
	}
	return out, nil
}

// RecentProjects returns tracked and automatically discovered projects in the
// order used by project settings and checkout. Pinned projects come first;
// within each pin group the newest detected agent activity wins.
func (m *Manager) RecentProjects() ([]Project, error) {
	projects, err := m.Projects()
	if err != nil {
		return nil, err
	}
	visible := projects[:0]
	for _, project := range projects {
		if project.Discovered && project.Linked && activity.IsInternalActivityPath(project.Path) {
			continue
		}
		visible = append(visible, project)
	}
	projects = visible
	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].Pinned != projects[j].Pinned {
			return projects[i].Pinned
		}
		if projects[i].LastActivityAt.IsZero() != projects[j].LastActivityAt.IsZero() {
			return !projects[i].LastActivityAt.IsZero()
		}
		if !projects[i].LastActivityAt.Equal(projects[j].LastActivityAt) {
			return projects[i].LastActivityAt.After(projects[j].LastActivityAt)
		}
		if !projects[i].CreatedAt.Equal(projects[j].CreatedAt) {
			return projects[i].CreatedAt.After(projects[j].CreatedAt)
		}
		return projects[i].DisplayName() < projects[j].DisplayName()
	})
	return projects, nil
}

// CheckoutProjects returns recent projects that have not been explicitly
// ignored. Ignored projects stay in project settings so they can be restored.
func (m *Manager) CheckoutProjects() ([]Project, error) {
	projects, err := m.RecentProjects()
	if err != nil {
		return nil, err
	}
	visible := projects[:0]
	for _, project := range projects {
		if project.Ignored {
			continue
		}
		if project.Discovered && project.Linked && activity.IsInternalActivityPath(project.Path) {
			continue
		}
		visible = append(visible, project)
	}
	return visible, nil
}

// DisplayName is intentionally separate from Name. Name is the stable,
// sanitized registry identifier, while Alias is user-facing presentation.
func (p Project) DisplayName() string {
	if strings.TrimSpace(p.Alias) != "" {
		return p.Alias
	}
	if isOpaqueProjectName(p.Name) {
		pathName := filepath.Base(filepath.Clean(strings.TrimSpace(p.Path)))
		if pathName != "." && pathName != string(filepath.Separator) && strings.TrimSpace(pathName) != "" && !isOpaqueProjectName(pathName) {
			return pathName
		}
	}
	if strings.TrimSpace(p.Name) == "" {
		pathName := filepath.Base(filepath.Clean(strings.TrimSpace(p.Path)))
		if pathName != "." && pathName != string(filepath.Separator) && strings.TrimSpace(pathName) != "" {
			return pathName
		}
	}
	return p.Name
}

func (m *Manager) Path(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw path <name>")
	}
	project, err := m.Get(args[0])
	if err != nil {
		return "", err
	}
	return project.Path, nil
}

func (m *Manager) Settings(args []string) (string, error) {
	usage := "usage: declaw project-settings <name> harness [pi|hermes|codex|claude|inherit]\n       declaw project-settings <name> give alias <display name>\n       declaw project-settings <name> pin [on|off]\n       declaw project-settings <name> ignore [on|off]\n       declaw project-settings <name> path\n       declaw project-settings <name> remove"
	legacyUsage := "usage: declaw project settings <name> provider [pi|hermes|codex|claude|inherit]\n       declaw project settings <name> codex-ui [app-server|codex|inherit]\n       declaw project settings <name> claude-ui [claude|print|inherit]"
	if len(args) == 0 || hasProjectHelpArg(args[0]) {
		return usage, nil
	}
	if len(args) < 2 {
		return "", errors.New(usage)
	}
	project, err := m.Get(args[0])
	if err != nil {
		return "", err
	}
	key := strings.ToLower(strings.TrimSpace(args[1]))
	if key == "give" && len(args) >= 3 && strings.EqualFold(args[2], "alias") {
		key = "alias"
		args = append([]string{args[0], "alias"}, args[3:]...)
	}

	if len(args) == 2 {
		switch key {
		case "harness", "provider":
			value := project.Harness
			if value == "" {
				value = project.Provider
			}
			if value == "" {
				return "inherit", nil
			}
			return value, nil
		case "alias":
			if strings.TrimSpace(project.Alias) == "" {
				return "none", nil
			}
			return project.Alias, nil
		case "pin", "pinned":
			return yesNo(project.Pinned), nil
		case "ignore", "ignored":
			return yesNo(project.Ignored), nil
		case "path":
			return project.Path, nil
		case "remove":
			return m.Remove([]string{project.Name})
		case "codex-ui":
			if project.CodexUI == "" {
				return "inherit", nil
			}
			return project.CodexUI, nil
		case "claude-ui":
			if project.ClaudeUI == "" {
				return "inherit", nil
			}
			return project.ClaudeUI, nil
		default:
			return "", fmt.Errorf("unknown project settings key %q\n\n%s", key, usage)
		}
	}

	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}
	current, ok := registry.Projects[project.Name]
	if !ok {
		return "", fmt.Errorf("unknown project %q", project.Name)
	}
	switch key {
	case "harness", "provider":
		if len(args) != 3 {
			return "", errors.New(usage)
		}
		value, err := activity.NormalizeHarness(args[2])
		if err != nil {
			return "", err
		}
		current.Harness = string(value)
		current.Provider = string(value)
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		if key == "provider" {
			if value == "" {
				return fmt.Sprintf("project %s provider: inherit", project.Name), nil
			}
			return fmt.Sprintf("project %s provider: %s", project.Name, value), nil
		}
		return fmt.Sprintf("project %s harness: %s", project.DisplayName(), displayHarness(string(value))), nil
	case "alias":
		if len(args) < 3 {
			return "", errors.New("usage: declaw project-settings <name> give alias <display name>")
		}
		value := strings.TrimSpace(strings.Join(args[2:], " "))
		if strings.ContainsAny(value, "\r\n") {
			return "", errors.New("alias must be a single line")
		}
		if strings.EqualFold(value, "none") || strings.EqualFold(value, "clear") {
			value = ""
		}
		if err := m.validateAlias(registry, project.Name, value); err != nil {
			return "", err
		}
		current.Alias = value
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		if value == "" {
			return fmt.Sprintf("project %s alias cleared", project.Name), nil
		}
		return fmt.Sprintf("project %s alias: %s", project.Name, value), nil
	case "pin", "pinned":
		if len(args) != 3 {
			return "", errors.New(usage)
		}
		enabled, err := parseBooleanSetting(args[2], current.Pinned)
		if err != nil {
			return "", err
		}
		current.Pinned = enabled
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		return fmt.Sprintf("project %s pinned: %s", current.DisplayName(), yesNo(enabled)), nil
	case "ignore", "ignored":
		if len(args) != 3 {
			return "", errors.New(usage)
		}
		enabled, err := parseBooleanSetting(args[2], current.Ignored)
		if err != nil {
			return "", err
		}
		current.Ignored = enabled
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		return fmt.Sprintf("project %s ignored: %s", current.DisplayName(), yesNo(enabled)), nil
	case "codex-ui":
		if len(args) != 3 {
			return "", errors.New(legacyUsage)
		}
		value := normalizeProjectUIValue(args[2])
		if value != "" {
			if err := settings.ValidateCodexUI(value); err != nil {
				return "", err
			}
		}
		current.CodexUI = value
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		if value == "" {
			return fmt.Sprintf("project %s codex-ui: inherit", project.Name), nil
		}
		return fmt.Sprintf("project %s codex-ui: %s", project.Name, value), nil
	case "claude-ui":
		if len(args) != 3 {
			return "", errors.New(legacyUsage)
		}
		value := normalizeProjectUIValue(args[2])
		if value != "" {
			if err := settings.ValidateClaudeUI(value); err != nil {
				return "", err
			}
		}
		current.ClaudeUI = value
		registry.Projects[project.Name] = current
		if err := m.saveRegistry(registry); err != nil {
			return "", err
		}
		if value == "" {
			return fmt.Sprintf("project %s claude-ui: inherit", project.Name), nil
		}
		return fmt.Sprintf("project %s claude-ui: %s", project.Name, value), nil
	default:
		return "", fmt.Errorf("unknown project settings key %q\n\n%s", key, usage)
	}
}

func (m *Manager) Remove(args []string) (string, error) {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "skip confirmation")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw remove <name> [--yes]")
	}

	project, err := m.Get(rest[0])
	if err != nil {
		return "", err
	}
	name := project.Name
	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}

	project, ok := registry.Projects[name]
	if !ok {
		return "", fmt.Errorf("unknown project %q", name)
	}
	if !*yes {
		if !stdinIsTerminal() {
			return "", errors.New("confirmation required; rerun with --yes")
		}
		action := "remove"
		impact := "This will delete the tracked project directory."
		if project.Linked {
			action = "untrack"
			impact = "This will only remove it from declaw; the directory will stay on disk."
		}
		prompt := fmt.Sprintf("%s project %s at %s? %s [y/N]", strings.Title(action), project.Name, project.Path, impact)
		if !askYesNo(prompt) {
			return "aborted", nil
		}
	}

	if !project.Linked {
		if err := os.RemoveAll(project.Path); err != nil {
			return "", err
		}
	}
	delete(registry.Projects, name)
	if err := m.saveRegistry(registry); err != nil {
		return "", err
	}
	if project.Linked {
		return fmt.Sprintf("untracked %s", name), nil
	}
	return fmt.Sprintf("removed %s", name), nil
}

func (m *Manager) Get(name string) (Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	if project, ok := registry.Projects[sanitizeName(name)]; ok {
		return project, nil
	}
	needle := strings.TrimSpace(name)
	for _, project := range registry.Projects {
		if strings.EqualFold(project.Alias, needle) || strings.EqualFold(project.DisplayName(), needle) {
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("unknown project %q", name)
}

// MergeActivities updates declaw's own registry with read-only observations
// from agent adapters. New observations become linked projects, so removing a
// discovered project never deletes its directory.
func (m *Manager) MergeActivities(records []activity.Record) error {
	if len(records) == 0 {
		return nil
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return err
	}
	changed := false
	for _, record := range records {
		path, ok := normalizedDirectory(record.Path)
		if !ok {
			continue
		}
		if activity.IsInternalActivityPath(path) {
			continue
		}
		selectedHarness, err := activity.NormalizeHarness(string(record.Harness))
		if err != nil || selectedHarness == "" {
			continue
		}
		name, project, found := findProjectByPath(registry.Projects, path)
		if record.SessionID != "" {
			sessionName, sessionProject, sessionFound := findProjectByActivitySession(registry.Projects, record.SessionID)
			if sessionFound && (!found || sessionName != name) {
				if !found || isStaleGeneratedProject(sessionProject) {
					if found {
						project = mergeProjectPreferences(project, sessionProject)
						delete(registry.Projects, sessionName)
						changed = true
					} else {
						name, project, found = sessionName, sessionProject, true
					}
				}
			}
		}
		if !found {
			name = uniqueProjectName(registry.Projects, sanitizeName(filepath.Base(path)))
			project = Project{
				Name:       name,
				Path:       path,
				Source:     "activity:" + strings.TrimSpace(record.Source),
				Linked:     true,
				Discovered: true,
				CreatedAt:  record.LastUsedAt,
			}
			if project.CreatedAt.IsZero() {
				project.CreatedAt = time.Now().UTC()
			}
		}
		if record.LastUsedAt.After(project.LastActivityAt) || shouldReconcileActivityTimestamp(project, record) {
			project.LastActivityAt = record.LastUsedAt.UTC()
			project.LastHarness = string(selectedHarness)
			project.ActivitySource = record.Source
			changed = true
		}
		if project.Source == "" {
			project.Source = "activity:" + strings.TrimSpace(record.Source)
			changed = true
		}
		if project.Path != path {
			project.Path = path
			changed = true
		}
		if project.Name == "" {
			project.Name = name
			changed = true
		}
		if record.SessionID != "" && project.ActivitySessionID != record.SessionID {
			project.ActivitySessionID = record.SessionID
			changed = true
		}
		if !found {
			registry.Projects[name] = project
			changed = true
		} else if current, ok := registry.Projects[name]; !ok || current != project {
			registry.Projects[name] = project
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return m.saveRegistry(registry)
}

func shouldReconcileActivityTimestamp(project Project, record activity.Record) bool {
	if !project.Discovered || !project.Linked || record.LastUsedAt.IsZero() || record.LastUsedAt.Equal(time.Unix(0, 0).UTC()) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(project.ActivitySource), strings.TrimSpace(record.Source)) && strings.EqualFold(strings.TrimSpace(record.Source), "claude local files")
}

func (m *Manager) SetHarness(name, value string) (Project, error) {
	harness, err := activity.NormalizeHarness(value)
	if err != nil {
		return Project{}, err
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	project, err := getFromRegistry(registry, name)
	if err != nil {
		return Project{}, err
	}
	project.Harness = string(harness)
	project.Provider = string(harness)
	registry.Projects[project.Name] = project
	if err := m.saveRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (m *Manager) SetAlias(name, alias string) (Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	project, err := getFromRegistry(registry, name)
	if err != nil {
		return Project{}, err
	}
	alias = strings.TrimSpace(alias)
	if strings.ContainsAny(alias, "\r\n") {
		return Project{}, errors.New("alias must be a single line")
	}
	if err := m.validateAlias(registry, project.Name, alias); err != nil {
		return Project{}, err
	}
	project.Alias = alias
	registry.Projects[project.Name] = project
	if err := m.saveRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (m *Manager) SetPinned(name string, pinned bool) (Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	project, err := getFromRegistry(registry, name)
	if err != nil {
		return Project{}, err
	}
	project.Pinned = pinned
	registry.Projects[project.Name] = project
	if err := m.saveRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (m *Manager) SetIgnored(name string, ignored bool) (Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	project, err := getFromRegistry(registry, name)
	if err != nil {
		return Project{}, err
	}
	project.Ignored = ignored
	registry.Projects[project.Name] = project
	if err := m.saveRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

// RecordActivity records a declaw checkout in the same normalized fields used
// by external activity adapters. It is a declaw-owned summary, not a source
// session or transcript.
func (m *Manager) RecordActivity(name, harness string, usedAt time.Time, source string) error {
	selected, err := activity.NormalizeHarness(harness)
	if err != nil {
		return err
	}
	if selected == "" {
		return errors.New("harness is required")
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return err
	}
	project, err := getFromRegistry(registry, name)
	if err != nil {
		return err
	}
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	if usedAt.After(project.LastActivityAt) || project.LastHarness == "" {
		project.LastActivityAt = usedAt.UTC()
		project.LastHarness = string(selected)
		project.ActivitySource = source
		registry.Projects[project.Name] = project
		return m.saveRegistry(registry)
	}
	return nil
}

// RecordActivityAtPath updates a project when a declaw-owned launch has a
// concrete workspace path. It intentionally does not create a project for a
// scratch workspace; external adapters handle discovery for real agent state.
func (m *Manager) RecordActivityAtPath(path, harness string, usedAt time.Time, source string) error {
	canonical, err := canonicalDirectory(path)
	if err != nil {
		return nil
	}
	selected, err := activity.NormalizeHarness(harness)
	if err != nil || selected == "" {
		return err
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return err
	}
	name, project, found := findProjectByPath(registry.Projects, canonical)
	if !found {
		return nil
	}
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	if !usedAt.After(project.LastActivityAt) && project.LastHarness != "" {
		return nil
	}
	project.LastActivityAt = usedAt.UTC()
	project.LastHarness = string(selected)
	project.ActivitySource = source
	registry.Projects[name] = project
	return m.saveRegistry(registry)
}

func (m *Manager) validateAlias(registry Registry, projectName, alias string) error {
	if strings.TrimSpace(alias) == "" {
		return nil
	}
	for key, project := range registry.Projects {
		if key == projectName {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(project.Alias), alias) || strings.EqualFold(project.Name, alias) || sanitizeName(alias) == key {
			return fmt.Errorf("alias %q is already used by project %q", alias, project.DisplayName())
		}
	}
	return nil
}

func (m *Manager) loadRegistry() (Registry, error) {
	raw, err := os.ReadFile(m.registryPath)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{Projects: map[string]Project{}}, nil
	}
	if err != nil {
		return Registry{}, err
	}

	var registry Registry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return Registry{}, err
	}
	if registry.Projects == nil {
		registry.Projects = map[string]Project{}
	}
	for key, project := range registry.Projects {
		if strings.TrimSpace(project.Name) == "" {
			project.Name = key
		}
		project.Provider = normalizeProjectProvider(project.Provider)
		project.Harness = normalizeProjectProvider(project.Harness)
		if project.Harness == "" {
			project.Harness = project.Provider
		}
		if project.Provider == "" {
			project.Provider = project.Harness
		}
		for fieldName, value := range map[string]string{"provider": project.Provider, "harness": project.Harness} {
			if value == "" {
				continue
			}
			if err := settings.ValidateProvider(value); err != nil {
				return Registry{}, fmt.Errorf("project %q has invalid %s: %w", key, fieldName, err)
			}
		}
		registry.Projects[key] = project
	}
	return registry, nil
}

func (m *Manager) saveRegistry(registry Registry) error {
	if registry.Projects == nil {
		registry.Projects = map[string]Project{}
	}
	for key, project := range registry.Projects {
		project.Provider = normalizeProjectProvider(project.Provider)
		project.Harness = normalizeProjectProvider(project.Harness)
		if project.Harness == "" {
			project.Harness = project.Provider
		}
		if project.Provider == "" {
			project.Provider = project.Harness
		}
		for fieldName, value := range map[string]string{"provider": project.Provider, "harness": project.Harness} {
			if value == "" {
				continue
			}
			if err := settings.ValidateProvider(value); err != nil {
				return fmt.Errorf("project %q has invalid %s: %w", key, fieldName, err)
			}
		}
		registry.Projects[key] = project
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(m.registryPath, data, 0o644)
}

func getFromRegistry(registry Registry, name string) (Project, error) {
	if project, ok := registry.Projects[sanitizeName(name)]; ok {
		return project, nil
	}
	needle := strings.TrimSpace(name)
	for _, project := range registry.Projects {
		if strings.EqualFold(project.Alias, needle) || strings.EqualFold(project.DisplayName(), needle) {
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("unknown project %q", name)
}

func findProjectByPath(registry map[string]Project, path string) (string, Project, bool) {
	for name, project := range registry {
		projectPath, ok := normalizedDirectory(project.Path)
		if ok && projectPath == path {
			return name, project, true
		}
	}
	return "", Project{}, false
}

func findProjectByActivitySession(registry map[string]Project, sessionID string) (string, Project, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", Project{}, false
	}
	for name, project := range registry {
		if project.ActivitySessionID == sessionID || name == sessionID || project.Name == sessionID {
			return name, project, true
		}
	}
	return "", Project{}, false
}

func isStaleGeneratedProject(project Project) bool {
	if !project.Discovered || !project.Linked {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(project.Source))
	if !strings.HasPrefix(source, "activity:codex") {
		return false
	}
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(project.Path)))
	return strings.Contains(path, "/.codex/visualizations/") || strings.Contains(path, "/.codex/attachments/")
}

func mergeProjectPreferences(target, stale Project) Project {
	if strings.TrimSpace(target.Alias) == "" {
		target.Alias = stale.Alias
	}
	target.Pinned = target.Pinned || stale.Pinned
	target.Ignored = target.Ignored || stale.Ignored
	return target
}

func uniqueProjectName(registry map[string]Project, base string) string {
	if base == "" {
		base = "project"
	}
	if _, exists := registry[base]; !exists {
		return base
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s-%d", base, index)
		if _, exists := registry[candidate]; !exists {
			return candidate
		}
	}
}

var uuidProjectNamePattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isOpaqueProjectName(value string) bool {
	return uuidProjectNamePattern.MatchString(strings.TrimSpace(value))
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func parseBooleanSetting(value string, current bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "yes", "y", "1", "enable", "enabled":
		return true, nil
	case "off", "false", "no", "n", "0", "disable", "disabled":
		return false, nil
	case "toggle":
		return !current, nil
	default:
		return false, fmt.Errorf("setting value must be on, off, or toggle; got %q", value)
	}
}

func normalizedDirectory(value string) (string, bool) {
	path, err := canonicalDirectory(value)
	if err != nil {
		return "", false
	}
	return path, true
}

func canonicalDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("project path is required")
	}
	absPath, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("project path is not accessible: %s", absPath)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path is not a directory: %s", absPath)
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = filepath.Clean(resolved)
	}
	return absPath, nil
}

func displayHarness(value string) string {
	if strings.TrimSpace(value) == "" {
		return "inherit"
	}
	return value
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-.")
}

// SanitizeName exposes the stable project identifier normalization to the
// interactive command builder.
func SanitizeName(value string) string {
	return sanitizeName(value)
}

func normalizeProjectProvider(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "inherit" {
		return ""
	}
	return value
}

func normalizeProjectUIValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "inherit" {
		return ""
	}
	return value
}

func hasProjectHelpArg(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func askYesNo(prompt string) bool {
	fmt.Printf("%s ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes"
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

package projects

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"declaw/internal/cliargs"
)

type Project struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type Registry struct {
	Projects map[string]Project `json:"projects"`
}

type Manager struct {
	registryPath string
}

func NewManager() (*Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(configDir, "declaw")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	return &Manager{
		registryPath: filepath.Join(root, "projects.json"),
	}, nil
}

func (m *Manager) Create(args []string) (string, error) {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	into := fs.String("into", "", "target directory")
	source := fs.String("source", "", "template source directory")
	if err := fs.Parse(cliargs.ReorderForFlagSet(args, map[string]bool{
		"into":   true,
		"source": true,
	})); err != nil {
		return "", err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return "", errors.New("usage: declaw create <name> [--into <dir>] [--source <dir>]")
	}

	name := sanitizeName(rest[0])
	if name == "" {
		return "", errors.New("project name must contain at least one alphanumeric character")
	}
	sourceRoot := strings.TrimSpace(*source)
	var err error
	if sourceRoot == "" {
		sourceRoot, err = detectSourceRoot()
		if err != nil {
			return "", err
		}
	}

	sourceRoot, err = filepath.Abs(sourceRoot)
	if err != nil {
		return "", err
	}

	parent := strings.TrimSpace(*into)
	if parent == "" {
		parent = filepath.Dir(sourceRoot)
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", err
	}

	target := filepath.Join(parent, name)
	if relTarget, err := filepath.Rel(sourceRoot, target); err == nil {
		if relTarget == "." || (!strings.HasPrefix(relTarget, ".."+string(os.PathSeparator)) && relTarget != "..") {
			return "", fmt.Errorf("target must not be inside the source template tree: %s", target)
		}
	}
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("target already exists: %s", target)
	}

	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}
	if _, exists := registry.Projects[name]; exists {
		return "", fmt.Errorf("project %q already exists in registry", name)
	}

	if err := copyTree(sourceRoot, target, sourceRoot); err != nil {
		return "", err
	}

	project := Project{
		Name:      name,
		Path:      target,
		Source:    sourceRoot,
		CreatedAt: time.Now().UTC(),
	}
	registry.Projects[name] = project
	if err := m.saveRegistry(registry); err != nil {
		return "", err
	}

	return fmt.Sprintf("created %s\n%s", name, target), nil
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
		lines = append(lines, fmt.Sprintf("%s\t%s", project.Name, project.Path))
	}
	return strings.Join(lines, "\n"), nil
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

func (m *Manager) Remove(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: declaw remove <name>")
	}

	name := sanitizeName(args[0])
	registry, err := m.loadRegistry()
	if err != nil {
		return "", err
	}

	project, ok := registry.Projects[name]
	if !ok {
		return "", fmt.Errorf("unknown project %q", name)
	}

	if err := os.RemoveAll(project.Path); err != nil {
		return "", err
	}
	delete(registry.Projects, name)
	if err := m.saveRegistry(registry); err != nil {
		return "", err
	}
	return fmt.Sprintf("removed %s", name), nil
}

func (m *Manager) Get(name string) (Project, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return Project{}, err
	}
	project, ok := registry.Projects[sanitizeName(name)]
	if !ok {
		return Project{}, fmt.Errorf("unknown project %q", name)
	}
	return project, nil
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
	return registry, nil
}

func (m *Manager) saveRegistry(registry Registry) error {
	if registry.Projects == nil {
		registry.Projects = map[string]Project{}
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(m.registryPath, data, 0o644)
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

func detectSourceRoot() (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe), filepath.Dir(filepath.Dir(exe)))
	}

	for _, candidate := range candidates {
		root, ok := findTemplateRoot(candidate)
		if ok {
			return root, nil
		}
	}
	return "", errors.New("could not detect workspace template root; pass --source explicitly")
}

func findTemplateRoot(start string) (string, bool) {
	current := start
	for {
		readme := filepath.Join(current, "README.md")
		workspace := filepath.Join(current, "WORKSPACE")
		if fileExists(readme) && dirExists(workspace) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyTree(sourceRoot, targetRoot, current string) error {
	info, err := os.Stat(current)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(sourceRoot, current)
	if err != nil {
		return err
	}

	if shouldSkip(rel, info) {
		return nil
	}

	targetPath := targetRoot
	if rel != "." {
		targetPath = filepath.Join(targetRoot, rel)
	}

	if info.IsDir() {
		if err := os.MkdirAll(targetPath, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(sourceRoot, targetRoot, filepath.Join(current, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	return copyFile(current, targetPath, info.Mode())
}

func shouldSkip(rel string, info os.FileInfo) bool {
	base := info.Name()
	if base == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
		return true
	}
	if base == ".DS_Store" {
		return true
	}
	if base == "bin" && info.IsDir() {
		return true
	}
	return false
}

func copyFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

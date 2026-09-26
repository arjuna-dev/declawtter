package projects

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"declaw/internal/activity"
)

func TestCreateMakesOnlyTheProjectDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	output, err := manager.Create([]string{"empty-project", "--into", parent, "--harness", "pi"})
	if err != nil {
		t.Fatal(err)
	}
	if output == "" {
		t.Fatal("Create returned an empty confirmation")
	}
	project, err := manager.Get("empty-project")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("created project contains files: %#v", entries)
	}
	if project.Harness != "pi" || project.Provider != "pi" {
		t.Fatalf("project harness fields = %#v, want pi", project)
	}
}

func TestAliasDoesNotChangeCanonicalPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	canonicalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Track([]string{"canonical-name", "--path", projectDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settings([]string{"canonical-name", "give", "alias", "Customer Portal"}); err != nil {
		t.Fatal(err)
	}
	project, err := manager.Get("Customer Portal")
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "canonical-name" || project.Path != canonicalProjectDir || project.DisplayName() != "Customer Portal" {
		t.Fatalf("aliased project = %#v", project)
	}
	if got, err := manager.Path([]string{"Customer Portal"}); err != nil || got != canonicalProjectDir {
		t.Fatalf("Path(alias) = %q, %v; want %q", got, err, canonicalProjectDir)
	}
}

func TestMergeActivitiesDiscoversAndRanksProjects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	olderDir := t.TempDir()
	newerDir := t.TempDir()
	canonicalNewerDir, err := filepath.EvalSymlinks(newerDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.MergeActivities([]activity.Record{
		{Path: olderDir, Harness: activity.Claude, LastUsedAt: time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC), Source: "Claude Code"},
		{Path: newerDir, Harness: activity.Hermes, LastUsedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC), Source: "Hermes"},
	}); err != nil {
		t.Fatal(err)
	}
	recent, err := manager.RecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Path != filepath.Clean(canonicalNewerDir) {
		t.Fatalf("recent projects = %#v", recent)
	}
	if !recent[0].Discovered || recent[0].LastHarness != "hermes" {
		t.Fatalf("discovered project metadata = %#v", recent[0])
	}
}

func TestProjectSettingsPersistPinAndIgnore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if _, err := manager.Track([]string{"demo", "--path", projectDir}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Settings([]string{"demo", "pin", "on"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settings([]string{"demo", "ignore", "on"}); err != nil {
		t.Fatal(err)
	}
	project, err := manager.Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !project.Pinned || !project.Ignored {
		t.Fatalf("project flags = %#v, want pinned and ignored", project)
	}
	if got, err := manager.Settings([]string{"demo", "pin"}); err != nil || got != "yes" {
		t.Fatalf("pin setting = %q, %v; want yes", got, err)
	}
	if got, err := manager.Settings([]string{"demo", "ignore"}); err != nil || got != "yes" {
		t.Fatalf("ignore setting = %q, %v; want yes", got, err)
	}
	checkout, err := manager.CheckoutProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(checkout) != 0 {
		t.Fatalf("checkout projects = %#v, want ignored project filtered", checkout)
	}

	if _, err := manager.Settings([]string{"demo", "ignore", "off"}); err != nil {
		t.Fatal(err)
	}
	checkout, err = manager.CheckoutProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(checkout) != 1 || checkout[0].Name != "demo" {
		t.Fatalf("checkout projects after unignore = %#v, want demo", checkout)
	}
}

func TestRecentProjectsPinsBeforeActivityAndSortsTheRest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	pathsByName := map[string]string{
		"older":  t.TempDir(),
		"newer":  t.TempDir(),
		"pinned": t.TempDir(),
	}
	for name, path := range pathsByName {
		if _, err := manager.Track([]string{name, "--path", path}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.MergeActivities([]activity.Record{
		{Path: pathsByName["older"], Harness: activity.Codex, LastUsedAt: time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC), Source: "Codex"},
		{Path: pathsByName["newer"], Harness: activity.Claude, LastUsedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC), Source: "Claude Code"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settings([]string{"pinned", "pin", "on"}); err != nil {
		t.Fatal(err)
	}

	recent, err := manager.RecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 {
		t.Fatalf("recent projects = %#v, want 3 projects", recent)
	}
	gotNames := []string{recent[0].Name, recent[1].Name, recent[2].Name}
	wantNames := []string{"pinned", "newer", "older"}
	for index := range wantNames {
		if gotNames[index] != wantNames[index] {
			t.Fatalf("recent project order = %#v, want %#v", gotNames, wantNames)
		}
	}
}

func TestDisplayNameFallsBackToDirectoryForUUIDProject(t *testing.T) {
	project := Project{
		Name: "019fc67e-4526-7380-b508-87a5ea778130",
		Path: "/Users/example/Customer-Portal",
	}
	if got, want := project.DisplayName(), "Customer-Portal"; got != want {
		t.Fatalf("DisplayName() = %q, want %q", got, want)
	}
}

func TestMergeActivitiesReconcilesStaleCodexVisualizationProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	realProjectDir := t.TempDir()
	canonicalRealProjectDir, err := filepath.EvalSymlinks(realProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	staleProjectDir := filepath.Join(t.TempDir(), ".codex", "visualizations", "019fc67e-4526-7380-b508-87a5ea778130")
	if err := os.MkdirAll(staleProjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Track([]string{"real-project", "--path", realProjectDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Track([]string{"019fc67e-4526-7380-b508-87a5ea778130", "--path", staleProjectDir}); err != nil {
		t.Fatal(err)
	}
	registry, err := manager.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	stale := registry.Projects["019fc67e-4526-7380-b508-87a5ea778130"]
	stale.Source = "activity:codex local files"
	stale.Discovered = true
	registry.Projects[stale.Name] = stale
	if err := manager.saveRegistry(registry); err != nil {
		t.Fatal(err)
	}

	if err := manager.MergeActivities([]activity.Record{{
		Path:       realProjectDir,
		Harness:    activity.Codex,
		LastUsedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
		Source:     "Codex local databases",
		SessionID:  stale.Name,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(stale.Name); err == nil {
		t.Fatal("stale Codex visualization project still exists")
	}
	project, err := manager.Get("real-project")
	if err != nil {
		t.Fatal(err)
	}
	if project.Path != canonicalRealProjectDir || project.ActivitySessionID != stale.Name {
		t.Fatalf("reconciled project = %#v", project)
	}
}

func TestMergeActivitiesReconcilesStaleClaudeFileTimestamp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	falseRecent := time.Date(2026, 9, 21, 13, 8, 0, 0, time.UTC)
	actualRecent := time.Date(2026, 9, 18, 9, 16, 0, 0, time.UTC)

	if err := manager.MergeActivities([]activity.Record{{
		Path:       projectDir,
		Harness:    activity.Claude,
		LastUsedAt: falseRecent,
		Source:     "claude local files",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.MergeActivities([]activity.Record{{
		Path:       projectDir,
		Harness:    activity.Claude,
		LastUsedAt: actualRecent,
		Source:     "claude local files",
	}}); err != nil {
		t.Fatal(err)
	}

	project, err := manager.Get(filepath.Base(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	if !project.LastActivityAt.Equal(actualRecent) {
		t.Fatalf("LastActivityAt = %s, want %s", project.LastActivityAt, actualRecent)
	}
}

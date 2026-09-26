package activity

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type filesystemSource struct {
	harness Harness
	name    string
	roots   []string
}

const recentCodexSessionFileLimit = 500

func newFilesystemSource(harness Harness) Source {
	return &filesystemSource{
		harness: harness,
		name:    string(harness) + " local files",
		roots:   knownDataLocations(harness),
	}
}

func (s *filesystemSource) Name() string {
	return s.name
}

func (s *filesystemSource) Discover() ([]Record, error) {
	var records []Record
	for _, root := range s.roots {
		rootRecords, err := discoverFilesystemRoot(root, s.harness, s.name)
		if err != nil {
			continue
		}
		records = append(records, rootRecords...)
	}
	return records, nil
}

func discoverFilesystemRoot(root string, harness Harness, sourceName string) ([]Record, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !isActivityDataFile(root) || info.Size() > 32*1024*1024 {
			return nil, nil
		}
		return recordsFromJSONFile(root, harness, sourceName, info.ModTime())
	}
	var records []Record
	if harness == Codex && isCodexSessionRoot(root) {
		return discoverRecentCodexSessionRoot(root, harness, sourceName)
	}
	filesSeen := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipActivityDirectory(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if filesSeen >= 10000 {
			return io.EOF
		}
		if !isActivityDataFile(path) {
			return nil
		}
		filesSeen++
		fileInfo, err := entry.Info()
		if err != nil || fileInfo.Size() > 32*1024*1024 {
			return nil
		}
		fileRecords, err := recordsFromJSONFile(path, harness, sourceName, fileInfo.ModTime())
		if err == nil {
			records = append(records, fileRecords...)
		}
		return nil
	})
	if err == io.EOF {
		err = nil
	}
	return records, err
}

func recordsFromCodexSessionFile(path string, harness Harness, sourceName string, modifiedAt time.Time) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 128*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(line) == 0 {
		return nil, nil
	}
	var metadata struct {
		Timestamp any    `json:"timestamp"`
		Cwd       string `json:"cwd"`
		Payload   struct {
			SessionID         string `json:"session_id"`
			ID                string `json:"id"`
			Timestamp         any    `json:"timestamp"`
			Cwd               string `json:"cwd"`
			WorkingDirectory  string `json:"workingDirectory"`
			ProjectPath       string `json:"projectPath"`
			ProjectRoot       string `json:"projectRoot"`
			Workspace         string `json:"workspace"`
			WorkspacePath     string `json:"workspacePath"`
			GitRepositoryRoot string `json:"git_repo_root"`
			GitRoot           string `json:"gitRoot"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &metadata); err != nil {
		return nil, err
	}
	pathValue := firstNonEmpty(metadata.Payload.Cwd, metadata.Payload.WorkingDirectory, metadata.Payload.ProjectPath, metadata.Payload.ProjectRoot, metadata.Payload.Workspace, metadata.Payload.WorkspacePath, metadata.Payload.GitRepositoryRoot, metadata.Payload.GitRoot, metadata.Cwd)
	if pathValue == "" {
		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			return nil, err
		}
		return recordsFromJSONValue(value, harness, sourceName, modifiedAt), nil
	}
	lastUsedAt := time.Time{}
	for _, rawTime := range []any{metadata.Timestamp, metadata.Payload.Timestamp} {
		if timestamp, ok := parseActivityTime(rawTime); ok && timestamp.After(lastUsedAt) {
			lastUsedAt = timestamp
		}
	}
	if lastUsedAt.IsZero() {
		lastUsedAt = modifiedAt
	}
	return []Record{{
		Path:       pathValue,
		Harness:    harness,
		LastUsedAt: lastUsedAt,
		Source:     sourceName,
		SessionID:  firstNonEmpty(metadata.Payload.SessionID, metadata.Payload.ID),
	}}, nil
}

func isCodexSessionRoot(root string) bool {
	name := strings.ToLower(filepath.Base(filepath.Clean(root)))
	return name == "sessions" || name == "archived_sessions"
}

// Codex keeps a large historical rollout tree. Walk the newest directory and
// file names first, retaining the recent slice that is useful for checkout,
// instead of reading gigabytes of old transcripts on every CLI invocation.
func discoverRecentCodexSessionRoot(root string, harness Harness, sourceName string) ([]Record, error) {
	var records []Record
	filesSeen := 0
	var visit func(string) error
	visit = func(directory string) error {
		if filesSeen >= recentCodexSessionFileLimit {
			return nil
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil
		}
		for index := len(entries) - 1; index >= 0; index-- {
			if filesSeen >= recentCodexSessionFileLimit {
				break
			}
			entry := entries[index]
			if entry.IsDir() {
				if shouldSkipActivityDirectory(entry.Name()) {
					continue
				}
				if err := visit(filepath.Join(directory, entry.Name())); err != nil {
					return err
				}
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if !isActivityDataFile(path) {
				continue
			}
			filesSeen++
			fileInfo, err := entry.Info()
			if err != nil || fileInfo.Size() > 32*1024*1024 {
				continue
			}
			fileRecords, err := recordsFromCodexSessionFile(path, harness, sourceName, fileInfo.ModTime())
			if err == nil {
				records = append(records, fileRecords...)
			}
		}
		return nil
	}
	return records, visit(root)
}

func shouldSkipActivityDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "cache", "caches", "code cache", "codecache", "gpu cache", "gpucache", "crashpad", "logs", "log", "session storage", "sessionstorage":
		return true
	default:
		return false
	}
}

func isActivityDataFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != ".jsonl" && ext != ".ndjson" {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	if strings.Contains(name, "setting") || strings.Contains(name, "config") || strings.Contains(name, "auth") || strings.Contains(name, "token") {
		return false
	}
	if isLikelyActivityDataName(name) {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "activity", "activities", "conversation", "conversations", "history", "histories", "local-agent-mode-sessions", "projects", "sessions", "threads", "transcripts", "workspaces", "workspace", "cowork":
			return true
		}
	}
	return false
}

func isLikelyActivityDataName(name string) bool {
	for _, marker := range []string{"activity", "conversation", "history", "session", "thread", "transcript", "workspace", "project", "event", "message", "rollout", "state", "metadata", "store", "recent"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func recordsFromJSONFile(path string, harness Harness, sourceName string, modifiedAt time.Time) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []Record
	pathPredicate := jsonPathPredicate(harness, path)
	decoder := json.NewDecoder(bufio.NewReader(file))
	for {
		var value any
		err := decoder.Decode(&value)
		if err == io.EOF {
			break
		}
		if err != nil {
			return records, err
		}
		records = append(records, recordsFromJSONValueWithPathPredicate(value, harness, sourceName, modifiedAt, pathPredicate)...)
	}
	return records, nil
}

func recordsFromJSONValue(value any, harness Harness, sourceName string, modifiedAt time.Time) []Record {
	return recordsFromJSONValueWithPathPredicate(value, harness, sourceName, modifiedAt, nil)
}

func recordsFromJSONValueWithPathPredicate(value any, harness Harness, sourceName string, modifiedAt time.Time, pathPredicate func(string) bool) []Record {
	var records []Record
	walkJSONWithPathPredicate(value, nil, func(pathValue string, timestamp time.Time, sessionID string) {
		records = append(records, Record{
			Path:       pathValue,
			Harness:    harness,
			LastUsedAt: activityTime(timestamp, modifiedAt),
			Source:     sourceName,
			SessionID:  sessionID,
		})
	}, pathPredicate)
	return records
}

func jsonPathPredicate(harness Harness, path string) func(string) bool {
	if harness != Claude {
		return nil
	}
	if strings.EqualFold(filepath.Base(path), "history.jsonl") {
		return isClaudeHistoryPathKey
	}
	return isClaudeSessionPathKey
}

func isClaudeHistoryPathKey(key string) bool {
	return compactKey(key) == "project" || isClaudeSessionPathKey(key)
}

func isClaudeSessionPathKey(key string) bool {
	switch compactKey(key) {
	case "cwd", "workingdirectory", "workingdir", "workdir", "projectpath", "projectroot", "projectdirectory", "projectfolder", "workspacepath", "workspacedirectory", "workspace", "gitreporoot", "gitroot", "primarypath", "folderpath", "repositorypath", "repopath", "rootpath":
		return true
	default:
		return false
	}
}

func walkJSON(value any, inheritedTime *time.Time, emit func(string, time.Time, string)) {
	walkJSONWithPathPredicate(value, inheritedTime, emit, nil)
}

func walkJSONWithPathPredicate(value any, inheritedTime *time.Time, emit func(string, time.Time, string), pathPredicate func(string) bool) {
	switch current := value.(type) {
	case []any:
		for _, child := range current {
			walkJSONWithPathPredicate(child, inheritedTime, emit, pathPredicate)
		}
	case map[string]any:
		currentTime := time.Time{}
		if inheritedTime != nil {
			currentTime = *inheritedTime
		}
		for key, child := range current {
			if timestamp, ok := parseActivityTime(child); ok && isActivityTimeKey(key) {
				if timestamp.After(currentTime) {
					currentTime = timestamp
				}
			}
		}
		pathValue := ""
		for key, child := range current {
			if stringValue, ok := child.(string); ok && isActivityPathKey(key) {
				if pathPredicate != nil && !pathPredicate(key) {
					continue
				}
				if looksLikeDirectory(stringValue) {
					pathValue = stringValue
					break
				}
			}
		}
		sessionID := ""
		for key, child := range current {
			if stringValue, ok := child.(string); ok && isSessionIDKey(key) {
				sessionID = stringValue
				break
			}
		}
		if pathValue != "" {
			emit(pathValue, currentTime, sessionID)
		}
		for _, child := range current {
			walkJSONWithPathPredicate(child, &currentTime, emit, pathPredicate)
		}
	}
}

func isActivityPathKey(key string) bool {
	key = compactKey(key)
	switch key {
	case "cwd", "workingdirectory", "workingdir", "workdir", "projectpath", "projectroot", "projectdirectory", "projectfolder", "project", "workspacepath", "workspacedirectory", "workspace", "gitreporoot", "gitroot", "primarypath", "folderpath", "repositorypath", "repopath", "rootpath", "directory", "dir", "repository", "repo":
		return true
	case "path":
		return true
	default:
		return false
	}
}

func isActivityTimeKey(key string) bool {
	switch compactKey(key) {
	case "timestamp", "time", "date", "lastactivityat", "lastactivity", "lastusedat", "lastused", "lastseenat", "lastseen", "lastactive", "startedat", "started", "endedat", "ended", "createdat", "created", "updatedat", "updated", "modifiedat", "modified", "mtime":
		return true
	default:
		return false
	}
}

func isSessionIDKey(key string) bool {
	switch compactKey(key) {
	case "sessionid", "threadid", "conversationid", "sessionkey", "id":
		return true
	default:
		return false
	}
}

func compactKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	return key
}

func looksLikeDirectory(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "\n") {
		return false
	}
	if strings.HasPrefix(value, "~"+string(os.PathSeparator)) || filepath.IsAbs(value) {
		return true
	}
	return false
}

func activityTime(recordTime, modifiedAt time.Time) time.Time {
	if !recordTime.IsZero() {
		return recordTime
	}
	return modifiedAt
}

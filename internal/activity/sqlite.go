package activity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The SQLite adapter is intentionally shell-backed. macOS ships sqlite3, and
// using its readonly mode lets declaw inspect agent databases without linking
// to or mutating an agent's database driver.
type sqliteSource struct {
	harness Harness
	name    string
	roots   []string
}

func newSQLiteSource(harness Harness) Source {
	return &sqliteSource{
		harness: harness,
		name:    string(harness) + " local databases",
		roots:   knownDataLocations(harness),
	}
}

func (s *sqliteSource) Name() string {
	return s.name
}

func (s *sqliteSource) Discover() ([]Record, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, nil
	}
	var records []Record
	for _, root := range s.roots {
		databases := findSQLiteFiles(root)
		for _, database := range databases {
			databaseRecords, err := discoverSQLiteDatabase(database, s.harness, s.name)
			if err == nil {
				records = append(records, databaseRecords...)
			}
		}
	}
	return records, nil
}

func findSQLiteFiles(root string) []string {
	info, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(root))
		if ext == ".db" || ext == ".sqlite" || ext == ".sqlite3" {
			return []string{root}
		}
		return nil
	}
	seen := map[string]bool{}
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipActivityDirectory(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
			return nil
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files
}

func discoverSQLiteDatabase(database string, harness Harness, sourceName string) ([]Record, error) {
	var records []Record
	switch harness {
	case Hermes:
		records = append(records, discoverHermesDatabase(database, sourceName)...)
	case Codex:
		records = append(records, discoverCodexDatabase(database, sourceName)...)
		// Codex databases contain large event payloads with unrelated paths
		// such as attachments and visualization files. Column metadata like
		// cwd and project_path is reliable, so inspect only those columns here.
		records = append(records, discoverGenericSQLitePathDatabase(database, harness, sourceName)...)
		return records, nil
	}
	records = append(records, discoverGenericSQLiteDatabase(database, harness, sourceName)...)
	return records, nil
}

func discoverGenericSQLiteDatabase(database string, harness Harness, sourceName string) []Record {
	return discoverGenericSQLiteDatabaseWithOptions(database, harness, sourceName, true)
}

func discoverGenericSQLitePathDatabase(database string, harness Harness, sourceName string) []Record {
	return discoverGenericSQLiteDatabaseWithOptions(database, harness, sourceName, false)
}

func discoverGenericSQLiteDatabaseWithOptions(database string, harness Harness, sourceName string, includeJSON bool) []Record {
	tableRows := sqliteRows(database, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	var records []Record
	for _, tableRow := range tableRows {
		table := tableRow["name"]
		if table == "" || strings.ContainsAny(table, "\r\n\x00") {
			continue
		}
		columns := sqliteRows(database, "PRAGMA table_info("+quoteSQLiteIdentifier(table)+")")
		pathColumn := chooseSQLiteColumn(columns, "cwd", "git_repo_root", "project_path", "projectPath", "workspace_path", "workspacePath", "working_directory", "workingDirectory", "primary_path", "folder_path", "folderPath", "root_path", "root", "path")
		if pathColumn != "" {
			timeColumn := chooseSQLiteColumn(columns, "last_activity_at", "last_used_at", "updated_at", "updated_at_ms", "source_updated_at", "source_recency_at", "modified_at", "started_at", "created_at", "last_seen", "timestamp", "time")
			idColumn := chooseSQLiteColumn(columns, "session_id", "thread_id", "conversation_id", "id")
			selectParts := []string{quoteSQLiteIdentifier(pathColumn) + " AS activity_path"}
			if timeColumn != "" {
				selectParts = append(selectParts, quoteSQLiteIdentifier(timeColumn)+" AS activity_at")
			} else {
				selectParts = append(selectParts, "0 AS activity_at")
			}
			if idColumn != "" {
				selectParts = append(selectParts, quoteSQLiteIdentifier(idColumn)+" AS session_id")
			} else {
				selectParts = append(selectParts, "'' AS session_id")
			}
			query := "SELECT " + strings.Join(selectParts, ", ") + " FROM " + quoteSQLiteIdentifier(table) + " WHERE trim(CAST(" + quoteSQLiteIdentifier(pathColumn) + " AS TEXT)) <> '' LIMIT 10000"
			for _, row := range sqliteRows(database, query) {
				records = append(records, recordFromSQLite(row["activity_path"], harness, sourceName, row["activity_at"], row["session_id"])...)
			}
		}

		if !includeJSON {
			continue
		}
		jsonColumn := chooseSQLiteColumn(columns, "item_json", "session_json", "metadata", "payload", "data", "event_json", "origin_json")
		if jsonColumn == "" {
			continue
		}
		timeColumn := chooseSQLiteColumn(columns, "created_at_ms", "last_activity_at", "updated_at", "created_at", "timestamp", "time")
		idColumn := chooseSQLiteColumn(columns, "session_id", "thread_id", "conversation_id", "id")
		selectParts := []string{quoteSQLiteIdentifier(jsonColumn) + " AS item_json"}
		if timeColumn != "" {
			selectParts = append(selectParts, quoteSQLiteIdentifier(timeColumn)+" AS activity_at")
		} else {
			selectParts = append(selectParts, "0 AS activity_at")
		}
		if idColumn != "" {
			selectParts = append(selectParts, quoteSQLiteIdentifier(idColumn)+" AS session_id")
		} else {
			selectParts = append(selectParts, "'' AS session_id")
		}
		query := "SELECT " + strings.Join(selectParts, ", ") + " FROM " + quoteSQLiteIdentifier(table) + " WHERE typeof(" + quoteSQLiteIdentifier(jsonColumn) + ") = 'text' LIMIT 10000"
		for _, row := range sqliteRows(database, query) {
			var value any
			if json.Unmarshal([]byte(row["item_json"]), &value) != nil {
				continue
			}
			for _, record := range recordsFromJSONValueWithPathPredicate(value, harness, sourceName, timeFromSQLite(row["activity_at"]), jsonPathPredicate(harness, "")) {
				if record.SessionID == "" {
					record.SessionID = row["session_id"]
				}
				records = append(records, record)
			}
		}
	}
	return records
}

func chooseSQLiteColumn(columns []map[string]string, names ...string) string {
	available := map[string]string{}
	for _, column := range columns {
		name := column["name"]
		available[compactKey(name)] = name
	}
	for _, name := range names {
		if actual, ok := available[compactKey(name)]; ok {
			return actual
		}
	}
	return ""
}

func discoverHermesDatabase(database, sourceName string) []Record {
	var records []Record
	if sqliteTableExists(database, "sessions") {
		rows := sqliteRows(database, `SELECT coalesce(cwd,'') AS cwd, coalesce(git_repo_root,'') AS git_repo_root, coalesce(last_activity_at, started_at, 0) AS activity_at, coalesce(id,'') AS session_id FROM sessions WHERE coalesce(cwd,'') <> '' OR coalesce(git_repo_root,'') <> ''`)
		for _, row := range rows {
			path := firstNonEmpty(row["cwd"], row["git_repo_root"])
			records = append(records, recordFromSQLite(path, Hermes, sourceName, row["activity_at"], row["session_id"])...)
		}
	}
	if sqliteTableExists(database, "project_folders") {
		rows := sqliteRows(database, `SELECT coalesce(path,'') AS path, coalesce(added_at, 0) AS activity_at FROM project_folders WHERE coalesce(path,'') <> ''`)
		for _, row := range rows {
			records = append(records, recordFromSQLite(row["path"], Hermes, sourceName, row["activity_at"], "")...)
		}
	}
	if sqliteTableExists(database, "projects") {
		rows := sqliteRows(database, `SELECT coalesce(primary_path,'') AS primary_path, coalesce(created_at, 0) AS activity_at FROM projects WHERE coalesce(primary_path,'') <> ''`)
		for _, row := range rows {
			records = append(records, recordFromSQLite(row["primary_path"], Hermes, sourceName, row["activity_at"], "")...)
		}
	}
	if sqliteTableExists(database, "discovered_repos") {
		rows := sqliteRows(database, `SELECT coalesce(root,'') AS root, coalesce(last_seen, 0) AS activity_at FROM discovered_repos WHERE coalesce(root,'') <> ''`)
		for _, row := range rows {
			records = append(records, recordFromSQLite(row["root"], Hermes, sourceName, row["activity_at"], "")...)
		}
	}
	return records
}

func discoverCodexDatabase(database, sourceName string) []Record {
	var records []Record
	if sqliteTableExists(database, "thread_items") {
		rows := sqliteRows(database, `SELECT coalesce(item_json,'') AS item_json, coalesce(created_at_ms, 0) AS created_at_ms, coalesce(thread_id,'') AS thread_id FROM thread_items WHERE item_json LIKE '%"cwd"%' OR item_json LIKE '%"workingDirectory"%' OR item_json LIKE '%"projectPath"%' ORDER BY created_at_ms DESC LIMIT 20000`)
		for _, row := range rows {
			var value any
			if json.Unmarshal([]byte(row["item_json"]), &value) != nil {
				continue
			}
			for _, record := range recordsFromJSONValueWithPathPredicate(value, Codex, sourceName, timeFromSQLite(row["created_at_ms"]), isCodexReliablePathKey) {
				if record.SessionID == "" {
					record.SessionID = row["thread_id"]
				}
				records = append(records, record)
			}
		}
	}
	return records
}

func isCodexReliablePathKey(key string) bool {
	switch compactKey(key) {
	case "cwd", "workingdirectory", "workingdir", "workdir", "projectpath", "projectroot", "projectdirectory", "projectfolder", "workspacepath", "workspacedirectory", "workspace", "gitreporoot", "gitroot", "primarypath", "folderpath", "repositorypath", "repopath", "rootpath":
		return true
	default:
		return false
	}
}

func recordFromSQLite(path string, harness Harness, sourceName, rawTime, sessionID string) []Record {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return []Record{{
		Path:       path,
		Harness:    harness,
		LastUsedAt: timeFromSQLite(rawTime),
		Source:     sourceName,
		SessionID:  sessionID,
	}}
}

func timeFromSQLite(value string) time.Time {
	parsed, _ := parseActivityTime(value)
	return parsed
}

func sqliteTableExists(database, table string) bool {
	rows := sqliteRows(database, `SELECT name FROM sqlite_master WHERE type='table' AND name=`+quoteSQLiteString(table)+` LIMIT 1`)
	return len(rows) > 0
}

func sqliteRows(database, query string) []map[string]string {
	command := exec.Command("sqlite3", "-readonly", "-json", database, query)
	output, err := command.Output()
	if err != nil || len(output) == 0 {
		return nil
	}
	var raw []map[string]any
	if json.Unmarshal(output, &raw) != nil {
		return nil
	}
	rows := make([]map[string]string, 0, len(raw))
	for _, item := range raw {
		row := map[string]string{}
		for key, value := range item {
			switch current := value.(type) {
			case string:
				row[key] = current
			case float64:
				row[key] = strconv.FormatFloat(current, 'f', -1, 64)
			case nil:
				row[key] = ""
			default:
				row[key] = fmt.Sprint(current)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func quoteSQLiteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

package paths

import (
	"errors"
	"os"
	"path/filepath"
)

func RootDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(configDir, "declaw")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func ProjectsDir() (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "projects")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func SupportDir() (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "support")
	if err := migrateDir(legacySupportDir, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func AgentWorkspaceDir() (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "ai-agent")
	if err := migrateDir(legacyAgentWorkspaceDir, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func LegacySupportDir() (string, error) {
	return legacySupportDir()
}

func LegacyAgentWorkspaceDir() (string, error) {
	return legacyAgentWorkspaceDir()
}

func legacySupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "declaw"), nil
}

func legacyAgentWorkspaceDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "declaw", "ai-agent"), nil
}

func migrateDir(sourceFn func() (string, error), target string) error {
	source, err := sourceFn()
	if err != nil {
		return err
	}
	if filepath.Clean(source) == filepath.Clean(target) {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.Rename(source, target)
}

package appdata

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	RootDirName      = ".nahida-desktop"
	DatabaseFile     = "data.db"
	LogsDir          = "logs"
	ToolsDir         = "tools"
	ModBisectDir     = "mod-bisect"
	NTEModsDir       = "NTE-Mods"
	DownloadSpoolDir = "download-spool"
	AgentDir         = "agent"
	AgentSkillsDir   = AgentDir + string(filepath.Separator) + "skills"
	ElevatedDir      = "elevated"
)

var ErrInvalidPath = errors.New("invalid app data path")

// Store owns files below the per-user .nahida-desktop directory.
// Paths outside Root are intentionally not supported by this API.
type Store struct {
	root string
}

// Open resolves and creates <homeDir>/.nahida-desktop.
func Open(homeDir string) (*Store, error) {
	s, err := Locate(homeDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, fmt.Errorf("create app data root: %w", err)
	}
	return s, nil
}

// Locate resolves <homeDir>/.nahida-desktop without creating it, so that startup can restore the permissions
// of a root that refuses the account first. Open fails on a root that is a junction to such a folder, because
// the folder cannot be read to tell that it exists.
func Locate(homeDir string) (*Store, error) {
	if strings.TrimSpace(homeDir) == "" {
		return nil, fmt.Errorf("open app data: %w: empty home directory", ErrInvalidPath)
	}
	home, err := filepath.Abs(homeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return &Store{root: filepath.Join(home, RootDirName)}, nil
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Resolve returns an absolute path below Root for a non-empty relative path.
func (s *Store) Resolve(relative string) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("app data store is not open")
	}
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relative)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relative)
	}
	target := filepath.Join(s.root, clean)
	inside, err := filepath.Rel(s.root, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relative)
	}
	return target, nil
}

func (s *Store) EnsureDir(relative string) (string, error) {
	path, err := s.Resolve(relative)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create app data directory %q: %w", relative, err)
	}
	return path, nil
}

func (s *Store) ReadFile(relative string) ([]byte, error) {
	path, err := s.Resolve(relative)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read app data file %q: %w", relative, err)
	}
	return data, nil
}

func (s *Store) WriteFile(relative string, data []byte, mode fs.FileMode) error {
	path, err := s.Resolve(relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create app data file parent %q: %w", relative, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write app data file %q: %w", relative, err)
	}
	return nil
}

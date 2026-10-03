package gameplatform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// maxVDFSize bounds the Steam files read into memory; localconfig.vdf of a large library stays far below it.
const maxVDFSize = 64 << 20

var (
	// ErrSteamUserNotFound means Steam has no signed-in account whose settings could be read.
	ErrSteamUserNotFound = errors.New("steam user configuration not found")
	// ErrSteamAppNotConfigured means the active account has no settings entry for the application yet.
	// Steam creates the entry the first time the account opens the game's library page or starts it.
	ErrSteamAppNotConfigured = errors.New("steam application has no configuration entry")
)

// Steam is a located Steam client installation.
type Steam struct {
	// Exe is the absolute path of steam.exe.
	Exe string
}

// SteamApp is a game installed in one of the client's libraries.
type SteamApp struct {
	ID         string
	Name       string
	InstallDir string
}

func (s Steam) dir() string { return filepath.Dir(s.Exe) }

// LaunchArgs returns the steam.exe arguments that start an application. Steam applies
// the launch options stored in the account settings itself.
func (s Steam) LaunchArgs(appID string) []string {
	return []string{"-silent", "-applaunch", appID}
}

// FindGame looks the game up in every Steam library: by application ID first,
// then by keywords in the install folder and store title.
func (s Steam) FindGame(game Game) (SteamApp, bool, error) {
	apps, err := s.installedApps()
	if err != nil {
		return SteamApp{}, false, err
	}
	for _, app := range apps {
		if slices.Contains(game.SteamAppIDs, app.ID) {
			return app, true, nil
		}
	}
	for _, app := range apps {
		if game.matches(filepath.Base(app.InstallDir), app.Name) {
			return app, true, nil
		}
	}
	return SteamApp{}, false, nil
}

func (s Steam) installedApps() ([]SteamApp, error) {
	var apps []SteamApp
	for _, library := range s.libraries() {
		manifests, err := filepath.Glob(filepath.Join(library, "steamapps", "appmanifest_*.acf"))
		if err != nil {
			return nil, err
		}
		slices.Sort(manifests)
		for _, manifest := range manifests {
			root, _, err := readVDF(manifest)
			if err != nil {
				// One unreadable manifest must not hide the other installed games.
				continue
			}
			state := root.child("AppState")
			id, _ := state.text("appid")
			installDir, _ := state.text("installdir")
			if id == "" || installDir == "" {
				continue
			}
			name, _ := state.text("name")
			apps = append(apps, SteamApp{
				ID: id, Name: name, InstallDir: filepath.Join(library, "steamapps", "common", installDir),
			})
		}
	}
	return apps, nil
}

// libraries lists the existing library folders: the client folder first, then the ones in libraryfolders.vdf.
func (s Steam) libraries() []string {
	var libraries []string
	add := func(path string) {
		path = filepath.Clean(path)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return
		}
		if !slices.ContainsFunc(libraries, func(known string) bool { return strings.EqualFold(known, path) }) {
			libraries = append(libraries, path)
		}
	}
	add(s.dir())
	for _, candidate := range []string{
		filepath.Join(s.dir(), "steamapps", "libraryfolders.vdf"),
		filepath.Join(s.dir(), "config", "libraryfolders.vdf"),
	} {
		root, _, err := readVDF(candidate)
		if err != nil {
			continue
		}
		for _, entry := range root.child("libraryfolders").entries() {
			// Old clients store the path as the value; current ones nest it in a block.
			if !entry.block {
				add(entry.value)
			} else if path, ok := entry.text("path"); ok {
				add(path)
			}
		}
		break
	}
	return libraries
}

// localConfigPath returns localconfig.vdf of the most recently signed-in account.
func (s Steam) localConfigPath() (string, error) {
	root, _, err := readVDF(filepath.Join(s.dir(), "config", "loginusers.vdf"))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSteamUserNotFound, err)
	}
	var accountID uint64
	latest := int64(-1)
	found := false
	for _, user := range root.child("users").entries() {
		steamID, err := strconv.ParseUint(user.key, 10, 64)
		if err != nil || !user.block {
			continue
		}
		if _, ok := user.text("AccountName"); !ok {
			continue
		}
		stamp, _ := user.text("Timestamp")
		timestamp, _ := strconv.ParseInt(stamp, 10, 64)
		if !found || timestamp > latest {
			// The userdata folder is named after the 32-bit account part of the SteamID64.
			accountID, latest, found = steamID&0xFFFFFFFF, timestamp, true
		}
	}
	if !found {
		return "", ErrSteamUserNotFound
	}
	path := filepath.Join(s.dir(), "userdata", strconv.FormatUint(accountID, 10), "config", "localconfig.vdf")
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrSteamUserNotFound, path)
	}
	return path, nil
}

// LaunchOptions returns the launch options the active account stores for an application.
func (s Steam) LaunchOptions(appID string) (string, error) {
	path, err := s.localConfigPath()
	if err != nil {
		return "", err
	}
	root, _, err := readVDF(path)
	if err != nil {
		return "", err
	}
	app, err := steamAppConfig(root, appID)
	if err != nil {
		return "", err
	}
	options, _ := app.text("LaunchOptions")
	return options, nil
}

// SetLaunchOptions stores the launch options of an application for the active account.
// Steam rewrites localconfig.vdf on exit, so the client must not be running.
func (s Steam) SetLaunchOptions(appID, options string) error {
	path, err := s.localConfigPath()
	if err != nil {
		return err
	}
	root, data, err := readVDF(path)
	if err != nil {
		return err
	}
	app, err := steamAppConfig(root, appID)
	if err != nil {
		return err
	}
	updated, err := setVDFString(data, app, "LaunchOptions", options)
	if err != nil {
		return err
	}

	// Steam must never see a half-written file, so the new content replaces the old one in a single rename.
	temp, err := os.CreateTemp(filepath.Dir(path), "localconfig-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(updated); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// steamAppConfig returns the settings block of an application. Missing structure is reported
// instead of created: only Steam decides the layout of its own file.
func steamAppConfig(root *vdfNode, appID string) (*vdfNode, error) {
	apps := root.find("UserLocalConfigStore", "Software", "Valve", "Steam", "apps")
	if apps == nil || !apps.block {
		return nil, errors.New("steam configuration has no application list")
	}
	app := apps.child(appID)
	if app == nil || !app.block {
		return nil, fmt.Errorf("%w: %s", ErrSteamAppNotConfigured, appID)
	}
	return app, nil
}

func readVDF(path string) (*vdfNode, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxVDFSize+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxVDFSize {
		return nil, nil, fmt.Errorf("%s exceeds the size limit", path)
	}
	root, err := parseVDF(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return root, data, nil
}

package mod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

const (
	manualSubGroupsSettingKey = "manual_subgroups"
	lastGameSettingKey        = "last_game"
	expandedGroupsSettingKey  = "expanded_groups"
)

var disabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

type Settings interface {
	GetSearchModPreview(context.Context) (bool, error)
	GetDisabledPrefixStyle(context.Context) (string, error)
	GetArchiveExtractPathMode(context.Context) (string, error)
	GetDeleteArchiveAfterExtract(context.Context) (bool, error)
	GetMoveFolderInsteadOfCopy(context.Context) (bool, error)
	GetCopyShaderFixesOnEnable(context.Context) (bool, error)
}

type ImporterSource interface {
	GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error)
}

type Options struct {
	AppData    *appdata.Store
	FS         *platform.FS
	Settings   Settings
	Archive    *infra.Archive
	HTTP       *infra.Client
	XXMI       ImporterSource
	Log        *infra.Log
	Dialog     *platform.Dialog
	EventEmit  func(string, ...any)
	Transfer   *transfer.Transfer
	GameBanana *gamebanana.GameBanana
	Native     *platform.Native
	Focus      func()
}

type Mod struct {
	appData           *appdata.Store
	client            *db.Client
	fs                *platform.FS
	settings          Settings
	archive           *infra.Archive
	http              *infra.Client
	xxmi              ImporterSource
	log               *infra.Log
	dialog            *platform.Dialog
	shaders           *ShaderFixes
	transfer          *transfer.Transfer
	gamebanana        *gamebanana.GameBanana
	native            *platform.Native
	paths             *pathSelector
	downloader        *infra.ParallelDownloader
	extractMu         sync.Mutex
	extractPrompts    map[string]chan string
	mu                sync.Mutex
	watchMu           sync.Mutex
	gameWatcher       *managedWatcher
	characterWatcher  *managedWatcher
	emit              func(string, ...any)
	nteSigBypasserURL string
	nteASILoaderURL   string
}

func New() *Mod { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Mod {
	if opts.FS == nil {
		opts.FS = platform.NewFS()
	}
	m := &Mod{
		appData: opts.AppData, fs: opts.FS, settings: opts.Settings, archive: opts.Archive, http: opts.HTTP,
		xxmi: opts.XXMI, log: opts.Log, dialog: opts.Dialog, emit: opts.EventEmit,
		transfer: opts.Transfer, gamebanana: opts.GameBanana, native: opts.Native,
		shaders: newShaderFixes(), extractPrompts: map[string]chan string{},
		downloader:        infra.NewParallelDownloader(),
		nteSigBypasserURL: defaultNteSigBypasserURL,
		nteASILoaderURL:   defaultNteASILoaderURL,
	}
	m.paths = newPathSelector(opts.Dialog, opts.FS, opts.EventEmit, opts.Focus)
	m.downloader.SetRequestConcurrency(16)
	if m.http != nil {
		m.downloader.Client = m.http.HTTPClient()
		m.downloader.GetHeaders = func(rawURL string) (map[string]string, error) {
			header, err := m.http.GetHeaders(rawURL)
			if err != nil {
				return nil, err
			}
			out := map[string]string{}
			for key := range header {
				out[key] = header.Get(key)
			}
			return out, nil
		}
	}
	m.shaders.getImporters = m.shaderImporters
	m.shaders.getGames = m.shaderGames
	m.shaders.logError = m.logShaderError
	return m
}

//wails:ignore
func (m *Mod) UseClient(client *db.Client) { m.client = client }

//wails:ignore
func (m *Mod) UseAppData(data *appdata.Store) { m.appData = data }

//wails:ignore
func (m *Mod) UseSettings(settings Settings) { m.settings = settings }

//wails:ignore
func (m *Mod) UseFocus(fn func()) {
	if m != nil && m.paths != nil {
		m.paths.focus = fn
	}
}

//wails:ignore
func (m *Mod) UseWindowReady(fn func(context.Context) (bool, error)) {
	if m != nil && m.paths != nil {
		m.paths.waitReady = fn
	}
}

// SelectDownloadPath is Electron desktop.lib.pathSelector.getSelectedPathWithModeModal
// for Drive downloads. A nil path means the user cancelled.
//
//wails:ignore
func (m *Mod) SelectDownloadPath(ctx context.Context, suggestedName, source string, suggestedNames []string, selectFile bool) (*string, *string, error) {
	if m == nil || m.paths == nil {
		return nil, nil, errors.New("path selector is not configured")
	}
	result, err := m.paths.getSelectedPathWithModeModal(ctx, suggestedName, nil, nil, source, suggestedNames, selectFile)
	if err != nil {
		return nil, nil, err
	}
	return result.Path, result.FileName, nil
}

func (m *Mod) requireClient() (*db.Client, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("mod service is not bound to a database")
	}
	return m.client, nil
}

type GameConfig struct {
	Game                string  `json:"game"`
	ModFolderPath       string  `json:"modFolderPath"`
	Importer            *string `json:"importer"`
	LinkedModFolderPath *string `json:"linkedModFolderPath"`
	GameInstallPath     *string `json:"gameInstallPath"`
	GameExecutablePath  *string `json:"gameExecutablePath"`
	NteLauncherPath     *string `json:"nteLauncherPath"`
	Order               int64   `json:"order"`
}

type GameUpdates struct {
	ModFolderPath       string  `json:"modFolderPath"`
	Importer            *string `json:"importer"`
	LinkedModFolderPath *string `json:"linkedModFolderPath"`
	GameInstallPath     *string `json:"gameInstallPath"`
	GameExecutablePath  *string `json:"gameExecutablePath"`
}

type ToggleKey struct {
	SectionName  string   `json:"sectionName"`
	IniFileName  string   `json:"iniFileName"`
	Key          *string  `json:"key,omitempty"`
	Back         *string  `json:"back,omitempty"`
	Type         *string  `json:"type,omitempty"`
	Variable     string   `json:"variable"`
	Values       []string `json:"values"`
	CurrentValue *string  `json:"currentValue,omitempty"`
}

type IniResult struct {
	Name         string      `json:"name"`
	Path         string      `json:"path"`
	ToggleKeys   []ToggleKey `json:"toggleKeys"`
	HasToggleKey bool        `json:"hasToggleKey"`
}

type ModInfo struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	IsEnabled bool        `json:"isEnabled"`
	Preview   *string     `json:"preview,omitempty"`
	Mtime     float64     `json:"mtime"`
	Size      float64     `json:"size"`
	Inis      []IniResult `json:"inis"`
}

type FolderGroup struct {
	Name               string    `json:"name"`
	Path               string    `json:"path"`
	Mods               []ModInfo `json:"mods"`
	Preview            *string   `json:"preview,omitempty"`
	ModCount           int       `json:"modCount"`
	EnabledModCount    int       `json:"enabledModCount"`
	IsManualSubGroup   bool      `json:"isManualSubGroup,omitempty"`
	HasSubGroups       bool      `json:"hasSubGroups,omitempty"`
	HasManualSubGroups bool      `json:"hasManualSubGroups,omitempty"`
}

func gameConfig(row db.GamePathRow) GameConfig {
	return GameConfig{
		Game: row.Game, ModFolderPath: row.ModFolderPath, Importer: row.Importer,
		LinkedModFolderPath: row.LinkedModFolderPath, GameInstallPath: row.GameInstallPath,
		GameExecutablePath: row.GameExecutablePath, NteLauncherPath: row.NteLauncherPath,
		Order: row.Order,
	}
}

func enrichedGameConfig(row db.GamePathRow) GameConfig {
	game := gameConfig(row)
	if !isNTEImporter(row.Importer) || row.GameInstallPath != nil {
		return game
	}
	root := row.ModFolderPath
	if row.LinkedModFolderPath != nil {
		root = *row.LinkedModFolderPath
	}
	installPath := deriveNteGameInstallPath(root)
	game.GameInstallPath = &installPath
	return game
}

func deriveNteGameInstallPath(modOrLinkedPath string) string {
	return filepath.Clean(filepath.Join(modOrLinkedPath, "..", "..", "..", "..", ".."))
}

func (m *Mod) GetGamePath(ctx context.Context, game string) (*string, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	row, err := client.GamePaths.GetByGame(ctx, strings.TrimSpace(game))
	if err != nil || row == nil {
		return nil, err
	}
	value := row.ModFolderPath
	return &value, nil
}

func (m *Mod) GetGames(ctx context.Context) ([]GameConfig, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.GamePaths.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]GameConfig, len(rows))
	for i := range rows {
		result[i] = enrichedGameConfig(rows[i])
	}
	return result, nil
}

func (m *Mod) SetGamePath(ctx context.Context, game, modFolderPath string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	game = strings.TrimSpace(game)
	path, err := validDirectory(modFolderPath)
	if err != nil {
		return err
	}
	existing, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return err
	}
	order := int64(0)
	if existing != nil {
		order = existing.Order
	}
	return client.GamePaths.Upsert(ctx, db.GamePathRow{Game: game, ModFolderPath: path, Order: order})
}

func (m *Mod) AddGame(
	ctx context.Context,
	game, modFolderPath string,
	importer, linkedModFolderPath, gameInstallPath, gameExecutablePath *string,
) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	game = strings.TrimSpace(game)
	if game == "" || strings.TrimSpace(modFolderPath) == "" {
		return errors.New("INVALID_PARAMS")
	}
	modFolderPath, err = configuredDirectory(modFolderPath)
	if err != nil {
		return err
	}
	linkedModFolderPath, err = optionalDirectory(linkedModFolderPath)
	if err != nil {
		return err
	}
	exists, err := client.GamePaths.FindByGameOrModFolderPath(ctx, game, modFolderPath)
	if err != nil {
		return err
	}
	if exists == nil && linkedModFolderPath != nil {
		exists, err = client.GamePaths.FindByGameOrModFolderPath(ctx, game, *linkedModFolderPath)
		if err != nil {
			return err
		}
	}
	if exists != nil {
		if exists.Game == game {
			return errors.New("DUPLICATE_GAME_NAME")
		}
		return errors.New("DUPLICATE_MOD_FOLDER_PATH")
	}
	rollbacks := make([]func() error, 0, 2)
	var bootstrapInstall *nteBootstrapInstall
	if !isNTEImporter(importer) {
		linkedModFolderPath, gameInstallPath, gameExecutablePath = nil, nil, nil
	} else {
		if err := configureNteModFolder(modFolderPath, linkedModFolderPath); err != nil {
			return err
		}
		rollbacks = append(rollbacks, func() error {
			return cleanupNteModFolder(modFolderPath, linkedModFolderPath)
		})
		executablePath, err := m.resolveNteBootstrapExecutablePath(ctx, modFolderPath, linkedModFolderPath, cleanOptional(gameInstallPath))
		if err != nil {
			m.rollbackNteDiskChanges(rollbacks)
			return err
		}
		bootstrapInstall, err = m.ensureNteBootstrapFiles(ctx, executablePath)
		if err != nil {
			m.rollbackNteDiskChanges(rollbacks)
			return err
		}
		if bootstrapInstall != nil {
			rollbacks = append(rollbacks, bootstrapInstall.Rollback)
		}
	}
	err = client.GamePaths.Insert(ctx, db.GamePathRow{
		Game: game, ModFolderPath: modFolderPath, Importer: cleanOptional(importer),
		LinkedModFolderPath: linkedModFolderPath, GameInstallPath: cleanOptional(gameInstallPath),
		GameExecutablePath: cleanOptional(gameExecutablePath),
	})
	if err != nil {
		m.rollbackNteDiskChanges(rollbacks)
		return err
	}
	if bootstrapInstall != nil {
		if err := bootstrapInstall.Commit(); err != nil && m.log != nil {
			m.log.Error(err.Error(), "Mod:commitNteBootstrap")
		}
	}
	return nil
}

func (m *Mod) UpdateGame(ctx context.Context, game string, updates GameUpdates) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	game = strings.TrimSpace(game)
	if game == "" || strings.TrimSpace(updates.ModFolderPath) == "" {
		return fmt.Errorf("%s", "Game and modFolderPath are required")
	}
	updates.ModFolderPath, err = configuredDirectory(updates.ModFolderPath)
	if err != nil {
		return err
	}
	updates.LinkedModFolderPath, err = optionalDirectory(updates.LinkedModFolderPath)
	if err != nil {
		return err
	}
	existing, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("game %s not found", game)
	}
	duplicate, err := client.GamePaths.FindByModFolderPathOtherGame(ctx, game, updates.ModFolderPath)
	if err != nil {
		return err
	}
	if duplicate == nil && updates.LinkedModFolderPath != nil {
		duplicate, err = client.GamePaths.FindByModFolderPathOtherGame(ctx, game, *updates.LinkedModFolderPath)
		if err != nil {
			return err
		}
	}
	if duplicate != nil {
		return errors.New("DUPLICATE_MOD_FOLDER_PATH")
	}
	updatesNTE := isNTEImporter(updates.Importer)
	existingNTE := isNTEImporter(existing.Importer)
	if !updatesNTE {
		updates.LinkedModFolderPath, updates.GameInstallPath, updates.GameExecutablePath = nil, nil, nil
	}
	shouldCleanupExisting := existingNTE && (!updatesNTE || hasNtePathChanges(*existing, updates))
	rollbacks := make([]func() error, 0, 2)
	if shouldCleanupExisting {
		if err := cleanupNteModFolder(existing.ModFolderPath, existing.LinkedModFolderPath); err != nil {
			return err
		}
		rollbacks = append(rollbacks, func() error {
			return configureNteModFolder(existing.ModFolderPath, existing.LinkedModFolderPath)
		})
	}
	if updatesNTE {
		if err := configureNteModFolder(updates.ModFolderPath, updates.LinkedModFolderPath); err != nil {
			m.rollbackNteDiskChanges(rollbacks)
			return err
		}
		rollbacks = append(rollbacks, func() error {
			return cleanupNteModFolder(updates.ModFolderPath, updates.LinkedModFolderPath)
		})
	}
	err = client.GamePaths.Update(ctx, game, db.GamePathUpdates{
		ModFolderPath: updates.ModFolderPath, Importer: cleanOptional(updates.Importer),
		LinkedModFolderPath: updates.LinkedModFolderPath, GameInstallPath: cleanOptional(updates.GameInstallPath),
		GameExecutablePath: cleanOptional(updates.GameExecutablePath),
	})
	if err != nil {
		m.rollbackNteDiskChanges(rollbacks)
	}
	return err
}

func (m *Mod) RemoveGame(ctx context.Context, game string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	game = strings.TrimSpace(game)
	existing, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return err
	}
	if existing != nil && isNTEImporter(existing.Importer) {
		if err := cleanupNteModFolder(existing.ModFolderPath, existing.LinkedModFolderPath); err != nil {
			return err
		}
	}
	return client.GamePaths.Delete(ctx, game)
}

func (m *Mod) rollbackNteDiskChanges(rollbacks []func() error) {
	for index := len(rollbacks) - 1; index >= 0; index-- {
		if err := rollbacks[index](); err != nil && m != nil && m.log != nil {
			m.log.Error(err.Error(), "Mod:rollbackNteDiskChanges")
		}
	}
}

func (m *Mod) ReorderGames(ctx context.Context, games []string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	rows, err := client.GamePaths.List(ctx)
	if err != nil {
		return err
	}
	if len(rows) != len(games) {
		return errors.New("INVALID_GAME_ORDER")
	}
	want := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		want[row.Game] = struct{}{}
	}
	seen := make(map[string]struct{}, len(games))
	for _, game := range games {
		if _, ok := want[game]; !ok {
			return errors.New("INVALID_GAME_ORDER")
		}
		if _, ok := seen[game]; ok {
			return errors.New("INVALID_GAME_ORDER")
		}
		seen[game] = struct{}{}
	}
	return client.GamePaths.Reorder(ctx, games)
}

func (m *Mod) SetNteLauncherPath(ctx context.Context, game, launcherPath string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	launcherPath, err = validFile(launcherPath)
	if err != nil {
		return errors.New("INVALID_LAUNCHER_PATH")
	}
	row, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("game %s not found", game)
	}
	return client.GamePaths.SetNteLauncherPath(ctx, game, launcherPath)
}

func (m *Mod) StartNteLauncher(ctx context.Context, game string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	row, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return err
	}
	if row == nil || row.NteLauncherPath == nil {
		return errors.New("NTE_LAUNCHER_PATH_NOT_SET")
	}
	path, err := validFile(*row.NteLauncherPath)
	if err != nil {
		return errors.New("NTE_LAUNCHER_PATH_NOT_FOUND")
	}
	return startDetached(path)
}

func (m *Mod) GetCharacters(
	ctx context.Context,
	game string,
	searchModPreview *bool,
) ([]FolderGroup, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	row, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ModFolderPath == "" {
		return nil, fmt.Errorf("no mod folder path set for %s", game)
	}
	search, err := m.resolvePreviewSetting(ctx, searchModPreview)
	if err != nil {
		return nil, err
	}
	if isNTEImporter(row.Importer) {
		gameConfig := gameConfig(*row)
		roots := nteRootsFor(gameConfig)
		return nteListGroups(roots, roots.modRoot, search), nil
	}
	groups := listGroups(row.ModFolderPath, search)
	return m.decorateGroups(ctx, game, "", groups), nil
}

func (m *Mod) GetSubGroups(
	ctx context.Context,
	folderPath string,
	searchModPreview *bool,
) ([]FolderGroup, error) {
	search, err := m.resolvePreviewSetting(ctx, searchModPreview)
	if err != nil {
		return nil, err
	}
	game, err := m.ownedPath(ctx, folderPath)
	if err != nil {
		return nil, err
	}
	if isNTEImporter(game.Importer) {
		return nteListGroups(nteRootsFor(*game), folderPath, search), nil
	}
	groups := listGroups(folderPath, search)
	return m.decorateGroups(ctx, game.Game, gameRelativePath(game.ModFolderPath, folderPath), groups), nil
}

func (m *Mod) GetMods(ctx context.Context, groupPath string) (FolderGroup, error) {
	game, err := m.ownedPath(ctx, groupPath)
	if err != nil {
		return FolderGroup{}, err
	}
	if isNTEImporter(game.Importer) {
		search, settingErr := m.resolvePreviewSetting(ctx, nil)
		if settingErr != nil {
			return FolderGroup{}, settingErr
		}
		return nteScanGroup(nteRootsFor(*game), groupPath, search), nil
	}
	group := scanGroup(groupPath)
	return m.filterManualMods(ctx, *game, groupPath, group), nil
}

func (m *Mod) GetModsLight(ctx context.Context, groupPath string) (FolderGroup, error) {
	game, err := m.ownedPath(ctx, groupPath)
	if err != nil {
		return FolderGroup{}, err
	}
	if isNTEImporter(game.Importer) {
		search, settingErr := m.resolvePreviewSetting(ctx, nil)
		if settingErr != nil {
			return FolderGroup{}, settingErr
		}
		return nteScanGroupLight(nteRootsFor(*game), groupPath, search), nil
	}
	group := scanGroupLight(groupPath)
	return m.filterManualMods(ctx, *game, groupPath, group), nil
}

func (m *Mod) GetLastGame(ctx context.Context) (*string, error) {
	return m.settingValue(ctx, lastGameSettingKey)
}

func (m *Mod) SetLastGame(ctx context.Context, game string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	return client.Settings.Upsert(ctx, lastGameSettingKey, stringPointer(game))
}

func (m *Mod) GetExpandedGroups(ctx context.Context) ([]string, error) {
	value, err := m.settingValue(ctx, expandedGroupsSettingKey)
	if err != nil || value == nil {
		return []string{}, err
	}
	return decodeStringSlice(*value), nil
}

func (m *Mod) SetExpandedGroups(ctx context.Context, paths []string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	value := string(raw)
	return client.Settings.Upsert(ctx, expandedGroupsSettingKey, &value)
}

func (m *Mod) settingValue(ctx context.Context, key string) (*string, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	row, err := client.Settings.Get(ctx, key)
	if err != nil || row == nil {
		return nil, err
	}
	return row.Value, nil
}

func (m *Mod) resolvePreviewSetting(ctx context.Context, override *bool) (bool, error) {
	if override != nil {
		return *override, nil
	}
	if m.settings == nil {
		return false, nil
	}
	return m.settings.GetSearchModPreview(ctx)
}

func (m *Mod) ownedPath(ctx context.Context, target string) (*GameConfig, error) {
	target, err := resolveForCompare(strings.TrimSpace(target))
	if err != nil {
		return nil, err
	}
	games, err := m.GetGames(ctx)
	if err != nil {
		return nil, err
	}
	var best *GameConfig
	bestRootLength := 0
	for i := range games {
		roots := []string{games[i].ModFolderPath}
		if games[i].LinkedModFolderPath != nil {
			roots = append(roots, *games[i].LinkedModFolderPath)
		}
		for _, root := range roots {
			resolvedRoot, resolveErr := resolveForCompare(root)
			if resolveErr != nil {
				continue
			}
			if pathWithin(resolvedRoot, target) && len(resolvedRoot) > bestRootLength {
				copy := games[i]
				best = &copy
				bestRootLength = len(resolvedRoot)
			}
		}
	}
	if best == nil {
		return nil, errors.New("MOD_PATH_OUTSIDE_MANAGED_ROOT")
	}
	return best, nil
}

func resolveForCompare(target string) (string, error) {
	// Follow Electron merge/validate resolveForCompare: cycle detection, readlink
	// for dangling links, then parent walk. EvalSymlinks alone rejects in-tree
	// dangling aliases that Electron still treats as owned.
	seen := map[string]struct{}{}
	var resolve func(string) (string, error)
	resolve = func(currentPath string) (string, error) {
		resolved, err := filepath.Abs(currentPath)
		if err != nil {
			return "", err
		}
		seenKey := strings.ToLower(filepath.Clean(resolved))
		if _, exists := seen[seenKey]; exists {
			return "", errors.New("MOD_PATH_OUTSIDE_MANAGED_ROOT")
		}
		seen[seenKey] = struct{}{}

		info, err := os.Lstat(resolved)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, readErr := os.Readlink(resolved)
				if readErr != nil {
					return "", readErr
				}
				next := linkTarget
				if !filepath.IsAbs(next) {
					next = filepath.Join(filepath.Dir(resolved), linkTarget)
				}
				return resolve(next)
			}
			realPath, evalErr := filepath.EvalSymlinks(resolved)
			if evalErr != nil {
				return "", evalErr
			}
			return filepath.Clean(realPath), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return filepath.Clean(resolved), nil
		}
		parentResolved, parentErr := resolve(parent)
		if parentErr != nil {
			return "", parentErr
		}
		return filepath.Join(parentResolved, filepath.Base(resolved)), nil
	}
	return resolve(target)
}

func validDirectory(input string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", errors.New("INVALID_MOD_FOLDER_PATH")
	}
	return filepath.Clean(path), nil
}

func configuredDirectory(input string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

func optionalDirectory(input *string) (*string, error) {
	input = cleanOptional(input)
	if input == nil {
		return nil, nil
	}
	value, err := configuredDirectory(*input)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func validFile(input string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("INVALID_FILE_PATH")
	}
	return filepath.Clean(path), nil
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isNTEImporter(importer *string) bool {
	return importer != nil && strings.Contains(strings.ToLower(*importer), "nte")
}

func stringPointer(value string) *string { return &value }

func decodeStringSlice(value string) []string {
	var paths []string
	if err := json.Unmarshal([]byte(value), &paths); err != nil {
		return []string{}
	}
	return paths
}

func pathWithin(root, target string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func isDisabled(name string) bool { return disabledPrefixRE.MatchString(strings.TrimSpace(name)) }

func stripDisabled(name string) string {
	return strings.TrimSpace(disabledPrefixRE.ReplaceAllString(strings.TrimSpace(name), ""))
}

func stableID(groupPath, modPath string) string {
	rel, err := filepath.Rel(groupPath, modPath)
	if err != nil {
		rel = modPath
	}
	parts := strings.FieldsFunc(filepath.ToSlash(rel), func(r rune) bool { return r == '/' })
	for i := range parts {
		parts[i] = strings.ToLower(stripDisabled(parts[i]))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "/")))
	return hex.EncodeToString(sum[:])
}

func fileMtimeMS(info fs.FileInfo) float64 {
	return float64(info.ModTime().UnixNano()) / float64(time.Millisecond)
}

func (m *Mod) shaderImporters() []shaderImporter {
	if m == nil || m.xxmi == nil {
		return nil
	}
	importers, err := m.xxmi.GetEnabledImporters(context.Background())
	if err != nil {
		m.logShaderError(err, "Mod:getEnabledImporters")
		return nil
	}
	out := make([]shaderImporter, len(importers))
	for i, importer := range importers {
		out[i] = shaderImporter{Key: importer.Key, ImporterFolder: importer.ImporterFolder}
	}
	return out
}

func (m *Mod) shaderGames() []shaderGame {
	if m == nil || m.client == nil {
		return nil
	}
	games, err := m.GetGames(context.Background())
	if err != nil {
		m.logShaderError(err, "Mod:getShaderFixesManifestSearchRoots:games")
		return nil
	}
	out := make([]shaderGame, len(games))
	for i, game := range games {
		importer := ""
		if game.Importer != nil {
			importer = *game.Importer
		}
		out[i] = shaderGame{Game: game.Game, ModFolderPath: game.ModFolderPath, Importer: importer}
	}
	return out
}

func (m *Mod) logShaderError(err error, where string) {
	if err != nil && m != nil && m.log != nil {
		m.log.Error(err.Error(), where)
	}
}

func (m *Mod) HasSingleTopLevelDirectory(ctx context.Context, archivePath string) (bool, error) {
	if m == nil || m.archive == nil {
		return false, errors.New("archive service is not configured")
	}
	return m.archive.HasSingleTopLevelDirectory(ctx, archivePath)
}

func (m *Mod) SelectFolder(ctx context.Context, game string) (*string, error) {
	path, err := m.pickDirectory("Select " + game + " Mod Folder")
	if err != nil || path == nil {
		return path, err
	}
	if err := m.SetGamePath(ctx, game, *path); err != nil {
		return nil, err
	}
	return path, nil
}

func (m *Mod) PickFolder() (*string, error) {
	return m.pickDirectory("")
}

func (m *Mod) PickExecutable() (*string, error) {
	if m == nil || m.dialog == nil {
		return nil, errors.New("dialog service is not configured")
	}
	result, err := m.dialog.ShowOpenDialog(platform.OpenDialogOptions{
		Properties: []string{"openFile"},
		Filters:    []platform.FileFilter{{Name: "Launcher", Extensions: []string{"exe"}}},
	})
	if err != nil {
		return nil, err
	}
	if result.Canceled || len(result.FilePaths) == 0 {
		return nil, nil
	}
	path := result.FilePaths[0]
	return &path, nil
}

func (m *Mod) pickDirectory(title string) (*string, error) {
	if m == nil || m.dialog == nil {
		return nil, errors.New("dialog service is not configured")
	}
	result, err := m.dialog.ShowOpenDialog(platform.OpenDialogOptions{
		Title:      title,
		Properties: []string{"openDirectory"},
	})
	if err != nil {
		return nil, err
	}
	if result.Canceled || len(result.FilePaths) == 0 {
		return nil, nil
	}
	path := result.FilePaths[0]
	return &path, nil
}

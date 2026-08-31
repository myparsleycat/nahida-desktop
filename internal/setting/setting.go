package setting

import (
	"context"
	"encoding/json"
	"fmt"

	"nahida.live/desktop/internal/db"
)

// Options configure locale-sensitive defaults and optional afterSet hooks.
type Options struct {
	Locale     string
	LLMBaseURL string
	Hooks      Hooks
}

// Setting is the Go port of Electron desktop/src/main/setting.ts.
// It reads and writes typed settings through the shipped db.Client.
type Setting struct {
	client *db.Client
	opts   Options
	specs  map[string]spec
}

func New(client *db.Client) *Setting {
	return NewWithOptions(client, Options{})
}

func NewWithOptions(client *db.Client, opts Options) *Setting {
	return &Setting{
		client: client,
		opts:   opts,
		specs:  buildSpecs(),
	}
}

// Open opens a caller-supplied SQLite file, reconciles the shipped schema,
// constructs Setting against that client, and seeds language via Get.
func Open(ctx context.Context, path string) (*Setting, error) {
	return OpenWithOptions(ctx, path, Options{})
}

func OpenWithOptions(ctx context.Context, path string, opts Options) (*Setting, error) {
	client, err := db.New(path)
	if err != nil {
		return nil, err
	}
	if err := client.Reconcile(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	s := NewWithOptions(client, opts)
	if err := s.MigrateElectronStorage(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if _, err := s.Get(ctx, KeyGeneralLanguage); err != nil {
		_ = client.Close()
		return nil, err
	}
	return s, nil
}

//wails:ignore
func (s *Setting) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	return err
}

//wails:ignore
func (s *Setting) UseClient(client *db.Client) {
	s.client = client
}

// UseHooks replaces afterSet hooks on an already constructed Setting.
//
//wails:ignore
func (s *Setting) UseHooks(h Hooks) {
	s.opts.Hooks = h
}

// UseLocale sets the OS locale used for the first-run language default.
//
//wails:ignore
func (s *Setting) UseLocale(locale string) {
	s.opts.Locale = locale
}

//wails:ignore
func (s *Setting) Client() *db.Client {
	return s.client
}

// MigrateElectronStorage preserves settings from the Electron database when
// the Go port uses a different storage key. Existing Go values always win.
//
//wails:ignore
func (s *Setting) MigrateElectronStorage(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("setting is not bound to a database")
	}
	for _, migration := range electronStorageKeyMigrations {
		if err := s.client.Settings.MoveIfMissing(ctx, migration.Source, migration.Destination); err != nil {
			return fmt.Errorf("migrate setting %q to %q: %w", migration.Source, migration.Destination, err)
		}
	}
	return nil
}

func (s *Setting) spec(key string) (spec, error) {
	if s == nil || s.client == nil {
		return spec{}, fmt.Errorf("setting is not bound to a database")
	}
	sp, ok := s.specs[key]
	if !ok {
		return spec{}, fmt.Errorf("unknown setting key %q", key)
	}
	return sp, nil
}

func (s *Setting) Get(ctx context.Context, key string) (any, error) {
	sp, err := s.spec(key)
	if err != nil {
		return nil, err
	}
	row, err := s.client.Settings.Get(ctx, sp.def.StorageKey)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Value == nil {
		fallback := sp.resolved(s, sp.getDefault(s))
		stored := sp.stored(s, fallback)
		if err := s.client.Settings.Upsert(ctx, sp.def.StorageKey, &stored); err != nil {
			return nil, err
		}
		return fallback, nil
	}

	resolved := sp.resolved(s, sp.fromStored(s, row.Value))
	stored := sp.stored(s, resolved)
	if stored != *row.Value {
		if err := s.client.Settings.Upsert(ctx, sp.def.StorageKey, &stored); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func (s *Setting) GetMany(ctx context.Context, keys []string) (map[string]any, error) {
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		value, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func (s *Setting) Set(ctx context.Context, key string, value any) error {
	sp, err := s.spec(key)
	if err != nil {
		return err
	}
	normalized := sp.resolved(s, value)
	stored := sp.stored(s, normalized)
	if err := s.client.Settings.Upsert(ctx, sp.def.StorageKey, &stored); err != nil {
		return err
	}
	if sp.afterSet != nil {
		if err := sp.afterSet(s, ctx, normalized); err != nil {
			return err
		}
	}
	s.opts.Hooks.set(key, normalized)
	return nil
}

type Bounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (s *Setting) GetBounds(ctx context.Context) (*Bounds, error) {
	return s.getStoredBounds(ctx, storageBounds)
}

func (s *Setting) SetBounds(ctx context.Context, bounds Bounds) error {
	return s.setStoredBounds(ctx, storageBounds, bounds)
}

func (s *Setting) GetSettingBounds(ctx context.Context) (*Bounds, error) {
	return s.getStoredBounds(ctx, storageSettingBounds)
}

func (s *Setting) SetSettingBounds(ctx context.Context, bounds Bounds) error {
	return s.setStoredBounds(ctx, storageSettingBounds, bounds)
}

func (s *Setting) getStoredBounds(ctx context.Context, key string) (*Bounds, error) {
	row, err := s.client.Settings.Get(ctx, key)
	if err != nil || row == nil || row.Value == nil {
		return nil, err
	}
	var bounds Bounds
	if err := json.Unmarshal([]byte(*row.Value), &bounds); err != nil {
		return nil, fmt.Errorf("decode %s bounds: %w", key, err)
	}
	return &bounds, nil
}

func (s *Setting) setStoredBounds(ctx context.Context, key string, bounds Bounds) error {
	raw, err := json.Marshal(bounds)
	if err != nil {
		return err
	}
	value := string(raw)
	return s.client.Settings.Upsert(ctx, key, &value)
}

func (s *Setting) GetImageCacheSize(ctx context.Context) (int64, error) {
	return s.client.ImageCache.SumSize(ctx)
}

func (s *Setting) ClearImageCache(ctx context.Context) error {
	return s.client.ImageCache.DeleteAll(ctx)
}

type AdvancedRow struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

func (s *Setting) AdvancedGetAll(ctx context.Context) ([]AdvancedRow, error) {
	// Touch debug.openConsole so a missing row is seeded, matching Electron.
	if _, err := s.Get(ctx, KeyDebugOpenConsole); err != nil {
		return nil, err
	}
	rows, err := s.client.Settings.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdvancedRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, AdvancedRow{
			Key:   row.Key,
			Value: maskSensitiveValue(row.Key, row.Value),
		})
	}
	return out, nil
}

func (s *Setting) AdvancedSet(ctx context.Context, key, value string) error {
	existing, err := s.client.Settings.Get(ctx, key)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("setting key %q not found", key)
	}
	stored := value
	if key == definitionsByKey[KeyDebugOpenConsole].StorageKey {
		stored = formatBool(parseBooleanSetting(&value, false))
	}
	if err := s.client.Settings.UpdateValue(ctx, key, &stored); err != nil {
		return err
	}
	if key == definitionsByKey[KeyDebugOpenConsole].StorageKey {
		s.opts.Hooks.openConsoleChanged(stored == "true")
	}
	s.opts.Hooks.set(key, stored)
	s.opts.Hooks.rendererReload()
	return nil
}

func (s *Setting) GetRunOnStartup(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyGeneralRunOnStartup)
}
func (s *Setting) SetRunOnStartup(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyGeneralRunOnStartup, enabled)
}

func (s *Setting) GetLanguage(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyGeneralLanguage)
}
func (s *Setting) SetLanguage(ctx context.Context, language string) error {
	return s.Set(ctx, KeyGeneralLanguage, language)
}

func (s *Setting) GetMoveTransferPageWhenStartTransfer(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyGeneralMoveTransferPageWhenStartTransfer)
}
func (s *Setting) SetMoveTransferPageWhenStartTransfer(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyGeneralMoveTransferPageWhenStartTransfer, enabled)
}

func (s *Setting) GetPowerSaveBlockInTransfer(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyGeneralPowerSaveBlockInTransfer)
}
func (s *Setting) SetPowerSaveBlockInTransfer(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyGeneralPowerSaveBlockInTransfer, enabled)
}

func (s *Setting) GetDefaultStartPage(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyGeneralDefaultStartPage)
}
func (s *Setting) SetDefaultStartPage(ctx context.Context, page string) error {
	return s.Set(ctx, KeyGeneralDefaultStartPage, page)
}

func (s *Setting) GetAutoUpdateMode(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyGeneralAutoUpdateMode)
}
func (s *Setting) SetAutoUpdateMode(ctx context.Context, mode string) error {
	return s.Set(ctx, KeyGeneralAutoUpdateMode, mode)
}

func (s *Setting) GetRunInBackground(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyGeneralRunInBackground)
}
func (s *Setting) SetRunInBackground(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyGeneralRunInBackground, enabled)
}

func (s *Setting) GetLogLevel(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyGeneralLogLevel)
}
func (s *Setting) SetLogLevel(ctx context.Context, level string) error {
	return s.Set(ctx, KeyGeneralLogLevel, level)
}

func (s *Setting) GetSidebarLayout(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModSidebarLayout)
}
func (s *Setting) SetSidebarLayout(ctx context.Context, mode string) error {
	return s.Set(ctx, KeyModSidebarLayout, mode)
}

func (s *Setting) GetCharacterSidebarWidth(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyModCharacterSidebarWidth)
}
func (s *Setting) SetCharacterSidebarWidth(ctx context.Context, width int) error {
	return s.Set(ctx, KeyModCharacterSidebarWidth, width)
}

func (s *Setting) GetArchiveExtractPathMode(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModArchiveExtractPathMode)
}
func (s *Setting) SetArchiveExtractPathMode(ctx context.Context, mode string) error {
	return s.Set(ctx, KeyModArchiveExtractPathMode, mode)
}

func (s *Setting) GetDeleteArchiveAfterExtract(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyModDeleteArchiveAfterExtract)
}
func (s *Setting) SetDeleteArchiveAfterExtract(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyModDeleteArchiveAfterExtract, enabled)
}

func (s *Setting) GetMoveFolderInsteadOfCopy(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyModMoveFolderInsteadOfCopy)
}
func (s *Setting) SetMoveFolderInsteadOfCopy(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyModMoveFolderInsteadOfCopy, enabled)
}

func (s *Setting) GetGridLayoutMode(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModGridLayoutMode)
}
func (s *Setting) SetGridLayoutMode(ctx context.Context, mode string) error {
	return s.Set(ctx, KeyModGridLayoutMode, mode)
}

func (s *Setting) GetGridResponsiveBaseWidth(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyModGridResponsiveBaseWidth)
}
func (s *Setting) SetGridResponsiveBaseWidth(ctx context.Context, width int) error {
	return s.Set(ctx, KeyModGridResponsiveBaseWidth, width)
}

func (s *Setting) GetGridFixedCardWidth(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyModGridFixedCardWidth)
}
func (s *Setting) SetGridFixedCardWidth(ctx context.Context, width int) error {
	return s.Set(ctx, KeyModGridFixedCardWidth, width)
}

func (s *Setting) GetGridFixedColumnCount(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyModGridFixedColumnCount)
}
func (s *Setting) SetGridFixedColumnCount(ctx context.Context, count int) error {
	return s.Set(ctx, KeyModGridFixedColumnCount, count)
}

func (s *Setting) GetSearchModPreview(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyModSearchModPreview)
}
func (s *Setting) SetSearchModPreview(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyModSearchModPreview, enabled)
}

func (s *Setting) GetBisectPreserveD3dx(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyGeneralBisectPreserveD3dx)
}

func (s *Setting) SetBisectPreserveD3dx(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyGeneralBisectPreserveD3dx, enabled)
}

func (s *Setting) GetCopyShaderFixesOnEnable(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyModCopyShaderFixesOnEnable)
}
func (s *Setting) SetCopyShaderFixesOnEnable(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyModCopyShaderFixesOnEnable, enabled)
}

func (s *Setting) GetDisabledPrefixStyle(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModDisabledPrefixStyle)
}
func (s *Setting) SetDisabledPrefixStyle(ctx context.Context, style string) error {
	return s.Set(ctx, KeyModDisabledPrefixStyle, style)
}

func (s *Setting) GetDownloadConcurrency(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyTransferDownloadConcurrency)
}
func (s *Setting) SetDownloadConcurrency(ctx context.Context, concurrency int) error {
	return s.Set(ctx, KeyTransferDownloadConcurrency, concurrency)
}

func (s *Setting) GetDownloadBandwidthLimitMibps(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyTransferDownloadBandwidthLimitMibps)
}
func (s *Setting) SetDownloadBandwidthLimitMibps(ctx context.Context, mibps int) error {
	return s.Set(ctx, KeyTransferDownloadBandwidthLimitMibps, mibps)
}

func (s *Setting) GetUploadConcurrency(ctx context.Context) (int, error) {
	return s.getInt(ctx, KeyTransferUploadConcurrency)
}
func (s *Setting) SetUploadConcurrency(ctx context.Context, concurrency int) error {
	return s.Set(ctx, KeyTransferUploadConcurrency, concurrency)
}

func (s *Setting) GetNameSortPolicy(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyDriveNameSortPolicy)
}
func (s *Setting) SetNameSortPolicy(ctx context.Context, policy string) error {
	return s.Set(ctx, KeyDriveNameSortPolicy, policy)
}

func (s *Setting) GetOpenConsole(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyDebugOpenConsole)
}
func (s *Setting) SetOpenConsole(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyDebugOpenConsole, enabled)
}

func (s *Setting) GetToneMapping(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModelViewerToneMapping)
}
func (s *Setting) SetToneMapping(ctx context.Context, toneMapping string) error {
	return s.Set(ctx, KeyModelViewerToneMapping, normalizeEnum(toneMapping, modelViewerToneMappings, defaultToneMapping))
}

func (s *Setting) GetEnvironment(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyModelViewerEnvironment)
}
func (s *Setting) SetEnvironment(ctx context.Context, environment string) error {
	return s.Set(ctx, KeyModelViewerEnvironment, normalizeEnum(environment, modelViewerEnvironments, defaultEnvironment))
}

func (s *Setting) GetExposure(ctx context.Context) (float64, error) {
	return s.getFloat(ctx, KeyModelViewerExposure)
}
func (s *Setting) SetExposure(ctx context.Context, exposure float64) error {
	return s.Set(ctx, KeyModelViewerExposure, exposure)
}

func (s *Setting) GetPersistToggles(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyXXMIPersistToggles)
}
func (s *Setting) SetPersistToggles(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyXXMIPersistToggles, enabled)
}

func (s *Setting) GetToggleViewerAutoGenerate(ctx context.Context) (bool, error) {
	return s.getBool(ctx, KeyXXMIToggleViewerAutoGenerate)
}
func (s *Setting) SetToggleViewerAutoGenerate(ctx context.Context, enabled bool) error {
	return s.Set(ctx, KeyXXMIToggleViewerAutoGenerate, enabled)
}

func (s *Setting) GetToggleViewerHotkey(ctx context.Context) (string, error) {
	return s.getString(ctx, KeyXXMIToggleViewerHotkey)
}
func (s *Setting) SetToggleViewerHotkey(ctx context.Context, hotkey string) error {
	return s.Set(ctx, KeyXXMIToggleViewerHotkey, hotkey)
}

func (s *Setting) getBool(ctx context.Context, key string) (bool, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return asBool(value), nil
}

func (s *Setting) getString(ctx context.Context, key string) (string, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return asString(value), nil
}

func (s *Setting) getInt(ctx context.Context, key string) (int, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	n, ok := asFloat(value)
	if !ok {
		return 0, fmt.Errorf("setting %q is not a number", key)
	}
	return int(n), nil
}

func (s *Setting) getFloat(ctx context.Context, key string) (float64, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	n, ok := asFloat(value)
	if !ok {
		return 0, fmt.Errorf("setting %q is not a number", key)
	}
	return n, nil
}

package setting

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func openTemp(t *testing.T, opts Options) (*Setting, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := OpenWithOptions(context.Background(), path, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func rawValue(t *testing.T, s *Setting, storageKey string) string {
	t.Helper()
	row, err := s.Client().Settings.Get(context.Background(), storageKey)
	if err != nil {
		t.Fatalf("settings.get(%q): %v", storageKey, err)
	}
	if row == nil || row.Value == nil {
		t.Fatalf("settings.get(%q): missing row", storageKey)
	}
	return *row.Value
}

func TestGetMissingWritesDefaultAndSurvivesReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got, err := s.Get(ctx, KeyModDeleteArchiveAfterExtract)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	enabled, ok := got.(bool)
	if !ok || !enabled {
		t.Fatalf("Get = %#v, want true", got)
	}
	if raw := rawValue(t, s, "mod_delete_archive_after_extract"); raw != "true" {
		t.Fatalf("stored %q, want %q", raw, "true")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got, err = reopened.Get(ctx, KeyModDeleteArchiveAfterExtract)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	enabled, ok = got.(bool)
	if !ok || !enabled {
		t.Fatalf("reopen Get = %#v, want true", got)
	}
}

func TestModCompressionDefaultsAndBounds(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t, Options{})
	ctx := context.Background()

	method, err := s.GetCompressionMethod(ctx)
	if err != nil || method != "xpress4k" {
		t.Fatalf("method = %q, err=%v", method, err)
	}
	threshold, err := s.GetCompressionThresholdMib(ctx)
	if err != nil || threshold != 1 {
		t.Fatalf("threshold = %d, err=%v", threshold, err)
	}
	enabled, err := s.GetCompressionEnabled(ctx)
	if err != nil || enabled {
		t.Fatalf("enabled = %v, err=%v", enabled, err)
	}

	if err := s.SetCompressionThresholdMib(ctx, 100); err != nil {
		t.Fatal(err)
	}
	threshold, err = s.GetCompressionThresholdMib(ctx)
	if err != nil || threshold != 64 {
		t.Fatalf("clamped threshold = %d, err=%v", threshold, err)
	}
	if err := s.SetCompressionMethod(ctx, "invalid"); err != nil {
		t.Fatal(err)
	}
	method, err = s.GetCompressionMethod(ctx)
	if err != nil || method != "xpress4k" {
		t.Fatalf("normalized method = %q, err=%v", method, err)
	}
}

func TestSetCompressionConfigStoresOneSnapshot(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	if err := s.SetCompressionConfig(ctx, "zstd", 32); err != nil {
		t.Fatal(err)
	}
	method, methodErr := s.GetCompressionMethod(ctx)
	threshold, thresholdErr := s.GetCompressionThresholdMib(ctx)
	if methodErr != nil || thresholdErr != nil || method != "zstd" || threshold != 32 {
		t.Fatalf("config = %q/%d, methodErr=%v thresholdErr=%v", method, threshold, methodErr, thresholdErr)
	}
}

func TestSetCompressionConfigRollsBackBothValues(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	if _, err := s.GetCompressionMethod(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCompressionThresholdMib(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.SQL().ExecContext(ctx, `CREATE TRIGGER fail_compression_threshold
BEFORE UPDATE ON setting
WHEN NEW.key = 'mod_compression_threshold_mib'
BEGIN
  SELECT RAISE(ABORT, 'threshold write failed');
END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCompressionConfig(ctx, "zstd", 32); err == nil {
		t.Fatal("expected compression config transaction to fail")
	}
	method, methodErr := s.GetCompressionMethod(ctx)
	threshold, thresholdErr := s.GetCompressionThresholdMib(ctx)
	if methodErr != nil || thresholdErr != nil || method != "xpress4k" || threshold != 1 {
		t.Fatalf("rolled back config = %q/%d, methodErr=%v thresholdErr=%v", method, threshold, methodErr, thresholdErr)
	}
}

func TestOpenMigratesElectronStorageKeysWithoutOverwritingGoValues(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	client, err := db.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for index, migration := range electronStorageKeyMigrations {
		value := electronMigrationTestValue(migration.Source, index)
		if err := client.Settings.Upsert(ctx, migration.Source, &value); err != nil {
			t.Fatal(err)
		}
	}
	currentLogLevel := "error"
	if err := client.Settings.Upsert(ctx, "general_log_level", &currentLogLevel); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := OpenWithOptions(ctx, path, Options{Locale: "ko-KR"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for index, migration := range electronStorageKeyMigrations {
		if legacy, err := s.Client().Settings.Get(ctx, migration.Source); err != nil || legacy != nil {
			t.Fatalf("legacy %q = %#v, %v", migration.Source, legacy, err)
		}
		row, err := s.Client().Settings.Get(ctx, migration.Destination)
		if err != nil || row == nil || row.Value == nil {
			t.Fatalf("destination %q = %#v, %v", migration.Destination, row, err)
		}
		want := electronMigrationTestValue(migration.Source, index)
		if migration.Destination == "general_log_level" {
			want = currentLogLevel
		}
		if *row.Value != want {
			t.Fatalf("destination %q = %q, want %q", migration.Destination, *row.Value, want)
		}
	}
	language, err := s.GetLanguage(ctx)
	if err != nil || language != "ja" {
		t.Fatalf("language = %q, %v", language, err)
	}
	if err := s.MigrateElectronStorage(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	bounds, err := s.GetBounds(ctx)
	if err != nil || bounds == nil || *bounds != (Bounds{X: 1, Y: 2, Width: 800, Height: 600}) {
		t.Fatalf("bounds = %#v, %v", bounds, err)
	}
	settingBounds, err := s.GetSettingBounds(ctx)
	if err != nil || settingBounds == nil || *settingBounds != (Bounds{X: 3, Y: 4, Width: 900, Height: 700}) {
		t.Fatalf("setting bounds = %#v, %v", settingBounds, err)
	}
}

func electronMigrationTestValue(source string, index int) string {
	switch source {
	case "language":
		return "ja"
	case "bounds":
		return `{"x":1,"y":2,"width":800,"height":600}`
	case "settingBounds":
		return `{"x":3,"y":4,"width":900,"height":700}`
	default:
		return fmt.Sprintf("legacy-%d", index)
	}
}

func TestSetGetEncodings(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()

	if err := s.Set(ctx, KeyGeneralRunOnStartup, true); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	got, err := s.Get(ctx, KeyGeneralRunOnStartup)
	if err != nil || got != true {
		t.Fatalf("get bool = %#v %v", got, err)
	}
	if raw := rawValue(t, s, "general_run_on_startup"); raw != "true" {
		t.Fatalf("bool stored %q, want true (not 1)", raw)
	}

	if err := s.Set(ctx, KeyModArchiveExtractPathMode, "keep_archive_root"); err != nil {
		t.Fatalf("set enum: %v", err)
	}
	got, err = s.Get(ctx, KeyModArchiveExtractPathMode)
	if err != nil || got != "keep_archive_root" {
		t.Fatalf("get enum = %#v %v", got, err)
	}
	if raw := rawValue(t, s, "mod_archive_extract_path_mode"); raw != "keep_archive_root" {
		t.Fatalf("enum stored %q", raw)
	}

	if err := s.Set(ctx, KeyModGridFixedColumnCount, 100); err != nil {
		t.Fatalf("set clamped: %v", err)
	}
	got, err = s.Get(ctx, KeyModGridFixedColumnCount)
	if err != nil || got != 8 {
		t.Fatalf("get clamped = %#v %v, want 8", got, err)
	}
	if raw := rawValue(t, s, "mod_grid_fixed_column_count"); raw != "8" {
		t.Fatalf("clamped stored %q", raw)
	}

	if err := s.Set(ctx, KeyDrivePasswordList, []string{" secret ", "", "alpha", "<tag>&value>"}); err != nil {
		t.Fatalf("set passwords: %v", err)
	}
	got, err = s.Get(ctx, KeyDrivePasswordList)
	if err != nil {
		t.Fatalf("get passwords: %v", err)
	}
	passwords, ok := got.([]string)
	if !ok || len(passwords) != 3 || passwords[0] != "secret" || passwords[1] != "alpha" || passwords[2] != "<tag>&value>" {
		t.Fatalf("passwords = %#v", got)
	}
	if raw := rawValue(t, s, "drive_password_list"); raw != `["secret","alpha","<tag>&value>"]` {
		t.Fatalf("password list stored %q", raw)
	}

	if err := s.Set(ctx, KeyModAutoResolveDownloadTargetSources, []string{"drive", "unknown", "gamebanana"}); err != nil {
		t.Fatalf("set sources: %v", err)
	}
	got, err = s.Get(ctx, KeyModAutoResolveDownloadTargetSources)
	if err != nil {
		t.Fatalf("get sources: %v", err)
	}
	sources, ok := got.([]string)
	if !ok || len(sources) != 2 || sources[0] != "drive" || sources[1] != "gamebanana" {
		t.Fatalf("sources = %#v", got)
	}
	if raw := rawValue(t, s, "mod_auto_resolve_download_target_sources"); raw != `["drive","gamebanana"]` {
		t.Fatalf("sources stored %q", raw)
	}
}

func TestInvalidLogLevelNormalizesBeforeStorageAndHook(t *testing.T) {
	t.Parallel()

	var levels []string
	s, _ := openTemp(t, Options{Hooks: Hooks{
		AfterLogLevelChanged: func(level string) {
			levels = append(levels, level)
		},
	}})
	ctx := context.Background()
	invalid := "verbose"
	if err := s.Client().Settings.Upsert(ctx, "general_log_level", &invalid); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, KeyGeneralLogLevel)
	if err != nil || got != defaultLogLevel {
		t.Fatalf("Get invalid log level = %#v, %v, want %q", got, err, defaultLogLevel)
	}
	if raw := rawValue(t, s, "general_log_level"); raw != defaultLogLevel {
		t.Fatalf("rewritten log level = %q, want %q", raw, defaultLogLevel)
	}
	if err := s.Set(ctx, KeyGeneralLogLevel, "VERBOSE"); err != nil {
		t.Fatal(err)
	}
	if raw := rawValue(t, s, "general_log_level"); raw != defaultLogLevel {
		t.Fatalf("stored log level = %q, want %q", raw, defaultLogLevel)
	}
	if len(levels) != 1 || levels[0] != defaultLogLevel {
		t.Fatalf("log level hooks = %v, want [%s]", levels, defaultLogLevel)
	}
}

func TestStoredErrorLogLevelIsPreserved(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	stored := "error"
	if err := s.Client().Settings.Upsert(ctx, "general_log_level", &stored); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, KeyGeneralLogLevel)
	if err != nil || got != "error" {
		t.Fatalf("Get stored error level = %#v, %v", got, err)
	}
	if raw := rawValue(t, s, "general_log_level"); raw != "error" {
		t.Fatalf("stored log level changed to %q", raw)
	}
}

func TestRunOnStartupInvokesPlatformHook(t *testing.T) {
	var values []bool
	s, _ := openTemp(t, Options{Hooks: Hooks{
		AfterRunOnStartupChanged: func(enabled bool) error {
			values = append(values, enabled)
			return nil
		},
	}})
	if err := s.SetRunOnStartup(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || !values[0] {
		t.Fatalf("run-on-startup hook = %v", values)
	}
}

func TestGetRewritesDirtyStoredValue(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()

	dirtyWidth := "100"
	if err := s.Client().Settings.Upsert(ctx, "mod_grid_responsive_base_width", &dirtyWidth); err != nil {
		t.Fatalf("seed dirty width: %v", err)
	}
	got, err := s.Get(ctx, KeyModGridResponsiveBaseWidth)
	if err != nil || got != 240 {
		t.Fatalf("dirty width Get = %#v %v, want 240", got, err)
	}
	if raw := rawValue(t, s, "mod_grid_responsive_base_width"); raw != "240" {
		t.Fatalf("dirty width rewritten to %q, want 240", raw)
	}

	dirtyEnum := "nope"
	if err := s.Client().Settings.Upsert(ctx, "mod_grid_layout_mode", &dirtyEnum); err != nil {
		t.Fatalf("seed dirty enum: %v", err)
	}
	got, err = s.Get(ctx, KeyModGridLayoutMode)
	if err != nil || got != "responsive" {
		t.Fatalf("dirty enum Get = %#v %v", got, err)
	}
	if raw := rawValue(t, s, "mod_grid_layout_mode"); raw != "responsive" {
		t.Fatalf("dirty enum rewritten to %q", raw)
	}

	one := "1"
	if err := s.Client().Settings.Upsert(ctx, "general_run_on_startup", &one); err != nil {
		t.Fatalf("seed dirty bool: %v", err)
	}
	got, err = s.Get(ctx, KeyGeneralRunOnStartup)
	if err != nil || got != false {
		t.Fatalf("\"1\" is not true: %#v %v", got, err)
	}
	if raw := rawValue(t, s, "general_run_on_startup"); raw != "false" {
		t.Fatalf("dirty bool rewritten to %q", raw)
	}
}

func TestDefaultLanguageFromLocale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		locale string
		want   string
	}{
		{"", "en"},
		{"ko", "ko"},
		{"ko-KR", "en"},
		{"ko_KR.UTF-8", "en"},
		{"ja-JP", "en"},
		{"zh-Hans-CN", "zh"},
		{"fr-FR", "en"},
		{"C", "en"},
	}
	for _, tc := range cases {
		if got := defaultLanguageFromLocale(tc.locale); got != tc.want {
			t.Fatalf("defaultLanguageFromLocale(%q) = %q, want %q", tc.locale, got, tc.want)
		}
	}
}

func TestSetLanguageInvokesHooks(t *testing.T) {
	t.Parallel()

	var languages []string
	var updates []struct {
		key   string
		value any
	}
	s, _ := openTemp(t, Options{
		Locale: "ko",
		Hooks: Hooks{
			AfterSet: func(key string, value any) {
				updates = append(updates, struct {
					key   string
					value any
				}{key, value})
			},
			AfterLanguageChanged: func(language string) {
				languages = append(languages, language)
			},
		},
	})
	ctx := context.Background()

	got, err := s.Get(ctx, KeyGeneralLanguage)
	if err != nil || got != "ko" {
		t.Fatalf("seeded language = %#v %v, want ko", got, err)
	}
	if len(languages) != 0 {
		t.Fatalf("Get must not fire language hook: %v", languages)
	}

	if err := s.Set(ctx, KeyGeneralLanguage, "ja"); err != nil {
		t.Fatalf("Set language: %v", err)
	}
	if len(languages) != 1 || languages[0] != "ja" {
		t.Fatalf("language hook = %v, want [ja]", languages)
	}
	if len(updates) != 1 || updates[0].key != KeyGeneralLanguage || updates[0].value != "ja" {
		t.Fatalf("AfterSet = %+v, want general.language=ja", updates)
	}
}

func TestGetMany(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{Locale: "ja-JP"})
	ctx := context.Background()
	keys := []string{KeyGeneralLanguage, KeyModGridLayoutMode, KeyDrivePasswordList}
	got, err := s.GetMany(ctx, keys)
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("GetMany len = %d, want 3", len(got))
	}
	if got[KeyGeneralLanguage] != "en" {
		t.Fatalf("language = %#v, want en", got[KeyGeneralLanguage])
	}
	if got[KeyModGridLayoutMode] != "responsive" {
		t.Fatalf("grid = %#v", got[KeyModGridLayoutMode])
	}
	list, ok := got[KeyDrivePasswordList].([]string)
	if !ok || len(list) != 0 {
		t.Fatalf("passwords = %#v", got[KeyDrivePasswordList])
	}
}

func TestAdvancedGetAllMasksAndSetRejectsUnknown(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()

	if err := s.Set(ctx, KeyDrivePasswordList, []string{"hunter2"}); err != nil {
		t.Fatalf("set passwords: %v", err)
	}
	token := "plain-token"
	if err := s.Client().Settings.Upsert(ctx, "auth_token", &token); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	before, err := s.Client().Settings.List(ctx)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}

	rows, err := s.AdvancedGetAll(ctx)
	if err != nil {
		t.Fatalf("AdvancedGetAll: %v", err)
	}
	if len(rows) > len(before)+1 {
		t.Fatalf("AdvancedGetAll invented rows: before %d after %d", len(before), len(rows))
	}

	var (
		sawToken     bool
		sawPasswords bool
		sawLanguage  bool
	)
	for _, row := range rows {
		switch row.Key {
		case "auth_token":
			sawToken = true
			if row.Value == nil || *row.Value != "********" {
				t.Fatalf("token not masked: %#v", row.Value)
			}
		case "drive_password_list":
			sawPasswords = true
			if row.Value == nil || *row.Value != "********" {
				t.Fatalf("password list not masked: %#v", row.Value)
			}
		case "general_language":
			sawLanguage = true
			if row.Value == nil || *row.Value == "********" {
				t.Fatalf("language should not be masked: %#v", row.Value)
			}
		}
	}
	if !sawToken || !sawPasswords || !sawLanguage {
		t.Fatalf("missing expected rows: token=%v passwords=%v language=%v", sawToken, sawPasswords, sawLanguage)
	}

	if err := s.AdvancedSet(ctx, "does_not_exist", "x"); err == nil {
		t.Fatal("AdvancedSet unknown key: want error")
	}

	publicKeys := map[string]struct{}{}
	for _, def := range allDefinitions {
		publicKeys[def.StorageKey] = struct{}{}
	}
	invented := 0
	for _, row := range rows {
		if _, ok := publicKeys[row.Key]; ok && row.Key != "general_language" && row.Key != "debug_open_console" && row.Key != "drive_password_list" {
			invented++
		}
	}
	if invented != 0 {
		t.Fatalf("AdvancedGetAll invented %d APP_SETTINGS rows", invented)
	}
}

func TestAdvancedSetEmitsUpdateAndReload(t *testing.T) {
	t.Parallel()

	var updates []struct {
		key   string
		value any
	}
	var reloads int
	var consoleEnabled []bool
	s, _ := openTemp(t, Options{
		Hooks: Hooks{
			AfterSet: func(key string, value any) {
				updates = append(updates, struct {
					key   string
					value any
				}{key, value})
			},
			AfterRendererReload: func() {
				reloads++
			},
			AfterOpenConsoleChanged: func(enabled bool) {
				consoleEnabled = append(consoleEnabled, enabled)
			},
		},
	})
	ctx := context.Background()

	if err := s.AdvancedSet(ctx, "does_not_exist", "x"); err == nil {
		t.Fatal("AdvancedSet unknown key: want error")
	}
	if len(updates) != 0 || reloads != 0 || len(consoleEnabled) != 0 {
		t.Fatalf("unknown key must not emit: updates=%+v reloads=%d console=%v", updates, reloads, consoleEnabled)
	}

	if err := s.AdvancedSet(ctx, "general_language", "zh"); err != nil {
		t.Fatalf("AdvancedSet language: %v", err)
	}
	if raw := rawValue(t, s, "general_language"); raw != "zh" {
		t.Fatalf("stored language = %q, want zh", raw)
	}
	if len(updates) != 1 || updates[0].key != "general_language" || updates[0].value != "zh" {
		t.Fatalf("AfterSet = %+v, want general_language=zh", updates)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}

	if _, err := s.Get(ctx, KeyDebugOpenConsole); err != nil {
		t.Fatalf("seed openConsole: %v", err)
	}
	if err := s.AdvancedSet(ctx, "debug_open_console", "true"); err != nil {
		t.Fatalf("AdvancedSet openConsole: %v", err)
	}
	if raw := rawValue(t, s, "debug_open_console"); raw != "true" {
		t.Fatalf("stored openConsole = %q, want true", raw)
	}
	if len(consoleEnabled) != 1 || !consoleEnabled[0] {
		t.Fatalf("openConsole hook = %v, want [true]", consoleEnabled)
	}
	if len(updates) != 2 || updates[1].key != "debug_open_console" || updates[1].value != "true" {
		t.Fatalf("AfterSet after openConsole = %+v", updates)
	}
	if reloads != 2 {
		t.Fatalf("reloads after openConsole = %d, want 2", reloads)
	}
}

func TestImageCacheThroughSetting(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	blob := []byte{0, 1, 2, 3, 255}
	if err := s.Client().ImageCache.InsertIgnore(ctx, db.ImageCacheRow{
		Hash: "abc", Image: blob, Size: int64(len(blob)),
	}); err != nil {
		t.Fatalf("InsertIgnore: %v", err)
	}
	sum, err := s.GetImageCacheSize(ctx)
	if err != nil || sum != int64(len(blob)) {
		t.Fatalf("GetImageCacheSize = %d %v, want %d", sum, err, len(blob))
	}
	if err := s.ClearImageCache(ctx); err != nil {
		t.Fatalf("ClearImageCache: %v", err)
	}
	sum, err = s.GetImageCacheSize(ctx)
	if err != nil || sum != 0 {
		t.Fatalf("after clear = %d %v, want 0", sum, err)
	}
}

func TestPersistTogglesEnablesRunInBackground(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	if err := s.Set(ctx, KeyGeneralRunInBackground, false); err != nil {
		t.Fatalf("disable background: %v", err)
	}
	if err := s.Set(ctx, KeyXXMIPersistToggles, true); err != nil {
		t.Fatalf("enable persist: %v", err)
	}
	got, err := s.Get(ctx, KeyGeneralRunInBackground)
	if err != nil || got != true {
		t.Fatalf("runInBackground = %#v %v, want true", got, err)
	}
	if raw := rawValue(t, s, "general_run_in_background"); raw != "true" {
		t.Fatalf("runInBackground stored %q", raw)
	}
	if raw := rawValue(t, s, "xxmi_persist_toggles"); raw != "true" {
		t.Fatalf("persist stored %q", raw)
	}
}

func TestEveryAppSettingGetSetRoundTrip(t *testing.T) {
	t.Parallel()

	s, _ := openTemp(t, Options{Locale: "en-US"})
	ctx := context.Background()
	for _, key := range AllPublicKeys() {
		value, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if err := s.Set(ctx, key, value); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
		again, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get after Set(%s): %v", key, err)
		}
		if storedString(again) != storedString(value) {
			t.Fatalf("Get/Set %s: %#v vs %#v", key, again, value)
		}
	}
}

func TestStorageKeysFollowScopeSnakeCase(t *testing.T) {
	t.Parallel()

	for _, def := range allDefinitions {
		scope, rest, ok := strings.Cut(def.PublicKey, ".")
		if !ok {
			t.Fatalf("public key %q has no scope separator", def.PublicKey)
		}
		want := snakeCase(scope) + "_" + snakeCase(rest)
		if def.StorageKey != want {
			t.Errorf("storage key for %q = %q, want %q", def.PublicKey, def.StorageKey, want)
		}
	}
}

// snakeCase converts camelCase to snake_case, e.g. "bisectPreserveD3dx" → "bisect_preserve_d3dx".
func snakeCase(s string) string {
	var b strings.Builder
	for i := range len(s) {
		ch := s[i]
		if i > 0 && ch >= 'A' && ch <= 'Z' {
			prev := s[i-1]
			if prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9' {
				b.WriteByte('_')
			}
		}
		if ch >= 'A' && ch <= 'Z' {
			b.WriteByte(ch + 'a' - 'A')
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

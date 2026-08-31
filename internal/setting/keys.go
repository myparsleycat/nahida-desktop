package setting

// Public keys are the API contract with the frontend (frontend/src/shared/settings.ts).
// Storage keys follow a single convention, {scope}_{snake_case}, derived from the
// public key. Electron keys that differ are migrated once during initialization.

const (
	KeyGeneralRunOnStartup                       = "general.runOnStartup"
	KeyGeneralLanguage                           = "general.language"
	KeyGeneralAutoUpdateMode                     = "general.autoUpdateMode"
	KeyGeneralRunInBackground                    = "general.runInBackground"
	KeyGeneralDefaultStartPage                   = "general.defaultStartPage"
	KeyGeneralLogLevel                           = "general.logLevel"
	KeyGeneralMoveTransferPageWhenStartTransfer  = "general.moveTransferPageWhenStartTransfer"
	KeyGeneralPowerSaveBlockInTransfer           = "general.powerSaveBlockInTransfer"
	KeyGeneralBisectPreserveD3dx                 = "general.bisectPreserveD3dx"
	KeyGeneralTitlebarActivityBadgeClickNavigate = "general.titlebarActivityBadgeClickNavigate"

	KeyModArchiveExtractPathMode           = "mod.archiveExtractPathMode"
	KeyModDeleteArchiveAfterExtract        = "mod.deleteArchiveAfterExtract"
	KeyModMoveFolderInsteadOfCopy          = "mod.moveFolderInsteadOfCopy"
	KeyModSearchModPreview                 = "mod.searchModPreview"
	KeyModAutoResolveDownloadTarget        = "mod.autoResolveDownloadTarget"
	KeyModAutoResolveDownloadTargetSources = "mod.autoResolveDownloadTargetSources"
	KeyModCopyShaderFixesOnEnable          = "mod.copyShaderFixesOnEnable"
	KeyModSidebarLayout                    = "mod.sidebarLayout"
	KeyModCharacterSidebarWidth            = "mod.characterSidebarWidth"
	KeyModGridLayoutMode                   = "mod.gridLayoutMode"
	KeyModGridResponsiveBaseWidth          = "mod.gridResponsiveBaseWidth"
	KeyModGridFixedCardWidth               = "mod.gridFixedCardWidth"
	KeyModGridFixedColumnCount             = "mod.gridFixedColumnCount"
	KeyModDisabledPrefixStyle              = "mod.disabledPrefixStyle"
	KeyModReturnToGamebananaAfterDownload  = "mod.returnToGamebananaAfterDownload"

	KeyToolsTouchProfileLlmProtocol     = "tools.touchProfileLlmProtocol"
	KeyToolsTouchProfileLlmEndpoint     = "tools.touchProfileLlmEndpoint"
	KeyToolsTouchProfileLlmModel        = "tools.touchProfileLlmModel"
	KeyToolsTouchProfileLlmReasoning    = "tools.touchProfileLlmReasoning"
	KeyToolsWuwaFixerUpdateNotification = "tools.wuwaFixerUpdateNotification"

	KeyTransferDownloadConcurrency         = "transfer.downloadConcurrency"
	KeyTransferDownloadBandwidthLimitMibps = "transfer.downloadBandwidthLimitMibps"
	KeyTransferUploadConcurrency           = "transfer.uploadConcurrency"

	KeyDriveNameSortPolicy   = "drive.nameSortPolicy"
	KeyDriveAutoTryPasswords = "drive.autoTryPasswords"
	KeyDrivePasswordList     = "drive.passwordList"

	KeyDebugOpenConsole = "debug.openConsole"

	KeyModelViewerToneMapping = "modelViewer.toneMapping"
	KeyModelViewerEnvironment = "modelViewer.environment"
	KeyModelViewerExposure    = "modelViewer.exposure"

	KeyXXMIPersistToggles           = "xxmi.persistToggles"
	KeyXXMIToggleViewerAutoGenerate = "xxmi.toggleViewerAutoGenerate"
	KeyXXMIToggleViewerHotkey       = "xxmi.toggleViewerHotkey"
)

const (
	storageBounds        = "general_bounds"
	storageSettingBounds = "general_setting_bounds"
)

const (
	ScopeGeneral     = "general"
	ScopeMod         = "mod"
	ScopeTools       = "tools"
	ScopeTransfer    = "transfer"
	ScopeDrive       = "drive"
	ScopeDebug       = "debug"
	ScopeModelViewer = "modelViewer"
	ScopeXXMI        = "xxmi"
)

type Definition struct {
	PublicKey  string
	Scope      string
	StorageKey string
}

var allDefinitions = []Definition{
	{KeyGeneralRunOnStartup, ScopeGeneral, "general_run_on_startup"},
	{KeyGeneralLanguage, ScopeGeneral, "general_language"},
	{KeyGeneralAutoUpdateMode, ScopeGeneral, "general_auto_update_mode"},
	{KeyGeneralRunInBackground, ScopeGeneral, "general_run_in_background"},
	{KeyGeneralDefaultStartPage, ScopeGeneral, "general_default_start_page"},
	{KeyGeneralLogLevel, ScopeGeneral, "general_log_level"},
	{KeyGeneralMoveTransferPageWhenStartTransfer, ScopeGeneral, "general_move_transfer_page_when_start_transfer"},
	{KeyGeneralPowerSaveBlockInTransfer, ScopeGeneral, "general_power_save_block_in_transfer"},
	{KeyGeneralBisectPreserveD3dx, ScopeGeneral, "general_bisect_preserve_d3dx"},
	{KeyGeneralTitlebarActivityBadgeClickNavigate, ScopeGeneral, "general_titlebar_activity_badge_click_navigate"},

	{KeyModArchiveExtractPathMode, ScopeMod, "mod_archive_extract_path_mode"},
	{KeyModDeleteArchiveAfterExtract, ScopeMod, "mod_delete_archive_after_extract"},
	{KeyModMoveFolderInsteadOfCopy, ScopeMod, "mod_move_folder_instead_of_copy"},
	{KeyModSearchModPreview, ScopeMod, "mod_search_mod_preview"},
	{KeyModAutoResolveDownloadTarget, ScopeMod, "mod_auto_resolve_download_target"},
	{KeyModAutoResolveDownloadTargetSources, ScopeMod, "mod_auto_resolve_download_target_sources"},
	{KeyModCopyShaderFixesOnEnable, ScopeMod, "mod_copy_shader_fixes_on_enable"},
	{KeyModSidebarLayout, ScopeMod, "mod_sidebar_layout"},
	{KeyModCharacterSidebarWidth, ScopeMod, "mod_character_sidebar_width"},
	{KeyModGridLayoutMode, ScopeMod, "mod_grid_layout_mode"},
	{KeyModGridResponsiveBaseWidth, ScopeMod, "mod_grid_responsive_base_width"},
	{KeyModGridFixedCardWidth, ScopeMod, "mod_grid_fixed_card_width"},
	{KeyModGridFixedColumnCount, ScopeMod, "mod_grid_fixed_column_count"},
	{KeyModDisabledPrefixStyle, ScopeMod, "mod_disabled_prefix_style"},
	{KeyModReturnToGamebananaAfterDownload, ScopeMod, "mod_return_to_gamebanana_after_download"},

	{KeyToolsTouchProfileLlmProtocol, ScopeTools, "tools_touch_profile_llm_protocol"},
	{KeyToolsTouchProfileLlmEndpoint, ScopeTools, "tools_touch_profile_llm_endpoint"},
	{KeyToolsTouchProfileLlmModel, ScopeTools, "tools_touch_profile_llm_model"},
	{KeyToolsTouchProfileLlmReasoning, ScopeTools, "tools_touch_profile_llm_reasoning"},
	{KeyToolsWuwaFixerUpdateNotification, ScopeTools, "tools_wuwa_fixer_update_notification"},

	{KeyTransferDownloadConcurrency, ScopeTransfer, "transfer_download_concurrency"},
	{KeyTransferDownloadBandwidthLimitMibps, ScopeTransfer, "transfer_download_bandwidth_limit_mibps"},
	{KeyTransferUploadConcurrency, ScopeTransfer, "transfer_upload_concurrency"},

	{KeyDriveNameSortPolicy, ScopeDrive, "drive_name_sort_policy"},
	{KeyDriveAutoTryPasswords, ScopeDrive, "drive_auto_try_passwords"},
	{KeyDrivePasswordList, ScopeDrive, "drive_password_list"},

	{KeyDebugOpenConsole, ScopeDebug, "debug_open_console"},

	{KeyModelViewerToneMapping, ScopeModelViewer, "model_viewer_tone_mapping"},
	{KeyModelViewerEnvironment, ScopeModelViewer, "model_viewer_environment"},
	{KeyModelViewerExposure, ScopeModelViewer, "model_viewer_exposure"},

	{KeyXXMIPersistToggles, ScopeXXMI, "xxmi_persist_toggles"},
	{KeyXXMIToggleViewerAutoGenerate, ScopeXXMI, "xxmi_toggle_viewer_auto_generate"},
	{KeyXXMIToggleViewerHotkey, ScopeXXMI, "xxmi_toggle_viewer_hotkey"},
}

type storageKeyMigration struct {
	Source      string
	Destination string
}

var electronStorageKeyMigrations = []storageKeyMigration{
	{"runOnStartup", "general_run_on_startup"},
	{"language", "general_language"},
	{"autoUpdate", "general_auto_update_mode"},
	{"runInBackground", "general_run_in_background"},
	{"defaultStartPage", "general_default_start_page"},
	{"logLevel", "general_log_level"},
	{"moveTransferPageWhenStartTransfer", "general_move_transfer_page_when_start_transfer"},
	{"powerSaveBlockInTransfer", "general_power_save_block_in_transfer"},
	{"bisect_preserve_d3dx", "general_bisect_preserve_d3dx"},
	{"titlebarActivityBadgeClickNavigate", "general_titlebar_activity_badge_click_navigate"},
	{"bounds", storageBounds},
	{"settingBounds", storageSettingBounds},
}

var definitionsByKey map[string]Definition

func init() {
	definitionsByKey = make(map[string]Definition, len(allDefinitions))
	for _, def := range allDefinitions {
		definitionsByKey[def.PublicKey] = def
	}
}

func DefinitionFor(publicKey string) (Definition, bool) {
	def, ok := definitionsByKey[publicKey]
	return def, ok
}

func AllPublicKeys() []string {
	keys := make([]string, len(allDefinitions))
	for i, def := range allDefinitions {
		keys[i] = def.PublicKey
	}
	return keys
}

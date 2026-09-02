import type { DriveNameSortPolicy } from "./drive";
import type {
    ArchiveExtractPathMode,
    DisabledPrefixStyle,
    DownloadSource,
    ModGridLayoutMode,
    SidebarLayoutMode,
} from "./mod";
import type { TouchProfileLlmProtocol, TouchProfileLlmReasoning } from "./touch-profile-llm";
import type { AutoUpdateMode } from "./updater";

export interface AppSettings {
    "general.runOnStartup": boolean;
    "general.language": string;
    "general.autoUpdateMode": AutoUpdateMode;
    "general.runInBackground": boolean;
    "general.defaultStartPage": string;
    "general.logLevel": string;
    "general.moveTransferPageWhenStartTransfer": boolean;
    "general.powerSaveBlockInTransfer": boolean;
    "general.bisectPreserveD3dx": boolean;
    "general.titlebarActivityBadgeClickNavigate": boolean;

    "mod.archiveExtractPathMode": ArchiveExtractPathMode;
    "mod.deleteArchiveAfterExtract": boolean;
    "mod.moveFolderInsteadOfCopy": boolean;
    "mod.searchModPreview": boolean;
    "mod.autoResolveDownloadTarget": boolean;
    "mod.autoResolveDownloadTargetSources": DownloadSource[];
    "mod.copyShaderFixesOnEnable": boolean;
    "mod.sidebarLayout": SidebarLayoutMode;
    "mod.characterSidebarWidth": number;
    "mod.gridLayoutMode": ModGridLayoutMode;
    "mod.gridResponsiveBaseWidth": number;
    "mod.gridFixedCardWidth": number;
    "mod.gridFixedColumnCount": number;
    "mod.disabledPrefixStyle": DisabledPrefixStyle;
    "mod.returnToGamebananaAfterDownload": boolean;

    "tools.touchProfileLlmProtocol": TouchProfileLlmProtocol;
    "tools.touchProfileLlmEndpoint": string;
    "tools.touchProfileLlmModel": string;
    "tools.touchProfileLlmReasoning": TouchProfileLlmReasoning;
    "tools.wuwaFixerUpdateNotification": boolean;

    "transfer.downloadConcurrency": number;
    "transfer.downloadBandwidthLimitMibps": number;
    "transfer.uploadConcurrency": number;

    "drive.nameSortPolicy": DriveNameSortPolicy;
    "drive.autoTryPasswords": boolean;
    "drive.passwordList": string[];

    "debug.openConsole": boolean;

    "modelViewer.toneMapping": "neutral" | "aces" | "none";
    "modelViewer.environment": "studio" | "soft" | "none";
    "modelViewer.exposure": number;

    "xxmi.persistToggles": boolean;
}

export type SettingKey = keyof AppSettings;

export type SettingScope =
    | "general"
    | "mod"
    | "tools"
    | "transfer"
    | "drive"
    | "debug"
    | "modelViewer"
    | "xxmi";

export interface SettingDefinition<K extends SettingKey = SettingKey> {
    publicKey: K;
    scope: SettingScope;
    storageKey: string;
    sensitive?: boolean;
}

export const APP_SETTINGS = {
    "general.runOnStartup": {
        publicKey: "general.runOnStartup",
        scope: "general",
        storageKey: "general_run_on_startup",
    },
    "general.language": {
        publicKey: "general.language",
        scope: "general",
        storageKey: "general_language",
    },
    "general.autoUpdateMode": {
        publicKey: "general.autoUpdateMode",
        scope: "general",
        storageKey: "general_auto_update_mode",
    },
    "general.runInBackground": {
        publicKey: "general.runInBackground",
        scope: "general",
        storageKey: "general_run_in_background",
    },
    "general.defaultStartPage": {
        publicKey: "general.defaultStartPage",
        scope: "general",
        storageKey: "general_default_start_page",
    },
    "general.logLevel": {
        publicKey: "general.logLevel",
        scope: "general",
        storageKey: "general_log_level",
    },
    "general.moveTransferPageWhenStartTransfer": {
        publicKey: "general.moveTransferPageWhenStartTransfer",
        scope: "general",
        storageKey: "general_move_transfer_page_when_start_transfer",
    },
    "general.powerSaveBlockInTransfer": {
        publicKey: "general.powerSaveBlockInTransfer",
        scope: "general",
        storageKey: "general_power_save_block_in_transfer",
    },
    "general.bisectPreserveD3dx": {
        publicKey: "general.bisectPreserveD3dx",
        scope: "general",
        storageKey: "general_bisect_preserve_d3dx",
    },
    "general.titlebarActivityBadgeClickNavigate": {
        publicKey: "general.titlebarActivityBadgeClickNavigate",
        scope: "general",
        storageKey: "general_titlebar_activity_badge_click_navigate",
    },

    "mod.archiveExtractPathMode": {
        publicKey: "mod.archiveExtractPathMode",
        scope: "mod",
        storageKey: "mod_archive_extract_path_mode",
    },
    "mod.deleteArchiveAfterExtract": {
        publicKey: "mod.deleteArchiveAfterExtract",
        scope: "mod",
        storageKey: "mod_delete_archive_after_extract",
    },
    "mod.moveFolderInsteadOfCopy": {
        publicKey: "mod.moveFolderInsteadOfCopy",
        scope: "mod",
        storageKey: "mod_move_folder_instead_of_copy",
    },
    "mod.searchModPreview": {
        publicKey: "mod.searchModPreview",
        scope: "mod",
        storageKey: "mod_search_mod_preview",
    },
    "mod.autoResolveDownloadTarget": {
        publicKey: "mod.autoResolveDownloadTarget",
        scope: "mod",
        storageKey: "mod_auto_resolve_download_target",
    },
    "mod.autoResolveDownloadTargetSources": {
        publicKey: "mod.autoResolveDownloadTargetSources",
        scope: "mod",
        storageKey: "mod_auto_resolve_download_target_sources",
    },
    "mod.copyShaderFixesOnEnable": {
        publicKey: "mod.copyShaderFixesOnEnable",
        scope: "mod",
        storageKey: "mod_copy_shader_fixes_on_enable",
    },
    "mod.sidebarLayout": {
        publicKey: "mod.sidebarLayout",
        scope: "mod",
        storageKey: "mod_sidebar_layout",
    },
    "mod.characterSidebarWidth": {
        publicKey: "mod.characterSidebarWidth",
        scope: "mod",
        storageKey: "mod_character_sidebar_width",
    },
    "mod.gridLayoutMode": {
        publicKey: "mod.gridLayoutMode",
        scope: "mod",
        storageKey: "mod_grid_layout_mode",
    },
    "mod.gridResponsiveBaseWidth": {
        publicKey: "mod.gridResponsiveBaseWidth",
        scope: "mod",
        storageKey: "mod_grid_responsive_base_width",
    },
    "mod.gridFixedCardWidth": {
        publicKey: "mod.gridFixedCardWidth",
        scope: "mod",
        storageKey: "mod_grid_fixed_card_width",
    },
    "mod.gridFixedColumnCount": {
        publicKey: "mod.gridFixedColumnCount",
        scope: "mod",
        storageKey: "mod_grid_fixed_column_count",
    },
    "mod.disabledPrefixStyle": {
        publicKey: "mod.disabledPrefixStyle",
        scope: "mod",
        storageKey: "mod_disabled_prefix_style",
    },
    "mod.returnToGamebananaAfterDownload": {
        publicKey: "mod.returnToGamebananaAfterDownload",
        scope: "mod",
        storageKey: "mod_return_to_gamebanana_after_download",
    },

    "tools.touchProfileLlmProtocol": {
        publicKey: "tools.touchProfileLlmProtocol",
        scope: "tools",
        storageKey: "tools_touch_profile_llm_protocol",
    },
    "tools.touchProfileLlmEndpoint": {
        publicKey: "tools.touchProfileLlmEndpoint",
        scope: "tools",
        storageKey: "tools_touch_profile_llm_endpoint",
    },
    "tools.touchProfileLlmModel": {
        publicKey: "tools.touchProfileLlmModel",
        scope: "tools",
        storageKey: "tools_touch_profile_llm_model",
    },
    "tools.touchProfileLlmReasoning": {
        publicKey: "tools.touchProfileLlmReasoning",
        scope: "tools",
        storageKey: "tools_touch_profile_llm_reasoning",
    },
    "tools.wuwaFixerUpdateNotification": {
        publicKey: "tools.wuwaFixerUpdateNotification",
        scope: "tools",
        storageKey: "tools_wuwa_fixer_update_notification",
    },

    "transfer.downloadConcurrency": {
        publicKey: "transfer.downloadConcurrency",
        scope: "transfer",
        storageKey: "transfer_download_concurrency",
    },
    "transfer.downloadBandwidthLimitMibps": {
        publicKey: "transfer.downloadBandwidthLimitMibps",
        scope: "transfer",
        storageKey: "transfer_download_bandwidth_limit_mibps",
    },
    "transfer.uploadConcurrency": {
        publicKey: "transfer.uploadConcurrency",
        scope: "transfer",
        storageKey: "transfer_upload_concurrency",
    },
    "drive.nameSortPolicy": {
        publicKey: "drive.nameSortPolicy",
        scope: "drive",
        storageKey: "drive_name_sort_policy",
    },
    "drive.autoTryPasswords": {
        publicKey: "drive.autoTryPasswords",
        scope: "drive",
        storageKey: "drive_auto_try_passwords",
    },
    // Intended design: drive share-link passwords are stored in plain text and
    // shown in the settings UI. These are low-sensitivity access passwords for
    // nahida.live share links, entered and managed by the user themselves. They
    // must stay readable so the user can review/edit them and so the auto-try
    // feature can send them as-is. This is not a security vulnerability.
    "drive.passwordList": {
        publicKey: "drive.passwordList",
        scope: "drive",
        storageKey: "drive_password_list",
    },

    "debug.openConsole": {
        publicKey: "debug.openConsole",
        scope: "debug",
        storageKey: "debug_open_console",
    },

    "modelViewer.toneMapping": {
        publicKey: "modelViewer.toneMapping",
        scope: "modelViewer",
        storageKey: "model_viewer_tone_mapping",
    },
    "modelViewer.environment": {
        publicKey: "modelViewer.environment",
        scope: "modelViewer",
        storageKey: "model_viewer_environment",
    },
    "modelViewer.exposure": {
        publicKey: "modelViewer.exposure",
        scope: "modelViewer",
        storageKey: "model_viewer_exposure",
    },

    "xxmi.persistToggles": {
        publicKey: "xxmi.persistToggles",
        scope: "xxmi",
        storageKey: "xxmi_persist_toggles",
    },
} as const satisfies Record<SettingKey, SettingDefinition>;

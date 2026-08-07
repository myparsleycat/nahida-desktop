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

    "mod.archiveExtractPathMode": ArchiveExtractPathMode;
    "mod.deleteArchiveAfterExtract": boolean;
    "mod.moveFolderInsteadOfCopy": boolean;
    "mod.virtualizationEnabled": boolean;
    "mod.virtualizationThreshold": number;
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

    "tools.touchProfileLlmProtocol": TouchProfileLlmProtocol;
    "tools.touchProfileLlmEndpoint": string;
    "tools.touchProfileLlmModel": string;
    "tools.touchProfileLlmReasoning": TouchProfileLlmReasoning;

    "transfer.downloadConcurrency": number;
    "transfer.downloadBandwidthLimitMibps": number;
    "transfer.uploadConcurrency": number;

    "drive.nameSortPolicy": DriveNameSortPolicy;

    "modelViewer.toneMapping": "neutral" | "aces" | "none";
    "modelViewer.environment": "studio" | "soft" | "none";
    "modelViewer.exposure": number;

    "xxmi.persistToggles": boolean;
    "xxmi.toggleViewerAutoGenerate": boolean;
    "xxmi.toggleViewerHotkey": string;
}

export type SettingKey = keyof AppSettings;

export type SettingScope =
    | "general"
    | "mod"
    | "tools"
    | "transfer"
    | "drive"
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
        storageKey: "runOnStartup",
    },
    "general.language": {
        publicKey: "general.language",
        scope: "general",
        storageKey: "language",
    },
    "general.autoUpdateMode": {
        publicKey: "general.autoUpdateMode",
        scope: "general",
        storageKey: "autoUpdate",
    },
    "general.runInBackground": {
        publicKey: "general.runInBackground",
        scope: "general",
        storageKey: "runInBackground",
    },
    "general.defaultStartPage": {
        publicKey: "general.defaultStartPage",
        scope: "general",
        storageKey: "defaultStartPage",
    },
    "general.logLevel": {
        publicKey: "general.logLevel",
        scope: "general",
        storageKey: "logLevel",
    },
    "general.moveTransferPageWhenStartTransfer": {
        publicKey: "general.moveTransferPageWhenStartTransfer",
        scope: "general",
        storageKey: "moveTransferPageWhenStartTransfer",
    },
    "general.powerSaveBlockInTransfer": {
        publicKey: "general.powerSaveBlockInTransfer",
        scope: "general",
        storageKey: "powerSaveBlockInTransfer",
    },
    "general.bisectPreserveD3dx": {
        publicKey: "general.bisectPreserveD3dx",
        scope: "general",
        storageKey: "bisect_preserve_d3dx",
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
    "mod.virtualizationEnabled": {
        publicKey: "mod.virtualizationEnabled",
        scope: "mod",
        storageKey: "mod_virtualization_enabled",
    },
    "mod.virtualizationThreshold": {
        publicKey: "mod.virtualizationThreshold",
        scope: "mod",
        storageKey: "mod_virtualization_threshold",
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
    "xxmi.toggleViewerAutoGenerate": {
        publicKey: "xxmi.toggleViewerAutoGenerate",
        scope: "xxmi",
        storageKey: "xxmi_toggle_viewer_auto_generate",
    },
    "xxmi.toggleViewerHotkey": {
        publicKey: "xxmi.toggleViewerHotkey",
        scope: "xxmi",
        storageKey: "xxmi_toggle_viewer_hotkey",
    },
} as const satisfies Record<SettingKey, SettingDefinition>;

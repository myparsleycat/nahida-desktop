import type { DriveNameSortPolicy } from "./drive";
import type { ArchiveExtractPathMode, ModGridLayoutMode, SidebarLayoutMode } from "./mod";
import type { AutoUpdateMode } from "./updater";

export interface AppSettings {
    "general.runOnStartup": boolean;
    "general.language": string;
    "general.autoUpdateMode": AutoUpdateMode;
    "general.runInBackground": boolean;
    "general.defaultStartPage": string;
    "general.titlebarStyle": string;
    "general.logLevel": string;
    "general.moveTransferPageWhenStartTransfer": boolean;
    "general.powerSaveBlockInTransfer": boolean;

    "mod.archiveExtractPathMode": ArchiveExtractPathMode;
    "mod.deleteArchiveAfterExtract": boolean;
    "mod.moveFolderInsteadOfCopy": boolean;
    "mod.virtualizationEnabled": boolean;
    "mod.virtualizationThreshold": number;
    "mod.searchModPreview": boolean;
    "mod.copyShaderFixesOnEnable": boolean;
    "mod.sidebarLayout": SidebarLayoutMode;
    "mod.characterSidebarWidth": number;
    "mod.gridLayoutMode": ModGridLayoutMode;
    "mod.gridResponsiveBaseWidth": number;
    "mod.gridFixedCardWidth": number;
    "mod.gridFixedColumnCount": number;

    "transfer.downloadConcurrency": number;
    "transfer.uploadConcurrency": number;
    "transfer.uploadCreateManyConcurrency": number;

    "drive.nameSortPolicy": DriveNameSortPolicy;

    "modelViewer.toneMapping": "neutral" | "aces" | "none";
    "modelViewer.environment": "studio" | "soft" | "none";
    "modelViewer.exposure": number;

    "xxmi.persistToggles": boolean;
    "xxmi.toggleViewerAutoGenerate": boolean;
    "xxmi.toggleViewerHotkey": string;
}

export type SettingKey = keyof AppSettings;

export type SettingScope = "general" | "mod" | "transfer" | "drive" | "modelViewer" | "xxmi";

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
    "general.titlebarStyle": {
        publicKey: "general.titlebarStyle",
        scope: "general",
        storageKey: "titlebarStyle",
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

    "transfer.downloadConcurrency": {
        publicKey: "transfer.downloadConcurrency",
        scope: "transfer",
        storageKey: "transfer_download_concurrency",
    },
    "transfer.uploadConcurrency": {
        publicKey: "transfer.uploadConcurrency",
        scope: "transfer",
        storageKey: "transfer_upload_concurrency",
    },
    "transfer.uploadCreateManyConcurrency": {
        publicKey: "transfer.uploadCreateManyConcurrency",
        scope: "transfer",
        storageKey: "transfer_upload_create_many_concurrency",
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

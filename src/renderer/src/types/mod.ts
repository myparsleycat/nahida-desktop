export interface ToggleKey {
    sectionName: string;
    iniFileName: string;
    key?: string;
    back?: string;
    type?: string;
    variable: string;
    values: string[];
    currentValue?: string;
}

export interface ModIni {
    name: string;
    path: string;
    toggleKeys: ToggleKey[];
}

export interface ModInfo {
    id: string;
    name: string;
    path: string;
    isEnabled: boolean;
    preview?: string;
    mtime: number;
    size: number;
    inis: {
        name: string;
        path: string;
        toggleKeys: ToggleKey[];
    }[];
}

export interface FolderGroup {
    name: string;
    path: string;
    mods: ModInfo[];
    preview?: string;
    modCount?: number;
    isManualSubGroup?: boolean;
    hasManualSubGroups?: boolean;
}

export interface Preset {
    id: string;
    game: string;
    name: string;
    description: string | null;
    createdAt: string;
    updatedAt: string;
    version: number;
    isLegacy: boolean;
}

export interface ApplyPresetResult {
    presetId: string;
    applied: string[];
    skipped: string[];
    missing: {
        modKey: string;
        expectedFolderName: string;
        expectedRelativePath: string;
    }[];
}

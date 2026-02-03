export interface ToggleKey {
    sectionName: string;
    iniFileName: string;
    key?: string[];
    back?: string[];
    type?: string;
    variable: string;
    values: string[];
    currentValue?: string;
}

export interface ModInfo {
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
}

export interface Preset {
    id: string;
    game: string;
    name: string;
    mods: string[];
}

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

export interface ModInfo {
    name: string;
    path: string;
    isEnabled: boolean;
    toggleKeys: ToggleKey[];
    preview?: string;
    ini?: {
        name: string;
        path: string;
    };
}

export interface FolderGroup {
    name: string;
    path: string;
    mods: ModInfo[];
    preview?: string;
}

export interface Preset {
    id: string;
    game: string;
    name: string;
    mods: string[];
}

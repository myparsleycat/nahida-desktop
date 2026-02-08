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

export interface IniResult {
    name: string;
    path: string;
    toggleKeys: ToggleKey[];
    hasToggleKey: boolean;
}

export function processIniFiles(paths: string[]): IniResult[];

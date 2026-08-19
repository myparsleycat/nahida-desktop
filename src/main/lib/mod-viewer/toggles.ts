import type { IniSections } from "./ini";

import { canonicalVarNames } from "./ini";

export type ToggleKey = {
    name: string;
    key: string;
    back: string;
    keyDisplay: string;
    vars: Record<string, string[]>;
    source?: string;
    iniPath?: string;
    section: string;
};

export function extractToggleKeys(
    sections: IniSections,
    varPrefix?: string,
    source?: string,
    canonicalVars?: Record<string, string>,
): Record<string, ToggleKey> {
    const canon = canonicalVars ?? canonicalVarNames(sections);
    const keys: Record<string, ToggleKey> = {};
    for (const [name, lines] of Object.entries(sections)) {
        if (!name.toLowerCase().startsWith("key")) {
            continue;
        }
        let keyCombo = "";
        let backCombo = "";
        let keyType: string | undefined;
        const cvars: Record<string, string[]> = {};
        let iniPath: string | undefined;
        for (const line of lines) {
            iniPath ??= line.iniPath;
            const eq = line.text.indexOf("=");
            if (eq < 0) {
                continue;
            }
            const key = line.text.slice(0, eq).trim();
            const value = line.text.slice(eq + 1).trim();
            const lowered = key.toLowerCase();
            if (lowered === "key") {
                keyCombo = value;
            } else if (lowered === "back") {
                backCombo = value;
            } else if (lowered === "type") {
                keyType = value.toLowerCase();
            } else if (key.startsWith("$")) {
                const variable = canon[key.slice(1).trim().toLowerCase()] ?? key.slice(1).trim();
                const values = value
                    .split(",")
                    .map((entry) => entry.trim())
                    .filter(Boolean);
                if (variable && values.length > 0) {
                    cvars[varPrefix ? `${varPrefix}${variable}` : variable] = values;
                }
            }
        }
        if (keyType !== "cycle" || Object.keys(cvars).length === 0) {
            continue;
        }
        const label = name.slice(0, 3).toLowerCase() === "key" ? name.slice(3) : name;
        keys[varPrefix ? `${varPrefix}${name}` : name] = {
            name: label,
            key: keyCombo,
            back: backCombo,
            keyDisplay: formatKeyCombo(keyCombo),
            vars: cvars,
            source,
            iniPath,
            section: name,
        };
    }
    return keys;
}

export function extractVariableDefaults(
    sections: IniSections,
    varPrefix?: string,
    canonicalVars?: Record<string, string>,
): Record<string, string> {
    const canon = canonicalVars ?? canonicalVarNames(sections);
    const defaults: Record<string, string> = {};
    const defaultRe = /^(?:global\s+)?(?:persist\s+)?\$(\w+)\s*=\s*([^,]+)$/i;
    for (const lines of Object.values(sections)) {
        for (const line of lines) {
            const match = defaultRe.exec(line.text.trim());
            if (!match) {
                continue;
            }
            const variable = canon[match[1].toLowerCase()] ?? match[1];
            const varKey = varPrefix ? `${varPrefix}${variable}` : variable;
            defaults[varKey] ??= match[2].trim();
        }
    }
    return defaults;
}

function formatKeyCombo(combo: string): string {
    if (!combo) {
        return "";
    }
    const mods: string[] = [];
    let main: string | undefined;
    for (const token of combo.split(/\s+/)) {
        const lowered = token.toLowerCase();
        if (lowered.startsWith("no_")) {
            continue;
        }
        if (lowered === "ctrl" || lowered === "shift" || lowered === "alt") {
            mods.push(lowered[0].toUpperCase() + lowered.slice(1));
        } else {
            main = token;
        }
    }
    if (main === undefined) {
        return combo;
    }
    return [...mods, main.length === 1 ? main.toUpperCase() : main].join("+");
}

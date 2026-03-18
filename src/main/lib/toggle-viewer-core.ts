import crypto from "node:crypto";
import path from "node:path";
import { formatKeySequence } from "@shared/key-formatter";

export interface IniEntry {
    key: string;
    value: string;
}

export interface IniSection {
    name: string;
    entries: IniEntry[];
}

export interface ParsedKeySection {
    sectionName: string;
    keyValue: string;
    backValue?: string;
}

export interface GeneratedToggleViewerArtifact {
    targetIniPath: string;
    toggleTxtPath: string;
    toggleIniPath: string;
    toggleTxtHash: string;
    toggleIniHash: string;
    txtContent: string;
    iniContent: string;
}

export function parseIni(content: string): IniSection[] {
    const lines = content.split(/\r?\n/);
    const sections: IniSection[] = [];
    let currentSection: IniSection | null = null;

    for (const rawLine of lines) {
        const trimmed = rawLine.trim();
        if (!trimmed || trimmed.startsWith(";")) continue;

        const sectionMatch = trimmed.match(/^\[(.+)\]$/);
        if (sectionMatch) {
            currentSection = {
                name: sectionMatch[1].trim(),
                entries: [],
            };
            sections.push(currentSection);
            continue;
        }

        if (!currentSection) continue;
        const eqIndex = trimmed.indexOf("=");
        if (eqIndex <= 0) continue;

        const rawKey = trimmed.slice(0, eqIndex).trim();
        const rawValue = trimmed.slice(eqIndex + 1).trim();
        const value = rawValue.split(";")[0].trim();

        currentSection.entries.push({
            key: rawKey,
            value,
        });
    }

    return sections;
}

export function findTargetKeySections(sections: IniSection[]): ParsedKeySection[] {
    const result: ParsedKeySection[] = [];

    for (const section of sections) {
        if (!section.name.toLowerCase().startsWith("key")) continue;

        const typeValue = getEntryValue(section, "type");
        if (typeValue?.toLowerCase() !== "cycle") continue;

        let hasMultiValueVariable = false;
        for (const entry of section.entries) {
            if (!entry.key.startsWith("$")) continue;
            const values = entry.value
                .split(",")
                .map((v) => v.trim())
                .filter(Boolean);
            if (values.length >= 2) {
                hasMultiValueVariable = true;
                break;
            }
        }
        if (!hasMultiValueVariable) continue;

        const keyValue = getEntryValue(section, "key");
        if (!keyValue) continue;

        const backValue = getEntryValue(section, "back");
        result.push({
            sectionName: section.name,
            keyValue,
            backValue: backValue || undefined,
        });
    }

    return result;
}

export function resolvePositionHash(sections: IniSection[]): string | null {
    const textureSections = sections.filter((s) =>
        s.name.toLowerCase().startsWith("textureoverride"),
    );
    const resourceSections = sections.filter((s) => s.name.toLowerCase().startsWith("resource"));

    for (const section of textureSections) {
        if (!section.name.toLowerCase().includes("bodyposition")) continue;
        const hash = getEntryValue(section, "hash");
        if (hash) return hash;
    }

    const bodyPositionResourceSet = new Set(
        resourceSections
            .map((s) => s.name)
            .filter((name) => name.toLowerCase().includes("bodyposition"))
            .map((name) => name.toLowerCase()),
    );

    if (bodyPositionResourceSet.size > 0) {
        for (const section of textureSections) {
            const vb0 = getEntryValue(section, "vb0");
            if (!vb0 || !bodyPositionResourceSet.has(vb0.toLowerCase())) continue;
            const hash = getEntryValue(section, "hash");
            if (hash) return hash;
        }
    }

    const positionResourceSet = new Set(
        resourceSections
            .map((s) => s.name)
            .filter((name) => name.toLowerCase().includes("position"))
            .map((name) => name.toLowerCase()),
    );
    if (positionResourceSet.size > 0) {
        for (const section of textureSections) {
            const lowerName = section.name.toLowerCase();
            if (!lowerName.includes("body")) continue;
            const vb0 = getEntryValue(section, "vb0");
            if (!vb0 || !positionResourceSet.has(vb0.toLowerCase())) continue;
            const hash = getEntryValue(section, "hash");
            if (hash) return hash;
        }
    }

    const firstPositionResource = resourceSections.find((section) =>
        section.name.toLowerCase().includes("position"),
    );
    if (firstPositionResource) {
        for (const section of textureSections) {
            const vb0 = getEntryValue(section, "vb0");
            if (vb0?.toLowerCase() !== firstPositionResource.name.toLowerCase()) continue;
            const hash = getEntryValue(section, "hash");
            if (hash) return hash;
        }
    }

    const commandListOverrideSharedResources = sections.find(
        (section) => section.name.toLowerCase() === "commandlistoverridesharedresources",
    );
    const sharedVb0 = commandListOverrideSharedResources
        ? getEntryValue(commandListOverrideSharedResources, "vb0")
        : null;
    if (sharedVb0 && sharedVb0.toLowerCase().includes("position")) {
        for (const section of textureSections) {
            if (!section.name.toLowerCase().includes("component")) continue;
            const hash = getEntryValue(section, "hash");
            if (hash) return hash;
        }
    }

    return null;
}

export function buildToggleViewerTxt(iniPath: string, keySections: ParsedKeySection[]) {
    const modName = path.basename(path.dirname(iniPath));
    const iniName = path.basename(iniPath);
    const lines: string[] = [`Mod: ${modName}`, "", `Ini: ${iniName}`, ""];

    for (let i = 0; i < keySections.length; i++) {
        const keySection = keySections[i];
        lines.push(`${keySection.sectionName}:`);
        lines.push(`    Key: ${formatKeySequence(keySection.keyValue, { asciiFallback: true })}`);
        if (keySection.backValue) {
            lines.push(
                `    Back: ${formatKeySequence(keySection.backValue, { asciiFallback: true })}`,
            );
        }
        if (i < keySections.length - 1) {
            lines.push("");
        }
    }

    return `${lines.join("\n")}\n`;
}

export function buildToggleViewerIni(hash: string, hotkey: string) {
    const lines = [
        "[Constants]",
        "global $active = 0",
        "global $enabled = 0",
        "",
        "[Key]",
        `key = ${hotkey}`,
        "condition = $active == 1",
        "type = cycle",
        "$enabled = 0,1",
        "",
        "[TextureOverrideCharacterPosition]",
        `hash = ${hash}`,
        "$active = 1",
        "",
        "[Present]",
        "post $active = 0",
        "run = CommandListKey",
        "",
        "[CommandListKey]",
        "if $active == 1 && $enabled == 1",
        "    pre Resource\\ShaderFixes\\help.ini\\NotificationParams = ResourceBox",
        "    pre run = CustomShader\\ShaderFixes\\help.ini\\FormatText",
        "    pre Resource\\ShaderFixes\\help.ini\\Notification = Resourcename1",
        "endif",
        "",
        "[ResourceBox]",
        "type = StructuredBuffer",
        "array = 1",
        "data = R32_FLOAT   -0.95 -1 1 1      1 1 1 1    0 0 0 0.95   0.05 0.05     1 2   0  1.0",
        "",
        "[Resourcename1]",
        "type = buffer",
        "format = R8_UINT",
        "filename = toggle-viewer.txt",
        "",
    ];

    return lines.join("\n");
}

export function replaceHotkeyInGeneratedIni(content: string, hotkey: string) {
    const newline = content.includes("\r\n") ? "\r\n" : "\n";
    const lines = content.split(/\r?\n/);
    let inKeySection = false;

    for (let i = 0; i < lines.length; i++) {
        const trimmed = lines[i].trim();
        const sectionMatch = trimmed.match(/^\[(.+)\]$/);
        if (sectionMatch) {
            inKeySection = sectionMatch[1].trim().toLowerCase() === "key";
            continue;
        }

        if (inKeySection && /^\s*key\s*=/.test(lines[i])) {
            lines[i] = `key = ${hotkey}`;
            return lines.join(newline);
        }
    }

    return content;
}

export function sha256(content: string) {
    return crypto.createHash("sha256").update(content).digest("hex");
}

export function generateToggleViewerArtifact(
    iniPath: string,
    content: string,
    hotkey: string,
): GeneratedToggleViewerArtifact | null {
    const sections = parseIni(content);
    const keySections = findTargetKeySections(sections);
    if (keySections.length === 0) {
        return null;
    }

    const positionHash = resolvePositionHash(sections);
    if (!positionHash) {
        return null;
    }

    const dir = path.dirname(iniPath);
    const txtContent = buildToggleViewerTxt(iniPath, keySections);
    const iniContent = buildToggleViewerIni(positionHash, hotkey);

    return {
        targetIniPath: iniPath,
        toggleTxtPath: path.join(dir, "toggle-viewer.txt"),
        toggleIniPath: path.join(dir, "toggle-viewer.ini"),
        toggleTxtHash: sha256(txtContent),
        toggleIniHash: sha256(iniContent),
        txtContent,
        iniContent,
    };
}

function getEntryValue(section: IniSection, key: string) {
    const found = section.entries.find((entry) => entry.key.toLowerCase() === key.toLowerCase());
    return found?.value || null;
}

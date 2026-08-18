export type IniLine =
    | { kind: "blank"; raw: string }
    | { kind: "comment"; raw: string }
    | { kind: "header"; raw: string; name: string }
    | { kind: "kv"; raw: string; key: string; value: string; indent: string }
    | { kind: "other"; raw: string };

export type IniSectionBlock = {
    header: string;
    name: string;
    lines: IniLine[];
};

export type ParsedIni = {
    preamble: IniLine[];
    sections: IniSectionBlock[];
};

const HEADER_RE = /^\[([^\]]+)\]\s*$/;

export function parseIniText(text: string): ParsedIni {
    const preamble: IniLine[] = [];
    const sections: IniSectionBlock[] = [];
    let current: IniSectionBlock | null = null;

    for (const raw of text.split(/\r?\n/)) {
        const line = classifyLine(raw);
        if (line.kind === "header") {
            current = { header: line.raw, name: line.name, lines: [] };
            sections.push(current);
            continue;
        }
        if (current) {
            current.lines.push(line);
            continue;
        }
        preamble.push(line);
    }

    return { preamble, sections };
}

export function serializeIni(parsed: ParsedIni) {
    const lines = [
        ...parsed.preamble.map((line) => line.raw),
        ...parsed.sections.flatMap((section) => [
            section.header,
            ...section.lines.map((line) => line.raw),
        ]),
    ];
    return `${lines.join("\n").replace(/\n+$/, "")}\n`;
}

export function classifyLine(raw: string): IniLine {
    if (raw.trim() === "") return { kind: "blank", raw };
    if (raw.trimStart().startsWith(";")) return { kind: "comment", raw };

    const header = raw.trim().match(HEADER_RE);
    if (header) return { kind: "header", raw, name: header[1].trim() };

    const indent = raw.match(/^\s*/)?.[0] ?? "";
    const eq = raw.indexOf("=");
    if (eq >= 0) {
        return {
            kind: "kv",
            raw,
            key: raw.slice(0, eq).trim(),
            value: raw.slice(eq + 1).trim(),
            indent,
        };
    }

    return { kind: "other", raw };
}

export function sectionValues(section: IniSectionBlock) {
    return Object.fromEntries(
        section.lines.flatMap((line) =>
            line.kind === "kv" ? [[line.key.toLowerCase(), line.value]] : [],
        ),
    ) as Record<string, string>;
}

export function extractMergedModPaths(text: string) {
    const rawMatches: string[] = [];
    for (const raw of text.split(/\r?\n/)) {
        const trimmed = raw.trim();
        if (trimmed.startsWith("[") && !trimmed.startsWith("[;")) break;
        const match = raw.match(/^\s*;\s*(?:merged mods?|合并mod)\s*:\s*(.+)$/i);
        if (match?.[1]) {
            rawMatches.push(match[1].trim());
        }
    }
    if (rawMatches.length === 0) return [];

    return rawMatches.flatMap((rawMatch) => {
        if (!rawMatch) return [];
        if (rawMatch.startsWith("[") && rawMatch.endsWith("]")) {
            try {
                const parsed = JSON.parse(rawMatch);
                if (Array.isArray(parsed)) {
                    return parsed
                        .filter(
                            (item): item is string =>
                                typeof item === "string" && item.trim().length > 0,
                        )
                        .map((item) => item.trim());
                }
            } catch {
                // fall through
            }
        }
        if (rawMatch.startsWith('"') && rawMatch.endsWith('"')) {
            try {
                const parsed = JSON.parse(rawMatch);
                if (typeof parsed === "string" && parsed.trim().length > 0) {
                    return [parsed.trim()];
                }
            } catch {
                // fall through
            }
        }
        if (rawMatches.length === 1 && rawMatch.includes(",")) {
            const parts = rawMatch
                .split(",")
                .map((entry) => entry.trim())
                .filter(Boolean);
            if (parts.length > 1 && parts.every((entry) => /\.ini$/i.test(entry))) {
                return parts;
            }
        }
        return [rawMatch];
    });
}

export function extractNamespace(text: string) {
    return text.match(/^\s*namespace\s*=\s*(.+)$/im)?.[1]?.trim() ?? null;
}

export function extractObjectGuid(text: string) {
    return text.match(/^\s*global\s+\$object_guid\s*=\s*(\S+)/im)?.[1] ?? null;
}

export function extractHashes(text: string) {
    return [
        ...new Set(
            [...text.matchAll(/^\s*hash\s*=\s*([0-9a-f]+)\s*$/gim)].map((match) =>
                match[1].toLowerCase(),
            ),
        ),
    ];
}

export function extractPositionHash(text: string) {
    const parsed = parseIniText(text);
    const position = parsed.sections.find((section) => /position$/i.test(section.name));
    if (position) {
        const hash = sectionValues(position).hash;
        if (hash) return hash.toLowerCase();
    }

    const component = parsed.sections.find((section) =>
        /textureoverride(?:_?)component0(?:_lod0)?$/i.test(section.name),
    );
    if (component) {
        const hash = sectionValues(component).hash;
        if (hash) return hash.toLowerCase();
    }

    return extractHashes(text)[0] ?? null;
}

export function hasKeySection(text: string) {
    return /^\s*\[Key/im.test(text);
}

export function persistVarCount(text: string) {
    return (text.match(/^\s*global\s+persist\s+\$/gim) ?? []).length;
}

export function hasNumberedResources(text: string) {
    return /^\s*\[Resource[^\]]*\.\d+\]/im.test(text);
}

export function hasCommandListDispatch(text: string) {
    return /\$swapvar/i.test(text) && /^\s*\[CommandList/im.test(text);
}

export function hasMasterSwapRef(text: string) {
    return /\$\\[^\\\s]+\\Master\\swapvar/i.test(text);
}

export function isFileDisabledIniName(fileName: string) {
    const lower = fileName.toLowerCase();
    return lower.startsWith("disabled") && lower.endsWith(".ini");
}

export function isBackupIniName(fileName: string) {
    return /^disabled_backup_/i.test(fileName);
}

export function isSupportIniName(fileName: string) {
    const lower = fileName.toLowerCase();
    return (
        [
            "crossibclassifier.ini",
            "ibskip.ini",
            "draw_image.ini",
            "cutoutmask.ini",
            "menu.ini",
            "help.ini",
            "selectionmenu.ini",
            "qh.ini",
        ].includes(lower) || /^preset.+\.ini$/.test(lower)
    );
}

export function detectDialect(text: string) {
    if (
        /;\s*WWMI/i.test(text) ||
        /\$\\WWMIv1\\/i.test(text) ||
        /\[TextureOverrideComponent\d/i.test(text)
    ) {
        return "wwmi" as const;
    }
    if (
        /;\s*EFMI/i.test(text) ||
        /\$\\EFMIv1\\/i.test(text) ||
        /\[TextureOverride_Component\d/i.test(text)
    ) {
        return "efmi" as const;
    }
    if (
        /\$\\SRMI\\/i.test(text) ||
        /Resource\\SRMI\\/i.test(text) ||
        /HeadBlend|HairBlend/i.test(text)
    ) {
        return "srmi" as const;
    }
    if (/\$\\ZZMI\\/i.test(text) || /Resource\\ZZMI\\/i.test(text)) {
        return "zzmi" as const;
    }
    if (/TextureOverride\w+Position/i.test(text) || /VertexLimitRaise/i.test(text)) {
        return "gimi" as const;
    }
    return "unknown" as const;
}

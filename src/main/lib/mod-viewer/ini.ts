import path from "node:path";

import fse from "fs-extra";

export type IniLine = {
    text: string;
    iniPath: string;
    lineNo: number;
    section: string;
};

export type IniSections = Record<string, IniLine[]>;

export type ResourceInfo = {
    filename?: string;
    stride?: number;
    format?: string;
};

const DECL_RE = /^global\s+(?:persist\s+)?\$(\w+)\b/i;
const VAR_RE = /\$(\w+)/g;
const DRAW_RE = /^drawindexed\s*=/i;
const IB_RE = /^ib\s*=/i;
const MAX_INI_FILES = 10;
const MAX_INI_DEPTH = 2;
const MAX_ESCAPE_DEPTH = 1;

export function parseIniText(text: string, iniPath: string): IniSections {
    const sections: IniSections = {};
    let current: string | null = null;

    for (const [index, raw] of text
        .replace(/^\uFEFF/, "")
        .split(/\r?\n/)
        .entries()) {
        let line = raw.trim();
        if (!line || line.startsWith(";")) {
            continue;
        }
        const lhs = line.split("=", 1)[0].trim().toLowerCase();
        if (lhs !== "key" && lhs !== "back") {
            line = line.split(";")[0].trim();
        }
        if (!line) {
            continue;
        }
        if (line.startsWith("[") && line.endsWith("]")) {
            current = line.slice(1, -1).trim();
            sections[current] ??= [];
            continue;
        }
        if (current === null) {
            continue;
        }
        sections[current].push({
            text: line,
            iniPath,
            lineNo: index + 1,
            section: current,
        });
    }
    return sections;
}

export async function parseIniFile(iniPath: string): Promise<IniSections> {
    return parseIniText(await fse.readFile(iniPath, "utf8"), iniPath);
}

export function canonicalVarNames(sections: IniSections): Record<string, string> {
    const declared: Record<string, string> = {};
    const seen: Record<string, string> = {};
    for (const lines of Object.values(sections)) {
        for (const line of lines) {
            const text = line.text.trim();
            const decl = DECL_RE.exec(text);
            if (decl) {
                declared[decl[1].toLowerCase()] ??= decl[1];
            }
            for (const match of text.matchAll(VAR_RE)) {
                seen[match[1].toLowerCase()] ??= match[1];
            }
        }
    }
    for (const [lower, name] of Object.entries(seen)) {
        declared[lower] ??= name;
    }
    return declared;
}

export function extractResources(sections: IniSections): Record<string, ResourceInfo> {
    const skip = [
        "TextureOverride",
        "CommandList",
        "ShaderOverride",
        "Present",
        "Key",
        "Constants",
    ];
    const resources: Record<string, ResourceInfo> = {};
    for (const [name, lines] of Object.entries(sections)) {
        if (skip.some((prefix) => name.startsWith(prefix))) {
            continue;
        }
        const info: ResourceInfo = {};
        for (const line of lines) {
            const eq = line.text.indexOf("=");
            if (eq < 0) {
                continue;
            }
            const key = line.text.slice(0, eq).trim().toLowerCase();
            const value = stripQuotes(line.text.slice(eq + 1).trim());
            if (key === "filename") {
                info.filename = value;
            } else if (key === "stride") {
                const stride = Number(value);
                if (Number.isFinite(stride)) {
                    info.stride = stride;
                }
            } else if (key === "format") {
                info.format = value;
            }
        }
        if (info.filename) {
            resources[name] = info;
        }
    }
    return resources;
}

export function resourceLookup(
    resources: Record<string, ResourceInfo>,
    name: string | undefined,
): ResourceInfo {
    if (!name) {
        return {};
    }
    if (resources[name]) {
        return resources[name];
    }
    const lowered = name.toLowerCase();
    return Object.entries(resources).find(([key]) => key.toLowerCase() === lowered)?.[1] ?? {};
}

export function sectionLookup(sections: IniSections, name: string): IniLine[] | undefined {
    if (sections[name]) {
        return sections[name];
    }
    const lowered = name.toLowerCase();
    return Object.entries(sections).find(([key]) => key.toLowerCase() === lowered)?.[1];
}

export async function discoverIniPaths(modDir: string): Promise<string[]> {
    const direct = await activeIniPaths(modDir);
    if (!(await someGeometryIni(direct))) {
        return direct;
    }

    const found = [...direct];
    if (found.length >= MAX_INI_FILES) {
        return found;
    }

    await walkNested(modDir, modDir, 0, found);
    return found;
}

export function rebaseResources(
    resources: Record<string, ResourceInfo>,
    iniPath: string,
    folderPath: string,
): Record<string, ResourceInfo> {
    const relDir = path.relative(folderPath, path.dirname(iniPath));
    if (!relDir || relDir === ".") {
        return resources;
    }
    for (const info of Object.values(resources)) {
        if (info.filename) {
            info.filename = path.normalize(path.join(relDir, info.filename));
        }
    }
    return resources;
}

export function iniScope(
    iniPath: string,
    folderPath: string,
    multi: boolean,
): { prefix: string | undefined; source: string | undefined } {
    if (!multi) {
        return { prefix: undefined, source: undefined };
    }
    const parentDir = path.dirname(iniPath);
    const source =
        path.normalize(parentDir) !== path.normalize(folderPath)
            ? path.relative(folderPath, parentDir).replaceAll("\\", "/")
            : path.parse(iniPath).name;
    const identity = path.parse(path.relative(folderPath, iniPath)).name.replaceAll("\\", "/");
    return { prefix: `${identity}::`, source };
}

export function iniRel(iniPath: string, folderPath: string): string {
    return path.relative(folderPath, iniPath).replaceAll("\\", "/");
}

export function safeResourcePath(modDir: string, rel: string | undefined): string | null {
    if (!rel || path.isAbsolute(rel) || path.parse(rel).root) {
        return null;
    }
    const root = path.resolve(modDir);
    const target = path.resolve(root, rel);
    if (isPathWithin(target, root)) {
        return target;
    }
    let ceiling = root;
    for (let depth = 0; depth < MAX_ESCAPE_DEPTH; depth++) {
        ceiling = path.dirname(ceiling);
        if (isPathWithin(target, ceiling)) {
            return target;
        }
    }
    return null;
}

export function stripComment(line: string): string {
    return line.split(";")[0].trim();
}

function stripQuotes(value: string): string {
    if (
        (value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))
    ) {
        return value.slice(1, -1);
    }
    return value;
}

function isPathWithin(target: string, root: string): boolean {
    const relative = path.relative(root, target);
    return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
}

async function activeIniPaths(folder: string): Promise<string[]> {
    const entries = await fse.readdir(folder, { withFileTypes: true }).catch(() => []);
    return entries
        .filter(
            (entry) =>
                entry.isFile() &&
                entry.name.toLowerCase().endsWith(".ini") &&
                !entry.name.toUpperCase().startsWith("DISABLED"),
        )
        .sort((left, right) => left.name.localeCompare(right.name))
        .map((entry) => path.join(folder, entry.name));
}

async function someGeometryIni(paths: string[]): Promise<boolean> {
    for (const iniPath of paths) {
        if (hasGeometrySections(await parseIniFile(iniPath))) {
            return true;
        }
    }
    return false;
}

function hasGeometrySections(sections: IniSections): boolean {
    let hasDraw = false;
    let hasIndex = false;
    for (const [name, lines] of Object.entries(sections)) {
        const lowered = name.toLowerCase();
        if (lowered.startsWith("textureoverride")) {
            for (const line of lines) {
                if (DRAW_RE.test(line.text)) {
                    hasDraw = true;
                } else if (IB_RE.test(line.text)) {
                    hasIndex = true;
                }
            }
        } else if (lowered.startsWith("commandlist")) {
            for (const line of lines) {
                if (IB_RE.test(line.text)) {
                    hasIndex = true;
                }
            }
        }
    }
    return hasDraw || hasIndex;
}

async function walkNested(
    modDir: string,
    current: string,
    depth: number,
    found: string[],
): Promise<void> {
    if (found.length >= MAX_INI_FILES) {
        return;
    }
    const entries = await fse.readdir(current, { withFileTypes: true }).catch(() => []);
    const dirs = entries
        .filter((entry) => entry.isDirectory())
        .sort((left, right) => left.name.localeCompare(right.name));
    if (depth > 0) {
        for (const iniPath of await activeIniPaths(current)) {
            found.push(iniPath);
            if (found.length >= MAX_INI_FILES) {
                return;
            }
        }
    }
    if (depth >= MAX_INI_DEPTH) {
        return;
    }
    for (const dir of dirs) {
        await walkNested(modDir, path.join(current, dir.name), depth + 1, found);
        if (found.length >= MAX_INI_FILES) {
            return;
        }
    }
}

import path from "node:path";
import fg from "fast-glob";
import fse from "fs-extra";
import type { IniSection } from "./types";

export async function loadIniBundle(
    input: string,
): Promise<{ iniPath: string; sections: IniSection[] }> {
    const iniPath = await findIni(input);
    const iniText = await fse.readFile(iniPath, "utf8");
    const sections = parseIni(iniText);
    const mergedRefs = extractMergedIniRefs(iniText, path.dirname(iniPath));

    if (mergedRefs.length === 0) {
        return { iniPath, sections };
    }

    const extraSections = (
        await Promise.all(
            mergedRefs
                .filter((refPath) => path.resolve(refPath) !== path.resolve(iniPath))
                .map(async (refPath) => {
                    if (!(await fse.pathExists(refPath))) {
                        return [];
                    }
                    const refText = await fse.readFile(refPath, "utf8");
                    return parseIni(refText);
                }),
        )
    ).flat();

    return {
        iniPath,
        sections: [...sections, ...extraSections],
    };
}

async function findIni(input: string): Promise<string> {
    const resolved = path.resolve(input);
    const stat = await fse.stat(resolved);
    if (stat.isFile()) {
        return resolved;
    }

    const candidates = await fg("**/*.ini", {
        cwd: resolved,
        absolute: true,
        onlyFiles: true,
        ignore: ["**/disabled*.ini"],
        caseSensitiveMatch: false,
    });

    if (candidates.length === 0) {
        throw new Error(`No .ini found in ${input}`);
    }

    const scored = await Promise.all(
        candidates.map(async (candidate) => ({
            path: candidate,
            score: scoreIniCandidate(candidate, await fse.readFile(candidate, "utf8")),
        })),
    );
    scored.sort((left, right) => right.score - left.score || left.path.localeCompare(right.path));
    return scored[0].path;
}

function scoreIniCandidate(candidatePath: string, text: string): number {
    const basename = path.basename(candidatePath).toLowerCase();
    let score = 0;

    if (basename === "merged.ini") score += 120;
    if (basename.startsWith("master") && basename.endsWith(".ini")) score += 140;
    if (text.includes("; Merged Mod:")) score += 80;
    if (/^\s*namespace\s*=.+$/im.test(text)) score += 60;

    const persistCount = (text.match(/^\s*global\s+persist\s+\$/gim) || []).length;
    const cycleCount = (text.match(/^\s*type\s*=\s*cycle\s*$/gim) || []).length;
    const overrideCount = (text.match(/^\s*\[TextureOverride/gim) || []).length;
    const resourceCount = (text.match(/^\s*\[Resource/gim) || []).length;

    score += persistCount * 15;
    score += cycleCount * 10;
    score += Math.min(overrideCount, 50);
    score += Math.min(resourceCount, 50);

    if (/^\s*\[KeyHelp\]/im.test(text)) score -= 25;
    if (basename.startsWith("disabled") && !text.includes("; Merged Mod:")) score -= 10;

    return score;
}

function extractMergedIniRefs(text: string, baseDir: string): string[] {
    const firstLine = text.split(/\r?\n/, 1)[0] || "";
    const match = firstLine.match(/;\s*Merged Mod:\s*(.+)$/i);
    if (!match) {
        return [];
    }

    return match[1]
        .split(",")
        .map((entry) => entry.trim())
        .filter(Boolean)
        .map((entry) => path.resolve(baseDir, entry));
}

function parseIni(text: string): IniSection[] {
    const sections: IniSection[] = [];
    let current: IniSection | null = null;

    for (const rawLine of text.split(/\r?\n/)) {
        const line = rawLine.trim();
        if (!line || line.startsWith(";")) continue;

        const headerMatch = line.match(/^\[([^\]]+)\]$/);
        if (headerMatch) {
            const full = headerMatch[1].trim();
            const kindMatch = full.match(
                /^(TextureOverride|ShaderOverride|Resource|Constants|Present|CommandList|CustomShader)(.*)$/,
            );
            current = {
                header: kindMatch ? kindMatch[1] : full,
                name: kindMatch ? kindMatch[2] : full,
                lines: [],
                values: {},
            };
            sections.push(current);
            continue;
        }

        if (!current) continue;
        current.lines.push(stripInlineComment(line));
        const eq = line.indexOf("=");
        if (eq < 0) continue;
        const key = line.slice(0, eq).trim();
        const value = stripInlineComment(line.slice(eq + 1).trim());
        current.values[key] = value;
    }

    return sections;
}

function stripInlineComment(value: string): string {
    let quote: '"' | "'" | null = null;
    for (let index = 0; index < value.length; index++) {
        const current = value[index];
        if ((current === '"' || current === "'") && value[index - 1] !== "\\") {
            quote = quote === current ? null : quote ? quote : (current as '"' | "'");
            continue;
        }
        if (quote) {
            continue;
        }
        if (current === ";" && index > 0 && /\s/.test(value[index - 1])) {
            return value.slice(0, index).trim();
        }
    }
    return value;
}

import path from "node:path";

import fg from "fast-glob";
import fse from "fs-extra";

import type { IniSection } from "./types";

import { extractMergedModPaths } from "../../services/mod-manager/merge/ini-text";
import { scoreIniCandidate } from "../mod-ini-score";

export async function loadIniBundle(
    input: string,
): Promise<{ iniPath: string; sections: IniSection[]; sourcePaths: string[] }> {
    const iniPath = await findIni(input);
    const iniText = await fse.readFile(iniPath, "utf8");
    const sections = parseIni(iniText);
    const mergedRefs = await extractMergedIniRefs(iniText, path.dirname(iniPath));

    if (mergedRefs.length === 0) {
        return { iniPath, sections, sourcePaths: [iniPath] };
    }

    const mergedIniFiles = await Promise.all(
        mergedRefs
            .filter((refPath) => path.resolve(refPath) !== path.resolve(iniPath))
            .map(async (refPath) => {
                const refText = await fse.readFile(refPath, "utf8");
                return { path: refPath, sections: parseIni(refText) };
            }),
    );

    return {
        iniPath,
        sections: [sections, ...mergedIniFiles.map((entry) => entry.sections)].flat(),
        sourcePaths: [iniPath, ...mergedIniFiles.map((entry) => entry.path)],
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

async function extractMergedIniRefs(text: string, baseDir: string) {
    const resolvedBaseDir = path.resolve(baseDir);
    return (
        await Promise.all(
            extractMergedModPaths(text).map((entry) => resolveMergedIniRef(entry, resolvedBaseDir)),
        )
    ).filter((entry): entry is string => !!entry);
}

async function resolveMergedIniRef(entry: string, baseDir: string) {
    const resolved = path.resolve(baseDir, entry);
    if (!isPathInside(baseDir, resolved) || !(await fse.pathExists(resolved))) return null;

    const realPath = await fse.realpath(resolved);
    if (!isPathInside(await fse.realpath(baseDir), realPath)) return null;

    const stat = await fse.stat(realPath);
    if (!stat.isFile()) return null;

    return realPath;
}

function isPathInside(parentPath: string, targetPath: string) {
    const relative = path.relative(parentPath, targetPath);
    return (
        relative !== "" &&
        relative !== ".." &&
        !relative.startsWith(`..${path.sep}`) &&
        !path.isAbsolute(relative)
    );
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
    return value.trim();
}

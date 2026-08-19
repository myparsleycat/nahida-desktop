import path from "node:path";

import { disabledPrefixString, stripDisabledPrefix } from "@shared/mod";
import fse from "fs-extra";

export type RollbackAction =
    | { kind: "remove"; path: string }
    | { kind: "move"; from: string; to: string }
    | { kind: "restore"; path: string; contents: string };

export function describeRollbackAction(entry: RollbackAction) {
    if (entry.kind === "remove") return entry.path;
    if (entry.kind === "restore") return `restore:${entry.path}`;
    return `${entry.from}->${entry.to}`;
}

export async function recordFileWrite(filePath: string, created: RollbackAction[]) {
    if (await fse.pathExists(filePath)) {
        created.push({
            kind: "restore",
            path: filePath,
            contents: await fse.readFile(filePath, "utf8"),
        });
        return;
    }
    created.push({ kind: "remove", path: filePath });
}

export function uniqueMergeDisabledName(fileName: string, used: Set<string>) {
    for (let counter = 1; counter <= 1000; counter += 1) {
        const name = mergeDisabledBackupName(fileName, counter);
        if (used.has(name.toLowerCase())) continue;
        used.add(name.toLowerCase());
        return name;
    }
    throw new Error("MERGE_DISABLE_CONFLICT");
}

export async function allocateMergeDisabledPath(sourcePath: string) {
    const dir = path.dirname(sourcePath);
    const used = new Set((await fse.readdir(dir)).map((name) => name.toLowerCase()));
    return path.join(dir, uniqueMergeDisabledName(path.basename(sourcePath), used));
}

export async function disableIniFile(iniPath: string, created: RollbackAction[]) {
    const dest = await allocateMergeDisabledPath(iniPath);
    await fse.move(iniPath, dest);
    created.push({ kind: "move", from: dest, to: iniPath });
}

export async function ensureMergeBackup(iniPath: string, created: RollbackAction[]) {
    const dir = path.dirname(iniPath);
    const originalName = path.basename(iniPath);
    if ((await fse.readdir(dir)).some((name) => isMergeBackupOf(name, originalName))) return;
    const dest = await allocateMergeDisabledPath(iniPath);
    await fse.copy(iniPath, dest);
    created.push({ kind: "remove", path: dest });
}

function mergeDisabledBackupName(fileName: string, counter: number) {
    if (/\.ini$/i.test(fileName)) {
        return counter === 1
            ? `DISABLED_BACKUP_${fileName}`
            : `DISABLED_BACKUP_${counter}_${fileName}`;
    }
    const base = stripDisabledPrefix(fileName) || fileName;
    const prefix = disabledPrefixString("space");
    return counter === 1 ? `${prefix}${base}` : `${prefix}${base} (${counter})`;
}

function isMergeBackupOf(fileName: string, originalName: string) {
    const lower = fileName.toLowerCase();
    const suffix = `_${originalName.toLowerCase()}`;
    if (!lower.endsWith(suffix)) return false;
    const prefix = lower.slice(0, -suffix.length);
    return prefix === "disabled_backup" || /^disabled_backup_\d+$/.test(prefix);
}

export type RollbackFailure = {
    action: RollbackAction;
    error: unknown;
};

export async function rollbackCreated(created: RollbackAction[]): Promise<RollbackFailure[]> {
    const failures: RollbackFailure[] = [];
    for (const entry of [...created].reverse()) {
        try {
            if (entry.kind === "move") {
                if (await fse.pathExists(entry.from)) {
                    await fse.move(entry.from, entry.to, { overwrite: true });
                }
                continue;
            }
            if (entry.kind === "restore") {
                await fse.writeFile(entry.path, entry.contents, "utf8");
                continue;
            }
            await fse.remove(entry.path);
        } catch (error) {
            failures.push({ action: entry, error });
        }
    }
    return failures;
}

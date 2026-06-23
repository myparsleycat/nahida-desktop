import path from "node:path";
import { app } from "electron";
import fg from "fast-glob";
import fse from "fs-extra";

export const BISECT_DISABLED_SUFFIX = "mod-bisect-disabled";
export const BISECT_KEEP_DISABLED_PREFIX = "DISABLED ";

export type JournalPurpose = "session" | "kept";

interface JournalFile {
    game: string;
    paths: string[];
    purpose: JournalPurpose;
    updatedAt: number;
}

export interface JournalEntry {
    paths: string[];
    purpose: JournalPurpose;
}

export class BisectJournal {
    private readonly dir: string;

    constructor() {
        this.dir = path.join(app.getPath("userData"), "mod-bisect");
    }

    public async write(
        game: string,
        paths: string[],
        purpose: JournalPurpose = "session",
    ): Promise<void> {
        const file = this.fileFor(game);
        if (paths.length === 0) {
            await fse.remove(file);
            return;
        }
        await fse.ensureDir(this.dir);
        const data: JournalFile = { game, paths, purpose, updatedAt: Date.now() };
        await fse.writeJson(file, data, { spaces: 2 });
    }

    public async clear(game: string): Promise<void> {
        await fse.remove(this.fileFor(game));
    }

    public async load(game: string): Promise<JournalEntry | null> {
        const file = this.fileFor(game);
        if (!(await fse.pathExists(file))) return null;
        try {
            const data = await fse.readJson(file);
            if (!Array.isArray(data?.paths)) return null;
            if (!data.paths.every((p: unknown) => typeof p === "string")) return null;
            const purpose: JournalPurpose = data.purpose === "kept" ? "kept" : "session";
            return { paths: data.paths, purpose };
        } catch {
            return null;
        }
    }

    public async listOrphans(modRootPath: string): Promise<string[]> {
        const disabledPaths = await fg(`**/*.${BISECT_DISABLED_SUFFIX}`, {
            cwd: modRootPath,
            absolute: true,
            onlyFiles: true,
            dot: false,
        });
        const suffixLen = BISECT_DISABLED_SUFFIX.length + 1;
        return disabledPaths.map((p) => p.slice(0, -suffixLen));
    }

    private fileFor(game: string): string {
        return path.join(this.dir, `${game}.json`);
    }
}

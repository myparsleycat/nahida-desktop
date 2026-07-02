import path from "node:path";
import { app } from "electron";
import fg from "fast-glob";

export const BISECT_DISABLED_SUFFIX = "mod-bisect-disabled";
export const BISECT_KEEP_DISABLED_PREFIX = "DISABLED ";

export class BisectJournal {
    private readonly dir: string;

    constructor() {
        this.dir = path.join(app.getPath("userData"), "mod-bisect");
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

    public d3dxBackupPath(game: string): string {
        return path.join(this.dir, `${game}.d3dx_user.ini.bak`);
    }
}

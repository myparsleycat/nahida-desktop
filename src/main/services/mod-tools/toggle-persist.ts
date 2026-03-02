import path from "node:path";
import { retry } from "es-toolkit";
import fse from "fs-extra";
import type { NahidaDesktop } from "@/main";

export class TogglePersist {
    private persistWatchers: string[] = [];
    private cachedD3dxUserIni: Map<string, Record<string, string>> = new Map();

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startPersistWatcher() {
        if (!this.desktop.service.xxmi) return;
        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();

        if (!xxmiPath || !xxmiConfig) return;

        const enabled = await this.desktop.setting.xxmi.getPersistToggles();
        if (!enabled) return;

        await this.stopPersistWatcher();

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        for (const importer of importers) {
            const d3dxPath = path.join(importer.importerFolder, "d3dx_user.ini");
            if (await fse.pathExists(d3dxPath)) {
                const content = await fse.readFile(d3dxPath, "utf-8");
                this.cachedD3dxUserIni.set(importer.key, this.parseD3dxUserIni(content));

                const watcherId = await this.desktop.lib.watcher.createWatcher(
                    d3dxPath,
                    { compareContents: true },
                    async (eventName, changedPath) => {
                        if (eventName === "modify") {
                            await this.handleD3dxUserIniChange(importer, changedPath);
                        }
                    },
                );
                this.persistWatchers.push(watcherId);
                this.desktop.logger.info(
                    `Started watching ${d3dxPath} for persist updates`,
                    "TogglePersist.startPersistWatcher",
                );
            }
        }
    }

    public async stopPersistWatcher() {
        for (const id of this.persistWatchers) {
            await this.desktop.lib.watcher.removeWatcher(id);
        }
        this.persistWatchers = [];
        this.cachedD3dxUserIni.clear();
    }

    private parseD3dxUserIni(content: string): Record<string, string> {
        const result: Record<string, string> = {};
        const lines = content.split(/\r?\n/);
        let inConstants = false;

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith(";")) continue;

            if (trimmed.startsWith("[")) {
                inConstants = trimmed === "[Constants]";
                continue;
            }

            if (inConstants && trimmed.startsWith("$")) {
                const parts = trimmed.split("=");
                if (parts.length >= 2) {
                    const key = parts[0].trim();
                    const value = parts.slice(1).join("=").trim();
                    result[key] = value;
                }
            }
        }
        return result;
    }

    private async handleD3dxUserIniChange(
        importer: { key: string; importerFolder: string },
        iniPath: string,
    ) {
        try {
            const content = await retry(
                async () => {
                    const isReadable = await this.desktop.lib.fs.isPathReadable(iniPath);
                    if (!isReadable) {
                        throw new Error(`Path ${iniPath} is not readable yet`);
                    }
                    return await fse.readFile(iniPath, "utf-8");
                },
                {
                    retries: 10,
                    delay: 200,
                },
            );

            const newParsed = this.parseD3dxUserIni(content);
            const oldParsed = this.cachedD3dxUserIni.get(importer.key) || {};

            for (const [key, newValue] of Object.entries(newParsed)) {
                const oldValue = oldParsed[key];
                if (newValue !== oldValue) {
                    const lastSlashIdx = key.lastIndexOf("\\");
                    if (lastSlashIdx > 1) {
                        // 1 because key starts with "$\"
                        const relIniPath = key.substring(2, lastSlashIdx);
                        const varName = key.substring(lastSlashIdx + 1);

                        const targetIniPath = path.join(importer.importerFolder, relIniPath);
                        if (await fse.pathExists(targetIniPath)) {
                            await this.updateModIniPersist(targetIniPath, varName, newValue);
                        }
                    }
                }
            }

            this.cachedD3dxUserIni.set(importer.key, newParsed);
        } catch (error) {
            this.desktop.logger.error(
                `Error handling d3dx_user.ini change: ${error}`,
                "TogglePersist.handleD3dxUserIniChange",
            );
        }
    }

    private async updateModIniPersist(targetIniPath: string, varName: string, newValue: string) {
        try {
            const content = await fse.readFile(targetIniPath, "utf-8");
            const lineEnding = content.includes("\r\n") ? "\r\n" : "\n";
            const lines = content.split(/\r?\n/);
            let inConstants = false;
            let modified = false;

            for (let i = 0; i < lines.length; i++) {
                const trimmed = lines[i].trim();
                if (trimmed.startsWith("[")) {
                    inConstants = trimmed === "[Constants]";
                    continue;
                }

                if (inConstants && trimmed.startsWith("global persist $")) {
                    const escapedVarName = varName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
                    const regex = new RegExp(`^global\\s+persist\\s+\\$${escapedVarName}\\s*=`);
                    if (regex.test(trimmed)) {
                        lines[i] = `global persist $${varName} = ${newValue}`;
                        modified = true;
                        break;
                    }
                }
            }

            if (modified) {
                await fse.writeFile(targetIniPath, lines.join(lineEnding), "utf-8");
                this.desktop.logger.info(
                    `Updated persist variable $${varName} to ${newValue} in ${targetIniPath}`,
                    "TogglePersist.updateModIniPersist",
                );
            }
        } catch (error) {
            this.desktop.logger.error(
                `Error updating mod ini ${targetIniPath}: ${error}`,
                "TogglePersist.updateModIniPersist",
            );
        }
    }
}

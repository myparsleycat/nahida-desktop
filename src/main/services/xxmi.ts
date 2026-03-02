import path from "node:path";
import { setting } from "@main/internal/db/schema";
import { findFileAcrossDrives, spawnPrivilegedProcess, waitForProcess } from "@native/native-util";
import { WaitResult } from "@native/native-util/constants";
import { type XXMIConfig, XXMIConfigSchema } from "@shared/schemas/xxmi";
import { delay, retry } from "es-toolkit";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

export class XXMI {
    private readonly desktop: NahidaDesktop;
    private xxmiConfig: XXMIConfig | null;
    private xxmiPath: string | null;

    private busy: boolean;
    private initPromise: Promise<void> | null = null;

    private persistWatchers: string[] = [];
    private cachedD3dxUserIni: Map<string, Record<string, string>> = new Map();

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.xxmiPath = null;
        this.xxmiConfig = null;

        this.busy = false;
    }

    public async init() {
        if (!this.initPromise) {
            this.initPromise = this.initialize();
        }
        await this.initPromise;
    }

    private async initialize() {
        this.xxmiPath = await this.getXXMIPath();
        if (!this.xxmiPath) {
            this.xxmiConfig = null;
            return;
        }

        const xxmiConfigPath = path.join(this.xxmiPath, "XXMI Launcher Config.json");
        try {
            const xxmiConfig = await fse.readJson(xxmiConfigPath);
            this.xxmiConfig = XXMIConfigSchema.parse(xxmiConfig);

            const persistEnabled = await this.desktop.setting.xxmi.getPersistToggles();
            if (persistEnabled) {
                await this.startPersistWatcher();
            }
        } catch (error) {
            this.desktop.logger.error(`Failed to initialize XXMI: ${error}`, "XXMI.initialize");
            this.xxmiConfig = null;
        }
    }

    private async checkConfigFile(configPath: string) {
        try {
            const xxmiConfig = await fse.readJson(configPath);
            XXMIConfigSchema.parse(xxmiConfig);
            return true;
        } catch (error) {
            this.desktop.logger.error(`Invalid XXMI config file: ${error}`, "XXMI.checkConfigFile");
            return false;
        }
    }

    public getXXMIConfig() {
        return this.xxmiConfig;
    }

    public async getXXMIData() {
        return {
            xxmiPath: this.xxmiPath,
            enabledImporters: this.getEnabledImporters(),
            xxmiConfig: this.xxmiConfig,
        };
    }

    public async getXXMIPath() {
        const path = await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, "xxmi.path"),
        });

        if (path?.value) {
            return path.value;
        }

        return null;
    }

    public async saveXXMIPath(inputPath: string) {
        const configPath = path.join(inputPath, "XXMI Launcher Config.json");

        if (!(await fse.pathExists(configPath))) {
            throw new Error("XXMI Launcher Config.json not found");
        } else if (!(await this.checkConfigFile(configPath))) {
            throw new Error("XXMI Launcher Config.json is invalid");
        }

        await this.desktop.lib.db
            .insert(setting)
            .values({
                key: "xxmi.path",
                value: inputPath,
            })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value: inputPath },
            });

        await this.initialize();
        if (this.xxmiConfig) {
            this.desktop.ipc.broadcast("renderer:reload");
        }
    }

    public async findXXMIPath() {
        const xxmiConfigName = "XXMI Launcher Config.json";
        const result = await findFileAcrossDrives(xxmiConfigName, {
            excludeDirs: ["Backups"],
        });

        if (result) {
            return path.dirname(result);
        }
        return null;
    }

    public getEnabledImporters() {
        const config = this.xxmiConfig;
        if (!config) {
            return [];
        }

        return Object.entries(config.Importers)
            .filter(([key]) => config.Packages.packages[key]?.latest_version)
            .map(([key, importer]) => {
                const packageInfo = config.Packages.packages[key];
                let importerFolder = importer.Importer.importer_folder;
                if (!path.isAbsolute(importerFolder) && this.xxmiPath) {
                    importerFolder = path.join(this.xxmiPath, importerFolder);
                }

                return {
                    key,
                    importerFolder,
                    packageInfo,
                };
            });
    }

    private getGameProcessName(importer: string, config: XXMIConfig["Importers"][string]): string {
        switch (importer.toUpperCase()) {
            case "GIMI":
                return config.Importer.game_exe_names[0];

            case "SRMI":
                return "StarRail.exe";

            case "WWMI":
                return "Client-Win64-Shipping.exe";

            case "ZZMI":
                return "ZenlessZoneZero.exe";

            case "EFMI":
            case "HIMI":
                return config.Importer.game_exe_names[0];

            default:
                return config.Importer.game_exe_names[0];
        }
    }

    public async startGame(importer: string) {
        if (this.busy) {
            throw new Error("XXMI is busy");
        }

        this.busy = true;

        try {
            await this.init();

            if (!this.xxmiPath || !this.xxmiConfig) {
                throw new Error("XXMI is not configured");
            }

            const config = this.xxmiConfig.Importers[importer];
            if (!config) {
                throw new Error(`Importer ${importer} not found`);
            }

            const launcherExe = path.join(this.xxmiPath, "Resources", "Bin", "XXMI Launcher.exe");
            if (!(await fse.pathExists(launcherExe))) {
                throw new Error(`XXMI Launcher not found at ${launcherExe}`);
            }

            const processName = this.getGameProcessName(importer, config);

            this.desktop.logger.info(
                `Starting game ${importer} via XXMI Launcher`,
                "XXMI.startGame",
            );

            await spawnPrivilegedProcess(
                launcherExe,
                `--nogui --xxmi ${importer}`,
                path.dirname(launcherExe),
            );

            this.desktop.logger.info(`Waiting for ${processName} to start...`, "XXMI.startGame");

            const { result, pid } = await waitForProcess({
                processName,
                timeout: this.xxmiConfig.Launcher.start_timeout || 60,
                withWindow: true,
                checkVisibility: true,
            });

            if (result === WaitResult.Timeout || !pid) {
                throw new Error(
                    `Failed to detect game process ${processName} after starting launcher.`,
                );
            }

            this.desktop.logger.info(`Detected ${processName} (PID: ${pid})`, "XXMI.startGame");
            await delay(1000);
        } catch (error) {
            this.desktop.logger.error(`Failed to start game: ${error}`, "XXMI.startGame");
            throw error;
        } finally {
            this.busy = false;
        }
    }

    public async startPersistWatcher() {
        if (!this.xxmiPath || !this.xxmiConfig) return;

        const enabled = await this.desktop.setting.xxmi.getPersistToggles();
        if (!enabled) return;

        await this.stopPersistWatcher();

        const importers = this.getEnabledImporters();
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
                    "XXMI.startPersistWatcher",
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
        if (!this.xxmiPath) return;

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
                "XXMI.handleD3dxUserIniChange",
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
                    "XXMI.updateModIniPersist",
                );
            }
        } catch (error) {
            this.desktop.logger.error(
                `Error updating mod ini ${targetIniPath}: ${error}`,
                "XXMI.updateModIniPersist",
            );
        }
    }
}

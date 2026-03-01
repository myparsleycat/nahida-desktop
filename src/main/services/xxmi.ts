import path from "node:path";
import { setting } from "@main/internal/db/schema";
import { findFileAcrossDrives, spawnPrivilegedProcess, waitForProcess } from "@native/native-util";
import { WaitResult } from "@native/native-util/constants";
import { type XXMIConfig, XXMIConfigSchema } from "@shared/schemas/xxmi";
import { delay } from "es-toolkit";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

export class XXMI {
    private readonly desktop: NahidaDesktop;
    private xxmiConfig: XXMIConfig | null;
    private xxmiPath: string | null;

    private busy: boolean;
    private initPromise: Promise<void> | null = null;

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
            .map(([key]) => {
                const packageInfo = config.Packages.packages[key];
                return {
                    key,
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

    private monitorInterval: NodeJS.Timeout | null = null;

    public startMonitor() {
        if (this.monitorInterval) return;

        this.scanForRunningGames().catch((err) => {
            this.desktop.logger.error(`Error in game scan: ${err}`, "XXMI.monitor");
        });

        this.monitorInterval = setInterval(() => {
            this.scanForRunningGames().catch((err) => {
                this.desktop.logger.error(`Error in game scan: ${err}`, "XXMI.monitor");
            });
        }, 5000);
    }

    public stopMonitor() {
        if (this.monitorInterval) {
            clearInterval(this.monitorInterval);
            this.monitorInterval = null;
        }
    }

    public async scanForRunningGames() {
        await this.init();

        if (this.busy || !this.xxmiConfig) return;

        try {
            const importers = await this.getEnabledImporters();
            const processList = await this.desktop.lib.native.getProcessList();

            for (const { key: importer } of importers) {
                const config = this.xxmiConfig.Importers[importer];
                const processName = this.getGameProcessName(importer, config);

                const found = processList.find(
                    (p) => p.name.toLowerCase() === processName.toLowerCase(),
                );

                if (found) {
                    if (this.desktop.service.overlay.currentTrackId === found.pid) {
                        return;
                    }

                    const title =
                        (await this.desktop.lib.native.getWindowTitle(found.pid)) || importer;

                    const isOverlayEnabled = await this.desktop.setting.overlay.getEnabled();
                    if (isOverlayEnabled) {
                        this.desktop.logger.info(
                            `Found running game ${importer} (${processName}, PID: ${found.pid}, Title: ${title}), attaching overlay...`,
                            "XXMI.scanForRunningGames",
                        );

                        await this.desktop.window.overlay.createOverlayWindow({
                            title,
                            pid: found.pid,
                        });
                    }
                    break;
                }
            }
        } catch (error) {
            this.desktop.logger.error(`Scan failed: ${error}`, "XXMI.scanForRunningGames");
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

            this.desktop.logger.info(
                `Detected ${processName} (PID: ${pid}), attaching overlay...`,
                "XXMI.startGame",
            );

            let attempts = 0;
            let title = importer;
            while (attempts < 10) {
                const fetchedTitle = await this.desktop.lib.native.getWindowTitle(pid);
                if (fetchedTitle) {
                    title = fetchedTitle;
                    break;
                }
                await delay(1000);
                attempts++;
            }

            const isOverlayEnabled = await this.desktop.setting.overlay.getEnabled();
            if (isOverlayEnabled) {
                await this.desktop.window.overlay.createOverlayWindow({
                    title,
                    pid: pid,
                });
            }

            const overlayWindow = this.desktop.window.overlay.window;
            if (overlayWindow) {
                overlayWindow.webContents.send("renderer:reload");
            }

            await delay(1000);
        } catch (error) {
            this.desktop.logger.error(`Failed to start game: ${error}`, "XXMI.startGame");
            throw error;
        } finally {
            this.busy = false;
        }
    }
}

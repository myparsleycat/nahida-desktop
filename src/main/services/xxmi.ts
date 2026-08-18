import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";

import {
    findFileAcrossDrives,
    getProcess,
    killProcess,
    spawnPrivilegedProcess,
    waitForProcess,
} from "@native/utils";
import { WaitResult } from "@native/utils/constants";
import { type XXMIConfig, XXMIConfigSchema } from "@shared/schemas/xxmi";
import { toErrorMessage } from "@shared/utils";
import { delay } from "es-toolkit";
import fse from "fs-extra";
import ky from "ky";
import { nanoid } from "nanoid";

import { drainWebStream, webStreamToNodeReadable } from "@/main/lib/web-stream-to-readable";

import type { NahidaDesktop } from "..";

import { getXxmiLibsReleases } from "./xxmi-libs-releases";

const XXMI_LAUNCHER_EXE = "XXMI Launcher.exe";

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

            const persistEnabled = await this.desktop.setting.xxmi.getPersistToggles();
            if (persistEnabled) {
                await this.desktop.service.modTools.startPersistWatcher();
            }

            const toggleViewerEnabled =
                await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
            if (toggleViewerEnabled) {
                await this.desktop.service.modTools.startToggleViewerWatcher();
            } else {
                await this.desktop.service.modTools.stopToggleViewerWatcher();
            }
        } catch (error) {
            this.desktop.logger.error(
                `Failed to initialize XXMI: ${String(error)}`,
                "XXMI.initialize",
            );
            this.xxmiConfig = null;
        }
    }

    private async checkConfigFile(configPath: string) {
        try {
            const xxmiConfig = await fse.readJson(configPath);
            XXMIConfigSchema.parse(xxmiConfig);
            return true;
        } catch (error) {
            this.desktop.logger.error(
                `Invalid XXMI config file: ${String(error)}`,
                "XXMI.checkConfigFile",
            );
            return false;
        }
    }

    public getXXMIConfig() {
        return this.xxmiConfig;
    }

    public async getXXMIData() {
        await this.init();
        return {
            xxmiPath: this.xxmiPath,
            dllVersion: await this.getDllVersion(),
            enabledImporters: this.getEnabledImporters(),
            xxmiConfig: this.xxmiConfig,
        };
    }

    private async getDllVersion() {
        if (!this.xxmiPath) return null;

        try {
            const manifest = await fse.readJson(
                path.join(this.xxmiPath, "Resources", "Packages", "XXMI", "Manifest.json"),
            );
            return typeof manifest?.version === "string" ? manifest.version : null;
        } catch {
            return null;
        }
    }

    public async getXXMIPath() {
        return await this.desktop.lib.db.settings.getValue("xxmi.path");
    }

    public async saveXXMIPath(inputPath: string) {
        const configPath = path.join(inputPath, "XXMI Launcher Config.json");

        if (!(await fse.pathExists(configPath))) {
            throw new Error("XXMI Launcher Config.json not found");
        } else if (!(await this.checkConfigFile(configPath))) {
            throw new Error("XXMI Launcher Config.json is invalid");
        }

        await this.desktop.lib.db.settings.upsert("xxmi.path", inputPath);

        await this.initialize();
        if (this.xxmiConfig) {
            this.desktop.ipc.broadcast("renderer:reload");
        }
    }

    public async findXXMIPath() {
        const xxmiConfigName = "XXMI Launcher Config.json";
        const appDataPath = process.env.APPDATA;
        if (appDataPath) {
            const configPath = path.join(appDataPath, "XXMI Launcher", xxmiConfigName);
            if ((await fse.pathExists(configPath)) && (await this.checkConfigFile(configPath))) {
                return path.dirname(configPath);
            }
        }

        const result = await findFileAcrossDrives(xxmiConfigName, {
            excludeDirs: ["Backups"],
        });

        if (result) {
            return path.dirname(result);
        }
        return null;
    }

    public async getLibsReleases() {
        return getXxmiLibsReleases(this.desktop.logger);
    }

    public async ensureLauncherClosed() {
        const deadline = Date.now() + 5000;
        while (true) {
            const running = getProcess(undefined, XXMI_LAUNCHER_EXE);
            if (!running) return;
            if (!killProcess(running.pid)) {
                throw new Error("Failed to close XXMI Launcher");
            }
            if (Date.now() >= deadline) {
                throw new Error("XXMI Launcher is still running");
            }
            await delay(100);
        }
    }

    public async installDllVersion({ version }: { version: string }) {
        await this.init();

        const selectedVersion = version.trim();
        if (!selectedVersion) {
            throw new Error("No version selected");
        }

        if (!this.xxmiPath) {
            throw new Error("XXMI is not configured");
        }

        if (this.busy) {
            throw new Error("XXMI is busy");
        }

        this.busy = true;
        const xxmiPath = this.xxmiPath;
        const destDir = path.join(xxmiPath, "Resources", "Packages", "XXMI");
        const workDir = path.join(os.tmpdir(), `nahida-xxmi-dll-${nanoid()}`);

        try {
            await this.ensureLauncherClosed();
            await fse.ensureDir(workDir);
            const zipPath = path.join(workDir, "package.zip");
            const githubHeaders = {
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                Referer: "https://github.com/SpectrumQT/XXMI-Libs-Package",
            };
            const resp = await ky.get(
                `https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/${selectedVersion}/XXMI-PACKAGE-${selectedVersion}.zip`,
                { headers: githubHeaders },
            );

            if (!resp.ok) {
                await drainWebStream(resp.body).catch(() => {});
                throw new Error(`Failed to download XXMI package: ${resp.statusText}`);
            }
            if (!resp.body) {
                throw new Error("No response body");
            }

            await pipeline(webStreamToNodeReadable(resp.body), fse.createWriteStream(zipPath));

            const extractedPath = await this.desktop.service.archive.extract(
                zipPath,
                path.join(workDir, "extracted"),
            );
            const entries = await fse.readdir(extractedPath, { withFileTypes: true });
            const onlyDir =
                entries.length === 1 && entries[0].isDirectory() ? entries[0].name : null;

            const stagingDir = path.join(workDir, "staging");
            await fse.copy(onlyDir ? path.join(extractedPath, onlyDir) : extractedPath, stagingDir);

            const manifestResp = await ky.get(
                `https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/${selectedVersion}/Manifest.json`,
                { headers: githubHeaders },
            );
            if (!manifestResp.ok) {
                await drainWebStream(manifestResp.body).catch(() => {});
                throw new Error(`Failed to download XXMI manifest: ${manifestResp.statusText}`);
            }
            const manifestBuffer = Buffer.from(await manifestResp.arrayBuffer());
            await fse.writeFile(path.join(stagingDir, "Manifest.json"), manifestBuffer);

            const manifestJson = JSON.parse(manifestBuffer.toString("utf8"));
            const manifestVersion =
                typeof manifestJson?.version === "string" ? manifestJson.version : null;
            if (manifestVersion !== selectedVersion) {
                throw new Error(
                    `Manifest version mismatch: expected ${selectedVersion}, got ${manifestVersion ?? "null"}`,
                );
            }

            const configPath = path.join(xxmiPath, "XXMI Launcher Config.json");
            const config = await fse.readJson(configPath);
            if (!config.Launcher) {
                throw new Error("XXMI Launcher config is missing Launcher section");
            }

            config.Launcher.auto_update = false;
            await fse.writeJson(configPath, config, { spaces: 4 });
            this.xxmiConfig = XXMIConfigSchema.parse(await fse.readJson(configPath));

            await fse.ensureDir(destDir);
            await fse.copy(stagingDir, destDir, { overwrite: true });

            this.desktop.logger.info(
                `Installed XXMI DLL version ${selectedVersion} to ${destDir}`,
                "XXMI.installDllVersion",
            );
        } catch (error) {
            this.desktop.logger.error(
                {
                    action: "xxmi:installDllVersion",
                    version: selectedVersion,
                    xxmiPath,
                    destDir,
                    error: toErrorMessage(error),
                },
                "XXMI.installDllVersion",
            );
            throw error;
        } finally {
            this.busy = false;
            await fse.remove(workDir).catch(() => {});
        }
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
            this.desktop.logger.error(`Failed to start game: ${String(error)}`, "XXMI.startGame");
            throw error;
        } finally {
            this.busy = false;
        }
    }
}

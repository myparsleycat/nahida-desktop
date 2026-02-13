import { spawn } from "node:child_process";
import path from "node:path";
import { db } from "@main/internal/db";
import { setting } from "@main/internal/db/schema";
import { DllInjector } from "@native/dll-injector";
import { findFileAcrossDrives, waitForProcess } from "@native/native-util";
import { WaitResult } from "@native/native-util/constants";
import { type XXMIConfig, XXMIConfigSchema } from "@shared/schemas/xxmi";
import { delay } from "es-toolkit";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

function usesGIFPSUnlocker(
    config: XXMIConfig["Importers"][string],
): config is XXMIConfig["Importers"][string] & {
    Importer: {
        unlock_fps: boolean;
        unlock_fps_value?: number;
    };
} {
    return "unlock_fps" in config.Importer && "unlock_fps_value" in config.Importer;
}

export class XXMI {
    private readonly desktop: NahidaDesktop;
    private xxmiConfig: XXMIConfig | null;
    private xxmiPath: string | null;
    private packagePath: string | null;
    private busy: boolean;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.xxmiPath = null;
        this.xxmiConfig = null;
        this.packagePath = null;
        this.busy = false;
        this.initialize();
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
            this.packagePath = path.join(this.xxmiPath, "Resources", "Packages", "XXMI");
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
            enabledImporters: await this.getEnabledImporters(),
            xxmiConfig: this.xxmiConfig,
        };
    }

    public async getXXMIPath() {
        const path = await db.query.setting.findFirst({
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

        await db
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

    public async getEnabledImporters() {
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

    private async updateD3dxIni(importerPath: string, gameExeName: string) {
        const d3dxIniPath = path.join(importerPath, "d3dx.ini");
        if (!(await fse.pathExists(d3dxIniPath))) {
            return;
        }

        let content = await fse.readFile(d3dxIniPath, "utf-8");

        const targetRegex = /^(\s*target\s*=\s*)(.*)$/m;
        if (targetRegex.test(content)) {
            content = content.replace(targetRegex, `$1${gameExeName}`);
        }

        const currentExe = path.basename(process.execPath);
        const loaderRegex = /^(\s*loader\s*=\s*)(.*)$/m;
        if (loaderRegex.test(content)) {
            content = content.replace(loaderRegex, `$1${currentExe}`);
        }

        await fse.writeFile(d3dxIniPath, content, "utf-8");
    }

    private async configureFpsUnlocker(
        importer: string,
        gameFolder: string,
        gameExeName: string,
    ): Promise<void> {
        const xxmiConfig = this.xxmiConfig;
        if (!xxmiConfig) {
            throw new Error("XXMI Config not found");
        }

        const config = xxmiConfig.Importers[importer];
        if (!config) {
            throw new Error("Importer not found");
        }

        if (!this.xxmiPath) {
            throw new Error("XXMI Path not found");
        }

        const fpsUnlockerPath = path.join(
            this.xxmiPath,
            "Resources",
            "Packages",
            "GI-FPS-Unlocker",
        );
        const fpsConfigTemplatePath = path.join(fpsUnlockerPath, "fps_config_template.json");
        const fpsConfigPath = path.join(fpsUnlockerPath, "fps_config.json");

        if (!(await fse.pathExists(fpsConfigPath))) {
            if (await fse.pathExists(fpsConfigTemplatePath)) {
                await fse.copy(fpsConfigTemplatePath, fpsConfigPath);
            }
        }

        let fpsConfig: Record<string, unknown> = {};
        if (await fse.pathExists(fpsConfigPath)) {
            fpsConfig = await fse.readJson(fpsConfigPath);
        }

        const gameExePath = path.join(gameFolder, gameExeName);

        let modified = false;

        if (fpsConfig.GamePath !== gameExePath) {
            fpsConfig.GamePath = gameExePath;
            modified = true;
        }

        const processPriorityMap: Record<string, number> = {
            Idle: 5,
            BelowNormal: 4,
            Normal: 3,
            AboveNormal: 2,
            High: 1,
            RealTime: 0,
        };
        const processPriority =
            processPriorityMap[config.Importer.process_priority || "AboveNormal"] || 2;

        if (fpsConfig.Priority !== processPriority) {
            fpsConfig.Priority = processPriority;
            modified = true;
        }

        if (fpsConfig.AdditionalCommandLine !== (config.Importer.launch_options || "")) {
            fpsConfig.AdditionalCommandLine = config.Importer.launch_options || "";
            modified = true;
        }

        type WindowModeSettings = {
            PopupWindow: boolean;
            Fullscreen: boolean;
            IsExclusiveFullscreen: boolean;
        };

        const windowModesMap: Record<string, WindowModeSettings> = {
            Windowed: {
                PopupWindow: false,
                Fullscreen: false,
                IsExclusiveFullscreen: false,
            },
            Borderless: {
                PopupWindow: true,
                Fullscreen: false,
                IsExclusiveFullscreen: false,
            },
            Fullscreen: {
                PopupWindow: false,
                Fullscreen: true,
                IsExclusiveFullscreen: false,
            },
            "Exclusive Fullscreen": {
                PopupWindow: false,
                Fullscreen: true,
                IsExclusiveFullscreen: true,
            },
        };

        const windowMode = config.Importer.window_mode || "Borderless";
        const windowSettings = windowModesMap[windowMode] || windowModesMap.Borderless;

        for (const [setting, value] of Object.entries(windowSettings)) {
            if (fpsConfig[setting] !== value) {
                fpsConfig[setting] = value;
                modified = true;
            }
        }

        const fpsTarget =
            usesGIFPSUnlocker(config) && config.Importer.unlock_fps_value
                ? config.Importer.unlock_fps_value
                : 120;
        if (fpsConfig.FPSTarget !== fpsTarget) {
            fpsConfig.FPSTarget = fpsTarget;
            modified = true;
        }

        if (modified) {
            await fse.writeJson(fpsConfigPath, fpsConfig, { spaces: 4 });
        }
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

    private getStartCommand(
        importer: string,
        config: XXMIConfig["Importers"][string],
    ): { startExePath: string; workDir: string; startArgs?: string[] } {
        const gameFolder = config.Importer.game_folder;

        if (usesGIFPSUnlocker(config) && config.Importer.unlock_fps) {
            if (!this.xxmiPath) {
                throw new Error("XXMI Path not found");
            }
            const startExePath = path.join(
                this.xxmiPath,
                "Resources",
                "Packages",
                "GI-FPS-Unlocker",
                "unlockfps_nc.exe",
            );
            return {
                startExePath,
                workDir: path.dirname(startExePath),
                startArgs: [],
            };
        }

        if (importer === "WWMI") {
            if (config.Importer.use_launch_options) {
                const exePath = path.join(
                    gameFolder,
                    "Client",
                    "Binaries",
                    "Win64",
                    "Client-Win64-Shipping.exe",
                );
                return {
                    startExePath: exePath,
                    workDir: path.dirname(exePath),
                    startArgs: ["-dx11"],
                };
            } else {
                const wrapperExe = path.join(gameFolder, "Wuthering Waves.exe");
                return {
                    startExePath: wrapperExe,
                    workDir: gameFolder,
                    startArgs: ["-dx11"],
                };
            }
        }

        if (importer === "EFMI") {
            const processName = this.getGameProcessName(importer, config);
            const gameExePath = path.join(gameFolder, processName);
            return {
                startExePath: gameExePath,
                workDir: path.dirname(gameExePath),
                startArgs: ["-force-d3d11"],
            };
        }

        const processName = this.getGameProcessName(importer, config);
        const gameExePath = path.join(gameFolder, processName);
        return {
            startExePath: gameExePath,
            workDir: path.dirname(gameExePath),
            startArgs: [],
        };
    }

    private validateAndGetPaths(importer: string) {
        if (!this.xxmiConfig) {
            throw new Error("XXMI Config not found");
        }

        const config = this.xxmiConfig.Importers[importer];
        if (!config) {
            throw new Error("Importer not found");
        }

        if (!this.xxmiPath) {
            throw new Error("XXMI Path not found");
        }

        const processName = this.getGameProcessName(importer, config);
        const importerFolder = path.join(this.xxmiPath, config.Importer.importer_folder);
        const dllPath = path.join(importerFolder, "d3d11.dll");

        return { config, processName, importerFolder, dllPath };
    }

    private async deployPackageFiles(importerFolder: string) {
        if (!this.packagePath) {
            throw new Error("Package Path not found");
        }

        const filesToDeploy = ["d3d11.dll", "d3dcompiler_47.dll"];

        for (const fileName of filesToDeploy) {
            const sourcePath = path.join(this.packagePath, fileName);
            const targetPath = path.join(importerFolder, fileName);

            if (await fse.pathExists(targetPath)) {
                this.desktop.logger.info(
                    `File ${fileName} already exists in ${importerFolder}, skip deployment.`,
                    "XXMI.deployPackageFiles",
                );
                continue;
            }

            if (await fse.pathExists(sourcePath)) {
                this.desktop.logger.info(
                    `Deploying ${fileName} to ${importerFolder}`,
                    "XXMI.deployPackageFiles",
                );
                await fse.copy(sourcePath, targetPath);
            } else {
                this.desktop.logger.warn(
                    `Source file ${sourcePath} not found for deployment`,
                    "XXMI.deployPackageFiles",
                );
            }
        }
    }

    private async prepareEnvironment(
        importer: string,
        config: XXMIConfig["Importers"][string],
        processName: string,
        importerFolder: string,
    ) {
        await this.deployPackageFiles(importerFolder);

        if (usesGIFPSUnlocker(config) && config.Importer.unlock_fps) {
            await this.configureFpsUnlocker(importer, config.Importer.game_folder, processName);
        }

        await this.updateD3dxIni(importerFolder, processName);
    }

    private getLaunchParams(importer: string, config: XXMIConfig["Importers"][string]) {
        const {
            startExePath,
            workDir,
            startArgs: gameStartArgs,
        } = this.getStartCommand(importer, config);

        let startArgs: string[] = gameStartArgs || [];

        if (config.Importer.use_launch_options && config.Importer.launch_options) {
            startArgs = startArgs.concat(config.Importer.launch_options.split(" "));
        }
        return { startExePath, workDir, startArgs };
    }

    private async injectWithHook(
        injector: DllInjector,
        processName: string,
        dllPath: string,
        launchParams: { startExePath: string; workDir: string; startArgs: string[] },
        config: XXMIConfig["Importers"][string],
        customLaunchCmd: string | undefined,
        extra_dll_paths: string[],
    ) {
        try {
            injector.hookLibrary(dllPath, processName);

            this.desktop.logger.info(`hooked ${processName}`, "XXMI.startGame");

            this.desktop.logger.info(
                `Starting process: method=${config.Importer.process_start_method}, exe=${launchParams.startExePath}, workDir=${launchParams.workDir}, args=${JSON.stringify(launchParams.startArgs)}`,
                "XXMI.startGame",
            );

            const CREATE_NEW_CONSOLE = 0x00000010;
            const CREATE_DEFAULT_ERROR_MODE = 0x04000000;
            const processFlags = CREATE_NEW_CONSOLE | CREATE_DEFAULT_ERROR_MODE;

            injector.openProcess(
                config.Importer.process_start_method,
                launchParams.startExePath,
                launchParams.workDir,
                launchParams.startArgs,
                processFlags,
                processName,
                extra_dll_paths,
                customLaunchCmd,
                this.xxmiConfig?.Launcher.start_timeout,
            );

            this.desktop.logger.info(`openProcess completed`, "XXMI.startGame");

            const hooked = await injector.waitForInjection(5);
            if (hooked) {
                this.desktop.logger.info(
                    `Successfully passed early ${path.basename(dllPath)} -> ${processName} hook check!`,
                    "XXMI.startGame",
                );
            }

            const { result } = await waitForProcess({
                processName,
                timeout: this.xxmiConfig?.Launcher.start_timeout,
                withWindow: true,
                checkVisibility: true,
            });

            if (result === WaitResult.Timeout) {
                if (hooked) {
                    throw `Failed to detect game process ${processName}!\n\nIf game crashed, try to adjust XXMI Delay in General Settings or clear Mods and ShaderFixes folders.\n\nIf game window takes more than ${this.xxmiConfig?.Launcher.start_timeout} seconds to appear, adjust Timeout in Launcher Settings.`;
                } else {
                    throw `Failed to start ${processName}!`;
                }
            }

            const lateHooked = await injector.waitForInjection(5);
            if (lateHooked) {
                this.desktop.logger.info(
                    `Successfully passed late ${path.basename(dllPath)} -> ${processName} hook check!`,
                    "XXMI.startGame",
                );
            } else if (!hooked) {
                this.desktop.logger.error(
                    `Failed to verify ${path.basename(dllPath)} -> ${processName} hook!`,
                    "XXMI.startGame",
                );
            }
        } finally {
            injector.unhookLibrary();
            injector.unload();
        }
    }

    private async injectDirectly(
        injector: DllInjector,
        processName: string,
        dllPath: string,
        launchParams: { startExePath: string; workDir: string; startArgs: string[] },
        config: XXMIConfig["Importers"][string],
        customLaunchCmd: string | undefined,
        extra_dll_paths: string[],
    ) {
        let dllPaths: string[] = [];
        if (config.Importer.custom_launch_inject_mode === "Bypass") {
            dllPaths = extra_dll_paths;
        } else {
            dllPaths = [dllPath].concat(extra_dll_paths);
        }

        const CREATE_NEW_CONSOLE = 0x00000010;
        const CREATE_DEFAULT_ERROR_MODE = 0x04000000;
        const processFlags = CREATE_NEW_CONSOLE | CREATE_DEFAULT_ERROR_MODE;

        injector.openProcess(
            config.Importer.process_start_method,
            launchParams.startExePath,
            launchParams.workDir,
            launchParams.startArgs,
            processFlags,
            processName,
            dllPaths,
            customLaunchCmd,
            this.xxmiConfig?.Launcher.start_timeout,
        );

        this.desktop.logger.info(`opened ${processName}`, "XXMI.startGame");

        const { result } = await waitForProcess({
            processName,
            withWindow: true,
            timeout: this.xxmiConfig?.Launcher.start_timeout,
            checkVisibility: true,
        });

        this.desktop.logger.info(`detected ${processName}`, "XXMI.startGame");

        if (result === WaitResult.Timeout) {
            throw `Failed to detect game process ${processName}!\n\nIf game crashed, try to adjust XXMI Delay in General Settings or clear Mods and ShaderFixes folders.\n\nIf game window takes more than ${this.xxmiConfig?.Launcher.start_timeout} seconds to appear, adjust Timeout in Launcher Settings.`;
        }
    }

    private async runPreLaunch(config: XXMIConfig["Importers"][string]): Promise<void> {
        if (config.Importer.run_pre_launch_enabled && config.Importer.run_pre_launch) {
            const cmd = config.Importer.run_pre_launch.trim();
            if (!cmd) return;

            this.desktop.logger.info(`Executing pre-launch command: ${cmd}`, "XXMI.runPreLaunch");

            const child = spawn(cmd, [], { shell: true });

            if (config.Importer.run_pre_launch_wait) {
                await new Promise<void>((resolve, reject) => {
                    child.on("exit", (code) => {
                        if (code === 0 || code === null) {
                            resolve();
                        } else {
                            reject(new Error(`Pre-launch command failed with code ${code}`));
                        }
                    });
                    child.on("error", reject);
                });
            }
        }
    }

    private async runPostLoad(config: XXMIConfig["Importers"][string]): Promise<void> {
        if (config.Importer.run_post_load_enabled && config.Importer.run_post_load) {
            const cmd = config.Importer.run_post_load.trim();
            if (!cmd) return;

            this.desktop.logger.info(`Executing post-load command: ${cmd}`, "XXMI.runPostLoad");

            const child = spawn(cmd, [], { shell: true });

            if (config.Importer.run_post_load_wait) {
                await new Promise<void>((resolve, reject) => {
                    child.on("exit", (code) => {
                        if (code === 0 || code === null) {
                            resolve();
                        } else {
                            reject(new Error(`Post-load command failed with code ${code}`));
                        }
                    });
                    child.on("error", reject);
                });
            }
        }
    }

    private getExtraDllPaths(config: XXMIConfig["Importers"][string]): string[] {
        if (!config.Importer.extra_libraries_enabled || !config.Importer.extra_libraries) {
            return [];
        }

        const dllPaths: string[] = [];
        const lines = config.Importer.extra_libraries.split("\n");

        for (const line of lines) {
            const dllPath = line.trim();
            if (!dllPath) continue;

            let absolutePath: string;
            if (path.isAbsolute(dllPath)) {
                absolutePath = dllPath;
            } else {
                if (!this.xxmiPath) {
                    throw new Error("XXMI Path not found");
                }
                absolutePath = path.join(this.xxmiPath, dllPath);
            }

            if (!fse.existsSync(absolutePath)) {
                throw new Error(
                    `Failed to inject extra library ${absolutePath}: File not found! Please check Advanced Settings → Inject Libraries.`,
                );
            }

            dllPaths.push(absolutePath);
        }

        return dllPaths;
    }

    public async startGame(importer: string) {
        if (this.busy) {
            throw new Error("XXMI is busy");
        }

        try {
            const { config, processName, importerFolder, dllPath } =
                this.validateAndGetPaths(importer);

            await this.runPreLaunch(config);

            await this.prepareEnvironment(importer, config, processName, importerFolder);

            const launchParams = this.getLaunchParams(importer, config);

            if (!this.packagePath) {
                throw new Error("Package Path not found");
            }

            const extra_dll_paths = this.getExtraDllPaths(config);

            let useHook = importer.toUpperCase() !== "EFMI";
            let customLaunchCmd: string | undefined;

            if (config.Importer.custom_launch_enabled) {
                useHook = config.Importer.custom_launch_inject_mode === "Hook";
                customLaunchCmd = config.Importer.custom_launch.trim();
            }

            const loaderPath = path.join(this.packagePath, "3dmloader.dll");
            const injector = new DllInjector(loaderPath);

            this.desktop.logger.info(`injecting ${processName}`, "XXMI.startGame");

            if (useHook) {
                try {
                    await this.injectWithHook(
                        injector,
                        processName,
                        dllPath,
                        launchParams,
                        config,
                        customLaunchCmd,
                        extra_dll_paths,
                    );
                } catch (e: any) {
                    this.desktop.logger.error(
                        `Injection failed: ${e.message || e}`,
                        "XXMI.startGame",
                    );
                    throw e; // Re-throw to be caught by outer handler
                }
            } else {
                await this.injectDirectly(
                    injector,
                    processName,
                    dllPath,
                    launchParams,
                    config,
                    customLaunchCmd,
                    extra_dll_paths,
                );
            }

            await this.runPostLoad(config);

            await delay(1000);
        } finally {
            this.busy = false;
        }
    }
}

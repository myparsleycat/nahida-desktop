import { exec } from "node:child_process";
import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { promisify } from "node:util";
import fse from "fs-extra";
import ky from "ky";
import ms from "ms";
import type { NahidaDesktop } from "@/main";

const execAsync = promisify(exec);

type ExecBuildError = Error & {
    code?: number | string;
    signal?: NodeJS.Signals;
    stdout?: string | Buffer;
    stderr?: string | Buffer;
};

type BuildD3DResult = {
    success: boolean;
    errorMessage?: string;
};

const TARGET_DLL_NAME = "d3d11.dll";

export class DllBuilder {
    private readonly VS_EDITIONS = ["Community", "Professional", "Enterprise", "Insiders"];
    private readonly VS_VERSIONS = ["2025", "2022", "18", "17"];
    private readonly RELEASES_FETCH_COOLDOWN_MS = ms("1m");

    private isBuilding = false;
    private currentProgress = "";
    private currentErrorMessage = "";

    private releasesCache: Record<string, string[]> = {
        SpectrumQT: ["master"],
        myparsleycat: ["master"],
    };
    private readonly releasesFetchedAt: Partial<Record<string, number>> = {};
    private readonly releasesFetchInFlight: Partial<Record<string, Promise<void>>> = {};

    constructor(private readonly desktop: NahidaDesktop) {
        this.updateReleases();
    }

    public getBuilderState() {
        return {
            isBuilding: this.isBuilding,
            progress: this.currentProgress,
            errorMessage: this.currentErrorMessage,
        };
    }

    public async updateReleases() {
        await Promise.all([
            this.fetchProviderReleases("SpectrumQT"),
            this.fetchProviderReleases("myparsleycat"),
        ]);
    }

    private async fetchProviderReleases(provider: string) {
        const inFlight = this.releasesFetchInFlight[provider];
        if (inFlight) {
            await inFlight;
            return;
        }

        const now = Date.now();
        const lastFetchedAt = this.releasesFetchedAt[provider] ?? 0;
        if (now - lastFetchedAt < this.RELEASES_FETCH_COOLDOWN_MS) {
            return;
        }

        const fetchPromise = this.fetchProviderReleasesInternal(provider);
        this.releasesFetchInFlight[provider] = fetchPromise;

        try {
            await fetchPromise;
            this.releasesFetchedAt[provider] = Date.now();
        } finally {
            delete this.releasesFetchInFlight[provider];
        }
    }

    private async fetchProviderReleasesInternal(provider: string) {
        try {
            const owner = provider === "myparsleycat" ? "myparsleycat" : "SpectrumQT";
            const url = `https://api.github.com/repos/${owner}/XXMI-Libs-Package/releases`;
            const resp = await ky.get(url, {
                headers: {
                    "User-Agent":
                        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                },
                // @ts-expect-error
                dispatcher: await this.desktop.httpService.getAgent(),
                throwHttpErrors: false,
            });

            if (!resp.ok) {
                this.releasesCache[provider] = ["master"];
                return;
            }

            // oxlint-disable-next-line typescript/no-explicit-any
            const releases = (await resp.json()) as any[];
            // oxlint-disable-next-line typescript/no-explicit-any
            this.releasesCache[provider] = ["master", ...releases.map((r: any) => r.tag_name)];
        } catch (error) {
            this.desktop.logger.error(error, "DllBuilder:fetchProviderReleases");
            this.releasesCache[provider] = ["master"];
        }
    }

    public getProviderReleases(provider: string) {
        return this.releasesCache[provider] || ["master"];
    }

    private updateProgress(code: string, errorMessage = "") {
        this.currentProgress = code;
        this.currentErrorMessage = errorMessage;
        this.desktop.ipc.broadcast("tools:progress", code);
    }

    public async buildNewD3DDLL({
        provider,
        version,
        importerKey,
        importerPath,
    }: {
        provider: string;
        version: string;
        importerKey: string;
        importerPath?: string;
    }): Promise<BuildD3DResult> {
        if (this.isBuilding) {
            return { success: false };
        }

        this.isBuilding = true;
        this.updateProgress("XXMI_INIT");
        if (!importerPath || !(await fse.pathExists(importerPath))) {
            this.updateProgress("XXMI_ERR_GIMI_NOT_FOUND");
            this.isBuilding = false;
            return { success: false };
        }

        const finalDestination = path.join(importerPath, TARGET_DLL_NAME);
        const destinationCheck = await this.desktop.lib.fs.isPathWritable(
            finalDestination,
            { detailed: true, parentPath: importerPath },
        );
        if (!destinationCheck.writable) {
            const errorCode = destinationCheck.locked
                ? "XXMI_ERR_DLL_IN_USE"
                : "XXMI_ERR_DLL_NOT_WRITABLE";
            const errorMessage = this.desktop.lib.fs.formatProcessList(destinationCheck.processes);
            this.updateProgress(errorCode, errorMessage);
            this.isBuilding = false;
            return { success: false, errorMessage };
        }

        this.updateProgress("XXMI_FIND_VS");
        const vcvarsPath = await this.findVsDevCmd();
        if (!vcvarsPath) {
            this.updateProgress("XXMI_ERR_VS_NOT_FOUND");
            this.isBuilding = false;
            return { success: false };
        }

        const tempDir = path.join(os.tmpdir(), "nahida-tools-d3d-build");
        await fse.ensureDir(tempDir);
        await fse.emptyDir(tempDir);

        try {
            const projectPath = await this.prepareSourceCode(tempDir, provider, version);

            this.updateProgress("XXMI_BUILDING");
            this.desktop.logger.info("Building D3D11 DLL...", "DllBuilder:buildNewD3DDLL");

            const buildSuccess = await this.executeMsBuild(vcvarsPath, projectPath);
            if (!buildSuccess) {
                this.isBuilding = false;
                return { success: false };
            }

            const builtDllPath = path.join(projectPath, "x64", "Release", TARGET_DLL_NAME);
            if (!(await fse.pathExists(builtDllPath))) {
                this.updateProgress("XXMI_ERR_DLL_NOT_FOUND");
                this.isBuilding = false;
                return { success: false };
            }

            try {
                await fse.copy(builtDllPath, finalDestination, { overwrite: true });
            } catch (error) {
                const lockInfo = await this.desktop.lib.fs.isLockedPathError(error, finalDestination);
                if (lockInfo.isLocked) {
                    const errorMessage = this.desktop.lib.fs.formatProcessList(lockInfo.processes);
                    this.updateProgress("XXMI_ERR_DLL_IN_USE", errorMessage);
                    this.isBuilding = false;
                    return { success: false, errorMessage };
                }
                throw error;
            }

            const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
            if (xxmiPath) {
                await this.enableUnsafeMode(xxmiPath, importerKey);
            }

            this.updateProgress("XXMI_BUILD_SUCCESS");
            this.desktop.logger.info(
                `Successfully built and installed d3d11.dll to ${finalDestination}`,
                "DllBuilder:buildNewD3DDLL",
            );

            this.isBuilding = false;
            return { success: true };
        } catch (error) {
            this.desktop.logger.error(error, "DllBuilder:buildNewD3DDLL");
            const errorMessage = this.extractBuildErrorMessage(error);
            this.updateProgress("XXMI_ERR_BUILD_FAILED", errorMessage);
            this.isBuilding = false;
            return { success: false, errorMessage };
        } finally {
            await fse.remove(tempDir).catch(() => {});
        }
    }

    private async findVsDevCmd(): Promise<string | null> {
        const baseDir = "C:\\Program Files\\Microsoft Visual Studio";

        for (const version of this.VS_VERSIONS) {
            for (const edition of this.VS_EDITIONS) {
                const candidatePath = path.join(
                    baseDir,
                    version,
                    edition,
                    "VC",
                    "Auxiliary",
                    "Build",
                    "vcvars64.bat",
                );
                if (await fse.pathExists(candidatePath)) {
                    return candidatePath;
                }
            }
        }
        return null;
    }

    private async prepareSourceCode(
        workDir: string,
        provider: string,
        version: string,
    ): Promise<string> {
        this.updateProgress("XXMI_DOWNLOAD_REPO");
        this.desktop.logger.info("Downloading XXMI Repo...", "DllBuilder:prepareSourceCode");

        const zipPath = await this.downloadXXMIRepo(workDir, provider, version);

        this.updateProgress("XXMI_EXTRACT_REPO");
        this.desktop.logger.info("Extracting Repo...", "DllBuilder:prepareSourceCode");

        const extractDir = await this.desktop.service.archive.extract(zipPath, workDir);

        const entries = await fse.readdir(extractDir);
        const repoDirName = entries.find((e) => e.startsWith("XXMI-Libs-Package"));

        return repoDirName ? path.join(extractDir, repoDirName) : extractDir;
    }

    private async downloadXXMIRepo(
        targetDir: string,
        provider: string,
        version: string,
    ): Promise<string> {
        const owner = provider === "myparsleycat" ? "myparsleycat" : "SpectrumQT";
        const url =
            version === "master"
                ? `https://github.com/${owner}/XXMI-Libs-Package/archive/refs/heads/master.zip`
                : `https://github.com/${owner}/XXMI-Libs-Package/archive/refs/tags/${version}.zip`;

        const zipPath = path.join(targetDir, "repo.zip");

        const resp = await ky.get(url, {
            headers: {
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                Referer: `https://github.com/${provider}/XXMI-Libs-Package`,
            },
            // @ts-expect-error
            dispatcher: await this.desktop.httpService.getAgent(),
        });

        if (!resp.ok) {
            throw new Error(`Failed to download repo: ${resp.statusText}`);
        }

        await pipeline(resp.body as ReadableStream, fse.createWriteStream(zipPath));

        return zipPath;
    }

    private async executeMsBuild(vcvarsPath: string, projectPath: string): Promise<boolean> {
        const buildCommand = [
            `"${vcvarsPath}"`,
            `cd /d "${projectPath}"`,
            "msbuild StereovisionHacks.sln /nologo /verbosity:minimal /consoleloggerparameters:ErrorsOnly /p:Configuration=Release /p:Platform=x64",
        ].join(" && ");

        try {
            await execAsync(buildCommand, { maxBuffer: 1024 * 1024 * 20 });
            return true;
        } catch (e) {
            const error = e as ExecBuildError;
            const stderr = this.formatBuildOutput(error.stderr);
            const stdout = this.formatBuildOutput(error.stdout);
            const details = [
                `Build failed: ${error.message}`,
                `Command: ${buildCommand}`,
                `Project path: ${projectPath}`,
                `Exit code: ${error.code ?? "unknown"}`,
                `Signal: ${error.signal ?? "none"}`,
                stderr ? `stderr:\n${stderr}` : "stderr: <empty>",
                stdout ? `stdout:\n${stdout}` : "stdout: <empty>",
            ];

            throw new Error(details.join("\n"));
        }
    }

    private formatBuildOutput(output?: string | Buffer): string {
        if (!output) {
            return "";
        }

        const text = Buffer.isBuffer(output) ? output.toString("utf8") : output;
        const trimmed = text.trim();
        if (!trimmed) {
            return "";
        }

        const lines = trimmed.split(/\r?\n/);
        const maxLines = 120;
        const omitted = lines.length - maxLines;
        const tail = omitted > 0 ? lines.slice(-maxLines) : lines;

        return omitted > 0
            ? `[showing last ${maxLines} lines, omitted ${omitted} earlier lines]\n${tail.join("\n")}`
            : tail.join("\n");
    }

    private extractBuildErrorMessage(error: unknown): string {
        const message = error instanceof Error ? error.message : String(error);
        const lines = message
            .split(/\r?\n/)
            .map((line) => line.trim())
            .filter(Boolean);
        const errorLines = lines.filter((line) => /\berror\s+[A-Z]+\d+:/i.test(line));

        if (errorLines.length > 0) {
            return errorLines.slice(0, 12).join("\n");
        }

        const outputIndex = lines.findIndex((line) => line === "stdout:" || line === "stderr:");
        const fallbackLines = outputIndex >= 0 ? lines.slice(outputIndex + 1) : lines;

        return fallbackLines.slice(-12).join("\n");
    }

    private async generateUnsafeModeSignature(xxmiPath: string) {
        const privateKeyPath = path.join(xxmiPath, "Resources", "Security", "private_key.der");
        const privateKeyBase64 = await fse.readFile(privateKeyPath, "utf8");
        const privateKeyBuffer = Buffer.from(privateKeyBase64, "base64");

        const privateKey = crypto.createPrivateKey({
            key: privateKeyBuffer,
            format: "der",
            type: "pkcs8",
        });

        const sign = crypto.createSign("SHA256");
        sign.update(os.userInfo().username);
        sign.end();

        const signature = sign.sign(privateKey);
        return signature.toString("base64");
    }

    private async enableUnsafeMode(xxmiPath: string, importerKey: string) {
        try {
            const configPath = path.join(xxmiPath, "XXMI Launcher Config.json");

            if (!(await fse.pathExists(configPath))) {
                this.desktop.logger.warn(
                    `Config file not found at ${configPath}`,
                    "DllBuilder:enableUnsafeMode",
                );
                return;
            }

            this.desktop.logger.info(
                `configPath found: ${configPath}`,
                "DllBuilder:enableUnsafeMode",
            );

            const config = await fse.readJson(configPath);

            const importerConfig = config?.Importers?.[importerKey]?.Migoto;
            if (importerConfig) {
                if (importerConfig.unsafe_mode === false) {
                    this.updateProgress("XXMI_ENABLE_UNSAFE_MODE");

                    importerConfig.unsafe_mode = true;
                    importerConfig.unsafe_mode_signature =
                        await this.generateUnsafeModeSignature(xxmiPath);

                    await fse.writeJson(configPath, config, { spaces: 4 });

                    this.desktop.logger.info(
                        `Enabled unsafe_mode for ${importerKey}`,
                        "DllBuilder:enableUnsafeMode",
                    );
                }
            }
        } catch (error) {
            this.desktop.logger.error(
                `Failed to update config for ${importerKey}: ${error}`,
                "DllBuilder:enableUnsafeMode",
            );
        }
    }
}

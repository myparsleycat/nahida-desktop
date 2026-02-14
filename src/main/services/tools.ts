import { exec } from "node:child_process";
import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { promisify } from "node:util";
import { setting } from "@main/internal/db/schema";
import { getAgent } from "@main/internal/fetcher";
import fse from "fs-extra";
import ky from "ky";
import type { NahidaDesktop } from "..";

const execAsync = promisify(exec);

export class Tools {
    private readonly desktop: NahidaDesktop;
    private readonly VS_EDITIONS = ["Community", "Professional", "Enterprise"];
    private readonly VS_VERSIONS = ["2025", "2022", "18", "17"];

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async getGIMIPath() {
        const result = await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, "savedGimiPath"),
        });

        return result?.value;
    }

    public async saveGIMIPath(gimiPath: string) {
        await this.desktop.lib.db
            .insert(setting)
            .values({
                key: "savedGimiPath",
                value: gimiPath,
            })
            .onConflictDoUpdate({
                target: setting.key,
                set: {
                    value: gimiPath,
                },
            });
    }

    public async buildNewD3DDLL({ gimiPath }: { gimiPath?: string }) {
        const targetDir = this.resolveTargetDirectory(gimiPath);
        if (!(await fse.pathExists(targetDir))) {
            this.desktop.ipc.broadcast("tools:progress", "GIMI 경로를 찾을 수 없습니다");
            return false;
        }

        this.desktop.ipc.broadcast("tools:progress", "Visual Studio 를 찾는중");
        const vcvarsPath = await this.findVsDevCmd();
        if (!vcvarsPath) {
            this.desktop.ipc.broadcast("tools:progress", "Visual Studio를 찾을 수 없습니다");
            return false;
        }

        const tempDir = path.join(os.tmpdir(), "nahida-tools-d3d-build");
        await fse.ensureDir(tempDir);
        await fse.emptyDir(tempDir);

        try {
            const projectPath = await this.prepareSourceCode(tempDir);

            this.desktop.ipc.broadcast("tools:progress", "빌드 중");
            this.desktop.logger.info("Building D3D11 DLL...", "Tools:buildNewD3DDLL");

            const buildSuccess = await this.executeMsBuild(vcvarsPath, projectPath);
            if (!buildSuccess) {
                return false;
            }

            const builtDllPath = path.join(projectPath, "x64", "Release", "d3d11.dll");
            if (!(await fse.pathExists(builtDllPath))) {
                this.desktop.ipc.broadcast(
                    "tools:progress",
                    "빌드 성공, 하지만 d3d11.dll 을 찾을 수 없습니다.",
                );
                return false;
            }

            const finalDestination = path.join(targetDir, "d3d11.dll");
            await fse.copy(builtDllPath, finalDestination, { overwrite: true });

            await this.enableUnsafeMode(targetDir);

            this.desktop.ipc.broadcast("tools:progress", "완료됨");
            this.desktop.logger.info(
                `Successfully built and installed d3d11.dll to ${finalDestination}`,
                "Tools:buildNewD3DDLL",
            );

            return true;
        } catch (error) {
            this.desktop.logger.error(error, "Tools:buildNewD3DDLL");
            this.desktop.ipc.broadcast("tools:progress", (error as Error).message);
            return false;
        } finally {
            await fse.remove(tempDir).catch(() => {});
        }
    }

    private resolveTargetDirectory(inputPath?: string): string {
        if (inputPath) {
            return inputPath;
        }
        return path.join(os.homedir(), "AppData", "Roaming", "XXMI Launcher", "GIMI");
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

    private async prepareSourceCode(workDir: string): Promise<string> {
        this.desktop.ipc.broadcast("tools:progress", "XXMI 리포지토리 다운로드 중");
        this.desktop.logger.info("Downloading XXMI Repo...", "Tools:prepareSourceCode");

        const zipPath = await this.downloadXXMIRepo(workDir);

        this.desktop.ipc.broadcast("tools:progress", "압축 해제 중");
        this.desktop.logger.info("Extracting Repo...", "Tools:prepareSourceCode");

        const extractDir = await this.desktop.service.archive.extract(zipPath, workDir);

        const entries = await fse.readdir(extractDir);
        const repoDirName = entries.find((e) => e.startsWith("XXMI-Libs-Package"));

        return repoDirName ? path.join(extractDir, repoDirName) : extractDir;
    }

    private async downloadXXMIRepo(targetDir: string): Promise<string> {
        const url = "https://github.com/SpectrumQT/XXMI-Libs-Package/archive/refs/heads/master.zip";
        const zipPath = path.join(targetDir, "repo.zip");

        const resp = await ky.get(url, {
            headers: {
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                Referer: "https://github.com/SpectrumQT/XXMI-Libs-Package",
            },
            // @ts-expect-error
            dispatcher: await getAgent(),
        });

        if (!resp.ok) {
            throw new Error(`Failed to download repo: ${resp.statusText}`);
        }

        await pipeline(resp.body as ReadableStream, fse.createWriteStream(zipPath));

        return zipPath;
    }

    private async executeMsBuild(vcvarsPath: string, projectPath: string): Promise<boolean> {
        const buildCommand = `"${vcvarsPath}" && cd /d "${projectPath}" && msbuild StereovisionHacks.sln /p:Configuration=Release /p:Platform=x64`;

        try {
            await execAsync(buildCommand);
            return true;
        } catch (e) {
            throw new Error(`Build failed: ${(e as Error).message}`);
        }
    }

    private async generateUnsafeModeSignature(gimiDir: string) {
        const privateKeyPath = path.join(gimiDir, "..", "Resources", "Security", "private_key.der");
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

    private async enableUnsafeMode(gimiDir: string) {
        try {
            const configPath = path.join(gimiDir, "..", "XXMI Launcher Config.json");

            if (!(await fse.pathExists(configPath))) {
                this.desktop.logger.warn(
                    `Config file not found at ${configPath}`,
                    "Tools:enableUnsafeMode",
                );
                return;
            }

            this.desktop.logger.info(`configPath found: ${configPath}`, "Tools:enableUnsafeMode");

            const config = await fse.readJson(configPath);

            if (config?.Importers?.GIMI?.Migoto) {
                if (config.Importers.GIMI.Migoto.unsafe_mode === false) {
                    this.desktop.ipc.broadcast(
                        "tools:progress",
                        "런처 설정 업데이트 (unsafe_mode 활성화)",
                    );

                    config.Importers.GIMI.Migoto.unsafe_mode = true;
                    config.Importers.GIMI.Migoto.unsafe_mode_signature =
                        await this.generateUnsafeModeSignature(gimiDir);

                    await fse.writeJson(configPath, config, { spaces: 4 });

                    this.desktop.logger.info("Enabled unsafe_mode", "Tools:enableUnsafeMode");
                }
            }
        } catch (error) {
            this.desktop.logger.error(
                `Failed to update config: ${error}`,
                "Tools:enableUnsafeMode",
            );
        }
    }
}

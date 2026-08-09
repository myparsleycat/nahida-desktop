import crypto from "node:crypto";
import path from "node:path";

import type {
    GitHubRateState,
    WuwaBackupGroup,
    WuwaBackupSize,
    WuwaFixerOptions,
    WuwaFixerPrepareResult,
    WuwaFixerStatus,
} from "@shared/types";
import { app, Notification } from "electron";
import fg from "fast-glob";
import fse from "fs-extra";

import type { NahidaDesktop } from "@/main";

const BAK_RE = /^(.*)_(\d{4}-\d{2}-\d{2} \d{2}-\d{2}-\d{2}(?:\.\d{3})?)\.BAK$/i;

const WUWA_RELEASES_LATEST_URL =
    "https://api.github.com/repos/Moonholder/Wuwa_Mod_Fixer/releases/latest";
const WUWA_CONFIG_URL =
    "https://raw.githubusercontent.com/Moonholder/Wuwa_Mod_Fixer/refs/heads/main/config.json";
const WUWA_FIXER_DIR_NAME = "wuwa-mod-fixer";
const WUWA_CHECK_COOLDOWN_MS = 2 * 60 * 1000;
const WUWA_AUTO_UPDATE_INTERVAL_MS = 60 * 60 * 1000;
const LAST_CHECK_KEY = "github:wuwa-mod-fixer:last-check";
const INSTALLED_VERSION_KEY = "mod_tools:wuwa-mod-fixer:installed-version";
const BINARY_PATH_KEY = "mod_tools:wuwa-mod-fixer:binary-path";
const LATEST_RELEASE_KEY = "mod_tools:wuwa-mod-fixer:latest-release";

type WuwaLatestAsset = {
    name: string;
    browserDownloadUrl: string;
    digest: string | null;
};

type WuwaLatestReleaseCache = {
    version: string;
    asset: WuwaLatestAsset;
    checkedAt: string;
};

type WuwaReleaseResponse = {
    tag_name?: string;
    assets?: Array<{
        name?: string;
        browser_download_url?: string;
        digest?: string | null;
    }>;
};

export class WuwaModFixer {
    private autoUpdateInterval: ReturnType<typeof setInterval> | undefined;

    constructor(private readonly desktop: NahidaDesktop) {}

    public async getRateStatus() {
        return await this.desktop.githubRate.getRateState();
    }

    public startAutoUpdateCheck(): void {
        this.stopAutoUpdateCheck();
        void this.runAutomaticUpdateCheck();
        this.autoUpdateInterval = setInterval(
            () => void this.runAutomaticUpdateCheck(),
            WUWA_AUTO_UPDATE_INTERVAL_MS,
        );
    }

    public stopAutoUpdateCheck(): void {
        clearInterval(this.autoUpdateInterval);
        this.autoUpdateInterval = undefined;
    }

    private async runAutomaticUpdateCheck(): Promise<void> {
        try {
            const installed = await this.getInstalledBinaryInfo();
            if (!installed.exists) return;

            const remoteResult = await this.refreshLatestReleaseIfNeeded({ force: true });
            const latestVersion = remoteResult.latestRelease?.version;
            if (
                !latestVersion ||
                !installed.version ||
                this.compareVersions(latestVersion, installed.version) <= 0
            ) {
                return;
            }

            await this.installOrUpdate();

            const notify = await this.desktop.setting.get("tools.wuwaFixerUpdateNotification");
            if (!notify) return;

            const description = `v${installed.version} → v${latestVersion}`;
            this.desktop.ipc.broadcast("fn:toast", "Wuwa Mod Fixer updated", { description });
            new Notification({
                title: "Wuwa Mod Fixer updated",
                body: description,
            }).show();
        } catch (error) {
            this.desktop.logger.warn(
                `Automatic Wuwa Mod Fixer update check failed: ${String(error)}`,
                "WuwaModFixer",
            );
        }
    }

    public async getStatus(importer: string | null = null): Promise<WuwaFixerStatus> {
        const installed = await this.getInstalledBinaryInfo();
        const rateState = await this.desktop.githubRate.getRateState();
        const latestRelease = await this.getCachedLatestRelease();
        const latestVersion = latestRelease?.version ?? null;
        const updateAvailable =
            !!installed.version &&
            !!latestVersion &&
            this.compareVersions(latestVersion, installed.version) > 0;

        return {
            supported: this.isSupportedImporter(importer),
            installed: installed.exists,
            installedVersion: installed.version,
            latestVersion,
            binaryPath: installed.binaryPath,
            updateAvailable,
            rateState,
            rateLimited: this.desktop.githubRate.isRateLimited(rateState),
            nextCheckAt: latestRelease
                ? new Date(
                      new Date(latestRelease.checkedAt).getTime() + WUWA_CHECK_COOLDOWN_MS,
                  ).toISOString()
                : null,
        };
    }

    public async prepareRun(importer: string | null = null): Promise<WuwaFixerPrepareResult> {
        const supported = this.isSupportedImporter(importer);
        const installed = await this.getInstalledBinaryInfo();
        const baseStatus = await this.getStatus(importer);

        if (!supported) {
            return {
                ...baseStatus,
                supported,
                checkedRemotely: false,
                needsInstall: !installed.exists,
            };
        }

        let remoteResult: Awaited<ReturnType<typeof this.refreshLatestReleaseIfNeeded>>;
        try {
            remoteResult = await this.refreshLatestReleaseIfNeeded();
        } catch (error) {
            if (!installed.exists) {
                throw error;
            }

            this.desktop.logger.warn(
                `Failed to refresh Wuwa Mod Fixer release info before run: ${String(error)}`,
                "WuwaModFixer",
            );

            return {
                ...baseStatus,
                supported,
                installed: installed.exists,
                installedVersion: installed.version,
                binaryPath: installed.binaryPath,
                checkedRemotely: false,
                needsInstall: false,
            };
        }

        const latestVersion = remoteResult.latestRelease?.version ?? baseStatus.latestVersion;
        const updateAvailable =
            !!installed.version &&
            !!latestVersion &&
            this.compareVersions(latestVersion, installed.version) > 0;

        return {
            supported,
            installed: installed.exists,
            installedVersion: installed.version,
            latestVersion,
            binaryPath: installed.binaryPath,
            updateAvailable,
            rateState: remoteResult.rateState ?? baseStatus.rateState,
            rateLimited: remoteResult.rateLimited,
            nextCheckAt: remoteResult.nextCheckAt,
            checkedRemotely: remoteResult.checkedRemotely,
            needsInstall: !installed.exists,
        };
    }

    public async installOrUpdate(): Promise<WuwaFixerStatus> {
        const release = await this.getLatestReleaseForInstall();
        const targetDir = this.getToolDir();
        await fse.ensureDir(targetDir);

        const fileName = release.asset.name;
        const tempPath = path.join(targetDir, `${fileName}.download`);
        const finalPath = path.join(targetDir, fileName);

        const response = await this.desktop.httpService.fetcher(release.asset.browserDownloadUrl, {
            method: "GET",
        });
        await this.desktop.githubRate.captureFromResponse(response);

        if (!response.ok) {
            throw new Error(`Failed to download Wuwa Mod Fixer: HTTP ${response.status}`);
        }

        const buffer = Buffer.from(await response.arrayBuffer());
        await this.verifyDigest(buffer, release.asset.digest);
        await fse.writeFile(tempPath, buffer);
        await fse.move(tempPath, finalPath, { overwrite: true });
        await this.cleanupOldBinaries(finalPath);
        await this.setAppState(INSTALLED_VERSION_KEY, release.version);
        await this.setAppState(BINARY_PATH_KEY, finalPath);

        return await this.getStatus("WWMI");
    }

    public async run(modPath: string, options: WuwaFixerOptions) {
        const installed = await this.getInstalledBinaryInfo();
        if (!installed.exists || !installed.binaryPath) {
            throw new Error("Wuwa Mod Fixer is not installed");
        }
        if (!(await fse.pathExists(modPath))) {
            throw new Error("Destination path does not exist");
        }

        const configPath = await this.ensureLatestConfig();

        const args = this.buildCliArgs(modPath, configPath, options);
        await this.desktop.service.modTools.fixTool.runExternalTool({
            displayName: "Wuwa Mod Fixer",
            filePath: installed.binaryPath,
            type: "exec",
            cwd: modPath,
            args,
            windowsExecutionMode: "direct",
        });
    }

    public async scanBackups(modPath: string): Promise<WuwaBackupGroup[]> {
        await this.requireModPath(modPath, "wuwaFixer:scanBackups");

        try {
            return await this.collectBackupGroups(modPath);
        } catch (error) {
            this.desktop.logger.error(error, `wuwaFixer:scanBackups:${modPath}`);
            throw error;
        }
    }

    public async getBackupSize(modPath: string): Promise<WuwaBackupSize> {
        await this.requireModPath(modPath, "wuwaFixer:getBackupSize");

        try {
            const bakPaths = await this.listBakFiles(modPath);
            let bytes = 0;

            for (const bakPath of bakPaths) {
                const stats = await fse.stat(bakPath).catch(() => null);
                if (!stats) {
                    continue;
                }

                if (stats.size > 0) {
                    bytes += stats.size;
                    continue;
                }

                const match = BAK_RE.exec(path.basename(bakPath));
                if (!match) {
                    continue;
                }

                const originalStats = await fse
                    .stat(path.join(path.dirname(bakPath), match[1]))
                    .catch(() => null);
                if (originalStats) {
                    bytes += originalStats.size;
                }
            }

            return { bytes, count: bakPaths.length };
        } catch (error) {
            this.desktop.logger.error(error, `wuwaFixer:getBackupSize:${modPath}`);
            throw error;
        }
    }

    public async rollbackToGroup(modPath: string, groupKey: string) {
        await this.requireModPath(modPath, "wuwaFixer:rollbackToGroup");

        try {
            const groups = await this.collectBackupGroups(modPath);
            const targetExists = groups.some((group) => group.groupKey === groupKey);
            if (!targetExists) {
                throw new Error(`Backup group not found: ${groupKey}`);
            }

            const candidates = groups
                .filter((group) => group.groupKey >= groupKey)
                .flatMap((group) => group.files);
            const filesToDelete = candidates.map((file) => file.currentPath);
            const earliestByOriginal = new Map<string, (typeof candidates)[number]>();

            for (const file of candidates) {
                const existing = earliestByOriginal.get(file.originalPath);
                if (!existing || file.timestamp < existing.timestamp) {
                    earliestByOriginal.set(file.originalPath, file);
                }
            }

            for (const [originalPath, bak] of earliestByOriginal) {
                const bakStats = await fse.stat(bak.currentPath).catch(() => null);
                if (bakStats && bakStats.size === 0) {
                    if (await fse.pathExists(originalPath)) {
                        await fse.remove(originalPath);
                    }
                    continue;
                }

                await fse.copy(bak.currentPath, originalPath, { overwrite: true });
            }

            await Promise.all(filesToDelete.map((bakPath) => fse.remove(bakPath).catch(() => {})));
        } catch (error) {
            this.desktop.logger.error(error, `wuwaFixer:rollbackToGroup:${modPath}:${groupKey}`);
            throw error;
        }
    }

    public async cleanBackups(modPath: string) {
        await this.requireModPath(modPath, "wuwaFixer:cleanBackups");

        try {
            const bakPaths = await this.listBakFiles(modPath);
            await Promise.all(bakPaths.map((bakPath) => fse.remove(bakPath)));
        } catch (error) {
            this.desktop.logger.error(error, `wuwaFixer:cleanBackups:${modPath}`);
            throw error;
        }
    }

    private buildCliArgs(modPath: string, configPath: string, options: WuwaFixerOptions) {
        if (options.derivedHashes && options.stableTexture) {
            throw new Error("Derived hashes and stable texture cannot be enabled together");
        }

        const args = ["--cli", "--path", modPath, "--config", configPath];

        if (options.derivedHashes) {
            args.push("--derived-hashes");
        }
        if (options.stableTexture) {
            args.push("--stable-texture");
        }
        if (options.aemeathMech) {
            args.push("--aemeath-mech");
        }
        if (options.rendering33) {
            args.push("--rendering-33");
        }
        if (options.aeroFix !== "none") {
            args.push("--aero-fix", options.aeroFix);
        }
        return args;
    }

    private async requireModPath(modPath: string, action: string) {
        if (!(await fse.pathExists(modPath))) {
            const error = new Error("Destination path does not exist");
            this.desktop.logger.error(error, `${action}:${modPath}`);
            throw error;
        }
    }

    private async listBakFiles(modPath: string) {
        return await fg("**/*.BAK", {
            cwd: modPath,
            absolute: true,
            onlyFiles: true,
            caseSensitiveMatch: false,
            dot: true,
            suppressErrors: true,
        });
    }

    private async collectBackupGroups(modPath: string): Promise<WuwaBackupGroup[]> {
        const bakPaths = await this.listBakFiles(modPath);
        const filesByGroup = new Map<string, WuwaBackupGroup["files"]>();

        for (const bakPath of bakPaths) {
            const match = BAK_RE.exec(path.basename(bakPath));
            if (!match) {
                continue;
            }

            const originalName = match[1];
            const fullTimestamp = match[2];
            const groupKey = fullTimestamp.slice(0, 16);
            const files = filesByGroup.get(groupKey) ?? [];
            files.push({
                currentPath: bakPath,
                originalPath: path.join(path.dirname(bakPath), originalName),
                timestamp: fullTimestamp,
                groupKey,
            });
            filesByGroup.set(groupKey, files);
        }

        return [...filesByGroup.entries()]
            .map(([groupKey, files]) => ({ groupKey, files }))
            .sort((left, right) => right.groupKey.localeCompare(left.groupKey));
    }

    private async ensureLatestConfig() {
        const toolDir = this.getToolDir();
        const configPath = path.join(toolDir, "config.json");
        const tempPath = path.join(toolDir, "config.json.download");

        await fse.ensureDir(toolDir);

        const response = await this.desktop.httpService.fetcher(WUWA_CONFIG_URL, {
            method: "GET",
        });

        if (!response.ok) {
            throw new Error(`Failed to download Wuwa Mod Fixer config: HTTP ${response.status}`);
        }

        const buffer = Buffer.from(await response.arrayBuffer());
        await fse.writeFile(tempPath, buffer);
        await fse.move(tempPath, configPath, { overwrite: true });
        return configPath;
    }

    private async getLatestReleaseForInstall(): Promise<WuwaLatestReleaseCache> {
        const refreshed = await this.refreshLatestReleaseIfNeeded({ force: true });
        if (refreshed.latestRelease) {
            return refreshed.latestRelease;
        }

        const cached = await this.getCachedLatestRelease();
        if (cached) {
            return cached;
        }

        throw new Error("Unable to fetch the latest Wuwa Mod Fixer release");
    }

    private async refreshLatestReleaseIfNeeded(options?: { force?: boolean }): Promise<{
        latestRelease: WuwaLatestReleaseCache | null;
        rateState: GitHubRateState | null;
        rateLimited: boolean;
        checkedRemotely: boolean;
        nextCheckAt: string | null;
    }> {
        const cachedRelease = await this.getCachedLatestRelease();
        const lastCheckAt = await this.getLastCheckAt();
        const cachedRateState = await this.desktop.githubRate.getRateState();
        const withinCooldown =
            !!lastCheckAt && Date.now() - new Date(lastCheckAt).getTime() < WUWA_CHECK_COOLDOWN_MS;

        if (!options?.force && cachedRelease && withinCooldown) {
            return {
                latestRelease: cachedRelease,
                rateState: cachedRateState,
                rateLimited: this.desktop.githubRate.isRateLimited(cachedRateState),
                checkedRemotely: false,
                nextCheckAt: new Date(
                    new Date(lastCheckAt).getTime() + WUWA_CHECK_COOLDOWN_MS,
                ).toISOString(),
            };
        }

        let rateState = cachedRateState;
        const rateCheck = await this.desktop.githubRate.canUseGitHubApi({
            refreshIfMissing: !rateState,
        });
        rateState = rateCheck.rateState;

        if (!rateCheck.allowed) {
            return {
                latestRelease: cachedRelease,
                rateState,
                rateLimited: true,
                checkedRemotely: false,
                nextCheckAt: rateState ? new Date(rateState.reset * 1000).toISOString() : null,
            };
        }

        const response = await this.desktop.httpService.fetcher(WUWA_RELEASES_LATEST_URL, {
            method: "GET",
            headers: {
                Accept: "application/vnd.github+json",
            },
        });
        rateState = await this.desktop.githubRate.captureFromResponse(response);

        if (!response.ok) {
            throw new Error(`Failed to fetch Wuwa Mod Fixer release: HTTP ${response.status}`);
        }

        const payload = (await response.json()) as WuwaReleaseResponse;
        const latestRelease = this.parseLatestRelease(payload);
        await this.setAppState(LAST_CHECK_KEY, new Date().toISOString());
        await this.setAppState(LATEST_RELEASE_KEY, JSON.stringify(latestRelease));

        return {
            latestRelease,
            rateState,
            rateLimited: this.desktop.githubRate.isRateLimited(rateState),
            checkedRemotely: true,
            nextCheckAt: new Date(Date.now() + WUWA_CHECK_COOLDOWN_MS).toISOString(),
        };
    }

    private parseLatestRelease(payload: WuwaReleaseResponse): WuwaLatestReleaseCache {
        const version = typeof payload.tag_name === "string" ? payload.tag_name : null;
        if (!version) {
            throw new Error("Latest Wuwa Mod Fixer release is missing tag_name");
        }

        const asset = payload.assets?.find((candidate) => {
            if (!candidate.name || !candidate.browser_download_url) {
                return false;
            }

            return /^Wuwa_Mod_Fixer_v.+\.exe$/i.test(candidate.name);
        });

        if (!asset?.name || !asset.browser_download_url) {
            throw new Error("Latest Wuwa Mod Fixer release is missing a Windows executable asset");
        }

        return {
            version,
            asset: {
                name: asset.name,
                browserDownloadUrl: asset.browser_download_url,
                digest: asset.digest ?? null,
            },
            checkedAt: new Date().toISOString(),
        };
    }

    private async cleanupOldBinaries(currentBinaryPath: string) {
        const toolDir = this.getToolDir();
        const files = await fse.readdir(toolDir).catch(() => []);

        await Promise.all(
            files.map(async (file) => {
                const filePath = path.join(toolDir, file);
                if (filePath === currentBinaryPath) {
                    return;
                }

                if (/^Wuwa_Mod_Fixer_v.+\.exe$/i.test(file)) {
                    await fse.remove(filePath);
                }
            }),
        );
    }

    private async verifyDigest(buffer: Buffer, digest: string | null) {
        if (!digest) {
            return;
        }

        const [algorithm, expected] = digest.split(":");
        if (!algorithm || !expected || algorithm.toLowerCase() !== "sha256") {
            throw new Error("Unsupported Wuwa Mod Fixer digest format");
        }

        const actual = crypto.createHash("sha256").update(buffer).digest("hex");
        if (actual.toLowerCase() !== expected.toLowerCase()) {
            throw new Error("Wuwa Mod Fixer download digest mismatch");
        }
    }

    private async getInstalledBinaryInfo(): Promise<{
        exists: boolean;
        binaryPath: string | null;
        version: string | null;
    }> {
        const binaryPath = await this.getAppState(BINARY_PATH_KEY);
        const storedVersion = await this.getAppState(INSTALLED_VERSION_KEY);

        if (binaryPath && (await fse.pathExists(binaryPath))) {
            return {
                exists: true,
                binaryPath,
                version:
                    storedVersion ?? this.extractVersionFromFileName(path.basename(binaryPath)),
            };
        }

        const toolDir = this.getToolDir();
        const files = await fse.readdir(toolDir).catch(() => []);
        const match = files.find((file) => /^Wuwa_Mod_Fixer_v.+\.exe$/i.test(file));

        if (!match) {
            return {
                exists: false,
                binaryPath: null,
                version: null,
            };
        }

        const resolvedBinaryPath = path.join(toolDir, match);
        const version = this.extractVersionFromFileName(match);
        await this.setAppState(BINARY_PATH_KEY, resolvedBinaryPath);
        if (version) {
            await this.setAppState(INSTALLED_VERSION_KEY, version);
        }

        return {
            exists: true,
            binaryPath: resolvedBinaryPath,
            version,
        };
    }

    private getToolDir() {
        return path.join(app.getPath("userData"), "tools", WUWA_FIXER_DIR_NAME);
    }

    private async getLastCheckAt() {
        return await this.getAppState(LAST_CHECK_KEY);
    }

    private async getCachedLatestRelease(): Promise<WuwaLatestReleaseCache | null> {
        const raw = await this.getAppState(LATEST_RELEASE_KEY);
        if (!raw) {
            return null;
        }

        try {
            return JSON.parse(raw) as WuwaLatestReleaseCache;
        } catch {
            return null;
        }
    }

    private async getAppState(key: string) {
        return await this.desktop.lib.db.appState.getValue(key);
    }

    private async setAppState(key: string, value: string) {
        await this.desktop.lib.db.appState.upsert(key, value, new Date().toISOString());
    }

    private isSupportedImporter(importer: string | null) {
        return importer === null || importer.toUpperCase() === "WWMI";
    }

    private extractVersionFromFileName(fileName: string) {
        const match = fileName.match(/^Wuwa_Mod_Fixer_(v[\d.]+)\.exe$/i);
        return match?.[1] ?? null;
    }

    private compareVersions(left: string, right: string) {
        const leftParts = left
            .replace(/^v/i, "")
            .split(".")
            .map((part) => Number(part));
        const rightParts = right
            .replace(/^v/i, "")
            .split(".")
            .map((part) => Number(part));
        const maxLength = Math.max(leftParts.length, rightParts.length);

        for (let index = 0; index < maxLength; index += 1) {
            const leftValue = leftParts[index] ?? 0;
            const rightValue = rightParts[index] ?? 0;

            if (leftValue > rightValue) {
                return 1;
            }
            if (leftValue < rightValue) {
                return -1;
            }
        }

        return 0;
    }
}

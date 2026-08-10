import path from "node:path";

import { formatDate } from "@shared/utils";
import { retry } from "es-toolkit";
import fse from "fs-extra";

import type { NahidaDesktop } from "@/main";

import {
    createEmptyTogglePersistProfile,
    fingerprintTogglePersistIni,
    parseTogglePersistProfile,
    TogglePersistLearner,
    type TogglePersistLearnedVariable,
    type TogglePersistProfile,
    togglePersistProfilePath,
} from "./toggle-persist-learning";

export class TogglePersist {
    private persistWatchers: string[] = [];
    private cachedD3dxUserIni: Map<string, Record<string, string>> = new Map();
    private persistLogs: string[] = [];
    private persistLearner = new TogglePersistLearner();
    private persistFlushTimers: Map<string, ReturnType<typeof setTimeout>> = new Map();
    private persistFileUpdateLocks: Map<string, Promise<unknown>> = new Map();
    private persistProfileUpdateLocks: Map<string, Promise<unknown>> = new Map();
    private persistProfileLoaders: Map<string, Promise<void>> = new Map();
    private persistProfileErrorsLogged: Set<string> = new Set();
    private d3dxUserIniChangeLocks: Map<string, Promise<void>> = new Map();
    private persistRevisions: Map<string, number> = new Map();
    private persistGeneration = 0;

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startPersistWatcher() {
        if (!this.desktop.service.xxmi) {
            await this.stopPersistWatcher();
            return;
        }

        const enabled = await this.desktop.setting.xxmi.getPersistToggles();
        if (!enabled) {
            await this.stopPersistWatcher();
            return;
        }

        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();

        await this.stopPersistWatcher();

        if (!xxmiPath || !xxmiConfig) return;

        const generation = ++this.persistGeneration;

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        for (const importer of importers) {
            const d3dxPath = path.join(importer.importerFolder, "d3dx_user.ini");
            if (await fse.pathExists(d3dxPath)) {
                const content = await fse.readFile(d3dxPath, "utf-8");
                this.cachedD3dxUserIni.set(importer.key, this.parseD3dxUserIni(content));

                const watcherId = await this.desktop.lib.watcher.create(
                    d3dxPath,
                    { compareContents: true },
                    (eventName, changedPath) => {
                        if (eventName === "modify") {
                            this.queueD3dxUserIniChange(importer, changedPath, generation);
                        }
                    },
                );
                this.persistWatchers.push(watcherId);
                this.logInfo(`Started watching ${d3dxPath} for persist updates`);
            }
        }
    }

    public async stopPersistWatcher() {
        this.persistGeneration++;
        const watcherCount = this.persistWatchers.length;
        for (const id of this.persistWatchers) {
            await this.desktop.lib.watcher.remove(id);
        }
        this.persistFlushTimers.forEach((timer) => clearTimeout(timer));
        this.persistWatchers = [];
        this.cachedD3dxUserIni.clear();
        this.persistLearner.clear();
        this.persistFlushTimers.clear();
        this.persistProfileLoaders.clear();
        this.persistProfileErrorsLogged.clear();
        this.d3dxUserIniChangeLocks.clear();
        this.persistRevisions.clear();
        if (watcherCount > 0) {
            this.logInfo(`Stopped persist watcher (${watcherCount})`);
        }
    }

    public getPersistLogs() {
        return [...this.persistLogs];
    }

    public async persistStateToIni(
        targetIniPath: string,
        state: Record<string, string | number>,
    ): Promise<{ updatedVariables: string[] }> {
        const updates = new Map<string, string>();

        for (const [varName, rawValue] of Object.entries(state)) {
            updates.set(varName.toLowerCase(), String(rawValue));
        }

        if (updates.size === 0) {
            return { updatedVariables: [] };
        }

        const updatedVariables = await this.withPersistFileLock(targetIniPath, () =>
            this.applyPersistUpdates(targetIniPath, updates),
        );
        return { updatedVariables };
    }

    private parseD3dxUserIni(content: string): Record<string, string> {
        const result: Record<string, string> = {};
        const lines = content.split(/\r?\n/);
        let inConstants = false;

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith(";")) continue;

            if (trimmed.startsWith("[")) {
                inConstants = /^\[Constants\]$/i.test(trimmed);
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

    private queueD3dxUserIniChange(
        importer: { key: string; importerFolder: string },
        iniPath: string,
        generation: number,
    ) {
        const lockKey = `${importer.key}:${iniPath.toLowerCase()}`;
        const previous = this.d3dxUserIniChangeLocks.get(lockKey) ?? Promise.resolve();
        const next = previous
            .catch(() => {})
            .then(async () => {
                if (!this.isActivePersistGeneration(generation)) return;
                await this.handleD3dxUserIniChange(importer, iniPath, generation);
            });

        this.d3dxUserIniChangeLocks.set(lockKey, next);

        void next
            .catch((error) => {
                this.logError(`Error handling queued d3dx_user.ini change: ${String(error)}`);
            })
            .finally(() => {
                if (this.d3dxUserIniChangeLocks.get(lockKey) === next) {
                    this.d3dxUserIniChangeLocks.delete(lockKey);
                }
            });
    }

    private async handleD3dxUserIniChange(
        importer: { key: string; importerFolder: string },
        iniPath: string,
        generation: number,
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

            if (!this.isActivePersistGeneration(generation)) return;

            const newParsed = this.parseD3dxUserIni(content);
            const oldParsed = this.cachedD3dxUserIni.get(importer.key) || {};
            const revision = (this.persistRevisions.get(importer.key) ?? 0) + 1;
            const observedAt = Date.now();
            const suppressedByFile = new Map<
                string,
                { targetIniPath: string; variables: Set<string> }
            >();
            const learnedByFile = new Map<
                string,
                { targetIniPath: string; variables: Map<string, TogglePersistLearnedVariable> }
            >();
            this.persistRevisions.set(importer.key, revision);

            for (const [key, newValue] of Object.entries(newParsed)) {
                if (!this.isActivePersistGeneration(generation)) return;

                const oldValue = oldParsed[key];
                if (newValue !== oldValue) {
                    const target = await this.resolvePersistTarget(importer.importerFolder, key);
                    if (target) {
                        await this.loadPersistProfile(target.targetIniPath, generation);
                        if (!this.isActivePersistGeneration(generation)) return;
                        const result = this.persistLearner.observe({
                            targetIniPath: target.targetIniPath,
                            varName: target.varName,
                            value: newValue,
                            revision,
                            at: observedAt,
                        });
                        this.schedulePersistFlush(
                            target.targetIniPath,
                            result.nextDueAt,
                            generation,
                        );

                        if (result.newlySuppressed.length > 0) {
                            const fileKey = target.targetIniPath.toLowerCase();
                            const pending = suppressedByFile.get(fileKey) ?? {
                                targetIniPath: target.targetIniPath,
                                variables: new Set<string>(),
                            };
                            result.newlySuppressed.forEach((name) => pending.variables.add(name));
                            suppressedByFile.set(fileKey, pending);
                        }

                        if (result.newlyLearned.length > 0) {
                            const fileKey = target.targetIniPath.toLowerCase();
                            const pending = learnedByFile.get(fileKey) ?? {
                                targetIniPath: target.targetIniPath,
                                variables: new Map<string, TogglePersistLearnedVariable>(),
                            };
                            result.newlyLearned.forEach((variable) =>
                                pending.variables.set(variable.name.toLowerCase(), variable),
                            );
                            learnedByFile.set(fileKey, pending);
                        }
                    }
                }
            }

            if (!this.isActivePersistGeneration(generation)) return;
            this.cachedD3dxUserIni.set(importer.key, newParsed);

            suppressedByFile.forEach((entry) => {
                this.logInfo(
                    `Suppressed continuously changing persist variables ${[...entry.variables]
                        .map((name) => `$${name}`)
                        .join(", ")} in ${entry.targetIniPath}`,
                );
            });
            await Promise.all(
                [...learnedByFile.values()].map((entry) =>
                    this.savePersistProfile(
                        entry.targetIniPath,
                        [...entry.variables.values()],
                        generation,
                    ),
                ),
            );
        } catch (error) {
            this.logError(`Error handling d3dx_user.ini change: ${String(error)}`);
        }
    }

    private async resolvePersistTarget(importerFolder: string, key: string) {
        const match = key.match(/^\$\\(.+\.ini)\\([^\\]+)$/i);
        if (!match) return null;

        const importerRoot = path.resolve(importerFolder);
        const targetIniPath = path.resolve(importerRoot, match[1]);
        const relativeTargetPath = path.relative(importerRoot, targetIniPath);

        if (
            relativeTargetPath === ".." ||
            relativeTargetPath.startsWith(`..${path.sep}`) ||
            path.isAbsolute(relativeTargetPath)
        ) {
            return null;
        }

        const stat = await fse.stat(targetIniPath).catch(() => null);
        if (!stat?.isFile()) return null;

        return { targetIniPath, varName: match[2] };
    }

    private async loadPersistProfile(targetIniPath: string, generation: number) {
        const targetKey = targetIniPath.toLowerCase();
        const existing = this.persistProfileLoaders.get(targetKey);
        if (existing) {
            await existing;
            return;
        }

        const loading = this.readAndRegisterPersistProfile(targetIniPath, generation);
        this.persistProfileLoaders.set(targetKey, loading);
        await loading;
    }

    private async readAndRegisterPersistProfile(targetIniPath: string, generation: number) {
        const profilePath = togglePersistProfilePath(targetIniPath);
        try {
            if (!(await fse.pathExists(profilePath))) return;
            const [profileContent, targetContent] = await Promise.all([
                fse.readFile(profilePath, "utf-8"),
                fse.readFile(targetIniPath, "utf-8"),
            ]);
            if (!this.isActivePersistGeneration(generation)) return;
            const profile = parseTogglePersistProfile(JSON.parse(profileContent) as unknown);
            const fileName = Object.keys(profile.files).find(
                (candidate) =>
                    candidate.toLowerCase() === path.basename(targetIniPath).toLowerCase(),
            );
            if (!fileName) return;
            const file = profile.files[fileName];
            if (file.fingerprint !== fingerprintTogglePersistIni(targetContent)) return;
            this.persistLearner.registerLearnedVariables(targetIniPath, file.variables);
        } catch (error) {
            this.logPersistProfileError("load", targetIniPath, profilePath, error);
        }
    }

    private async savePersistProfile(
        targetIniPath: string,
        variables: TogglePersistLearnedVariable[],
        generation: number,
    ) {
        if (variables.length === 0 || !this.isActivePersistGeneration(generation)) return;
        const profilePath = togglePersistProfilePath(targetIniPath);
        await this.withPersistProfileLock(profilePath, async () => {
            if (!this.isActivePersistGeneration(generation)) return;
            try {
                const targetContent = await fse.readFile(targetIniPath, "utf-8");
                const profile = await this.readPersistProfileForUpdate(profilePath, targetIniPath);
                const actualFileName = path.basename(targetIniPath);
                const existingFileName = Object.keys(profile.files).find(
                    (candidate) => candidate.toLowerCase() === actualFileName.toLowerCase(),
                );
                const fingerprint = fingerprintTogglePersistIni(targetContent);
                const existingFile = existingFileName ? profile.files[existingFileName] : undefined;
                const file =
                    existingFile?.fingerprint === fingerprint
                        ? existingFile
                        : { fingerprint, variables: {} };

                variables.forEach((variable) => {
                    file.variables[variable.name.toLowerCase()] = variable;
                });
                if (existingFileName && existingFileName !== actualFileName) {
                    delete profile.files[existingFileName];
                }
                profile.files[actualFileName] = file;

                const written = await this.writeFileAtomic(
                    profilePath,
                    `${JSON.stringify(profile, null, 2)}\n`,
                    generation,
                );
                if (written) this.persistProfileLoaders.delete(targetIniPath.toLowerCase());
            } catch (error) {
                this.logPersistProfileError("save", targetIniPath, profilePath, error);
            }
        });
    }

    private async readPersistProfileForUpdate(
        profilePath: string,
        targetIniPath: string,
    ): Promise<TogglePersistProfile> {
        if (!(await fse.pathExists(profilePath))) return createEmptyTogglePersistProfile();
        try {
            return parseTogglePersistProfile(
                JSON.parse(await fse.readFile(profilePath, "utf-8")) as unknown,
            );
        } catch (error) {
            this.logPersistProfileError("parse-before-save", targetIniPath, profilePath, error);
            return createEmptyTogglePersistProfile();
        }
    }

    private async withPersistProfileLock<T>(profilePath: string, work: () => Promise<T>) {
        const lockKey = profilePath.toLowerCase();
        const previous = this.persistProfileUpdateLocks.get(lockKey) ?? Promise.resolve();
        const next = previous.catch(() => {}).then(work);
        this.persistProfileUpdateLocks.set(lockKey, next);

        try {
            return await next;
        } finally {
            if (this.persistProfileUpdateLocks.get(lockKey) === next) {
                this.persistProfileUpdateLocks.delete(lockKey);
            }
        }
    }

    private logPersistProfileError(
        stage: string,
        targetIniPath: string,
        profilePath: string,
        error: unknown,
    ) {
        const errorKey = `${stage}:${profilePath.toLowerCase()}`;
        if (this.persistProfileErrorsLogged.has(errorKey)) return;
        this.persistProfileErrorsLogged.add(errorKey);
        this.logError(
            `Error processing toggle persist profile: stage=${stage}, targetIniPath=${targetIniPath}, profilePath=${profilePath}, error=${String(error)}`,
        );
    }

    private schedulePersistFlush(
        targetIniPath: string,
        dueAt: number | undefined,
        generation: number,
    ) {
        if (!this.isActivePersistGeneration(generation)) return;

        const fileKey = targetIniPath.toLowerCase();
        const existingTimer = this.persistFlushTimers.get(fileKey);
        if (existingTimer) {
            clearTimeout(existingTimer);
            this.persistFlushTimers.delete(fileKey);
        }
        if (dueAt === undefined) return;

        this.persistFlushTimers.set(
            fileKey,
            setTimeout(
                () => {
                    this.persistFlushTimers.delete(fileKey);
                    void this.flushReadyPersistUpdates(targetIniPath, generation);
                },
                Math.max(0, dueAt - Date.now()),
            ),
        );
    }

    private async flushReadyPersistUpdates(targetIniPath: string, generation: number) {
        if (!this.isActivePersistGeneration(generation)) return;
        const ready = this.persistLearner.takeReady(targetIniPath, Date.now());
        this.schedulePersistFlush(targetIniPath, ready.nextDueAt, generation);
        if (ready.updates.size === 0) return;
        await this.enqueuePersistFileUpdate(targetIniPath, ready.updates, generation);
    }

    private async enqueuePersistFileUpdate(
        targetIniPath: string,
        updates: Map<string, string>,
        generation: number,
    ) {
        await this.withPersistFileLock(targetIniPath, async () => {
            if (!this.isActivePersistGeneration(generation)) return [];
            return await this.updateModIniPersist(targetIniPath, updates, generation);
        });
    }

    private async withPersistFileLock<T>(targetIniPath: string, work: () => Promise<T>) {
        const lockKey = targetIniPath.toLowerCase();
        const previous = this.persistFileUpdateLocks.get(lockKey) ?? Promise.resolve();
        const next = previous.catch(() => {}).then(work);
        this.persistFileUpdateLocks.set(lockKey, next);

        try {
            return await next;
        } finally {
            if (this.persistFileUpdateLocks.get(lockKey) === next) {
                this.persistFileUpdateLocks.delete(lockKey);
            }
        }
    }

    private async updateModIniPersist(
        targetIniPath: string,
        updates: Map<string, string>,
        generation: number,
    ): Promise<string[]> {
        try {
            const updatedVars = await this.applyPersistUpdates(targetIniPath, updates, generation);

            if (updatedVars.length > 0) {
                const summary =
                    updatedVars.length === 1
                        ? `Updated persist variable $${updatedVars[0]} in ${targetIniPath}`
                        : `Updated persist variables ${updatedVars
                              .map((name) => `$${name}`)
                              .join(", ")} in ${targetIniPath}`;
                this.logInfo(summary);
            }

            return updatedVars;
        } catch (error) {
            this.logError(`Error updating mod ini ${targetIniPath}: ${String(error)}`);
            return [];
        }
    }

    private async applyPersistUpdates(
        targetIniPath: string,
        updates: Map<string, string>,
        generation?: number,
    ): Promise<string[]> {
        if (!this.isActivePersistGeneration(generation)) return [];

        const content = await fse.readFile(targetIniPath, "utf-8");
        const lineEnding = content.includes("\r\n") ? "\r\n" : "\n";
        const lines = content.split(/\r?\n/);

        let inConstants = false;
        let modified = false;
        const updatedVars: string[] = [];

        for (let i = 0; i < lines.length; i++) {
            const trimmed = lines[i].trim();
            if (trimmed.startsWith("[")) {
                inConstants = /^\[Constants\]$/i.test(trimmed);
                continue;
            }

            if (inConstants && trimmed.startsWith("global persist $")) {
                const match = trimmed.match(/^global\s+persist\s+\$(.+?)\s*=\s*(.+)$/i);
                if (!match) continue;
                const existingVarName = match[1].trim();
                const varKey = existingVarName.toLowerCase();
                const nextValue = updates.get(varKey);
                if (nextValue === undefined) continue;

                const currentValue = match[2].trim();
                const trimmedNextValue = nextValue.trim();
                if (currentValue === trimmedNextValue) continue;

                lines[i] = `global persist $${existingVarName} = ${trimmedNextValue}`;
                updatedVars.push(existingVarName);
                modified = true;
            }
        }

        if (modified) {
            const written = await this.writeFileAtomic(
                targetIniPath,
                lines.join(lineEnding),
                generation,
            );
            if (!written) return [];
        }

        return updatedVars;
    }

    private async writeFileAtomic(targetIniPath: string, content: string, generation?: number) {
        const tempPath = path.join(
            path.dirname(targetIniPath),
            `.${path.basename(targetIniPath)}.${process.pid}.${Date.now()}.tmp`,
        );

        try {
            await fse.writeFile(tempPath, content, "utf-8");
            if (!this.isActivePersistGeneration(generation)) return false;
            // Sync rename keeps the generation check and commit in one turn.
            fse.renameSync(tempPath, targetIniPath);
            return true;
        } finally {
            await fse.remove(tempPath).catch(() => {});
        }
    }

    private isActivePersistGeneration(generation?: number) {
        return generation === undefined || this.persistGeneration === generation;
    }

    private addPersistLog(level: "INFO" | "ERROR", message: string) {
        const now = new Date();
        const entry = `[${formatDate(now)}] [${level}] ${message}`;
        this.persistLogs.push(entry);
        if (this.persistLogs.length > 10) {
            this.persistLogs = this.persistLogs.slice(-10);
        }
        const mainWindow = this.desktop.window.main.window;
        if (mainWindow) {
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "setting:xxmi:persistLogs",
                this.getPersistLogs(),
            );
        }
    }

    private logInfo(message: string) {
        this.desktop.logger.info(message, "TogglePersist");
        this.addPersistLog("INFO", message);
    }

    private logError(message: string) {
        this.desktop.logger.error(message, "TogglePersist");
        this.addPersistLog("ERROR", message);
    }
}

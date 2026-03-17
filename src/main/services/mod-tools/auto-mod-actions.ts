import path from "node:path";
import fg from "fast-glob";
import fse from "fs-extra";
import { debounce } from "es-toolkit";
import {
    AUTO_MOD_ACTIONS_BACKUP_PREFIX,
    type AutoModActionsRestoreResult,
    hasAnyAutoModActionsEnabled,
    isAutoModActionsImporterEnabled,
} from "@shared/auto-mod-actions";
import type { NahidaDesktop } from "@/main";

interface PendingModDirectory {
    importerKey: string;
    modsPath: string;
    modPath: string;
}

interface IniSectionRange {
    name: string;
    startLineIndex: number;
    endLineIndex: number;
}

interface ParsedBackupFile {
    backupPath: string;
    originalPath: string;
    modPath: string;
    timestamp: number;
}

const OR_FIX_RUN_LINE = "run = CommandList\\global\\ORFix\\ORFix";
const NN_FIX_RUN_LINE = "run = CommandList\\global\\ORFix\\NNFix";

export class AutoModActions {
    private static readonly suppressWindowMs = 3000;
    private watcherIds: string[] = [];
    private pendingModDirectories = new Map<string, PendingModDirectory>();
    private suppressedModDirectories = new Map<string, number>();
    private flushDebouncer: (() => void) | null = null;
    private isFlushing = false;
    private pendingFlush = false;

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startWatcher() {
        if (!this.desktop.service.xxmi) {
            return;
        }

        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();
        if (!xxmiPath || !xxmiConfig) {
            await this.stopWatcher();
            return;
        }

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const config = await this.desktop.setting.xxmi.getAutoModActionsConfig();
        if (!hasAnyAutoModActionsEnabled(config, importers.map((importer) => importer.key))) {
            await this.stopWatcher();
            return;
        }

        await this.stopWatcher();

        this.flushDebouncer = debounce(() => {
            void this.flushPendingModDirectories();
        }, 300);

        for (const importer of importers) {
            const modsPath = path.join(importer.importerFolder, "mods");
            if (!(await fse.pathExists(modsPath))) {
                continue;
            }

            const watcherId = await this.desktop.lib.watcher.createWatcher(
                modsPath,
                {},
                async (eventName, changedPath) => {
                    await this.handleWatcherChange(importer.key, modsPath, eventName, changedPath);
                    this.flushDebouncer?.();
                },
            );
            this.watcherIds.push(watcherId);

            await this.queueDirectoriesFromRoot(importer.key, modsPath, modsPath);
        }

        this.flushDebouncer?.();
    }

    public async stopWatcher() {
        for (const watcherId of this.watcherIds) {
            await this.desktop.lib.watcher.removeWatcher(watcherId);
        }

        this.watcherIds = [];
        this.pendingModDirectories.clear();
        this.suppressedModDirectories.clear();
        this.flushDebouncer = null;
        this.isFlushing = false;
        this.pendingFlush = false;
    }

    public async restoreImporterBackups(importerKey: string): Promise<AutoModActionsRestoreResult> {
        await this.desktop.service.xxmi.init();

        const importer = this.desktop.service.xxmi
            .getEnabledImporters()
            .find((item) => item.key === importerKey);
        if (!importer) {
            throw new Error(`Importer ${importerKey} not found`);
        }

        const modsPath = path.join(importer.importerFolder, "mods");
        if (!(await fse.pathExists(modsPath))) {
            return {
                importerKey,
                scannedBackups: 0,
                restoredFiles: 0,
                removedBackups: 0,
            };
        }

        const backupPaths = await fg("**/*.ini", {
            absolute: true,
            cwd: modsPath,
            onlyFiles: true,
            suppressErrors: true,
        });

        const parsedBackups = backupPaths
            .map((backupPath) => this.parseBackupFile(path.resolve(backupPath)))
            .filter((item): item is ParsedBackupFile => item !== null);

        const backupsByOriginalPath = new Map<string, ParsedBackupFile[]>();
        for (const parsedBackup of parsedBackups) {
            const key = parsedBackup.originalPath.toLowerCase();
            const bucket = backupsByOriginalPath.get(key) ?? [];
            bucket.push(parsedBackup);
            backupsByOriginalPath.set(key, bucket);
        }

        let restoredFiles = 0;
        let removedBackups = 0;

        for (const backupGroup of backupsByOriginalPath.values()) {
            const sortedGroup = backupGroup.sort((left, right) => right.timestamp - left.timestamp);
            const latestBackup = sortedGroup[0];
            this.suppressModPath(latestBackup.modPath);

            try {
                await fse.ensureDir(path.dirname(latestBackup.originalPath));
                await fse.copy(latestBackup.backupPath, latestBackup.originalPath, {
                    overwrite: true,
                });
                restoredFiles += 1;
            } catch (error) {
                this.desktop.logger.error(
                    error,
                    `AutoModActions:restore:${latestBackup.originalPath}`,
                );
                continue;
            }

            for (const backup of sortedGroup) {
                try {
                    await fse.remove(backup.backupPath);
                    removedBackups += 1;
                } catch (error) {
                    this.desktop.logger.error(
                        error,
                        `AutoModActions:removeBackup:${backup.backupPath}`,
                    );
                }
            }
        }

        return {
            importerKey,
            scannedBackups: parsedBackups.length,
            restoredFiles,
            removedBackups,
        };
    }

    private async handleWatcherChange(
        importerKey: string,
        modsPath: string,
        eventName: "create" | "modify" | "remove",
        changedPath: string,
    ) {
        if (eventName === "remove") {
            return;
        }

        const resolvedPath = path.resolve(changedPath);
        if (this.isSuppressedPath(resolvedPath)) {
            return;
        }

        if (this.isEligibleIniPath(resolvedPath)) {
            this.queueModDirectory(importerKey, modsPath, path.dirname(resolvedPath));
            return;
        }

        try {
            const stat = await fse.stat(resolvedPath);
            if (stat.isDirectory()) {
                await this.queueDirectoriesFromRoot(importerKey, modsPath, resolvedPath);
            }
        } catch {}
    }

    private async queueDirectoriesFromRoot(importerKey: string, modsPath: string, rootPath: string) {
        const iniPaths = await this.findIniPaths(rootPath);
        for (const iniPath of iniPaths) {
            this.queueModDirectory(importerKey, modsPath, path.dirname(iniPath));
        }
    }

    private queueModDirectory(importerKey: string, modsPath: string, modPath: string) {
        const normalizedKey = path.resolve(modPath).toLowerCase();
        this.pendingModDirectories.set(normalizedKey, {
            importerKey,
            modsPath,
            modPath: path.resolve(modPath),
        });
    }

    private async flushPendingModDirectories() {
        if (this.isFlushing) {
            this.pendingFlush = true;
            return;
        }

        this.isFlushing = true;
        try {
            const pendingDirectories = [...this.pendingModDirectories.values()];
            this.pendingModDirectories.clear();

            for (const pendingDirectory of pendingDirectories) {
                await this.processModDirectory(pendingDirectory);
            }
        } finally {
            this.isFlushing = false;
            if (this.pendingFlush) {
                this.pendingFlush = false;
                this.flushDebouncer?.();
            }
        }
    }

    private async processModDirectory({ importerKey, modPath }: PendingModDirectory) {
        this.suppressModPath(modPath);

        const config = await this.desktop.setting.xxmi.getAutoModActionsConfig();
        const importerConfig = config.importers[importerKey];
        if (!isAutoModActionsImporterEnabled(importerKey, importerConfig)) {
            return;
        }

        const backedUpIniPaths = new Set<string>();
        let iniPaths = await this.findIniPaths(modPath);

        if (importerConfig.autoFixer.enabled && importerConfig.autoFixer.presetId) {
            await this.backupIniPaths(iniPaths, backedUpIniPaths);

            try {
                await this.desktop.service.modTools.fixTool.runPresetGhost(
                    importerConfig.autoFixer.presetId,
                    modPath,
                );
            } catch (error) {
                this.desktop.logger.error(error, `AutoModActions:autoFixer:${modPath}`);
            }

            iniPaths = await this.findIniPaths(modPath);
        }

        const isGIMI = importerKey.trim().toUpperCase() === "GIMI";
        const shouldApplyOrFix = isGIMI && importerConfig.orFix?.enabled === true;
        const shouldApplyFaceHeadFix = isGIMI && importerConfig.faceHeadFix?.enabled === true;
        if (!shouldApplyOrFix && !shouldApplyFaceHeadFix) {
            return;
        }

        for (const iniPath of iniPaths) {
            let originalContent = "";
            try {
                originalContent = await fse.readFile(iniPath, "utf-8");
            } catch (error) {
                this.desktop.logger.error(error, `AutoModActions:read:${iniPath}`);
                continue;
            }

            let nextContent = originalContent;
            if (shouldApplyOrFix) {
                nextContent = this.applyORFix(nextContent);
            }
            if (shouldApplyFaceHeadFix) {
                nextContent = this.applyFaceHeadFix(nextContent);
            }

            if (nextContent === originalContent) {
                continue;
            }

            const normalizedIniPath = path.resolve(iniPath).toLowerCase();
            if (!backedUpIniPaths.has(normalizedIniPath) && (await fse.pathExists(iniPath))) {
                await this.backupIniFile(iniPath);
                backedUpIniPaths.add(normalizedIniPath);
            }

            try {
                await fse.writeFile(iniPath, nextContent, "utf-8");
            } catch (error) {
                this.desktop.logger.error(error, `AutoModActions:write:${iniPath}`);
            }
        }

        this.suppressModPath(modPath);
    }

    private async backupIniPaths(iniPaths: string[], backedUpIniPaths: Set<string>) {
        for (const iniPath of iniPaths) {
            const normalizedIniPath = path.resolve(iniPath).toLowerCase();
            if (backedUpIniPaths.has(normalizedIniPath)) {
                continue;
            }

            await this.backupIniFile(iniPath);
            backedUpIniPaths.add(normalizedIniPath);
        }
    }

    private async backupIniFile(iniPath: string) {
        const backupName = `${AUTO_MOD_ACTIONS_BACKUP_PREFIX}${Date.now()}_${path.basename(iniPath)}`;
        const backupPath = path.join(path.dirname(iniPath), backupName);
        try {
            await fse.copy(iniPath, backupPath, { overwrite: false, errorOnExist: true });
        } catch (error) {
            this.desktop.logger.error(error, `AutoModActions:backup:${iniPath}`);
        }
    }

    private async findIniPaths(rootPath: string) {
        if (!(await fse.pathExists(rootPath))) {
            return [];
        }

        const matches = await fg("**/*.ini", {
            absolute: true,
            cwd: rootPath,
            onlyFiles: true,
            suppressErrors: true,
        });

        return matches
            .map((targetPath) => path.resolve(targetPath))
            .filter((targetPath) => this.isEligibleIniPath(targetPath));
    }

    private isEligibleIniPath(targetPath: string) {
        const lowerPath = targetPath.toLowerCase();
        if (!lowerPath.endsWith(".ini")) {
            return false;
        }

        return !path.basename(targetPath).toLowerCase().startsWith("disabled");
    }

    private applyORFix(content: string) {
        const lineEnding = content.includes("\r\n") ? "\r\n" : "\n";
        const lines = content.split(/\r?\n/);
        const sections = this.parseSections(lines);

        for (let sectionIndex = sections.length - 1; sectionIndex >= 0; sectionIndex -= 1) {
            const section = sections[sectionIndex];
            if (!/^CommandList.*(Head|Body|Dress|Extra)$/i.test(section.name)) {
                continue;
            }

            const bodyLines = lines.slice(section.startLineIndex + 1, section.endLineIndex);
            const bodyContent = bodyLines
                .map((line) => this.stripInlineComment(line).trim().toLowerCase())
                .join("\n");

            if (
                bodyContent.includes(OR_FIX_RUN_LINE.toLowerCase()) ||
                bodyContent.includes(NN_FIX_RUN_LINE.toLowerCase())
            ) {
                continue;
            }

            const hasNormalMap = bodyLines.some((line) =>
                this.stripInlineComment(line).toLowerCase().includes("normalmap"),
            );
            const insertionIndex = this.findSectionInsertionIndex(lines, section);
            lines.splice(insertionIndex, 0, hasNormalMap ? OR_FIX_RUN_LINE : NN_FIX_RUN_LINE);
        }

        return lines.join(lineEnding);
    }

    private applyFaceHeadFix(content: string) {
        const lineEnding = content.includes("\r\n") ? "\r\n" : "\n";
        const lines = content.split(/\r?\n/);
        const sections = this.parseSections(lines);

        for (const section of sections) {
            if (!section.name.toLowerCase().includes("facehead")) {
                continue;
            }

            for (
                let lineIndex = section.startLineIndex + 1;
                lineIndex < section.endLineIndex;
                lineIndex += 1
            ) {
                const nextLine = lines[lineIndex];
                if (!nextLine || nextLine.trim().startsWith(";")) {
                    continue;
                }

                lines[lineIndex] = nextLine.replace(/^(\s*)ps-t0(\s*=.*)$/i, "$1this$2");
            }
        }

        return lines.join(lineEnding);
    }

    private parseSections(lines: string[]): IniSectionRange[] {
        const sections: IniSectionRange[] = [];
        let currentSection: IniSectionRange | null = null;

        for (let index = 0; index < lines.length; index += 1) {
            const line = lines[index];
            const match = line.trim().match(/^\[(.+)\]$/);
            if (!match) {
                continue;
            }

            if (currentSection) {
                currentSection.endLineIndex = index;
            }

            currentSection = {
                name: match[1].trim(),
                startLineIndex: index,
                endLineIndex: lines.length,
            };
            sections.push(currentSection);
        }

        return sections;
    }

    private findSectionInsertionIndex(lines: string[], section: IniSectionRange) {
        let insertionIndex = section.endLineIndex;
        while (
            insertionIndex > section.startLineIndex + 1 &&
            lines[insertionIndex - 1].trim() === ""
        ) {
            insertionIndex -= 1;
        }

        return insertionIndex;
    }

    private stripInlineComment(line: string) {
        return line.split(";")[0] ?? line;
    }

    private parseBackupFile(backupPath: string): ParsedBackupFile | null {
        const fileName = path.basename(backupPath);
        const escapedPrefix = AUTO_MOD_ACTIONS_BACKUP_PREFIX.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        const match = fileName.match(new RegExp(`^${escapedPrefix}(\\d+)_(.+\\.ini)$`, "i"));
        if (!match) {
            return null;
        }

        return {
            backupPath,
            originalPath: path.join(path.dirname(backupPath), match[2]),
            modPath: path.dirname(backupPath),
            timestamp: Number.parseInt(match[1], 10) || 0,
        };
    }

    private suppressModPath(modPath: string) {
        this.suppressedModDirectories.set(
            path.resolve(modPath).toLowerCase(),
            Date.now() + AutoModActions.suppressWindowMs,
        );
    }

    private isSuppressedPath(targetPath: string) {
        const now = Date.now();
        const normalizedTargetPath = path.resolve(targetPath).toLowerCase();

        for (const [modPath, expiresAt] of this.suppressedModDirectories.entries()) {
            if (expiresAt <= now) {
                this.suppressedModDirectories.delete(modPath);
                continue;
            }

            if (
                normalizedTargetPath === modPath ||
                normalizedTargetPath.startsWith(`${modPath}${path.sep}`)
            ) {
                return true;
            }
        }

        return false;
    }
}

import crypto from "node:crypto";
import path from "node:path";
import { toggleViewerArtifact } from "@main/internal/db/schema";
import { findFiles } from "@native/native-fs";
import { formatKeySequence } from "@shared/key-formatter";
import { and, eq } from "drizzle-orm";
import { debounce } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "@/main";

interface IniEntry {
    key: string;
    value: string;
}

interface IniSection {
    name: string;
    entries: IniEntry[];
}

interface ParsedKeySection {
    sectionName: string;
    keyValue: string;
    backValue?: string;
}

type ToggleViewerTaskType = "scan" | "generate" | "delete";
const DEFAULT_TOGGLE_VIEWER_HOTKEY = "no_shift no_ ctrl alt H";

export class ToggleViewer {
    private static readonly fullScanConcurrency = 8;
    private watcherIds: string[] = [];
    private scanDebouncer: (() => void) | null = null;
    private logs: string[] = [];
    private isScanning = false;
    private pendingScan = false;
    private pendingChangedIniPaths = new Set<string>();
    private activeAbortController: AbortController | null = null;
    private currentTask: ToggleViewerTaskType | null = null;

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startWatcher() {
        if (!this.desktop.service.xxmi) return;
        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();
        if (!xxmiPath || !xxmiConfig) return;

        const enabled = await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
        if (!enabled) return;

        await this.stopWatcher();

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const modsPaths: string[] = [];
        for (const importer of importers) {
            const modsPath = path.join(importer.importerFolder, "mods");
            if (await fse.pathExists(modsPath)) {
                modsPaths.push(modsPath);
            }
        }

        if (modsPaths.length === 0) {
            this.logInfo("No mods folder found for enabled importers");
            return;
        }

        this.scanDebouncer = debounce(async () => {
            if (this.currentTask && this.currentTask !== "scan") return;
            if (this.currentTask === "scan" && this.activeAbortController?.signal.aborted) {
                return;
            }

            if (!this.currentTask) {
                this.currentTask = "scan";
                this.activeAbortController = new AbortController();
            }

            try {
                await this.scanChangedIniPaths({
                    signal: this.activeAbortController?.signal,
                });
            } finally {
                if (this.currentTask === "scan") {
                    this.currentTask = null;
                    this.activeAbortController = null;
                }
            }
        }, 500);

        for (const modsPath of modsPaths) {
            const watcherId = await this.desktop.lib.watcher.createWatcher(
                modsPath,
                { compareContents: true },
                async (eventName, changedPath) => {
                    const lowerChangedPath = changedPath.toLowerCase();
                    if (
                        lowerChangedPath.endsWith("\\toggle-viewer.txt") ||
                        lowerChangedPath.endsWith("\\toggle-viewer.ini") ||
                        lowerChangedPath.endsWith("/toggle-viewer.txt") ||
                        lowerChangedPath.endsWith("/toggle-viewer.ini")
                    ) {
                        return;
                    }

                    await this.handleWatcherChange(eventName, changedPath);

                    this.scanDebouncer?.();
                },
            );
            this.watcherIds.push(watcherId);
        }

        this.logInfo(`Started toggle viewer watcher (${modsPaths.length})`);

        if (!this.currentTask) {
            this.currentTask = "scan";
            this.activeAbortController = new AbortController();
            void this.scanAllImporters({ signal: this.activeAbortController.signal })
                .catch((error) => {
                    this.logError(`Initial toggle viewer scan failed: ${error}`);
                })
                .finally(() => {
                    if (this.currentTask === "scan") {
                        this.currentTask = null;
                        this.activeAbortController = null;
                    }
                });
        }
    }

    public async stopWatcher() {
        this.cancelCurrentWork();
        const count = this.watcherIds.length;
        for (const watcherId of this.watcherIds) {
            await this.desktop.lib.watcher.removeWatcher(watcherId);
        }
        this.watcherIds = [];
        this.scanDebouncer = null;
        this.pendingChangedIniPaths.clear();
        if (count > 0) {
            this.logInfo(`Stopped toggle viewer watcher (${count})`);
        }
    }

    public getLogs() {
        return [...this.logs];
    }

    public getState() {
        return {
            isRunning: this.currentTask !== null,
            mode: this.currentTask,
        };
    }

    public cancelCurrentWork() {
        if (!this.activeAbortController) return;
        this.activeAbortController.abort();
        this.logInfo("Requested stop for current toggle viewer task");
    }

    public async runBatchGenerate() {
        if (this.currentTask) {
            throw new Error("Toggle viewer task is already running");
        }

        this.currentTask = "generate";
        this.activeAbortController = new AbortController();
        try {
            await this.scanAllImporters({ signal: this.activeAbortController.signal });
        } finally {
            this.currentTask = null;
            this.activeAbortController = null;

            const autoGenerateEnabled =
                await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
            if (autoGenerateEnabled && this.watcherIds.length === 0) {
                try {
                    await this.startWatcher();
                } catch (error) {
                    this.logError(
                        `Failed to start watcher after manual generate completion: ${error}`,
                    );
                }
            }
        }
    }

    public async runBatchDelete() {
        if (this.currentTask) {
            throw new Error("Toggle viewer task is already running");
        }

        const autoGenerateEnabled = await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
        if (autoGenerateEnabled) {
            await this.desktop.setting.xxmi.setToggleViewerAutoGenerate(false);
            this.logInfo("Disabled toggle viewer auto-generate before batch delete");
        } else {
            await this.stopWatcher();
        }

        this.currentTask = "delete";
        this.activeAbortController = new AbortController();
        const signal = this.activeAbortController.signal;

        try {
            const importers = this.desktop.service.xxmi.getEnabledImporters();
            let deletedFiles = 0;
            let deletedRecords = 0;

            const enabledModsRoots = importers.map((importer) =>
                path.resolve(path.join(importer.importerFolder, "mods")),
            );
            if (enabledModsRoots.length === 0) {
                this.logInfo("Batch delete skipped: no enabled importer mods folder");
                return;
            }
            const enabledModsRootAliases = await this.expandRootAliases(enabledModsRoots);

            const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
            const targetRecords = records.filter((record) =>
                enabledModsRootAliases.some((modsRoot) =>
                    this.isPathInRoot(record.targetIniPath, modsRoot),
                ),
            );

            for (const record of targetRecords) {
                if (signal.aborted) {
                    this.logInfo("Batch delete cancelled");
                    return;
                }

                const deletionTargets = [record.toggleTxtPath, record.toggleIniPath];
                let hasError = false;

                for (const targetPath of deletionTargets) {
                    const result = await this.removeManagedArtifactFile(targetPath);
                    if (result === "deleted") {
                        deletedFiles += 1;
                    } else if (result === "error") {
                        hasError = true;
                    }
                }

                if (hasError) {
                    this.logError(
                        `Keeping artifact record due to delete error: ${record.targetIniPath}`,
                    );
                    continue;
                }

                await this.desktop.lib.db
                    .delete(toggleViewerArtifact)
                    .where(
                        and(
                            eq(toggleViewerArtifact.id, record.id),
                            eq(toggleViewerArtifact.targetIniPath, record.targetIniPath),
                        ),
                    );
                deletedRecords += 1;
            }

            if (!signal.aborted) {
                this.logInfo(
                    `Batch delete completed. deletedFiles=${deletedFiles}, deletedRecords=${deletedRecords}, targetRecords=${targetRecords.length}`,
                );
            }
        } finally {
            this.currentTask = null;
            this.activeAbortController = null;
        }
    }

    public async applyHotkeyToArtifacts(hotkey: string) {
        const normalizedHotkey = hotkey.trim() || DEFAULT_TOGGLE_VIEWER_HOTKEY;
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        let updatedCount = 0;

        for (const record of records) {
            try {
                if (!(await fse.pathExists(record.toggleIniPath))) {
                    continue;
                }

                const currentContent = await fse.readFile(record.toggleIniPath, "utf-8");
                const nextContent = this.replaceHotkeyInGeneratedIni(
                    currentContent,
                    normalizedHotkey,
                );
                if (nextContent === currentContent) {
                    continue;
                }

                await fse.writeFile(record.toggleIniPath, nextContent, "utf-8");
                const updatedAt = new Date().toISOString();
                await this.desktop.lib.db
                    .update(toggleViewerArtifact)
                    .set({
                        toggleIniHash: this.sha256(nextContent),
                        updatedAt,
                    })
                    .where(eq(toggleViewerArtifact.id, record.id));
                updatedCount += 1;
            } catch (error) {
                this.logError(`Failed to apply hotkey to ${record.toggleIniPath}: ${error}`);
            }
        }

        this.logInfo(`Applied toggle viewer hotkey to artifacts: updated=${updatedCount}`);
    }

    private async scanAllImporters(options?: { signal?: AbortSignal }) {
        if (this.isScanning) {
            this.pendingScan = true;
            return;
        }
        this.isScanning = true;
        const signal = options?.signal;

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const seenTargetIniPaths = new Set<string>();
        const toggleViewerHotkey = await this.getToggleViewerHotkey();

        try {
            for (const importer of importers) {
                const modsPath = path.join(importer.importerFolder, "mods");
                if (!(await fse.pathExists(modsPath))) {
                    continue;
                }

                if (signal?.aborted) {
                    this.logInfo("Toggle viewer scan cancelled");
                    return;
                }

                const iniCandidates = await this.findIniCandidates(modsPath);
                await this.processIniBatch(
                    iniCandidates,
                    seenTargetIniPaths,
                    toggleViewerHotkey,
                    signal,
                );
            }

            if (signal?.aborted) {
                this.logInfo("Toggle viewer scan cancelled");
                return;
            }

            await this.deleteStaleRecords(seenTargetIniPaths);
            this.logInfo(
                `Scan complete. matched=${seenTargetIniPaths.size}, importers=${importers.length}`,
            );
        } finally {
            this.isScanning = false;
            if (this.pendingScan) {
                this.pendingScan = false;
                if (!signal?.aborted) {
                    this.scanDebouncer?.();
                }
            }
        }
    }

    private async scanChangedIniPaths(options?: { signal?: AbortSignal }) {
        if (this.isScanning) {
            this.pendingScan = true;
            return;
        }
        this.isScanning = true;
        const signal = options?.signal;
        const toggleViewerHotkey = await this.getToggleViewerHotkey();

        try {
            const targets = [...this.pendingChangedIniPaths];
            this.pendingChangedIniPaths.clear();

            if (targets.length === 0) {
                return;
            }

            let processedCount = 0;
            for (const iniPath of targets) {
                if (signal?.aborted) {
                    this.logInfo("Toggle viewer scan cancelled");
                    return;
                }

                const processed = await this.processIniOrDeleteRecord(iniPath, toggleViewerHotkey);
                if (processed) {
                    processedCount += 1;
                }
            }

            this.logInfo(
                `Incremental scan complete. queued=${targets.length}, processed=${processedCount}`,
            );
        } finally {
            this.isScanning = false;
            if (this.pendingScan) {
                this.pendingScan = false;
                if (!signal?.aborted) {
                    this.scanDebouncer?.();
                }
            }
        }
    }

    private async processIniBatch(
        iniPaths: string[],
        seenTargetIniPaths: Set<string>,
        toggleViewerHotkey: string,
        signal?: AbortSignal,
    ) {
        let cursor = 0;
        const workerCount = Math.max(
            1,
            Math.min(ToggleViewer.fullScanConcurrency, iniPaths.length),
        );

        const workers = Array.from({ length: workerCount }, async () => {
            while (true) {
                if (signal?.aborted) {
                    return;
                }

                const index = cursor++;
                if (index >= iniPaths.length) {
                    return;
                }

                const iniPath = path.resolve(iniPaths[index]);
                const processed = await this.processIni(iniPath, toggleViewerHotkey);
                if (processed) {
                    seenTargetIniPaths.add(iniPath);
                }
            }
        });

        await Promise.all(workers);
    }

    private async handleWatcherChange(
        eventName: "create" | "modify" | "remove",
        changedPath: string,
    ) {
        const normalizedPath = path.resolve(changedPath);
        if (this.isIniPath(normalizedPath)) {
            if (eventName === "remove") {
                await this.deleteArtifactRecordByTargetIniPath(normalizedPath);
                return;
            }
            this.pendingChangedIniPaths.add(normalizedPath);
            return;
        }

        if (eventName === "remove") {
            await this.deleteArtifactRecordsByPathPrefix(normalizedPath);
            return;
        }

        await this.queueIniCandidatesUnderPath(normalizedPath);
    }

    private async queueIniCandidatesUnderPath(targetPath: string) {
        try {
            const stat = await fse.stat(targetPath);
            if (stat.isDirectory()) {
                const iniPaths = findFiles([targetPath], [".ini"], ["toggle-viewer.ini"]).map((p) =>
                    path.resolve(p),
                );
                for (const iniPath of iniPaths) {
                    this.pendingChangedIniPaths.add(iniPath);
                }
            }
        } catch {}
    }

    private isIniPath(targetPath: string) {
        const lower = targetPath.toLowerCase();
        return lower.endsWith(".ini") && !lower.endsWith(`${path.sep}toggle-viewer.ini`);
    }

    private async findIniCandidates(modsPath: string) {
        return findFiles([modsPath], [".ini"], ["toggle-viewer.ini"]).map((p) => path.resolve(p));
    }

    private async processIni(iniPath: string, toggleViewerHotkey: string) {
        const dir = path.dirname(iniPath);

        let content = "";
        try {
            content = await fse.readFile(iniPath, "utf-8");
        } catch (error) {
            this.logError(`Failed to read ini ${iniPath}: ${error}`);
            return false;
        }

        const sections = this.parseIni(content);
        const keySections = this.findTargetKeySections(sections);
        if (keySections.length === 0) {
            return false;
        }

        const positionHash = this.resolvePositionHash(sections);
        if (!positionHash) {
            this.logInfo(`Position hash not found: ${iniPath}`);
            return false;
        }

        const toggleViewerTxtPath = path.join(dir, "toggle-viewer.txt");
        const toggleViewerIniPath = path.join(dir, "toggle-viewer.ini");

        const txtContent = this.buildToggleViewerTxt(iniPath, keySections);
        const iniContent = this.buildToggleViewerIni(positionHash, toggleViewerHotkey);

        await this.writeIfChanged(toggleViewerTxtPath, txtContent);
        await this.writeIfChanged(toggleViewerIniPath, iniContent);

        const txtHash = this.sha256(txtContent);
        const iniHash = this.sha256(iniContent);
        const updatedAt = new Date().toISOString();

        await this.desktop.lib.db
            .insert(toggleViewerArtifact)
            .values({
                id: nanoid(),
                targetIniPath: iniPath,
                toggleTxtPath: toggleViewerTxtPath,
                toggleIniPath: toggleViewerIniPath,
                toggleTxtHash: txtHash,
                toggleIniHash: iniHash,
                updatedAt,
            })
            .onConflictDoUpdate({
                target: toggleViewerArtifact.targetIniPath,
                set: {
                    toggleTxtPath: toggleViewerTxtPath,
                    toggleIniPath: toggleViewerIniPath,
                    toggleTxtHash: txtHash,
                    toggleIniHash: iniHash,
                    updatedAt,
                },
            });

        this.logInfo(`Generated toggle-viewer artifacts: ${iniPath}`);
        return true;
    }

    private async processIniOrDeleteRecord(iniPath: string, toggleViewerHotkey: string) {
        if (!(await fse.pathExists(iniPath))) {
            await this.deleteArtifactRecordByTargetIniPath(iniPath);
            return false;
        }

        const processed = await this.processIni(iniPath, toggleViewerHotkey);
        if (!processed) {
            await this.deleteArtifactRecordByTargetIniPath(iniPath);
        }
        return processed;
    }

    private parseIni(content: string): IniSection[] {
        const lines = content.split(/\r?\n/);
        const sections: IniSection[] = [];
        let currentSection: IniSection | null = null;

        for (const rawLine of lines) {
            const trimmed = rawLine.trim();
            if (!trimmed || trimmed.startsWith(";")) continue;

            const sectionMatch = trimmed.match(/^\[(.+)\]$/);
            if (sectionMatch) {
                currentSection = {
                    name: sectionMatch[1].trim(),
                    entries: [],
                };
                sections.push(currentSection);
                continue;
            }

            if (!currentSection) continue;
            const eqIndex = trimmed.indexOf("=");
            if (eqIndex <= 0) continue;

            const rawKey = trimmed.slice(0, eqIndex).trim();
            const rawValue = trimmed.slice(eqIndex + 1).trim();
            const value = rawValue.split(";")[0].trim();

            currentSection.entries.push({
                key: rawKey,
                value,
            });
        }

        return sections;
    }

    private findTargetKeySections(sections: IniSection[]): ParsedKeySection[] {
        const result: ParsedKeySection[] = [];

        for (const section of sections) {
            if (!section.name.toLowerCase().startsWith("key")) continue;

            const typeValue = this.getEntryValue(section, "type");
            if (typeValue?.toLowerCase() !== "cycle") continue;

            let hasMultiValueVariable = false;
            for (const entry of section.entries) {
                if (!entry.key.startsWith("$")) continue;
                const values = entry.value
                    .split(",")
                    .map((v) => v.trim())
                    .filter(Boolean);
                if (values.length >= 2) {
                    hasMultiValueVariable = true;
                    break;
                }
            }
            if (!hasMultiValueVariable) continue;

            const keyValue = this.getEntryValue(section, "key");
            if (!keyValue) continue;

            const backValue = this.getEntryValue(section, "back");
            result.push({
                sectionName: section.name,
                keyValue,
                backValue: backValue || undefined,
            });
        }

        return result;
    }

    private resolvePositionHash(sections: IniSection[]): string | null {
        const textureSections = sections.filter((s) =>
            s.name.toLowerCase().startsWith("textureoverride"),
        );
        const resourceSections = sections.filter((s) =>
            s.name.toLowerCase().startsWith("resource"),
        );

        // case 1: TextureOverride section name contains BodyPosition and has hash.
        for (const section of textureSections) {
            if (!section.name.toLowerCase().includes("bodyposition")) continue;
            const hash = this.getEntryValue(section, "hash");
            if (hash) return hash;
        }

        // case 2: Resource*BodyPosition exists, then use hash of TextureOverride that references it via vb0.
        const bodyPositionResourceSet = new Set(
            resourceSections
                .map((s) => s.name)
                .filter((name) => name.toLowerCase().includes("bodyposition"))
                .map((name) => name.toLowerCase()),
        );

        if (bodyPositionResourceSet.size > 0) {
            for (const section of textureSections) {
                const vb0 = this.getEntryValue(section, "vb0");
                if (!vb0 || !bodyPositionResourceSet.has(vb0.toLowerCase())) continue;
                const hash = this.getEntryValue(section, "hash");
                if (hash) return hash;
            }
        }

        // case 3: Resource*Position exists, and TextureOverride referencing it via vb0 has body in section name.
        const positionResourceSet = new Set(
            resourceSections
                .map((s) => s.name)
                .filter((name) => name.toLowerCase().includes("position"))
                .map((name) => name.toLowerCase()),
        );
        if (positionResourceSet.size > 0) {
            for (const section of textureSections) {
                const lowerName = section.name.toLowerCase();
                if (!lowerName.includes("body")) continue;
                const vb0 = this.getEntryValue(section, "vb0");
                if (!vb0 || !positionResourceSet.has(vb0.toLowerCase())) continue;
                const hash = this.getEntryValue(section, "hash");
                if (hash) return hash;
            }
        }

        // case 4: Fallback to first Resource*Position reference (vb0) in TextureOverride.
        const firstPositionResource = resourceSections.find((section) =>
            section.name.toLowerCase().includes("position"),
        );
        if (firstPositionResource) {
            for (const section of textureSections) {
                const vb0 = this.getEntryValue(section, "vb0");
                if (vb0?.toLowerCase() !== firstPositionResource.name.toLowerCase()) continue;
                const hash = this.getEntryValue(section, "hash");
                if (hash) return hash;
            }
        }

        // case 5: WWMI-style fallback where vb0 position binding is in CommandListOverrideSharedResources.
        const commandListOverrideSharedResources = sections.find(
            (section) => section.name.toLowerCase() === "commandlistoverridesharedresources",
        );
        const sharedVb0 = commandListOverrideSharedResources
            ? this.getEntryValue(commandListOverrideSharedResources, "vb0")
            : null;
        if (sharedVb0 && sharedVb0.toLowerCase().includes("position")) {
            for (const section of textureSections) {
                if (!section.name.toLowerCase().includes("component")) continue;
                const hash = this.getEntryValue(section, "hash");
                if (hash) return hash;
            }
        }

        return null;
    }

    private buildToggleViewerTxt(iniPath: string, keySections: ParsedKeySection[]) {
        const modName = path.basename(path.dirname(iniPath));
        const iniName = path.basename(iniPath);
        const lines: string[] = [`Mod: ${modName}`, "", `Ini: ${iniName}`, "", "Keys:"];

        for (let i = 0; i < keySections.length; i++) {
            const keySection = keySections[i];
            lines.push(`    ${keySection.sectionName}:`);
            lines.push(
                `        Key: ${formatKeySequence(keySection.keyValue, { asciiFallback: true })}`,
            );
            if (keySection.backValue) {
                lines.push(
                    `        Back: ${formatKeySequence(keySection.backValue, { asciiFallback: true })}`,
                );
            }
            if (i < keySections.length - 1) {
                lines.push("");
            }
        }

        return `${lines.join("\n")}\n`;
    }

    private buildToggleViewerIni(hash: string, hotkey: string) {
        const lines = [
            "[Constants]",
            "global $active = 0",
            "global $enabled = 0",
            "",
            "[Key]",
            `key = ${hotkey}`,
            "condition = $active == 1",
            "type = cycle",
            "$enabled = 0,1",
            "",
            "[TextureOverrideCharacterPosition]",
            `hash = ${hash}`,
            "$active = 1",
            "",
            "[Present]",
            "post $active = 0",
            "run = CommandListKey",
            "",
            "[CommandListKey]",
            "if $active == 1 && $enabled == 1",
            "    pre Resource\\ShaderFixes\\help.ini\\NotificationParams = ResourceBox",
            "    pre run = CustomShader\\ShaderFixes\\help.ini\\FormatText",
            "    pre Resource\\ShaderFixes\\help.ini\\Notification = Resourcename1",
            "endif",
            "",
            "[ResourceBox]",
            "type = StructuredBuffer",
            "array = 1",
            "data = R32_FLOAT   -0.95 -1 1 1      1 1 1 1    0 0 0 0.95   0.05 0.05     1 2   0  1.0",
            "",
            "[Resourcename1]",
            "type = buffer",
            "format = R8_UINT",
            "filename = toggle-viewer.txt",
            "",
        ];

        return lines.join("\n");
    }

    private replaceHotkeyInGeneratedIni(content: string, hotkey: string) {
        const newline = content.includes("\r\n") ? "\r\n" : "\n";
        const lines = content.split(/\r?\n/);
        let inKeySection = false;

        for (let i = 0; i < lines.length; i++) {
            const trimmed = lines[i].trim();
            const sectionMatch = trimmed.match(/^\[(.+)\]$/);
            if (sectionMatch) {
                inKeySection = sectionMatch[1].trim().toLowerCase() === "key";
                continue;
            }

            if (inKeySection && /^\s*key\s*=/.test(lines[i])) {
                lines[i] = `key = ${hotkey}`;
                return lines.join(newline);
            }
        }

        return content;
    }

    private async getToggleViewerHotkey() {
        try {
            return await this.desktop.setting.xxmi.getToggleViewerHotkey();
        } catch {
            return DEFAULT_TOGGLE_VIEWER_HOTKEY;
        }
    }

    private getEntryValue(section: IniSection, key: string) {
        const found = section.entries.find(
            (entry) => entry.key.toLowerCase() === key.toLowerCase(),
        );
        return found?.value || null;
    }

    private sha256(content: string) {
        return crypto.createHash("sha256").update(content).digest("hex");
    }

    private async writeIfChanged(filePath: string, nextContent: string) {
        if (await fse.pathExists(filePath)) {
            const currentContent = await fse.readFile(filePath, "utf-8");
            if (currentContent === nextContent) {
                return;
            }
        }
        await fse.writeFile(filePath, nextContent, "utf-8");
    }

    private isPathInRoot(targetPath: string, rootPath: string) {
        const resolvedTarget = path.resolve(targetPath).toLowerCase();
        const resolvedRoot = path.resolve(rootPath).toLowerCase();
        return (
            resolvedTarget === resolvedRoot ||
            resolvedTarget.startsWith(`${resolvedRoot}${path.sep}`)
        );
    }

    private async expandRootAliases(roots: string[]) {
        const aliasSet = new Set<string>();

        for (const root of roots) {
            const resolvedRoot = path.resolve(root);
            aliasSet.add(resolvedRoot);

            try {
                if (await fse.pathExists(resolvedRoot)) {
                    aliasSet.add(path.resolve(await fse.realpath(resolvedRoot)));
                }
            } catch {}
        }

        return [...aliasSet];
    }

    private async removeManagedArtifactFile(filePath: string) {
        try {
            if (!(await fse.pathExists(filePath))) {
                return "missing" as const;
            }

            const stat = await fse.stat(filePath);
            if (!stat.isFile()) {
                this.logError(`Skipped non-file path while deleting artifact: ${filePath}`);
                return "error" as const;
            }

            await fse.remove(filePath);
            return "deleted" as const;
        } catch (error) {
            this.logError(`Failed to delete artifact file ${filePath}: ${error}`);
            return "error" as const;
        }
    }

    private async deleteStaleRecords(seenTargetIniPaths: Set<string>) {
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        for (const record of records) {
            if (seenTargetIniPaths.has(record.targetIniPath)) continue;
            await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
        }
    }

    private async deleteArtifactRecordByTargetIniPath(targetIniPath: string) {
        const records = await this.desktop.lib.db
            .select()
            .from(toggleViewerArtifact)
            .where(eq(toggleViewerArtifact.targetIniPath, targetIniPath));
        for (const record of records) {
            await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
        }
    }

    private async deleteArtifactRecordsByPathPrefix(targetPathPrefix: string) {
        const normalizedPrefix = path.resolve(targetPathPrefix).toLowerCase();
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        for (const record of records) {
            const normalizedTarget = path.resolve(record.targetIniPath).toLowerCase();
            if (
                normalizedTarget === normalizedPrefix ||
                normalizedTarget.startsWith(`${normalizedPrefix}${path.sep}`)
            ) {
                await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
            }
        }
    }

    private async deleteArtifactRecordByIdAndPath(id: string, targetIniPath: string) {
        await this.desktop.lib.db
            .delete(toggleViewerArtifact)
            .where(
                and(
                    eq(toggleViewerArtifact.id, id),
                    eq(toggleViewerArtifact.targetIniPath, targetIniPath),
                ),
            );
        this.logInfo(`Removed stale toggle-viewer artifact record: ${targetIniPath}`);
    }

    private addLog(level: "INFO" | "ERROR", message: string) {
        const entry = `[${new Date().toISOString()}] [${level}] ${message}`;
        this.logs.push(entry);
        if (this.logs.length > 30) {
            this.logs = this.logs.slice(-30);
        }

        const mainWindow = this.desktop.window.main.window;
        if (mainWindow) {
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "setting:xxmi:toggleViewerLogs",
                this.getLogs(),
            );
        }
    }

    private logInfo(message: string) {
        this.desktop.logger.info(message, "ToggleViewer");
        this.addLog("INFO", message);
    }

    private logError(message: string) {
        this.desktop.logger.error(message, "ToggleViewer");
        this.addLog("ERROR", message);
    }
}

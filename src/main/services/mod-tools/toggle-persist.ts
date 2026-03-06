import path from "node:path";
import { debounce, retry } from "es-toolkit";
import fse from "fs-extra";
import type { NahidaDesktop } from "@/main";

export class TogglePersist {
    private static readonly modifierTokens = new Set([
        "ctrl",
        "shift",
        "alt",
        "no_ctrl",
        "no_shift",
        "no_alt",
    ]);
    private static readonly xboxTokens = new Set([
        "xb_left_trigger",
        "xb_right_trigger",
        "xb_left_shoulder",
        "xb_right_shoulder",
        "xb_left_thumb",
        "xb_right_thumb",
        "xb_dpad_up",
        "xb_dpad_down",
        "xb_dpad_left",
        "xb_dpad_right",
        "xb_a",
        "xb_b",
        "xb_x",
        "xb_y",
        "xb_start",
        "xb_back",
        "xb_guide",
    ]);

    private persistWatchers: string[] = [];
    private cachedD3dxUserIni: Map<string, Record<string, string>> = new Map();
    private persistLogs: string[] = [];
    private persistUpdateDebouncers: Map<string, () => void> = new Map();
    private persistFileUpdateLocks: Map<string, Promise<void>> = new Map();
    private pendingPersistUpdates: Map<
        string,
        { targetIniPath: string; varName: string; newValue: string }
    > = new Map();

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startPersistWatcher() {
        if (!this.desktop.service.xxmi) return;
        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();

        if (!xxmiPath || !xxmiConfig) return;

        const enabled = await this.desktop.setting.xxmi.getPersistToggles();
        if (!enabled) return;

        await this.stopPersistWatcher();

        const importers = this.desktop.service.xxmi.getEnabledImporters();
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
                this.logInfo(`Started watching ${d3dxPath} for persist updates`);
            }
        }
    }

    public async stopPersistWatcher() {
        const watcherCount = this.persistWatchers.length;
        for (const id of this.persistWatchers) {
            await this.desktop.lib.watcher.removeWatcher(id);
        }
        this.persistWatchers = [];
        this.cachedD3dxUserIni.clear();
        this.persistUpdateDebouncers.clear();
        this.persistFileUpdateLocks.clear();
        this.pendingPersistUpdates.clear();
        if (watcherCount > 0) {
            this.logInfo(`Stopped persist watcher (${watcherCount})`);
        }
    }

    public getPersistLogs() {
        return [...this.persistLogs];
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
                            this.queuePersistUpdate(targetIniPath, varName, newValue);
                        }
                    }
                }
            }

            this.cachedD3dxUserIni.set(importer.key, newParsed);
        } catch (error) {
            this.logError(`Error handling d3dx_user.ini change: ${error}`);
        }
    }

    private queuePersistUpdate(targetIniPath: string, varName: string, newValue: string) {
        const updateKey = `${targetIniPath}::${varName.toLowerCase()}`;
        this.pendingPersistUpdates.set(updateKey, { targetIniPath, varName, newValue });

        let debounced = this.persistUpdateDebouncers.get(updateKey);
        if (!debounced) {
            debounced = debounce(async () => {
                const pending = this.pendingPersistUpdates.get(updateKey);
                if (!pending) return;
                this.pendingPersistUpdates.delete(updateKey);
                await this.enqueuePersistFileUpdate(
                    pending.targetIniPath,
                    pending.varName,
                    pending.newValue,
                );
            }, 200);
            this.persistUpdateDebouncers.set(updateKey, debounced);
        }

        debounced();
    }

    private async enqueuePersistFileUpdate(
        targetIniPath: string,
        varName: string,
        newValue: string,
    ) {
        const lockKey = targetIniPath.toLowerCase();
        const previous = this.persistFileUpdateLocks.get(lockKey) ?? Promise.resolve();
        const next = previous
            .catch(() => {})
            .then(() => this.updateModIniPersist(targetIniPath, varName, newValue));
        this.persistFileUpdateLocks.set(lockKey, next);

        try {
            await next;
        } finally {
            if (this.persistFileUpdateLocks.get(lockKey) === next) {
                this.persistFileUpdateLocks.delete(lockKey);
            }
        }
    }

    private async updateModIniPersist(targetIniPath: string, varName: string, newValue: string) {
        try {
            const content = await fse.readFile(targetIniPath, "utf-8");
            const lineEnding = content.includes("\r\n") ? "\r\n" : "\n";
            const lines = content.split(/\r?\n/);
            // if (this.isAnimationPersistVariable(lines, varName)) {
            //     return;
            // }

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
                    const regex = new RegExp(
                        `^global\\s+persist\\s+\\$(${escapedVarName})\\s*=\\s*(.+)$`,
                        "i",
                    );
                    const match = trimmed.match(regex);
                    if (match) {
                        const currentValue = match[2].trim();
                        if (currentValue === newValue.trim()) {
                            break;
                        }
                        const existingVarName = match[1];
                        lines[i] = `global persist $${existingVarName} = ${newValue}`;
                        modified = true;
                        break;
                    }
                }
            }

            if (modified) {
                await fse.writeFile(targetIniPath, lines.join(lineEnding), "utf-8");
                this.logInfo(
                    `Updated persist variable $${varName} to ${newValue} in ${targetIniPath}`,
                );
            }
        } catch (error) {
            this.logError(`Error updating mod ini ${targetIniPath}: ${error}`);
        }
    }

    private isAnimationPersistVariable(lines: string[], varName: string): boolean {
        let inKeySection = false;
        let keyValue: string | null = null;
        const escapedVarName = varName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        const varRegex = new RegExp(`^\\$${escapedVarName}\\s*=`);

        const evaluateSection = (hasVarAssignment: boolean) => {
            if (!hasVarAssignment) return false;
            if (!keyValue) return true;
            return !this.isAllowedKeyBinding(keyValue);
        };

        let hasTargetVarAssignment = false;

        for (const rawLine of lines) {
            const trimmed = rawLine.trim();
            if (!trimmed || trimmed.startsWith(";")) continue;

            if (trimmed.startsWith("[")) {
                if (evaluateSection(hasTargetVarAssignment)) {
                    return true;
                }
                inKeySection = /^\[Key/i.test(trimmed);
                keyValue = null;
                hasTargetVarAssignment = false;
                continue;
            }

            if (!inKeySection) continue;

            const keyMatch = trimmed.match(/^key\s*=\s*(.+)$/i);
            if (keyMatch) {
                keyValue = keyMatch[1].split(";")[0].trim();
                continue;
            }

            if (varRegex.test(trimmed)) {
                hasTargetVarAssignment = true;
            }
        }

        return evaluateSection(hasTargetVarAssignment);
    }

    private isAllowedKeyBinding(keyValue: string): boolean {
        const tokens = keyValue
            .toLowerCase()
            .split(/[+\s]+/)
            .map((token) => token.trim())
            .filter(Boolean);

        if (tokens.length === 0) return false;
        return tokens.every((token) => this.isAllowedKeyToken(token));
    }

    private isAllowedKeyToken(token: string): boolean {
        if (token.length === 1) return true;
        if (TogglePersist.modifierTokens.has(token)) return true;
        if (TogglePersist.xboxTokens.has(token)) return true;
        if (/^vk_[a-z0-9_]+$/i.test(token)) return true;
        return false;
    }

    private addPersistLog(level: "INFO" | "ERROR", message: string) {
        const entry = `[${new Date().toISOString()}] [${level}] ${message}`;
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

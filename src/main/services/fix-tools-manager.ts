import crypto from "node:crypto";
import path from "node:path";
import { scriptPreset, scriptPresetItem, script as scriptTable } from "@main/internal/db/schema";
import { ScriptExecutor } from "@main/lib/script-executor";
import { eq } from "drizzle-orm";
import { sortBy } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

export class FixToolsManager {
    private currentAbortController: AbortController | null = null;
    private activeExecutor: ScriptExecutor | null = null;

    constructor(private desktop: NahidaDesktop) {}

    public get isRunning(): boolean {
        return this.activeExecutor !== null || this.currentAbortController !== null;
    }

    public async saveScript(inputPath: string) {
        if (!inputPath) throw new Error("Path is required");

        const fileExists = await fse.pathExists(inputPath);
        if (!fileExists) throw new Error("File does not exist");

        const fileName = path.basename(inputPath);
        const fileData = await fse.readFile(inputPath);
        const fileHash = crypto.createHash("sha256").update(fileData).digest("hex");

        const _script = await this.desktop.lib.db.query.script.findFirst({
            where: (t, { eq, or }) => or(eq(t.sha256, fileHash), eq(t.name, fileName)),
        });

        if (_script) {
            if (_script.sha256 === fileHash) throw new Error("Already exists same file");
            if (_script.name === fileName) throw new Error("Already exists same name");
        }

        const ext = path.extname(inputPath).toLowerCase();
        const fileType = ext === ".py" ? "python" : ext === ".exe" ? "exec" : null;

        if (!fileType) {
            throw new Error("Invalid file type (only .py or .exe allowed)");
        }

        await this.desktop.lib.db.insert(scriptTable).values({
            id: nanoid(),
            name: fileName,
            type: fileType,
            source: fileData,
            size: fileData.length,
            sha256: fileHash,
        });
    }

    public async deleteScript(scriptId: string) {
        const script = await this.desktop.lib.db.query.script.findFirst({
            where: eq(scriptTable.id, scriptId),
        });
        if (!script) throw new Error("Script not found");

        const usedInPresets = await this.desktop.lib.db.query.scriptPresetItem.findFirst({
            where: eq(scriptPresetItem.scriptId, scriptId),
            with: { preset: true },
        });
        if (usedInPresets) {
            throw new Error(`Script is used in a preset: ${usedInPresets.preset.name}`);
        }

        await this.desktop.lib.db.delete(scriptTable).where(eq(scriptTable.id, scriptId));
    }

    public async getScripts() {
        return await this.desktop.lib.db.query.script.findMany({
            columns: { id: true, name: true, type: true, size: true },
        });
    }

    public async getPresets() {
        return await this.desktop.lib.db.query.scriptPreset.findMany({
            columns: { id: true, name: true },
            with: { scripts: true },
        });
    }

    public async createPreset({ name, scriptIds }: { name: string; scriptIds: string[] }) {
        const trimmedName = name?.trim();
        if (!trimmedName) {
            throw new Error("Invalid preset name: name cannot be empty or only whitespace");
        }

        const nameConflict = await this.desktop.lib.db.query.scriptPreset.findFirst({
            where: eq(scriptPreset.name, trimmedName),
        });

        if (nameConflict) throw new Error("Preset with same name already exists");
        if (scriptIds.length === 0) throw new Error("No scripts selected");

        const presetId = nanoid();

        await this.desktop.lib.db.transaction(async (tx) => {
            await tx.insert(scriptPreset).values({ id: presetId, name: trimmedName });

            const presetItems = scriptIds.map((scriptId, index) => ({
                presetId: presetId,
                scriptId: scriptId,
                order: index,
            }));

            await tx.insert(scriptPresetItem).values(presetItems);
        });
    }

    public async deletePreset(presetId: string) {
        await this.desktop.lib.db.delete(scriptPreset).where(eq(scriptPreset.id, presetId));
    }

    public cancelRun() {
        if (this.currentAbortController) {
            this.currentAbortController.abort();
            this.desktop.ipc.broadcast("ftm:log", "Cancelled...");
        }
    }

    private prepareExecution(mainWindow: Electron.BrowserWindow) {
        if (this.isRunning) {
            throw new Error("Another process is running.");
        }

        this.currentAbortController = new AbortController();
        this.activeExecutor = new ScriptExecutor((msg) => {
            this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", msg);
        });

        return this.currentAbortController.signal;
    }

    private cleanupExecution() {
        this.currentAbortController = null;
        this.activeExecutor = null;
    }

    public async runScript(scriptId: string, destPath: string) {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) throw new Error("Main window not found");

        let prepared = false;
        try {
            const signal = this.prepareExecution(mainWindow);
            prepared = true;

            const _script = await this.desktop.lib.db.query.script.findFirst({
                where: eq(scriptTable.id, scriptId),
            });

            if (!_script) throw new Error("Script not found");
            if (!(await fse.pathExists(destPath))) {
                throw new Error("Destination path does not exist");
            }
            const stat = await fse.stat(destPath);
            if (!stat.isDirectory()) {
                throw new Error("Destination path is not a directory");
            }

            await this._runScriptSafe(_script, destPath, mainWindow, signal);
        } catch (e) {
            this.desktop.logger.error(e);
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Error: ${(e as Error).message}`,
            );
        } finally {
            if (prepared) {
                this.cleanupExecution();
            }
        }
    }

    public async runPreset(presetId: string, destPath: string) {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) throw new Error("Main window not found");

        let prepared = false;
        try {
            const signal = this.prepareExecution(mainWindow);
            prepared = true;

            const preset = await this.desktop.lib.db.query.scriptPreset.findFirst({
                where: eq(scriptPreset.id, presetId),
                with: { scripts: true },
            });

            if (!preset) throw new Error("Preset not found");
            if (preset.scripts.length === 0) throw new Error("Preset has no scripts");
            if (!(await fse.pathExists(destPath))) {
                throw new Error("Destination path does not exist");
            }
            const stat = await fse.stat(destPath);
            if (!stat.isDirectory()) {
                throw new Error("Destination path is not a directory");
            }

            const sortedItems = sortBy(preset.scripts, ["order"]);

            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Starting Preset: ${preset.name}`,
            );

            for (const item of sortedItems) {
                if (signal.aborted) {
                    this.desktop.ipc.postMessageToWindow(
                        mainWindow,
                        "ftm:log",
                        `Preset execution aborted by user.`,
                    );
                    break;
                }

                const _script = await this.desktop.lib.db.query.script.findFirst({
                    where: eq(scriptTable.id, item.scriptId),
                });

                if (!_script) {
                    this.desktop.ipc.postMessageToWindow(
                        mainWindow,
                        "ftm:log",
                        `Script not found (ID: ${item.scriptId}), skipping...`,
                    );
                    continue;
                }

                await this._runScriptSafe(_script, destPath, mainWindow, signal);
            }

            if (!signal.aborted) {
                this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", `Preset Completed`);
            }
        } catch (e) {
            this.desktop.logger.error(e);
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Error: ${(e as Error).message}`,
            );
        } finally {
            if (prepared) {
                this.cleanupExecution();
            }
        }
    }

    public sendInput(input: string) {
        if (this.activeExecutor?.isRunning()) {
            this.activeExecutor.sendInput(input);
            this.desktop.logger.info(`Sent input: ${JSON.stringify(input)}`, "FixToolsManager");
        } else {
            this.desktop.logger.warn(
                "Cannot send input: No active script running",
                "FixToolsManager",
            );
        }
    }

    private async _runScriptSafe(
        script: typeof scriptTable.$inferSelect,
        destPath: string,
        mainWindow: Electron.BrowserWindow,
        signal: AbortSignal,
    ): Promise<boolean> {
        if (!this.activeExecutor) return false;

        const now = new Date();
        const tempFileName = `${script.sha256}-${now.getTime()}.${script.type === "python" ? "py" : "exe"}`;
        const scriptPath = path.join(destPath, tempFileName);

        try {
            await fse.writeFile(scriptPath, script.source);

            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Running ${script.name}...`,
            );

            await this.activeExecutor.execute(
                scriptPath,
                script.type as "python" | "exec",
                destPath,
                signal,
            );

            this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", `Completed ${script.name}`);
            return true;
        } catch (e) {
            const errorMessage = (e as Error).message;
            if (errorMessage === "Aborted") {
                this.desktop.ipc.postMessageToWindow(
                    mainWindow,
                    "ftm:log",
                    `Cancelled ${script.name}`,
                );
            } else {
                this.desktop.ipc.postMessageToWindow(
                    mainWindow,
                    "ftm:log",
                    `Failed ${script.name}: ${errorMessage}`,
                );
            }
            return false;
        } finally {
            await fse.remove(scriptPath).catch((err) => {
                this.desktop.logger.error(`Failed to cleanup temp file: ${err}`, "FixToolsManager");
            });
        }
    }
}

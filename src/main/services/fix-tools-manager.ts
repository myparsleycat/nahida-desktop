import crypto from "node:crypto";
import path from "node:path";
import { fixTool, fixToolPreset, fixToolPresetItem } from "@main/internal/db/schema";
import { ToolExecutor } from "@main/lib/tool-executor";
import { eq } from "drizzle-orm";
import { sortBy } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

export class FixToolsManager {
    private currentAbortController: AbortController | null = null;
    private activeExecutor: ToolExecutor | null = null;

    constructor(private desktop: NahidaDesktop) {}

    public async saveTool(inputPath: string) {
        if (!inputPath) {
            throw new Error("path is required");
        }

        const fileExists = await fse.pathExists(inputPath);
        if (!fileExists) {
            throw new Error("file does not exist");
        }

        const fileName = path.basename(inputPath);
        const fileData = await fse.readFile(inputPath);
        const fileHash = crypto.createHash("sha256").update(fileData).digest("hex");

        const tool = await this.desktop.lib.db.query.fixTool.findFirst({
            where: (t, { eq, or }) => or(eq(t.sha256, fileHash), eq(t.name, fileName)),
        });

        if (tool && tool.sha256 === fileHash) {
            throw new Error("already exists same file");
        } else if (tool && tool.name === fileName) {
            throw new Error("already exists same name");
        }

        const ext = path.extname(inputPath).toLowerCase();
        const toolType = ext === ".py" ? "python" : ext === ".exe" ? "exec" : "";
        if (toolType !== "python" && toolType !== "exec") {
            throw new Error("invalid file type");
        }

        await this.desktop.lib.db.insert(fixTool).values({
            id: nanoid(),
            name: fileName,
            type: toolType,
            source: fileData,
            size: fileData.length,
            sha256: fileHash,
        });
    }

    public async deleteTool(toolId: string) {
        await this.desktop.lib.db.delete(fixTool).where(eq(fixTool.id, toolId));
    }

    public async getTools() {
        return await this.desktop.lib.db.query.fixTool.findMany({
            columns: { id: true, name: true, type: true, size: true },
        });
    }

    public async getPresets() {
        return await this.desktop.lib.db.query.fixToolPreset.findMany({
            columns: { id: true, name: true },
            with: { tools: true },
        });
    }

    public async createPreset({ name, toolIds }: { name: string; toolIds: string[] }) {
        const nameConflict = await this.desktop.lib.db.query.fixToolPreset.findFirst({
            where: eq(fixToolPreset.name, name),
        });

        if (nameConflict) {
            throw new Error("preset with same name already exists");
        }

        const presetId = nanoid();

        await this.desktop.lib.db.insert(fixToolPreset).values({
            id: presetId,
            name,
        });

        if (toolIds.length > 0) {
            const presetItems = toolIds.map((toolId, index) => ({
                presetId: presetId,
                toolId: toolId,
                order: index,
            }));

            await this.desktop.lib.db.insert(fixToolPresetItem).values(presetItems);
        } else {
            throw new Error("no tools selected");
        }
    }

    public async deletePreset(presetId: string) {
        await this.desktop.lib.db.delete(fixToolPreset).where(eq(fixToolPreset.id, presetId));
    }

    public cancelRun() {
        if (this.currentAbortController) {
            this.currentAbortController.abort();
            this.currentAbortController = null;
            this.desktop.ipc.broadcast("ftm:log", "작업이 취소되었습니다.");
        }
    }

    public async runFixTool(toolId: string, destPath: string) {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) {
            throw new Error("main window not found");
        }

        try {
            this.currentAbortController = new AbortController();
            const signal = this.currentAbortController.signal;

            this.activeExecutor = new ToolExecutor((msg) => {
                this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", msg);
            });

            const tool = await this.desktop.lib.db.query.fixTool.findFirst({
                where: eq(fixTool.id, toolId),
            });

            if (!tool) {
                throw new Error("tool not found");
            }

            const pathExists = await fse.pathExists(destPath);
            if (!pathExists) {
                throw new Error("destination path does not exist");
            }

            await this._runToolSafe(tool, destPath, mainWindow, signal);
        } catch (e) {
            this.desktop.logger.error(e);
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Error: ${(e as Error).message}`,
            );
        } finally {
            this.currentAbortController = null;
            this.activeExecutor = null;
        }
    }

    public async runPreset(presetId: string, destPath: string) {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) {
            throw new Error("main window not found");
        }

        try {
            this.currentAbortController = new AbortController();
            const signal = this.currentAbortController.signal;

            this.activeExecutor = new ToolExecutor((msg) => {
                this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", msg);
            });

            const preset = await this.desktop.lib.db.query.fixToolPreset.findFirst({
                where: eq(fixToolPreset.id, presetId),
                with: {
                    tools: true,
                },
            });

            if (!preset) {
                throw new Error("preset not found");
            } else if (preset.tools.length === 0) {
                throw new Error("preset has no tools");
            }

            const pathExists = await fse.pathExists(destPath);
            if (!pathExists) {
                throw new Error("destination path does not exist");
            }

            const sortedItems = sortBy(preset.tools, ["order"]);

            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Starting Preset: ${preset.name}`,
            );

            for (const item of sortedItems) {
                if (signal.aborted) break;

                const tool = await this.desktop.lib.db.query.fixTool.findFirst({
                    where: eq(fixTool.id, item.toolId),
                });

                if (!tool) {
                    this.desktop.ipc.postMessageToWindow(
                        mainWindow,
                        "ftm:log",
                        `Tool not found (ID: ${item.toolId}), skipping...`,
                    );
                    continue;
                }

                await this._runToolSafe(tool, destPath, mainWindow, signal);
            }

            this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", "Preset Completed");
        } catch (e) {
            this.desktop.logger.error(e);
            if ((e as Error).message !== "Aborted") {
                this.desktop.ipc.postMessageToWindow(
                    mainWindow,
                    "ftm:log",
                    `Error: ${(e as Error).message}`,
                );
            }
            throw e;
        } finally {
            this.currentAbortController = null;
            this.activeExecutor = null;
        }
    }

    public sendInput(input: string) {
        if (this.activeExecutor?.isRunning()) {
            this.activeExecutor.sendInput(input);
            this.desktop.logger.info(
                `Sent input to tool: ${JSON.stringify(input)}`,
                "FixToolsManager",
            );
        }
    }

    private async _runToolSafe(
        tool: typeof fixTool.$inferSelect,
        destPath: string,
        mainWindow: Electron.BrowserWindow,
        signal: AbortSignal,
    ) {
        if (!this.activeExecutor) return;

        const now = new Date();
        const tempFileName = `${tool.sha256}-${now.getTime()}.${tool.type === "python" ? "py" : "exe"}`;
        const toolPath = path.join(destPath, tempFileName);

        try {
            await fse.writeFile(toolPath, tool.source);

            this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", `Running ${tool.name}...`);

            await this.activeExecutor.execute(
                toolPath,
                tool.type as "python" | "exec",
                destPath,
                signal,
            );

            this.desktop.ipc.postMessageToWindow(mainWindow, "ftm:log", `Completed ${tool.name}`);
        } catch (e) {
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "ftm:log",
                `Failed ${tool.name}: ${(e as Error).message}`,
            );
        } finally {
            await fse.remove(toolPath).catch(() => {});
        }
    }
}

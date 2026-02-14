import crypto from "node:crypto";
import path from "node:path";
import { fixTool, fixToolPreset, fixToolPresetItem } from "@main/internal/db/schema";
import { eq } from "drizzle-orm";
import { sortBy } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

export class FixToolsManager {
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
            where: (t, { eq, or }) => or(eq(t.sha256, fileHash), eq(t.source, inputPath)),
        });

        if (tool && tool.sha256 === fileHash) {
            throw new Error("already exists same file");
        } else if (tool && tool.name === fileName) {
            throw new Error("already exists same name");
        }

        const toolType = path.extname(inputPath).toLowerCase() === ".py" ? "python" : "batch";
        if (toolType !== "python") {
            throw new Error("invalid file type");
        }

        await this.desktop.lib.db.insert(fixTool).values({
            id: nanoid(),
            name: fileName,
            type: toolType,
            source: fileData.toString(),
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

    public async runPreset(presetId: string, destPath: string) {
        const preset = await this.desktop.lib.db.query.fixToolPreset.findFirst({
            where: eq(fixToolPreset.id, presetId),
            with: {
                tools: {
                    with: { tool: true },
                },
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

        const sortedTools = sortBy(preset.tools, ["order"]).map((t) => t.tool);

        for (const tool of sortedTools) {
            const now = new Date();
            const tempFileName = `${tool.sha256}-${now.getTime()}.${tool.type}`;
            const toolPath = path.join(destPath, tempFileName);

            await fse.writeFile(toolPath, tool.source);
        }
    }
}

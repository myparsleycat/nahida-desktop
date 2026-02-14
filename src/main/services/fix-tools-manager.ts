import { db } from "@main/internal/db";
import { fixToolPreset, fixToolPresetItem } from "@main/internal/db/schema";
import { eq } from "drizzle-orm";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

export class FixToolsManager {
    constructor(private desktop: NahidaDesktop) {}

    public async getTools() {
        return await db.query.fixTool.findMany({
            columns: { id: true, name: true, type: true, size: true },
        });
    }

    public async getPresets() {
        return await db.query.fixToolPreset.findMany({
            columns: { id: true, name: true },
            with: { tools: true },
        });
    }

    public async createPreset({ name, toolIds }: { name: string; toolIds: string[] }) {
        return await db.transaction(async (tx) => {
            const presetId = nanoid();

            await tx.insert(fixToolPreset).values({
                id: presetId,
                name,
            });

            if (toolIds.length > 0) {
                const presetItems = toolIds.map((toolId, index) => ({
                    presetId: presetId,
                    toolId: toolId,
                    order: index,
                }));

                await tx.insert(fixToolPresetItem).values(presetItems);
            }
        });
    }

    public async deletePreset(presetId: string) {
        await db.delete(fixToolPreset).where(eq(fixToolPreset.id, presetId));
    }
}

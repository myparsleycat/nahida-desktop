import type { ScriptBasicRow, ScriptPresetWithScripts } from "@bindings/db";
import { Tools } from "@bindings/tools";

export type FixToolScript = {
    id: string;
    name: string;
    type: string;
    size: number;
};

export type FixToolPreset = {
    id: string;
    name: string;
    scripts: Array<{ order: number; presetId: string; scriptId: string }>;
};

export function mapFixToolScripts(scripts: ScriptBasicRow[] | null | undefined): FixToolScript[] {
    return (scripts ?? []).map((script) => ({
        id: script.ID,
        name: script.Name,
        size: script.Size,
        type: script.Type,
    }));
}

export function mapFixToolPresets(
    presets: ScriptPresetWithScripts[] | null | undefined,
): FixToolPreset[] {
    return (presets ?? []).map((preset) => ({
        id: preset.ID,
        name: preset.Name,
        scripts: (preset.Scripts ?? []).map((script) => ({
            order: script.Order,
            presetId: script.PresetID,
            scriptId: script.ScriptID,
        })),
    }));
}

export async function getFixToolScripts(): Promise<FixToolScript[]> {
    return mapFixToolScripts(await Tools.GetScripts());
}

export async function getFixToolPresets(): Promise<FixToolPreset[]> {
    return mapFixToolPresets(await Tools.GetPresets());
}

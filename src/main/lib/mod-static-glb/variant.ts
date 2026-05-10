import path from "node:path";
import type { Logger } from "../../internal/logger";
import { detectPresentAnimations } from "./animation";
import { loadIniBundle } from "./ini-loader";
import * as overrideAnalysis from "./override-analysis";
import { collectResources } from "./resource-loader";
import { collectRealtimeShapeKeys } from "./shape-key";
import { humanizeVariableLabel, mapToRecord, normalizeKey } from "./shared";
import type {
    IniSection,
    SlotVariableBinding,
    StaticGlbVariantSlider,
    StaticGlbVariantVariable,
    VariableStateValue,
} from "./types";
import {
    collectViewerUiAssetPaths,
    deriveVariableUiToken,
    findFirstResourcePath,
} from "./ui-asset";

export async function analyzeModVariants(options: {
    modPath: string;
    assetPath?: string;
    logger?: Logger;
    onWarning?: (message: string) => void;
}) {
    const { iniPath, sections } = await loadIniBundle(options.modPath);
    const modDir = path.dirname(iniPath);
    const defaultVariables = overrideAnalysis.collectDefaultIniVariables(sections);
    const slotBindings = collectSlotVariableBindings(sections, defaultVariables);
    const resources = collectResources(sections);
    const shapeKeys = collectRealtimeShapeKeys(sections, resources, modDir);
    const animations = detectPresentAnimations(sections, defaultVariables, slotBindings);
    const realtimeShapeKeyVariableIds = new Set(
        shapeKeys.flatMap((shapeKey) =>
            shapeKey.dimensions.map((dimension) => normalizeKey(dimension.variableId)),
        ),
    );
    const variables = (
        await buildVariantVariables(slotBindings, sections, modDir, {
            ...options,
            realtimeShapeKeyVariableIds,
        })
    ).map((variable) => ({
        ...variable,
        defaultValue: defaultVariables.get(normalizeKey(variable.id)) ?? 0,
    }));

    return {
        iniPath,
        defaultState: mapToRecord(
            defaultVariables,
            variables.map((variable) => variable.id),
        ),
        variables,
        uiAssets: collectViewerUiAssetPaths(sections),
        shapeKeys,
        animations,
    };
}

export function collectSlotVariableBindings(
    sections: IniSection[],
    defaultVariables: Map<string, number | string>,
): SlotVariableBinding[] {
    const bindings: SlotVariableBinding[] = [
        ...collectKeyCycleBindings(sections),
        ...collectButtonAmountBindings(sections),
    ];
    const clickedSection = sections.find(
        (section) =>
            section.header === "CommandList" &&
            normalizeKey(section.name) === normalizeKey("ClickedSlot"),
    );
    if (!clickedSection) {
        return dedupeSlotBindings(bindings);
    }

    let currentSlot: number | null = null;

    for (const rawLine of clickedSection.lines) {
        const trimmed = rawLine.trim();
        const slotMatch = trimmed.match(/^(?:if|elif)\s+\$clickedslot\s*==\s*(\d+)$/i);
        if (slotMatch) {
            currentSlot = Number(slotMatch[1]);
            continue;
        }

        if (trimmed.toLowerCase() === "endif") {
            currentSlot = null;
            continue;
        }

        if (currentSlot === null) continue;

        const toggleMatch = trimmed.match(/^\$([\w.]+)\s*=\s*1\s*-\s*\$\1$/i);
        if (toggleMatch) {
            bindings.push({
                slot: currentSlot,
                variable: normalizeKey(toggleMatch[1]),
                values: [0, 1],
            });
            currentSlot = null;
            continue;
        }

        const incrementMatch = trimmed.match(/^\$([\w.]+)\s*=\s*\$\1\s*\+\s*1$/i);
        if (!incrementMatch) continue;

        const variable = normalizeKey(incrementMatch[1]);
        const defaultValue = Number(defaultVariables.get(variable) ?? 0);
        const variablePattern = escapeRegex(variable);
        const assignmentPattern = new RegExp(`^\\s*\\$${variablePattern}\\s*=\\s*(\\d+)\\s*$`, "i");
        const conditionPattern = new RegExp(
            `^\\s*(?:if|elif)\\s+\\$${variablePattern}\\s*>\\s*(\\d+)\\s*$`,
            "i",
        );
        const assignedValues = clickedSection.lines
            .map((line) => line.match(assignmentPattern))
            .filter((match): match is RegExpMatchArray => match !== null)
            .map((match) => Number(match[1]));
        const conditionalValues = clickedSection.lines
            .map((line) => line.match(conditionPattern))
            .filter((match): match is RegExpMatchArray => match !== null)
            .map((match) => Number(match[1]));
        const maxValue = Math.max(defaultValue, ...assignedValues, ...conditionalValues);
        const values = Array.from({ length: maxValue + 1 }, (_, index) => index);
        bindings.push({
            slot: currentSlot,
            variable,
            values,
        });
        currentSlot = null;
    }

    return dedupeSlotBindings(bindings);
}

function collectButtonAmountBindings(sections: IniSection[]): SlotVariableBinding[] {
    const bindings: SlotVariableBinding[] = [];

    for (const section of sections) {
        if (section.header !== "CommandList") {
            continue;
        }

        let currentSlot: number | null = null;
        for (const rawLine of section.lines) {
            const trimmed = rawLine.trim();
            const slotMatch = trimmed.match(/^if\s+\$button_amount\s*>=\s*(\d+)$/i);
            if (slotMatch) {
                currentSlot = Number(slotMatch[1]);
                continue;
            }

            if (currentSlot === null) {
                continue;
            }

            const cycleMatch = trimmed.match(/^if\s+\$([\w.]+)\s*<\s*(-?\d+(?:\.\d+)?)$/i);
            if (!cycleMatch) {
                continue;
            }

            const maxValue = Number(cycleMatch[2]);
            if (!Number.isFinite(maxValue)) {
                continue;
            }

            bindings.push({
                slot: currentSlot,
                variable: normalizeKey(cycleMatch[1]),
                values: Array.from(
                    { length: Math.max(0, Math.floor(maxValue)) + 1 },
                    (_, index) => index,
                ),
            });
            currentSlot = null;
        }
    }

    return bindings;
}

function collectKeyCycleBindings(sections: IniSection[]): SlotVariableBinding[] {
    return sections
        .filter((section) => normalizeKey(section.header).startsWith("key"))
        .flatMap((section, index) => {
            const type = section.values.type?.toLowerCase();
            if (type !== "cycle") {
                return [];
            }

            return Object.entries(section.values)
                .filter(([key, value]) => key.startsWith("$") && value.includes(","))
                .map(([key, value]) => ({
                    slot: index + 1,
                    variable: normalizeKey(key),
                    values: value
                        .split(",")
                        .map((entry) => entry.trim())
                        .filter((entry) => entry !== "")
                        .map((entry) => overrideAnalysis.parseIniScalar(entry)),
                }));
        });
}

function dedupeSlotBindings(bindings: SlotVariableBinding[]): SlotVariableBinding[] {
    const seen = new Set<string>();
    return bindings.filter((binding) => {
        const key = `${binding.slot}:${binding.variable}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

export async function buildVariantVariables(
    bindings: SlotVariableBinding[],
    sections: IniSection[],
    modDir: string,
    options: {
        logger?: Logger;
        onWarning?: (message: string) => void;
        realtimeShapeKeyVariableIds?: Set<string>;
    },
): Promise<StaticGlbVariantVariable[]> {
    const resourceMap = new Map(
        sections
            .filter((section) => section.header === "Resource" && !!section.values.filename)
            .map((section) => [normalizeKey(section.name), section.values.filename]),
    );

    const variables: StaticGlbVariantVariable[] = [];

    for (const binding of mergeBindingsByVariable(bindings)) {
        const iconResource = findFirstResourcePath(resourceMap, [
            `MenuItem.${binding.slot}`,
            `MenuItem.${deriveVariableUiToken(binding.variable)}`,
            `Button_${binding.slot - 1}`,
            `Button_${binding.slot}`,
        ]);
        const slider = inferSliderConfig(
            binding.variable,
            binding.values,
            options.realtimeShapeKeyVariableIds?.has(normalizeKey(binding.variable)) ?? false,
        );
        variables.push({
            id: binding.variable,
            label: resolveVariantVariableLabel(binding.variable, iconResource),
            defaultValue: 0,
            values: binding.values.map((value) => ({
                value,
                label: String(value),
            })),
            order: binding.slot,
            slot: binding.slot,
            iconPath: iconResource ? path.resolve(modDir, iconResource) : undefined,
            controlType: slider ? "slider" : "buttons",
            slider,
        });
    }

    return variables;
}

function resolveVariantVariableLabel(variableId: string, iconResource?: string): string {
    if (iconResource) {
        const stem = path.basename(iconResource, path.extname(iconResource));
        if (!/^(?:button|icon|item)[._-]?\d+$/i.test(stem)) {
            return humanizeVariableLabel(stem);
        }
    }

    return humanizeVariableLabel(variableId);
}

function mergeBindingsByVariable(bindings: SlotVariableBinding[]): SlotVariableBinding[] {
    const merged = new Map<string, SlotVariableBinding>();

    for (const binding of bindings.sort((a, b) => a.slot - b.slot)) {
        const existing = merged.get(binding.variable);
        if (!existing) {
            merged.set(binding.variable, {
                slot: binding.slot,
                variable: binding.variable,
                values: [...binding.values],
            });
            continue;
        }

        existing.slot = Math.min(existing.slot, binding.slot);
        existing.values = mergeVariableValues(existing.values, binding.values);
    }

    return Array.from(merged.values()).sort((a, b) => a.slot - b.slot);
}

function mergeVariableValues(
    left: VariableStateValue[],
    right: VariableStateValue[],
): VariableStateValue[] {
    const merged: VariableStateValue[] = [];
    const seen = new Set<string>();

    for (const value of [...left, ...right]) {
        const key = String(value);
        if (seen.has(key)) {
            continue;
        }
        seen.add(key);
        merged.push(value);
    }

    if (merged.every((value) => typeof value === "number")) {
        return [...merged].sort((a, b) => Number(a) - Number(b));
    }

    return merged;
}

function inferSliderConfig(
    variableId: string,
    values: VariableStateValue[],
    forceNumericSlider = false,
): StaticGlbVariantSlider | undefined {
    const token = deriveVariableUiToken(variableId).toLowerCase();
    if (!forceNumericSlider && !token.startsWith("slider")) {
        return undefined;
    }

    const numericValues = values.filter((value): value is number => typeof value === "number");
    if (numericValues.length < 3 || numericValues.length !== values.length) {
        return undefined;
    }

    const sorted = [...numericValues].sort((a, b) => a - b);
    const steps = sorted
        .slice(1)
        .map((value, index) => Number((value - sorted[index]).toFixed(6)))
        .filter((step) => step > 0);
    const step = steps.length > 0 ? Math.min(...steps) : 1;

    return {
        min: sorted[0] ?? 0,
        max: sorted[sorted.length - 1] ?? 0,
        step,
    };
}

function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

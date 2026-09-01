/**
 * Strict command generation is derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
import type { MenuMakerHandler } from "@bindings/menumaker";

import type { MenuMakerSettings, MenuMakerSlot } from "./types";

import { normalizeMenuMakerKey } from "./parser";

export function calculateMenuMakerPreviewScale(settings: MenuMakerSettings): number {
    return Math.max(
        0.25,
        Math.min(
            3,
            1920 / Math.max(1, settings.baseWidth),
            1080 / Math.max(1, settings.baseHeight),
        ),
    );
}

export function menuMakerColumnCount(columns: number): number {
    return Math.max(1, columns || 3);
}

export function moveMenuMakerSlot(
    slots: MenuMakerSlot[],
    fromId: string,
    toId: string,
): MenuMakerSlot[] {
    const fromIndex = slots.findIndex((slot) => slot.id === fromId);
    const toIndex = slots.findIndex((slot) => slot.id === toId);
    if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return slots;
    const next = [...slots];
    const [item] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, item);
    return next;
}

export function mergeMenuMakerSlots(
    slots: MenuMakerSlot[],
    selectedIds: string[],
    mode: "allKeys" | "guiOnly",
): MenuMakerSlot[] {
    const selected = slots.filter((slot) => selectedIds.includes(slot.id));
    if (selected.length < 2) return slots;
    const primary = selected[0];
    const merged: MenuMakerSlot = {
        ...primary,
        id: `merged-${selected.map((slot) => slot.id).join("-")}`,
        handlers: uniqueHandlers(selected),
        originalKeys: uniqueNormalizedKeys(selected.flatMap((slot) => slot.originalKeys ?? [])),
        name: selected.map((slot) => slot.name).join(" + "),
        mergeMode: mode,
    };
    return slots.flatMap((slot) => {
        if (slot.id === primary.id) return [merged];
        return selectedIds.includes(slot.id) ? [] : [slot];
    });
}

export function canSplitMenuMakerSlot(slot: MenuMakerSlot): boolean {
    return (slot.handlers ?? []).length <= 1;
}

function uniqueHandlers(slots: MenuMakerSlot[]): MenuMakerHandler[] {
    const seen = new Set<number>();
    return slots.flatMap((slot) =>
        (slot.handlers ?? []).filter((handler) => {
            if (seen.has(handler.sourceIndex)) return false;
            seen.add(handler.sourceIndex);
            return true;
        }),
    );
}

function uniqueNormalizedKeys(values: string[]): string[] {
    const seen = new Set<string>();
    return values.filter((value) => {
        const normalized = normalizeMenuMakerKey(value);
        if (seen.has(normalized)) return false;
        seen.add(normalized);
        return true;
    });
}

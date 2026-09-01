/**
 * Strict key mapping behavior is derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
import type { MenuMakerHandler, MenuMakerSlot as BoundSlot } from "@bindings/menumaker";

import type { MenuMakerIcon, MenuMakerSlot } from "./types";

const ICON_RULES: [RegExp, string, string][] = [
    [/hat|cap|crown/i, "crown", "#f6be4a"],
    [/hair|bang|wig/i, "scissors", "#c98c4a"],
    [/face|mask|head/i, "smile", "#ff8fb3"],
    [/eye|iris|pupil/i, "eye", "#74d6ff"],
    [/glass|spect/i, "glasses", "#74d6ff"],
    [/dress|skirt|cloth|outfit|costume|body|shirt/i, "shirt", "#ff8fb3"],
    [/weapon|sword|blade|gun/i, "sword", "#dddddd"],
    [/color|skin|tone|tint/i, "palette", "#ff4fb3"],
    [/animation|anim|fx/i, "sparkles", "#f6be4a"],
];

export function normalizeMenuMakerKey(key: string): string {
    return key
        .trim()
        .replace(/\s+/g, " ")
        .replace(/\bno_modifiers\b/gi, "")
        .replace(/\s+/g, " ")
        .trim()
        .toLowerCase();
}

export function updateSlotKey(slot: MenuMakerSlot, key: string): MenuMakerSlot {
    if ((slot.handlers ?? []).some((handler) => (handler.keys ?? []).length > 1)) {
        throw new Error("MENU_MAKER_MULTI_KEY_HANDLER");
    }
    const handlers = (slot.handlers ?? []).map((handler) => ({ ...handler, key, keys: [key] }));
    return { ...slot, key, handlers };
}

export function slotSignature(slots: BoundSlot[]): string {
    return JSON.stringify(
        slots.map((slot) => ({
            key: normalizeMenuMakerKey(slot.key),
            handlers: (slot.handlers ?? []).map((handler) => ({
                section: handler.section.toLowerCase(),
                sourceIndex: handler.sourceIndex,
                condition: handler.condition.trim(),
                variables: assignmentVariables(handler),
            })),
        })),
    );
}

export function suggestMenuMakerSlotIcon(slot: Pick<BoundSlot, "handlers">): MenuMakerIcon {
    const searchable = (slot.handlers ?? [])
        .flatMap((handler) => [
            handler.section,
            ...assignmentVariables(handler),
            ...(handler.commandLists ?? []),
        ])
        .join(" ");
    const icon = ICON_RULES.find(([rule]) => rule.test(searchable));
    return {
        kind: "lucide",
        name: icon?.[1] ?? "circle-dot",
        color: icon?.[2] ?? "#ff4fb3",
    };
}

export function withSuggestedIcons(slots: BoundSlot[] | null | undefined): MenuMakerSlot[] {
    return (slots ?? []).map((slot) => ({
        ...slot,
        icon: suggestMenuMakerSlotIcon(slot),
    }));
}

function assignmentVariables(handler: MenuMakerHandler): string[] {
    return (handler.assignments ?? [])
        .map((entry) => entry.variable)
        .filter((variable): variable is string => Boolean(variable))
        .map((variable) => variable.toLowerCase());
}

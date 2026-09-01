import { describe, expect, it } from "vitest";

import { coveredImageDrawRect, MENU_MAKER_CROP_PREVIEW_SIZE } from "./crop";
import {
    canSplitMenuMakerSlot,
    calculateMenuMakerPreviewScale,
    menuMakerColumnCount,
    mergeMenuMakerSlots,
    moveMenuMakerSlot,
} from "./generator";
import { normalizeMenuMakerKey, updateSlotKey } from "./parser";
import {
    DEFAULT_MENU_MAKER_SETTINGS,
    MENU_MAKER_BASE_SLOT_SIZE,
    type MenuMakerSlot,
} from "./types";

function slot(
    id: string,
    key: string,
    handlerCount = 1,
    extraKeys: string[] = [],
    sourceIndex = 0,
): MenuMakerSlot {
    return {
        id,
        key,
        originalKeys: [key, ...extraKeys],
        name: id,
        skip: false,
        mergeMode: "strict",
        icon: { kind: "lucide", name: "circle-dot", color: "#ff4fb3" },
        handlers: Array.from({ length: handlerCount }, (_, index) => ({
            id: `${id}-${index}`,
            section: `Key${id}`,
            sourceIndex: sourceIndex + index,
            keys: extraKeys.length ? [key, ...extraKeys] : [key],
            key,
            condition: "",
            type: "cycle",
            back: "",
            wrap: true,
            entries: [],
            assignments: [],
            commandLists: [],
            rawEntries: [],
            steps: 0,
            commandName: `CommandListCycle${id}`,
            backCommandName: `CommandListCycle${id}Back`,
            activateCommandName: `CommandListActivate${id}`,
            stepVar: `$ks_step_${id}`,
            activatePulseVar: `$gui_activate_pulse_${id}`,
        })),
    };
}

describe("Menu Maker editor helpers", () => {
    it("normalizes no_modifiers without touching no_modifier", () => {
        expect(normalizeMenuMakerKey(" no_modifiers   5 ")).toBe("5");
        expect(normalizeMenuMakerKey("no_modifier 5")).toBe("no_modifier 5");
    });

    it("rejects splitting strict multi-handler slots and updates every handler key", () => {
        const multi = slot("swap", "5", 2);
        expect(canSplitMenuMakerSlot(multi)).toBe(false);
        const updated = updateSlotKey(multi, "7");
        expect(updated.handlers?.every((handler) => handler.keys?.[0] === "7")).toBe(true);
    });

    it("rejects key edits when a handler already has multiple keys", () => {
        const multiKey = slot("swap", "5", 1, ["6"]);
        expect(() => updateSlotKey(multiKey, "7")).toThrow("MENU_MAKER_MULTI_KEY_HANDLER");
    });
});

describe("Menu Maker slot layout helpers", () => {
    it("scales the workspace preview from both reference resolution dimensions", () => {
        expect(calculateMenuMakerPreviewScale(DEFAULT_MENU_MAKER_SETTINGS)).toBe(1);
        expect(
            calculateMenuMakerPreviewScale({
                ...DEFAULT_MENU_MAKER_SETTINGS,
                baseWidth: 3840,
                baseHeight: 2160,
            }),
        ).toBe(0.5);
        expect(
            calculateMenuMakerPreviewScale({
                ...DEFAULT_MENU_MAKER_SETTINGS,
                baseWidth: 3440,
                baseHeight: 1080,
            }),
        ).toBeCloseTo(1920 / 3440);
    });

    it("defaults unset columns to three like backend geometry", () => {
        expect(menuMakerColumnCount(0)).toBe(3);
        expect(menuMakerColumnCount(Number.NaN)).toBe(3);
        expect(menuMakerColumnCount(-2)).toBe(1);
        expect(menuMakerColumnCount(4)).toBe(4);
    });

    it("moves the dragged slot to the drop target index", () => {
        const [first, second, third] = [slot("a", "1"), slot("b", "2"), slot("c", "3")];
        const slots = [first, second, third];
        expect(moveMenuMakerSlot(slots, first.id, third.id).map((item) => item.id)).toEqual([
            second.id,
            third.id,
            first.id,
        ]);
        expect(moveMenuMakerSlot(slots, third.id, first.id).map((item) => item.id)).toEqual([
            third.id,
            first.id,
            second.id,
        ]);
        expect(moveMenuMakerSlot(slots, first.id, first.id)).toBe(slots);
    });

    it("merges selected slots and keeps unique handlers", () => {
        const slots = [
            slot("a", "1", 1, [], 0),
            slot("b", "2", 1, [], 1),
            slot("c", "3", 1, [], 2),
        ];
        const merged = mergeMenuMakerSlots(
            slots,
            slots.map((item) => item.id),
            "allKeys",
        );
        expect(merged).toHaveLength(1);
        expect(merged[0].name).toBe("a + b + c");
        expect(merged[0].mergeMode).toBe("allKeys");
        expect(merged[0].handlers).toHaveLength(3);
    });
});

describe("Menu Maker crop framing", () => {
    it("keeps export drawing proportional to the preview canvas", () => {
        const offset = { x: 24, y: -18 };
        const preview = coveredImageDrawRect(800, 400, MENU_MAKER_CROP_PREVIEW_SIZE, 2, offset);
        const exported = coveredImageDrawRect(800, 400, MENU_MAKER_BASE_SLOT_SIZE, 2, offset);
        const scale = MENU_MAKER_BASE_SLOT_SIZE / MENU_MAKER_CROP_PREVIEW_SIZE;
        expect(exported.x).toBeCloseTo(preview.x * scale);
        expect(exported.y).toBeCloseTo(preview.y * scale);
        expect(exported.width).toBeCloseTo(preview.width * scale);
        expect(exported.height).toBeCloseTo(preview.height * scale);
        const scaledExport = coveredImageDrawRect(
            800,
            400,
            MENU_MAKER_BASE_SLOT_SIZE * 2,
            2,
            offset,
        );
        expect(scaledExport.width).toBeCloseTo(exported.width * 2);
        expect(scaledExport.height).toBeCloseTo(exported.height * 2);
    });
});

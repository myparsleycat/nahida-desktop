import { describe, expect, it } from "vitest";

import { mapKeyboardEventToInternal } from "./utils";

function keyEvent(partial: {
    key: string;
    code: string;
    ctrlKey?: boolean;
    altKey?: boolean;
    shiftKey?: boolean;
    metaKey?: boolean;
}) {
    return {
        ctrlKey: false,
        altKey: false,
        shiftKey: false,
        metaKey: false,
        ...partial,
    } as KeyboardEvent;
}

describe("mapKeyboardEventToInternal", () => {
    it("maps Minus to underscore", () => {
        expect(mapKeyboardEventToInternal(keyEvent({ key: "-", code: "Minus" }))).toBe("_");
        expect(
            mapKeyboardEventToInternal(keyEvent({ key: "_", code: "Minus", shiftKey: true })),
        ).toBe("shift _");
    });

    it("maps Equal to equals", () => {
        expect(mapKeyboardEventToInternal(keyEvent({ key: "=", code: "Equal" }))).toBe("=");
        expect(
            mapKeyboardEventToInternal(keyEvent({ key: "+", code: "Equal", shiftKey: true })),
        ).toBe("shift =");
    });

    it("falls back to underscore and equals when code is missing", () => {
        expect(mapKeyboardEventToInternal(keyEvent({ key: "-", code: "" }))).toBe("_");
        expect(mapKeyboardEventToInternal(keyEvent({ key: "+", code: "" }))).toBe("=");
    });

    it("keeps numpad subtract and add as virtual keys", () => {
        expect(mapKeyboardEventToInternal(keyEvent({ key: "-", code: "NumpadSubtract" }))).toBe(
            "vk_subtract",
        );
        expect(mapKeyboardEventToInternal(keyEvent({ key: "+", code: "NumpadAdd" }))).toBe(
            "vk_add",
        );
    });

    it("does not change letter, digit, or bracket mappings", () => {
        expect(mapKeyboardEventToInternal(keyEvent({ key: "a", code: "KeyA" }))).toBe("a");
        expect(mapKeyboardEventToInternal(keyEvent({ key: "1", code: "Digit1" }))).toBe("1");
        expect(mapKeyboardEventToInternal(keyEvent({ key: "[", code: "BracketLeft" }))).toBe("[");
        expect(
            mapKeyboardEventToInternal(keyEvent({ key: "A", code: "KeyA", ctrlKey: true })),
        ).toBe("ctrl a");
    });
});

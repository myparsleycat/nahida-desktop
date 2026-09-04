import { describe, expect, it } from "vitest";

import {
    applySelectionHistoryValues,
    createSelectionHistoryEntry,
    pushSelectionHistory,
    type SelectionHistory,
} from "./selection-history";

describe("body shape sparse selection history", () => {
    it("stores only changed vertices with their first and final values", () => {
        const before = new Float32Array([0, 0.25, 0.5, 0.75]);
        const after = new Float32Array([0, 0.5, 0.5, 1]);

        const entry = createSelectionHistoryEntry(before, after)!;

        expect([...entry.indices]).toEqual([1, 3]);
        expect([...entry.before]).toEqual([0.25, 0.75]);
        expect([...entry.after]).toEqual([0.5, 1]);
    });

    it("round trips undo and redo without touching unrelated vertices", () => {
        const entry = createSelectionHistoryEntry(
            new Float32Array([0, 0.25, 0.5]),
            new Float32Array([1, 0.25, 0.75]),
        )!;
        const weights = new Float32Array([1, 0.25, 0.75]);

        applySelectionHistoryValues(weights, entry.indices, entry.before);
        expect([...weights]).toEqual([0, 0.25, 0.5]);
        applySelectionHistoryValues(weights, entry.indices, entry.after);
        expect([...weights]).toEqual([1, 0.25, 0.75]);
    });

    it("evicts oldest entries by count and byte budget while retaining the latest entry", () => {
        const history: SelectionHistory = { undo: [], redo: [] };
        const entries = Array.from({ length: 4 }, (_, index) => ({
            indices: new Uint32Array([index]),
            before: new Float32Array([0]),
            after: new Float32Array([1]),
        }));

        for (const entry of entries) pushSelectionHistory(history, entry, { limit: 2 });
        expect(history.undo).toEqual(entries.slice(2));

        const oversized: SelectionHistory = { undo: [], redo: [] };
        pushSelectionHistory(oversized, entries[0], { byteLimit: 1 });
        expect(oversized.undo).toEqual([entries[0]]);
    });
});

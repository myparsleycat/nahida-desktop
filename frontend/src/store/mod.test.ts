import { afterEach, describe, expect, it } from "vitest";

import { modStore } from "./mod";

afterEach(() => {
    modStore.setState({
        isMergeMode: false,
        selectedModPaths: new Set(),
        isMergeDialogOpen: false,
    });
});

describe("mod merge selections", () => {
    it("removes downloading mods and closes an invalid merge dialog", () => {
        modStore.setState({
            isMergeMode: true,
            selectedModPaths: new Set(["A", "B"]),
            isMergeDialogOpen: true,
        });

        modStore.getState().removeMergeSelections(["A"]);

        expect(modStore.getState().selectedModPaths).toEqual(new Set(["B"]));
        expect(modStore.getState().isMergeDialogOpen).toBe(false);
        expect(modStore.getState().isMergeMode).toBe(true);
    });

    it("leaves the dialog open while at least two selections remain", () => {
        modStore.setState({
            isMergeMode: true,
            selectedModPaths: new Set(["A", "B", "C"]),
            isMergeDialogOpen: true,
        });

        modStore.getState().removeMergeSelections(["A"]);

        expect(modStore.getState().selectedModPaths).toEqual(new Set(["B", "C"]));
        expect(modStore.getState().isMergeDialogOpen).toBe(true);
    });
});

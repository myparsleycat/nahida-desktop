import { afterEach, describe, expect, it } from "vitest";

import { modStore } from "./mod";

afterEach(() => {
    modStore.setState({
        isMergeMode: false,
        selectedModPaths: new Set(),
        isMergeDialogOpen: false,
        expandedGroups: new Set(),
        persistentGroups: new Set(),
        selectedGroup: null,
        iniListExpandedByGroupPath: {},
    });
});

describe("renameGroupPath", () => {
    it("moves the folder and its descendants without touching a sibling sharing the prefix", () => {
        modStore.setState({
            expandedGroups: new Set(["M\\Foo", "M\\Foo\\Sub", "M\\Foo2"]),
            persistentGroups: new Set(["M\\Foo2"]),
            selectedGroup: { name: "Sub", path: "M\\Foo\\Sub", mods: [] },
        });

        modStore.getState().renameGroupPath("M\\Foo", "M\\DISABLED Foo");

        const state = modStore.getState();
        expect(state.expandedGroups).toEqual(
            new Set(["M\\DISABLED Foo", "M\\DISABLED Foo\\Sub", "M\\Foo2"]),
        );
        expect(state.persistentGroups).toEqual(new Set(["M\\Foo2"]));
        expect(state.selectedGroup?.path).toBe("M\\DISABLED Foo\\Sub");
    });

    it("flips the selected folder's state and keeps its INI expansion", () => {
        modStore.setState({
            selectedGroup: { name: "Foo", path: "M\\Foo", isEnabled: true, mods: [] },
            iniListExpandedByGroupPath: { "M\\Foo": { mod: true } },
        });

        modStore.getState().renameGroupPath("M\\Foo", "M\\DISABLED Foo");

        const state = modStore.getState();
        expect(state.selectedGroup).toMatchObject({ path: "M\\DISABLED Foo", isEnabled: false });
        expect(state.iniListExpandedByGroupPath).toEqual({ "M\\DISABLED Foo": { mod: true } });
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

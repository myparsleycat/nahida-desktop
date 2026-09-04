import { describe, expect, it } from "vitest";

import {
    collectSelectedAncestorIds,
    hasSelectedAncestor,
    pruneSubtreeSelection,
    toggleSubtreeSelection,
} from "./import-folder-tree-state";

function lookup(parents: Record<string, string | null>) {
    return (id: string) => parents[id];
}

describe("drive import subtree selection", () => {
    const getParentId = lookup({ root: null, parent: "root", child: "parent", leaf: "child" });

    it("derives checked ancestors as indeterminate without descendant sets", () => {
        expect([...collectSelectedAncestorIds(new Set(["leaf"]), getParentId)]).toEqual([
            "child",
            "parent",
            "root",
        ]);
    });

    it("does not add a folder whose ancestor is already selected", () => {
        const selected = new Set(["parent"]);
        expect(toggleSubtreeSelection(selected, "leaf", getParentId)).toEqual(selected);
        expect(hasSelectedAncestor("leaf", selected, getParentId)).toBe(true);
    });

    it("removes selected descendants when their parent is selected", () => {
        expect(toggleSubtreeSelection(new Set(["child", "leaf"]), "parent", getParentId)).toEqual(
            new Set(["parent"]),
        );
    });

    it("prunes redundant descendants immediately before import", () => {
        expect(pruneSubtreeSelection(new Set(["root", "child", "leaf"]), getParentId)).toEqual(
            new Set(["root"]),
        );
    });

    it("terminates on missing parents and cycles", () => {
        const cyclic = lookup({ a: "b", b: "c", c: "a", orphan: "missing" });
        expect(collectSelectedAncestorIds(new Set(["a", "orphan"]), cyclic)).toEqual(
            new Set(["b", "c", "missing"]),
        );
    });

    it("walks a 10,000-level parent chain without recursion", () => {
        const parents = new Map<string, string | null>();
        parents.set("0", null);
        for (let index = 1; index <= 10_000; index++) {
            parents.set(String(index), String(index - 1));
        }

        const ancestors = collectSelectedAncestorIds(new Set(["10000"]), (id) => parents.get(id));
        expect(ancestors.size).toBe(10_000);
        expect(ancestors.has("0")).toBe(true);
    });
});

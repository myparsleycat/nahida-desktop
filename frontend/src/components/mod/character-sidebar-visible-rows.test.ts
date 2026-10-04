import assert from "node:assert/strict";

import type { FolderGroup } from "@renderer/types/mod";
import type { Classification } from "@shared/types";
import { describe, it } from "vitest";

import {
    buildVisibleSidebarRows,
    partitionByClassification,
    type VisibleSidebarRow,
} from "./character-sidebar-visible-rows";

const element: Classification = {
    id: "element",
    game: "Game",
    name: "Element",
    active: true,
    groups: [
        { id: "pyro", name: "Pyro" },
        { id: "hydro", name: "Hydro" },
        { id: "cryo", name: "Cryo" },
    ],
};

function folder(name: string, groupId?: string, extra: Partial<FolderGroup> = {}): FolderGroup {
    return {
        name,
        path: `mods/${name}`,
        mods: [],
        modCount: 1,
        classifications: groupId ? { element: groupId, weapon: "sword" } : undefined,
        ...extra,
    };
}

const groups = [
    folder("Klee", "pyro"),
    folder("Furina", "hydro"),
    folder("Diluc", "pyro"),
    folder("Traveler"),
    folder("Removed", "deleted-group"),
];

function build(
    options: Partial<Parameters<typeof buildVisibleSidebarRows>[1]> = {},
    input = groups,
) {
    return buildVisibleSidebarRows(input, {
        searchTerm: "",
        sortKey: "name",
        sortDirection: "ascending",
        hideEmptyGroups: false,
        expandedGroups: new Set(),
        persistentGroups: new Set(),
        subGroupsByPath: new Map(),
        manualSubGroupsByPath: new Map(),
        ...options,
    });
}

function describeRows(rows: VisibleSidebarRow[]) {
    return rows.map((row) =>
        row.kind === "section"
            ? `[${row.name ?? "unassigned"} ${row.count}${row.collapsed ? " collapsed" : ""}]`
            : `${"  ".repeat(row.depth)}${row.group.name}`,
    );
}

describe("partitionByClassification", () => {
    it("keeps the classification's group order and collects the rest last", () => {
        const sections = partitionByClassification(groups, element);

        assert.deepEqual(
            sections.map((section) => [
                section.key,
                section.name,
                section.groups.map((g) => g.name),
            ]),
            [
                ["element:pyro", "Pyro", ["Klee", "Diluc"]],
                ["element:hydro", "Hydro", ["Furina"]],
                ["element:cryo", "Cryo", []],
                ["element:", null, ["Traveler", "Removed"]],
            ],
        );
    });
});

describe("buildVisibleSidebarRows", () => {
    it("lists folders without sections when no classification is active", () => {
        assert.deepEqual(describeRows(build()), ["Diluc", "Furina", "Klee", "Removed", "Traveler"]);
    });

    it("groups sorted folders under section headers and keeps empty groups visible", () => {
        assert.deepEqual(describeRows(build({ classification: element })), [
            "[Pyro 2]",
            "Diluc",
            "Klee",
            "[Hydro 1]",
            "Furina",
            "[Cryo 0]",
            "[unassigned 2]",
            "Removed",
            "Traveler",
        ]);
    });

    it("hides the unassigned section once every folder has a group", () => {
        const rows = build({ classification: element }, [folder("Klee", "pyro")]);

        assert.deepEqual(describeRows(rows), ["[Pyro 1]", "Klee", "[Hydro 0]", "[Cryo 0]"]);
    });

    it("drops the folders of a collapsed section but keeps its header and count", () => {
        const rows = build({
            classification: element,
            collapsedSections: new Set(["element:pyro", "element:"]),
        });

        assert.deepEqual(describeRows(rows), [
            "[Pyro 2 collapsed]",
            "[Hydro 1]",
            "Furina",
            "[Cryo 0]",
            "[unassigned 2 collapsed]",
        ]);
    });

    it("searches inside collapsed sections and hides sections without a match", () => {
        const rows = build({
            classification: element,
            collapsedSections: new Set(["element:pyro"]),
            searchTerm: "L",
        });

        assert.deepEqual(describeRows(rows), [
            "[Pyro 2]",
            "Diluc",
            "Klee",
            "[unassigned 2]",
            "Traveler",
        ]);
    });

    it("applies the empty-folder filter per section and nests sub groups under their parent", () => {
        const variant = folder("Variant");
        const rows = build(
            {
                classification: element,
                hideEmptyGroups: true,
                expandedGroups: new Set(["mods/Diluc"]),
                subGroupsByPath: new Map([["mods/Diluc", [variant]]]),
            },
            [folder("Diluc", "pyro"), folder("Empty", "pyro", { modCount: 0 }), folder("Bare")],
        );

        assert.deepEqual(describeRows(rows), [
            "[Pyro 1]",
            "Diluc",
            "  Variant",
            "[Hydro 0]",
            "[Cryo 0]",
            "[unassigned 1]",
            "Bare",
        ]);
    });
});

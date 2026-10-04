import assert from "node:assert/strict";

import type { FolderGroup } from "@renderer/types/mod";
import type { Classification } from "@shared/types";
import { describe, it } from "vitest";

import {
    buildVisibleSidebarRows,
    canAssignClassification,
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

    it("lists classified sub folders in their section instead of under the parent", () => {
        const parent = folder("Collection");
        const pyro = folder("Diluc", "pyro", { path: `${parent.path}/Diluc` });
        const hydro = folder("Furina", "hydro", { path: "mods\\Collection\\Furina" });
        const removed = folder("Removed", "deleted-group", { path: `${parent.path}/Removed` });
        const plain = folder("Plain", undefined, { path: `${parent.path}/Plain` });
        const variant = folder("Variant", undefined, { path: `${pyro.path}/Variant` });
        const collection = { ...parent, classifiedSubGroups: [pyro, hydro, removed] };
        const options = {
            classification: element,
            expandedGroups: new Set([parent.path, pyro.path]),
            subGroupsByPath: new Map([
                [parent.path, [pyro, hydro, removed, plain]],
                [pyro.path, [variant]],
            ]),
        };

        // The parent stays collapsed here, so the sections rely on classifiedSubGroups alone.
        const collapsed = build({ classification: element }, [collection, folder("Klee", "pyro")]);
        assert.deepEqual(describeRows(collapsed), [
            "[Pyro 2]",
            "Diluc",
            "Klee",
            "[Hydro 1]",
            "Furina",
            "[Cryo 0]",
            "[unassigned 1]",
            "Collection",
        ]);
        assert.deepEqual(
            collapsed.flatMap((row) => (row.kind === "group" ? [row.parentGroupName] : [])),
            ["Collection", undefined, "Collection", undefined],
        );

        assert.deepEqual(describeRows(build(options, [collection])), [
            "[Pyro 1]",
            "Diluc",
            "  Variant",
            "[Hydro 1]",
            "Furina",
            "[Cryo 0]",
            "[unassigned 1]",
            "Collection",
            "  Plain",
            "  Removed",
        ]);
        assert.deepEqual(describeRows(build({ ...options, classification: null }, [collection])), [
            "Collection",
            "  Diluc",
            "    Variant",
            "  Furina",
            "  Plain",
            "  Removed",
        ]);
        assert.deepEqual(
            describeRows(
                build(
                    {
                        ...options,
                        searchTerm: "furina",
                        collapsedSections: new Set(["element:hydro"]),
                    },
                    [collection],
                ),
            ),
            ["[Hydro 1]", "Furina"],
        );
    });
});

describe("canAssignClassification", () => {
    const root = String.raw`C:\Games\Mods`;

    it("accepts folders down to two levels below a top-level folder", () => {
        for (const path of ["Diluc", String.raw`Diluc\Costume`, String.raw`Diluc\Costume\Red`]) {
            assert.equal(canAssignClassification(root, `${root}\\${path}`), true, path);
        }
    });

    it("rejects the mod root, deeper folders and folders outside the root", () => {
        for (const path of [
            root,
            String.raw`C:\Games\Mods\Diluc\Costume\Red\Textures`,
            String.raw`C:\Games\Other\Diluc`,
        ]) {
            assert.equal(canAssignClassification(root, path), false, path);
        }
    });

    it("ignores case, separator style and trailing separators", () => {
        assert.equal(
            canAssignClassification("C:/Games/Mods/", String.raw`c:\games\mods\Diluc`),
            true,
        );
    });
});

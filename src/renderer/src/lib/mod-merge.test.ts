import type { ClassifyMergePacksResult, MergePackClassification } from "@shared/types";
import { describe, expect, it } from "vitest";

import {
    buildDefaultPlan,
    canUseClassic,
    collectLeaves,
    isThreedmigotoImporter,
    planHasClassicViolation,
    planIsValid,
    suggestedEngine,
} from "./mod-merge";

function createMockPack(
    partial: Partial<MergePackClassification> & { path: string },
): MergePackClassification {
    return {
        path: partial.path,
        name: partial.name ?? "Pack",
        family: partial.family ?? "ordinary",
        dialect: partial.dialect ?? "gimi",
        primaryIniPath: partial.primaryIniPath ?? `${partial.path}/mod.ini`,
        hashes: partial.hashes ?? ["hash1"],
        objectGuid: partial.objectGuid ?? null,
        allowsClassic: partial.allowsClassic ?? true,
        warnings: partial.warnings ?? [],
    };
}

describe("isThreedmigotoImporter", () => {
    it("recognizes known 3Dmigoto importers case-insensitively", () => {
        expect(isThreedmigotoImporter("GIMI")).toBe(true);
        expect(isThreedmigotoImporter("srmi")).toBe(true);
        expect(isThreedmigotoImporter("ZZMI")).toBe(true);
        expect(isThreedmigotoImporter("HIMI")).toBe(true);
        expect(isThreedmigotoImporter("wwmi")).toBe(true);
        expect(isThreedmigotoImporter("EFMI")).toBe(true);
    });

    it("returns false for non-3Dmigoto or missing importers", () => {
        expect(isThreedmigotoImporter(null)).toBe(false);
        expect(isThreedmigotoImporter(undefined)).toBe(false);
        expect(isThreedmigotoImporter("OTHER")).toBe(false);
    });
});

describe("suggestedEngine and canUseClassic", () => {
    it("suggests classic when all packs allow classic", () => {
        const pack1 = createMockPack({ path: "mod1", allowsClassic: true });
        const pack2 = createMockPack({ path: "mod2", allowsClassic: true });
        expect(canUseClassic(pack1)).toBe(true);
        expect(suggestedEngine([pack1, pack2])).toBe("classic");
    });

    it("suggests namespace when at least one pack does not allow classic", () => {
        const pack1 = createMockPack({ path: "mod1", allowsClassic: true });
        const pack2 = createMockPack({ path: "mod2", allowsClassic: false });
        expect(canUseClassic(pack2)).toBe(false);
        expect(suggestedEngine([pack1, pack2])).toBe("namespace");
    });
});

describe("buildDefaultPlan", () => {
    it("returns null when all packs are support family", () => {
        const result: ClassifyMergePacksResult = {
            packs: [
                createMockPack({ path: "mod1", family: "support" }),
                createMockPack({ path: "mod2", family: "support" }),
            ],
            hashOverlap: true,
            warnings: [],
        };
        expect(buildDefaultPlan(result, "TestPack")).toBeNull();
    });

    it("returns null when packs array is empty", () => {
        const result: ClassifyMergePacksResult = {
            packs: [],
            hashOverlap: true,
            warnings: [],
        };
        expect(buildDefaultPlan(result, "TestPack")).toBeNull();
    });

    it("filters out support packs and constructs plan with usable packs", () => {
        const result: ClassifyMergePacksResult = {
            packs: [
                createMockPack({ path: "mod1", family: "ordinary", allowsClassic: true }),
                createMockPack({ path: "support1", family: "support" }),
                createMockPack({ path: "mod2", family: "ordinary", allowsClassic: true }),
            ],
            hashOverlap: true,
            warnings: [],
        };
        const plan = buildDefaultPlan(result, "My Pack");
        expect(plan).not.toBeNull();
        expect(plan?.name).toBe("MyPack");
        expect(plan?.engine).toBe("classic");
        expect(plan?.children).toEqual([
            { kind: "leaf", path: "mod1" },
            { kind: "leaf", path: "mod2" },
        ]);
        expect(plan?.includeVanilla).toBe(false);
    });

    it("sets includeVanilla to true when engine is namespace", () => {
        const result: ClassifyMergePacksResult = {
            packs: [
                createMockPack({ path: "mod1", family: "ordinary", allowsClassic: false }),
                createMockPack({ path: "mod2", family: "ordinary", allowsClassic: true }),
            ],
            hashOverlap: true,
            warnings: [],
        };
        const plan = buildDefaultPlan(result, "NamespacePack");
        expect(plan).not.toBeNull();
        expect(plan?.engine).toBe("namespace");
        expect(plan?.includeVanilla).toBe(true);
    });
});

describe("planIsValid", () => {
    it("returns false for null or undefined", () => {
        expect(planIsValid(null)).toBe(false);
        expect(planIsValid(undefined)).toBe(false);
    });

    it("returns true for leaf node", () => {
        expect(planIsValid({ kind: "leaf", path: "mod1" })).toBe(true);
    });

    it("validates group node properties and leaf count", () => {
        const validGroup = {
            kind: "group" as const,
            id: "root",
            engine: "classic" as const,
            name: "Pack",
            forwardKey: "vk_right",
            backKey: "vk_left",
            includeVanilla: false,
            children: [
                { kind: "leaf" as const, path: "mod1" },
                { kind: "leaf" as const, path: "mod2" },
            ],
        };
        expect(planIsValid(validGroup)).toBe(true);

        // Less than 2 leaves
        expect(planIsValid({ ...validGroup, children: [{ kind: "leaf", path: "mod1" }] })).toBe(
            false,
        );

        // Empty name
        expect(planIsValid({ ...validGroup, name: "   " })).toBe(false);

        expect(planIsValid({ ...validGroup, name: "Bad]Name" })).toBe(false);

        // Empty forwardKey
        expect(planIsValid({ ...validGroup, forwardKey: "" })).toBe(false);

        // Namespace engine requires backKey
        expect(
            planIsValid({
                ...validGroup,
                engine: "namespace",
                backKey: "   ",
            }),
        ).toBe(false);
    });
});

describe("collectLeaves and planHasClassicViolation", () => {
    it("collects leaves recursively", () => {
        const tree = {
            kind: "group" as const,
            id: "root",
            engine: "classic" as const,
            name: "Root",
            forwardKey: "vk_right",
            backKey: "vk_left",
            includeVanilla: false,
            children: [
                { kind: "leaf" as const, path: "mod1" },
                {
                    kind: "group" as const,
                    id: "sub",
                    engine: "classic" as const,
                    name: "Sub",
                    forwardKey: "vk_right",
                    backKey: "vk_left",
                    includeVanilla: false,
                    children: [
                        { kind: "leaf" as const, path: "mod2" },
                        { kind: "leaf" as const, path: "mod3" },
                    ],
                },
            ],
        };
        expect(collectLeaves(tree)).toEqual(["mod1", "mod2", "mod3"]);
    });

    it("detects classic violations in classic engine group", () => {
        const packMap = new Map<string, MergePackClassification>([
            ["mod1", createMockPack({ path: "mod1", allowsClassic: true })],
            ["mod2", createMockPack({ path: "mod2", allowsClassic: false })],
        ]);

        const plan = {
            kind: "group" as const,
            id: "root",
            engine: "classic" as const,
            name: "Root",
            forwardKey: "vk_right",
            backKey: "vk_left",
            includeVanilla: false,
            children: [
                { kind: "leaf" as const, path: "mod1" },
                { kind: "leaf" as const, path: "mod2" },
            ],
        };
        expect(planHasClassicViolation(plan, packMap)).toBe(true);

        // With namespace engine, classic violation is not triggered
        expect(planHasClassicViolation({ ...plan, engine: "namespace" }, packMap)).toBe(false);
    });
});

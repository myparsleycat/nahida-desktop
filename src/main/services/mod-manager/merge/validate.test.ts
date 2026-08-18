import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import type { MergeModsRequest, MergePlanGroup } from "@shared/types";
import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import {
    assertMergeRequestPaths,
    assertOwnedModPaths,
    collectManagedModRoots,
    parseMergeModsRequest,
    parseModPaths,
} from "./validate.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

const groupPath = path.join(os.tmpdir(), "nhd-merge-group");
const leafA = path.join(groupPath, "A");
const leafB = path.join(groupPath, "B");

function validRequest(overrides: Partial<MergeModsRequest> = {}): MergeModsRequest {
    return {
        groupPath,
        placement: "new_folder",
        packName: "Merged",
        root: validRoot(),
        ...overrides,
    };
}

function validRoot(overrides: Partial<MergePlanGroup> = {}): MergePlanGroup {
    return {
        kind: "group",
        id: "root",
        engine: "classic",
        name: "Merged",
        forwardKey: "vk_right",
        backKey: "",
        includeVanilla: false,
        children: [
            { kind: "leaf", path: leafA },
            { kind: "leaf", path: leafB },
        ],
        ...overrides,
    };
}

describe("parseModPaths", () => {
    it("accepts absolute owned-looking paths", () => {
        assert.deepEqual(parseModPaths([leafA, leafB]), [leafA, leafB]);
    });

    it("rejects empty or relative payloads", () => {
        assert.throws(() => parseModPaths([]), /Invalid merge pack payload/);
        assert.throws(() => parseModPaths("nope"), /Invalid merge pack payload/);
        assert.throws(() => parseModPaths(["relative/path"]), /Invalid merge pack payload/);
        assert.throws(() => parseModPaths([""]), /Invalid merge pack payload/);
    });
});

describe("parseMergeModsRequest", () => {
    it("accepts a complete request", () => {
        assert.deepEqual(parseMergeModsRequest(validRequest()), validRequest());
    });

    it("rejects missing or invalid top-level fields", () => {
        assert.throws(() => parseMergeModsRequest(null), /Invalid merge request payload/);
        assert.throws(
            () => parseMergeModsRequest(validRequest({ placement: "elsewhere" as never })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ packName: "..\\escape" })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ packName: "Merged/evil" })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ packName: "Bad]Name" })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ packName: "A=B" })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ packName: "Line\nBreak" })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () => parseMergeModsRequest(validRequest({ groupPath: "relative-group" })),
            /Invalid merge request payload/,
        );
    });

    it("rejects invalid recursive plan nodes", () => {
        assert.throws(
            () => parseMergeModsRequest(validRequest({ root: { kind: "leaf", path: leafA } })),
            /Invalid merge request payload/,
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({ children: [{ kind: "leaf", path: leafA }] }),
                    }),
                ),
            /Invalid merge request payload/,
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({
                            children: [
                                { kind: "leaf", path: leafA },
                                { kind: "leaf", path: leafA },
                            ],
                        }),
                    }),
                ),
            /Invalid merge request payload/,
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({ engine: "namespace", backKey: "" }),
                    }),
                ),
            /Invalid merge request payload/,
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({ name: "..\\outside" }),
                    }),
                ),
            /Invalid merge request payload/,
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({ name: "Bad]Name" }),
                    }),
                ),
            /Invalid merge request payload/,
        );
        assert.doesNotThrow(() =>
            parseMergeModsRequest(
                validRequest({ packName: "나히다", root: validRoot({ name: "나히다" }) }),
            ),
        );
        assert.doesNotThrow(() =>
            parseMergeModsRequest(
                validRequest({ packName: "AnbyS0", root: validRoot({ name: "AnbyS0" }) }),
            ),
        );
        assert.throws(
            () =>
                parseMergeModsRequest(
                    validRequest({
                        root: validRoot({
                            children: [
                                { kind: "leaf", path: leafA },
                                {
                                    kind: "group",
                                    id: "inner",
                                    engine: "classic",
                                    name: "Inner",
                                    forwardKey: "]",
                                    backKey: "",
                                    includeVanilla: false,
                                    children: [{ kind: "leaf", path: leafB }],
                                },
                            ],
                        }),
                    }),
                ),
            /Invalid merge request payload/,
        );
    });
});

describe("merge path ownership", () => {
    it("collects both managed mod roots", () => {
        assert.deepEqual(
            collectManagedModRoots([
                { modFolderPath: "E:\\mods\\game", linkedModFolderPath: "E:\\games\\mods" },
                { modFolderPath: "E:\\mods\\other", linkedModFolderPath: null },
            ]),
            ["E:\\mods\\game", "E:\\games\\mods", "E:\\mods\\other"],
        );
    });

    it("accepts paths inside a managed root and the selected group", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-owned-"));
        tempRoots.push(root);
        const group = path.join(root, "Klee");
        const first = path.join(group, "A");
        const second = path.join(group, "B");
        await fse.ensureDir(first);
        await fse.ensureDir(second);

        await assertOwnedModPaths([first, second], [root]);
        await assertMergeRequestPaths(
            {
                groupPath: group,
                placement: "new_folder",
                packName: "Merged",
                root: {
                    kind: "group",
                    id: "root",
                    engine: "classic",
                    name: "Merged",
                    forwardKey: "vk_right",
                    backKey: "",
                    includeVanilla: false,
                    children: [
                        { kind: "leaf", path: first },
                        { kind: "leaf", path: second },
                    ],
                },
            },
            [root],
        );
    });

    it("rejects paths outside the managed root or selected group", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-owned-"));
        const outsider = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-out-"));
        tempRoots.push(root, outsider);
        const group = path.join(root, "Klee");
        const first = path.join(group, "A");
        const second = path.join(group, "B");
        const sibling = path.join(root, "Other", "C");
        await fse.ensureDir(first);
        await fse.ensureDir(second);
        await fse.ensureDir(sibling);

        await assert.rejects(assertOwnedModPaths([outsider], [root]), /managed mod folder/);
        await assert.rejects(
            assertOwnedModPaths([path.join(root, "..", path.basename(outsider))], [root]),
            /managed mod folder/,
        );
        await assert.rejects(assertOwnedModPaths([first], []), /managed mod folder/);
        await assert.rejects(
            assertMergeRequestPaths(
                {
                    groupPath: group,
                    placement: "new_folder",
                    packName: "Merged",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "classic",
                        name: "Merged",
                        forwardKey: "vk_right",
                        backKey: "",
                        includeVanilla: false,
                        children: [
                            { kind: "leaf", path: first },
                            { kind: "leaf", path: sibling },
                        ],
                    },
                },
                [root],
            ),
            /selected group/,
        );
        await assert.rejects(
            assertMergeRequestPaths(
                {
                    groupPath: group,
                    placement: "new_folder",
                    packName: "Merged",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "classic",
                        name: "Merged",
                        forwardKey: "vk_right",
                        backKey: "",
                        includeVanilla: false,
                        children: [
                            { kind: "leaf", path: first },
                            { kind: "leaf", path: group },
                        ],
                    },
                },
                [root],
            ),
            /selected group/,
        );
    });
});

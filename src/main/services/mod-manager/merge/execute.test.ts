import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { writeClassicMerge } from "./classic.ts";
import { ModMergeService } from "./execute.ts";
import { writeNamespaceMerge } from "./namespace.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

const ordinary = (hash = "abcdef01") => `[TextureOverrideKleePosition]
hash = ${hash}
vb0 = ResourcePosition

[ResourcePosition]
filename = KleePosition.buf
`;

describe("merge writers together", () => {
    it("namespaces an ordinary pack onto an existing master", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const host = path.join(root, "Host");
        const extra = path.join(root, "Extra");
        await fse.ensureDir(host);
        await fse.ensureDir(extra);
        const child = path.join(host, "child.ini");
        const extraIni = path.join(extra, "Klee.ini");
        await fse.writeFile(child, ordinary());
        await fse.writeFile(extraIni, ordinary());
        const masterPath = await writeNamespaceMerge({
            masterDir: host,
            name: "Klee",
            sources: [{ iniPath: child, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        await writeNamespaceMerge({
            masterDir: host,
            name: "Klee",
            sources: [
                { iniPath: child, index: 0 },
                { iniPath: extraIni, index: 1 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
            existingMasterPath: masterPath,
        });

        const extraText = await fse.readFile(extraIni, "utf8");
        const master = await fse.readFile(masterPath, "utf8");
        assert.match(extraText, /if \$\\Klee\\Master\\swapvar==1/);
        assert.match(master, /\$swapvar = 0,1/);
    });

    it("can remaster two namespace packs into one swap space", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const a = path.join(root, "A", "a.ini");
        const b = path.join(root, "B", "b.ini");
        await fse.ensureDir(path.dirname(a));
        await fse.ensureDir(path.dirname(b));
        await fse.writeFile(a, ordinary());
        await fse.writeFile(b, ordinary());
        await writeNamespaceMerge({
            masterDir: path.dirname(a),
            name: "Alpha",
            sources: [{ iniPath: a, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });
        await writeNamespaceMerge({
            masterDir: path.dirname(b),
            name: "Beta",
            sources: [{ iniPath: b, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        await writeNamespaceMerge({
            masterDir: path.dirname(a),
            name: "Klee",
            sources: [
                { iniPath: a, index: 0 },
                { iniPath: b, index: 1 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        assert.match(await fse.readFile(a, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(b, "utf8"), /if \$\\Klee\\Master\\swapvar==1/);
        assert.doesNotMatch(await fse.readFile(a, "utf8"), /\$\\Alpha\\Master\\swapvar/);
    });

    it("namespaces a classic pack after writing merged.ini", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const classicDir = path.join(root, "Classic");
        const ordinaryDir = path.join(root, "Ordinary");
        await fse.ensureDir(classicDir);
        await fse.ensureDir(ordinaryDir);
        const a = path.join(classicDir, "A.ini");
        const b = path.join(classicDir, "B.ini");
        const ordinaryIni = path.join(ordinaryDir, "C.ini");
        await fse.writeFile(a, ordinary());
        await fse.writeFile(b, ordinary());
        await fse.writeFile(ordinaryIni, ordinary());
        const merged = await writeClassicMerge({
            outputDir: classicDir,
            sources: [
                { iniPath: a, groupIndex: 0 },
                { iniPath: b, groupIndex: 1 },
            ],
            forwardKey: "vk_right",
        });

        await writeNamespaceMerge({
            masterDir: ordinaryDir,
            name: "Klee",
            sources: [
                { iniPath: merged, index: 0 },
                { iniPath: ordinaryIni, index: 1 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        assert.match(await fse.readFile(merged, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(ordinaryIni, "utf8"), /if \$\\Klee\\Master\\swapvar==1/);
    });

    it("disables leftover masters when remastering two namespace packs", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        const bDir = path.join(root, "B");
        const a = path.join(aDir, "a.ini");
        const b = path.join(bDir, "b.ini");
        await fse.ensureDir(aDir);
        await fse.ensureDir(bDir);
        await fse.writeFile(a, ordinary());
        await fse.writeFile(b, ordinary());
        await writeNamespaceMerge({
            masterDir: aDir,
            name: "Alpha",
            sources: [{ iniPath: a, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });
        await writeNamespaceMerge({
            masterDir: bDir,
            name: "Beta",
            sources: [{ iniPath: b, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await service.mergeMods({
            groupPath: root,
            placement: "in_place",
            packName: "Klee",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "Klee",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: aDir },
                    { kind: "leaf", path: bDir },
                ],
            },
        });

        assert.equal(await fse.pathExists(path.join(aDir, "MasterKlee.ini")), true);
        assert.equal(await fse.pathExists(path.join(aDir, "MasterAlpha.ini")), false);
        assert.equal(await fse.pathExists(path.join(bDir, "MasterBeta.ini")), false);
        assert.equal(await fse.pathExists(path.join(bDir, "DISABLED_BACKUP_MasterBeta.ini")), true);
        assert.match(await fse.readFile(a, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(b, "utf8"), /if \$\\Klee\\Master\\swapvar==1/);
    });

    it("shares one swapvar index across INIs from the same pack", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        const mainIni = path.join(packA, "Klee.ini");
        const helperIni = path.join(packA, "ORFix.ini");
        const extraIni = path.join(packB, "Klee.ini");
        await fse.writeFile(mainIni, ordinary());
        await fse.writeFile(helperIni, ordinary("abcdef02"));
        await fse.writeFile(extraIni, ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await service.mergeMods({
            groupPath: root,
            placement: "in_place",
            packName: "Klee",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "Klee",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: packA },
                    { kind: "leaf", path: packB },
                ],
            },
        });

        assert.match(await fse.readFile(mainIni, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(helperIni, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(extraIni, "utf8"), /if \$\\Klee\\Master\\swapvar==1/);
        assert.match(
            await fse.readFile(path.join(packA, "MasterKlee.ini"), "utf8"),
            /\$swapvar = 0,1/,
        );
    });

    it("keeps one swapvar index per copied pack in a new folder", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        await fse.writeFile(path.join(packA, "Klee.ini"), ordinary());
        await fse.writeFile(path.join(packA, "ORFix.ini"), ordinary("abcdef02"));
        await fse.writeFile(path.join(packB, "Klee.ini"), ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        const result = await service.mergeMods({
            groupPath: root,
            placement: "new_folder",
            packName: "Klee",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "Klee",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: true,
                children: [
                    { kind: "leaf", path: packA },
                    { kind: "leaf", path: packB },
                ],
            },
        });

        const copiedA = path.join(result.outputPath, "PackA");
        const copiedB = path.join(result.outputPath, "PackB");
        assert.match(
            await fse.readFile(path.join(copiedA, "Klee.ini"), "utf8"),
            /if \$\\Klee\\Master\\swapvar==1/,
        );
        assert.match(
            await fse.readFile(path.join(copiedA, "ORFix.ini"), "utf8"),
            /if \$\\Klee\\Master\\swapvar==1/,
        );
        assert.match(
            await fse.readFile(path.join(copiedB, "Klee.ini"), "utf8"),
            /if \$\\Klee\\Master\\swapvar==2/,
        );
        assert.match(
            await fse.readFile(path.join(result.outputPath, "MasterKlee.ini"), "utf8"),
            /\$swapvar = 0,1,2/,
        );
    });

    it("drops DISABLED prefixes when copying disabled packs into a new folder", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const disabled = path.join(root, "DISABLED Aino Nude toggle - 복사본");
        const extra = path.join(root, "Extra");
        await fse.ensureDir(disabled);
        await fse.ensureDir(extra);
        await fse.writeFile(path.join(disabled, "Aino.ini"), ordinary());
        await fse.writeFile(path.join(extra, "Aino.ini"), ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        const result = await service.mergeMods({
            groupPath: root,
            placement: "new_folder",
            packName: "AinoNudetoggle",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "AinoNudetoggle",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: disabled },
                    { kind: "leaf", path: extra },
                ],
            },
        });

        assert.equal(
            await fse.pathExists(path.join(result.outputPath, "Aino Nude toggle - 복사본")),
            true,
        );
        assert.equal(
            await fse.pathExists(
                path.join(result.outputPath, "DISABLED Aino Nude toggle - 복사본"),
            ),
            false,
        );
        assert.equal(await fse.pathExists(disabled), true);
    });

    it("does not re-disable already disabled packs with repeated prefixes without separators", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const disabled = path.join(root, "disableddisabled PackA");
        const extra = path.join(root, "Extra");
        await fse.ensureDir(disabled);
        await fse.ensureDir(extra);
        await fse.writeFile(path.join(disabled, "Aino.ini"), ordinary());
        await fse.writeFile(path.join(extra, "Aino.ini"), ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        const result = await service.mergeMods({
            groupPath: root,
            placement: "new_folder",
            packName: "AinoNudetoggle",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "AinoNudetoggle",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: disabled },
                    { kind: "leaf", path: extra },
                ],
            },
        });

        assert.equal(await fse.pathExists(path.join(result.outputPath, "PackA")), true);
        assert.equal(await fse.pathExists(disabled), true);
    });

    it("disables an original pack under a free name when DISABLED already exists", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        const existingDisabled = path.join(root, "DISABLED PackA");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        await fse.ensureDir(existingDisabled);
        await fse.writeFile(path.join(packA, "Klee.ini"), ordinary());
        await fse.writeFile(path.join(packB, "Klee.ini"), ordinary());
        await fse.writeFile(path.join(existingDisabled, "keep.ini"), "user-disabled");

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await service.mergeMods({
            groupPath: root,
            placement: "new_folder",
            packName: "Klee",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "Klee",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: packA },
                    { kind: "leaf", path: packB },
                ],
            },
        });

        assert.equal(await fse.pathExists(packA), false);
        assert.equal(await fse.pathExists(path.join(root, "DISABLED PackA (2)")), true);
        assert.equal(
            await fse.readFile(path.join(existingDisabled, "keep.ini"), "utf8"),
            "user-disabled",
        );
    });

    it("restores wrapped child INIs when a nested namespace merge fails", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        const empty = path.join(root, "Empty");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        await fse.ensureDir(empty);
        const aIni = path.join(packA, "Klee.ini");
        const bIni = path.join(packB, "Klee.ini");
        await fse.writeFile(aIni, ordinary());
        await fse.writeFile(bIni, ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await assert.rejects(
            () =>
                service.mergeMods({
                    groupPath: root,
                    placement: "in_place",
                    packName: "Klee",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "namespace",
                        name: "Klee",
                        forwardKey: "]",
                        backKey: "[",
                        includeVanilla: false,
                        children: [
                            {
                                kind: "group",
                                id: "inner",
                                engine: "namespace",
                                name: "Inner",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [
                                    { kind: "leaf", path: packA },
                                    { kind: "leaf", path: packB },
                                ],
                            },
                            {
                                kind: "group",
                                id: "fail",
                                engine: "namespace",
                                name: "Fail",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [{ kind: "leaf", path: empty }],
                            },
                        ],
                    },
                }),
            /NAMESPACE_MERGE_NEEDS_CHILD/,
        );

        assert.equal(await fse.readFile(aIni, "utf8"), ordinary());
        assert.equal(await fse.readFile(bIni, "utf8"), ordinary());
        assert.equal(await fse.pathExists(path.join(packA, "DISABLED_BACKUP_Klee.ini")), false);
        assert.equal(await fse.pathExists(path.join(packB, "DISABLED_BACKUP_Klee.ini")), false);
        assert.equal(await fse.pathExists(path.join(packA, "MasterInner.ini")), false);
        assert.equal(await fse.pathExists(path.join(packB, "MasterInner.ini")), false);
    });

    it("restores a leftover master when its DISABLED backup already exists", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        const empty = path.join(root, "Empty");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        await fse.ensureDir(empty);
        const aIni = path.join(packA, "Klee.ini");
        const bIni = path.join(packB, "Klee.ini");
        await fse.writeFile(aIni, ordinary());
        await fse.writeFile(bIni, ordinary());
        await writeNamespaceMerge({
            masterDir: packA,
            name: "Alpha",
            sources: [{ iniPath: aIni, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });
        await writeNamespaceMerge({
            masterDir: packB,
            name: "Beta",
            sources: [{ iniPath: bIni, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const masterBeta = path.join(packB, "MasterBeta.ini");
        const disabledBeta = path.join(packB, "DISABLEDMasterBeta.ini");
        const masterBetaText = await fse.readFile(masterBeta, "utf8");
        await fse.writeFile(disabledBeta, "stale-master-backup");

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await assert.rejects(
            () =>
                service.mergeMods({
                    groupPath: root,
                    placement: "in_place",
                    packName: "Klee",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "namespace",
                        name: "Klee",
                        forwardKey: "]",
                        backKey: "[",
                        includeVanilla: false,
                        children: [
                            {
                                kind: "group",
                                id: "inner",
                                engine: "namespace",
                                name: "Inner",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [
                                    { kind: "leaf", path: packA },
                                    { kind: "leaf", path: packB },
                                ],
                            },
                            {
                                kind: "group",
                                id: "fail",
                                engine: "namespace",
                                name: "Fail",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [{ kind: "leaf", path: empty }],
                            },
                        ],
                    },
                }),
            /NAMESPACE_MERGE_NEEDS_CHILD/,
        );

        assert.equal(await fse.readFile(masterBeta, "utf8"), masterBetaText);
        assert.equal(await fse.readFile(disabledBeta, "utf8"), "stale-master-backup");
    });

    it("re-enables classic source INIs when a later nested merge fails", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const packA = path.join(root, "PackA");
        const packB = path.join(root, "PackB");
        const wwmi = path.join(root, "WWMI");
        const extra = path.join(root, "Extra");
        await fse.ensureDir(packA);
        await fse.ensureDir(packB);
        await fse.ensureDir(wwmi);
        await fse.ensureDir(extra);
        const aIni = path.join(packA, "Klee.ini");
        const bIni = path.join(packB, "Klee.ini");
        await fse.writeFile(aIni, ordinary());
        await fse.writeFile(bIni, ordinary());
        await fse.writeFile(
            path.join(wwmi, "mod.ini"),
            `; WWMI ALPHA-2 INI
[Constants]
global $object_guid = 170961
[TextureOverrideComponent0]
hash = 7748c1d8
`,
        );
        await fse.writeFile(path.join(extra, "Klee.ini"), ordinary());

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await assert.rejects(
            () =>
                service.mergeMods({
                    groupPath: root,
                    placement: "in_place",
                    packName: "Klee",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "namespace",
                        name: "Klee",
                        forwardKey: "]",
                        backKey: "[",
                        includeVanilla: false,
                        children: [
                            {
                                kind: "group",
                                id: "classic",
                                engine: "classic",
                                name: "Classic",
                                forwardKey: "vk_right",
                                backKey: "vk_left",
                                includeVanilla: false,
                                children: [
                                    { kind: "leaf", path: packA },
                                    { kind: "leaf", path: packB },
                                ],
                            },
                            {
                                kind: "group",
                                id: "locked",
                                engine: "classic",
                                name: "Locked",
                                forwardKey: "vk_right",
                                backKey: "vk_left",
                                includeVanilla: false,
                                children: [
                                    { kind: "leaf", path: wwmi },
                                    { kind: "leaf", path: extra },
                                ],
                            },
                        ],
                    },
                }),
            /CLASSIC_LOCKED/,
        );

        assert.equal(await fse.pathExists(packA), true);
        assert.equal(await fse.pathExists(packB), true);
        assert.equal(await fse.readFile(aIni, "utf8"), ordinary());
        assert.equal(await fse.readFile(bIni, "utf8"), ordinary());
        assert.equal(await fse.pathExists(path.join(packA, "DISABLED_BACKUP_Klee.ini")), false);
        assert.equal(await fse.pathExists(path.join(packB, "DISABLED_BACKUP_Klee.ini")), false);
        assert.equal(await fse.pathExists(path.join(root, "Classic")), false);
    });

    it("stages packs through temporary paths when in-place destinations collide", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const disabledPackA = path.join(root, "DISABLED PackA");
        const packA = path.join(root, "PackA");
        await fse.ensureDir(disabledPackA);
        await fse.ensureDir(packA);
        const disabledIni = path.join(disabledPackA, "Klee.ini");
        const packAIni = path.join(packA, "Klee.ini");
        await fse.writeFile(disabledIni, ordinary("abcdef01"));
        await fse.writeFile(packAIni, ordinary("abcdef02"));

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await service.mergeMods({
            groupPath: root,
            placement: "in_place",
            packName: "Klee",
            root: {
                kind: "group",
                id: "root",
                engine: "namespace",
                name: "Klee",
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
                children: [
                    { kind: "leaf", path: disabledPackA },
                    { kind: "leaf", path: packA },
                ],
            },
        });

        const finalPackA = path.join(root, "PackA");
        const finalPackA2 = path.join(root, "PackA (2)");
        assert.equal(await fse.pathExists(disabledPackA), false);
        assert.equal(await fse.pathExists(finalPackA), true);
        assert.equal(await fse.pathExists(finalPackA2), true);
        assert.match(
            await fse.readFile(path.join(finalPackA, "Klee.ini"), "utf8"),
            /hash = abcdef01[\s\S]*if \$\\Klee\\Master\\swapvar==0/,
        );
        assert.match(
            await fse.readFile(path.join(finalPackA2, "Klee.ini"), "utf8"),
            /hash = abcdef02[\s\S]*if \$\\Klee\\Master\\swapvar==1/,
        );
    });

    it("restores staged packs to original paths when an in-place merge with collisions fails", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const disabledPackA = path.join(root, "DISABLED PackA");
        const packA = path.join(root, "PackA");
        const empty = path.join(root, "Empty");
        await fse.ensureDir(disabledPackA);
        await fse.ensureDir(packA);
        await fse.ensureDir(empty);
        const disabledIni = path.join(disabledPackA, "Klee.ini");
        const packAIni = path.join(packA, "Klee.ini");
        await fse.writeFile(disabledIni, ordinary("abcdef01"));
        await fse.writeFile(packAIni, ordinary("abcdef02"));

        const service = new ModMergeService({
            logger: { error() {} },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await assert.rejects(
            () =>
                service.mergeMods({
                    groupPath: root,
                    placement: "in_place",
                    packName: "Klee",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "namespace",
                        name: "Klee",
                        forwardKey: "]",
                        backKey: "[",
                        includeVanilla: false,
                        children: [
                            {
                                kind: "group",
                                id: "inner",
                                engine: "namespace",
                                name: "Inner",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [
                                    { kind: "leaf", path: disabledPackA },
                                    { kind: "leaf", path: packA },
                                ],
                            },
                            {
                                kind: "group",
                                id: "fail",
                                engine: "namespace",
                                name: "Fail",
                                forwardKey: "]",
                                backKey: "[",
                                includeVanilla: false,
                                children: [{ kind: "leaf", path: empty }],
                            },
                        ],
                    },
                }),
            /NAMESPACE_MERGE_NEEDS_CHILD/,
        );

        assert.equal(await fse.pathExists(disabledPackA), true);
        assert.equal(await fse.pathExists(packA), true);
        assert.equal(await fse.pathExists(path.join(root, "PackA (2)")), false);
        assert.match(await fse.readFile(disabledIni, "utf8"), /hash = abcdef01/);
        assert.match(await fse.readFile(packAIni, "utf8"), /hash = abcdef02/);
    });

    it("logs the original merge error together with rollback failures and rethrows", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-exec-"));
        tempRoots.push(root);
        const empty = path.join(root, "Empty");
        await fse.ensureDir(empty);

        const loggedErrors: Array<{ context: unknown; tag: string }> = [];
        const service = new ModMergeService({
            logger: {
                error(context: unknown, tag: string) {
                    loggedErrors.push({ context, tag });
                },
            },
            lib: {
                fs: {
                    getUniqueName: (name: string) => name,
                },
            },
        } as never);

        await assert.rejects(
            () =>
                service.mergeMods({
                    groupPath: root,
                    placement: "in_place",
                    packName: "TestPack",
                    root: {
                        kind: "group",
                        id: "root",
                        engine: "namespace",
                        name: "TestPack",
                        forwardKey: "]",
                        backKey: "[",
                        includeVanilla: false,
                        children: [{ kind: "leaf", path: empty }],
                    },
                }),
            /NAMESPACE_MERGE_NEEDS_CHILD/,
        );

        assert.equal(loggedErrors.length, 1);
        assert.equal(loggedErrors[0].tag, "Mod:mergeMods:context");
        const ctx = loggedErrors[0].context as {
            operation: string;
            error: string;
            rollbackFailures: unknown[];
        };
        assert.equal(ctx.operation, "mod:mergeMods");
        assert.match(ctx.error, /NAMESPACE_MERGE_NEEDS_CHILD/);
        assert.deepEqual(ctx.rollbackFailures, []);
    });
});

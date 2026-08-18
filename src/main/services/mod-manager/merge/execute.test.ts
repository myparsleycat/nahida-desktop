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
        assert.equal(await fse.pathExists(path.join(bDir, "DISABLEDMasterBeta.ini")), true);
        assert.match(await fse.readFile(a, "utf8"), /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(await fse.readFile(b, "utf8"), /if \$\\Klee\\Master\\swapvar==1/);
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
});

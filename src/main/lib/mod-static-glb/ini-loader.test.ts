import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { writeNamespaceMerge } from "../../services/mod-manager/merge/namespace.ts";
import { loadIniBundle } from "./ini-loader.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

async function makeRoot() {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ini-loader-"));
    tempRoots.push(root);
    return root;
}

describe("loadIniBundle merged refs", () => {
    it("loads valid in-directory merged references", async () => {
        const root = await makeRoot();
        const childDir = path.join(root, "AmberMain");
        await fse.ensureDir(childDir);
        await fse.writeFile(
            path.join(childDir, "Amber.ini"),
            `[TextureOverrideAmberPosition]
hash = abcdef01
`,
        );
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mods: ${path.join("AmberMain", "Amber.ini")}
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );

        const bundle = await loadIniBundle(mergedPath);
        assert.equal(bundle.sourcePaths.length, 2);
        assert.ok(
            bundle.sourcePaths.some((entry) => path.basename(path.dirname(entry)) === "AmberMain"),
        );
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition", "AmberPosition"],
        );
    });

    it("loads valid in-directory absolute merged references", async () => {
        const root = await makeRoot();
        const childDir = path.join(root, "AmberMain");
        await fse.ensureDir(childDir);
        const childIni = path.join(childDir, "Amber.ini");
        await fse.writeFile(
            childIni,
            `[TextureOverrideAmberPosition]
hash = abcdef01
`,
        );
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mod: ${childIni}
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );

        const bundle = await loadIniBundle(mergedPath);
        assert.equal(bundle.sourcePaths.length, 2);
        assert.ok(
            bundle.sourcePaths.some((entry) => path.basename(path.dirname(entry)) === "AmberMain"),
        );
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition", "AmberPosition"],
        );
    });

    it("rejects outside absolute, parent-directory, and missing merged references", async () => {
        const root = await makeRoot();
        const outside = await makeRoot();
        const outsideIni = path.join(outside, "secret.ini");
        await fse.writeFile(
            outsideIni,
            `[TextureOverrideOutsidePosition]
hash = 11111111
`,
        );
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mods: ${outsideIni}, ..\\${path.basename(outside)}\\secret.ini, missing.ini
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );

        const bundle = await loadIniBundle(mergedPath);
        assert.deepEqual(bundle.sourcePaths, [path.resolve(mergedPath)]);
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition"],
        );
    });

    it("rejects directory merged references", async () => {
        const root = await makeRoot();
        const childDir = path.join(root, "AmberMain");
        await fse.ensureDir(childDir);
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mods: AmberMain
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );
        const bundle = await loadIniBundle(mergedPath);
        assert.deepEqual(bundle.sourcePaths, [path.resolve(mergedPath)]);
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition"],
        );
    });

    it("rejects a merged reference that escapes through a symlink", async () => {
        const root = await makeRoot();
        const outside = await makeRoot();
        await fse.writeFile(
            path.join(outside, "secret.ini"),
            `[TextureOverrideOutsidePosition]
hash = 11111111
`,
        );
        await fse.symlink(outside, path.join(root, "escape"), "junction");
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mods: escape\\secret.ini
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );
        const bundle = await loadIniBundle(mergedPath);
        assert.deepEqual(bundle.sourcePaths, [path.resolve(mergedPath)]);
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition"],
        );
    });

    it("loads child sections from a namespace merge master ini created by writeNamespaceMerge", async () => {
        const root = await makeRoot();
        const childDir = path.join(root, "AmberMain");
        await fse.ensureDir(childDir);
        const childIni = path.join(childDir, "Amber.ini");
        await fse.writeFile(
            childIni,
            `[TextureOverrideAmberPosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Amber",
            sources: [{ iniPath: childIni, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const bundle = await loadIniBundle(masterPath);
        assert.equal(bundle.sourcePaths.length, 2);
        assert.ok(
            bundle.sourcePaths.some((entry) => path.resolve(entry) === path.resolve(childIni)),
        );
        assert.ok(
            bundle.sections.some(
                (s) => s.header === "TextureOverride" && s.name === "AmberPosition",
            ),
        );
        assert.ok(bundle.sections.some((s) => s.header === "Constants"));
    });

    it("loads child INI sections when source directory contains a comma", async () => {
        const root = await makeRoot();
        const childDir = path.join(root, "Amber, (Summer Outfit)");
        await fse.ensureDir(childDir);
        const childIni = path.join(childDir, "Amber.ini");
        await fse.writeFile(
            childIni,
            `[TextureOverrideAmberPosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );
        const mergedPath = path.join(root, "merged.ini");
        await fse.writeFile(
            mergedPath,
            `; Merged Mod: .\\${path.join("Amber, (Summer Outfit)", "Amber.ini")}
[TextureOverrideMergedPosition]
hash = fedcba98
`,
        );

        const bundle = await loadIniBundle(mergedPath);
        assert.equal(bundle.sourcePaths.length, 2);
        assert.ok(
            bundle.sourcePaths.some((entry) => path.resolve(entry) === path.resolve(childIni)),
        );
        assert.deepEqual(
            bundle.sections.map((section) => section.name),
            ["MergedPosition", "AmberPosition"],
        );
    });
});

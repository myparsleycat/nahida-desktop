import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { extractMergedModPaths } from "./ini-text.ts";
import {
    collectNamespaceChildren,
    unwrapNamespace,
    wrapHashedSections,
    writeNamespaceMerge,
} from "./namespace.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

const childIni = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`;

describe("namespace merge writer", () => {
    it("wraps hashed sections and writes a master stub", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(childPath, childIni);

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [{ iniPath: childPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        const child = await fse.readFile(childPath, "utf8");
        assert.match(master, /namespace = Klee\\Master/);
        assert.match(master, /hash = abcdef01/);
        assert.match(child, /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(child, /endif/);
        assert.equal(await fse.pathExists(path.join(root, "DISABLED_BACKUP_Klee.ini")), true);
    });

    it("does not treat an unrelated DISABLED ini as a merge backup", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Klee.ini");
        const disabled = path.join(root, "DISABLEDKlee.ini");
        await fse.writeFile(childPath, childIni);
        await fse.writeFile(disabled, "user-disabled");

        await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [{ iniPath: childPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        assert.equal(await fse.readFile(disabled, "utf8"), "user-disabled");
        assert.equal(
            await fse.readFile(path.join(root, "DISABLED_BACKUP_Klee.ini"), "utf8"),
            childIni,
        );
    });

    it("unwraps an existing namespace wrap before remastering", async () => {
        const wrapped = wrapHashedSections(childIni, "Old", 3);
        const unwrapped = await unwrapNamespace(wrapped);
        assert.doesNotMatch(unwrapped, /\$\\Old\\Master\\swapvar/);
        assert.match(unwrapped, /hash = abcdef01/);
        assert.match(unwrapped, /vb0 = ResourcePosition/);
    });

    it("flushes wrapped section before indented section headers", () => {
        const input = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
  [ResourcePosition]
type = Buffer
`;
        const wrapped = wrapHashedSections(input, "Klee", 0);
        assert.match(wrapped, /endif\n\n  \[ResourcePosition\]/);
    });

    it("unwraps a two-branch if/else-if/endif chain", async () => {
        const twoBranchIni = `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
if $\\Klee\\Master\\swapvar == 0
\tvb0 = ResourcePosition0
else if $\\Klee\\Master\\swapvar == 1
\tvb0 = ResourcePosition1
endif
ps-t0 = ResourceTexture
`;
        const unwrapped = await unwrapNamespace(twoBranchIni);
        assert.doesNotMatch(unwrapped, /\$\\Klee\\Master\\swapvar/);
        assert.doesNotMatch(unwrapped, /else if/i);
        assert.doesNotMatch(unwrapped, /endif/i);
        assert.match(unwrapped, /hash = abcdef01/);
        assert.match(unwrapped, /vb0 = ResourcePosition0/);
        assert.match(unwrapped, /vb0 = ResourcePosition1/);
        assert.match(unwrapped, /ps-t0 = ResourceTexture/);
    });

    it("preserves separate branch bodies for multiple vb0 assignments when wrapping", () => {
        const input = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition0
vb0 = ResourcePosition1
ps-t0 = ResourceTexture
`;
        const wrapped = wrapHashedSections(input, "Klee", 0);
        assert.match(wrapped, /match_priority = 0/);
        assert.match(wrapped, /if \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition0/);
        assert.match(
            wrapped,
            /else if \$\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition1\n\tps-t0 = ResourceTexture\nendif/,
        );
    });

    it("generates master INI with all swapvar indices when child contains multiple vb0 variants", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-multi-vb0-"));
        tempRoots.push(root);
        const multiVb0Ini = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition0
vb0 = ResourcePosition1
`;
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(childPath, multiVb0Ini);

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [{ iniPath: childPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        const child = await fse.readFile(childPath, "utf8");
        assert.match(master, /\$swapvar = 0,1/);
        assert.match(child, /if \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition0/);
        assert.match(
            child,
            /else if \$\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition1\nendif/,
        );
    });

    it("extractMergedModPaths handles one path per line, JSON arrays, and legacy lists", () => {
        const multiLine = [
            "; Merged Mod: C:\\Mods\\Klee, (Red Dress)\\Klee.ini",
            "; Merged Mod: C:\\Mods\\Klee, (Blue Dress)\\Klee.ini",
            "namespace = Klee\\Master",
        ].join("\n");
        assert.deepEqual(extractMergedModPaths(multiLine), [
            "C:\\Mods\\Klee, (Red Dress)\\Klee.ini",
            "C:\\Mods\\Klee, (Blue Dress)\\Klee.ini",
        ]);

        const jsonArray =
            '; Merged Mod: ["C:\\\\Mods\\\\Klee, (Red)\\\\Klee.ini", "D:\\\\Other.ini"]';
        assert.deepEqual(extractMergedModPaths(jsonArray), [
            "C:\\Mods\\Klee, (Red)\\Klee.ini",
            "D:\\Other.ini",
        ]);

        const singleCommaPath = "; Merged Mod: C:\\Mods\\Klee, (Red Dress)\\Klee.ini";
        assert.deepEqual(extractMergedModPaths(singleCommaPath), [
            "C:\\Mods\\Klee, (Red Dress)\\Klee.ini",
        ]);

        const legacyList = "; Merged Mod: a.ini, b.ini, c.ini";
        assert.deepEqual(extractMergedModPaths(legacyList), ["a.ini", "b.ini", "c.ini"]);

        const commaInDirLegacyList =
            "; Merged Mods: C:\\Mods\\Klee, (Red Dress)\\Klee.ini, D:\\Mods\\Klee, (Blue Dress)\\Klee.ini";
        assert.deepEqual(extractMergedModPaths(commaInDirLegacyList), [
            "C:\\Mods\\Klee, (Red Dress)\\Klee.ini",
            "D:\\Mods\\Klee, (Blue Dress)\\Klee.ini",
        ]);
    });

    it("round-trips merged paths containing commas and rediscovers them on subsequent merges", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-comma-"));
        tempRoots.push(root);

        const folder1 = path.join(root, "Klee, (Red Dress)");
        const folder2 = path.join(root, "Klee, (Blue Dress)");
        const folder3 = path.join(root, "Klee, (Green Dress)");
        await fse.ensureDir(folder1);
        await fse.ensureDir(folder2);
        await fse.ensureDir(folder3);

        const child1 = path.join(folder1, "Klee.ini");
        const child2 = path.join(folder2, "Klee.ini");
        const child3 = path.join(folder3, "Klee.ini");
        await fse.writeFile(child1, childIni);
        await fse.writeFile(child2, childIni);
        await fse.writeFile(child3, childIni);

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [
                { iniPath: child1, index: 0 },
                { iniPath: child2, index: 1 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const masterContent = await fse.readFile(masterPath, "utf8");
        const rel1 = path.relative(root, child1);
        const rel2 = path.relative(root, child2);
        assert.ok(
            masterContent.includes(`; Merged Mod: .\\${rel1}`) ||
                masterContent.includes(`; Merged Mod: ${rel1}`),
        );
        assert.ok(
            masterContent.includes(`; Merged Mod: .\\${rel2}`) ||
                masterContent.includes(`; Merged Mod: ${rel2}`),
        );

        const discovered = await collectNamespaceChildren(masterPath);
        assert.deepEqual(
            discovered.map((entry) => path.resolve(entry)).sort(),
            [path.resolve(child1), path.resolve(child2)].sort(),
        );

        // Subsequent merge referencing existing master
        const updatedMasterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [
                { iniPath: child1, index: 0 },
                { iniPath: child2, index: 1 },
                { iniPath: child3, index: 2 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
            existingMasterPath: masterPath,
        });

        const redisoveredAfterRemerge = await collectNamespaceChildren(updatedMasterPath);
        assert.deepEqual(
            redisoveredAfterRemerge.map((entry) => path.resolve(entry)).sort(),
            [path.resolve(child1), path.resolve(child2), path.resolve(child3)].sort(),
        );
    });

    it("unwraps a parenthesized simple master condition", async () => {
        const wrapped = `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
if ($\\Klee\\Master\\swapvar == 0)
	vb0 = ResourcePosition
endif
`;
        const unwrapped = await unwrapNamespace(wrapped);
        assert.doesNotMatch(unwrapped, /\$\\Klee\\Master\\swapvar/);
        assert.match(unwrapped, /vb0 = ResourcePosition/);
    });

    it("rejects compound leftover master conditions instead of flattening them", async () => {
        const compound = `[TextureOverrideKleePosition]
hash = abcdef01
if ($\\Klee\\Master\\swapvar == 0 && $foo == 1)
	vb0 = ResourcePosition
endif
`;
        await assert.rejects(unwrapNamespace(compound), /NAMESPACE_UNWRAP_INCOMPLETE/);

        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-compound-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(childPath, compound);
        await assert.rejects(
            writeNamespaceMerge({
                masterDir: root,
                name: "Klee",
                sources: [{ iniPath: childPath, index: 0 }],
                forwardKey: "]",
                backKey: "[",
                includeVanilla: false,
            }),
            /NAMESPACE_UNWRAP_INCOMPLETE/,
        );
    });

    it("drops a top-level master else when unwrapping", async () => {
        const withElse = `[TextureOverrideKleePosition]
hash = abcdef01
if $\\Klee\\Master\\swapvar == 0
	vb0 = ResourcePosition0
else
	vb0 = ResourcePosition1
endif
`;
        const unwrapped = await unwrapNamespace(withElse);
        assert.doesNotMatch(unwrapped, /\$\\Klee\\Master\\swapvar/);
        assert.doesNotMatch(unwrapped, /^else$/im);
        assert.match(unwrapped, /vb0 = ResourcePosition0/);
        assert.match(unwrapped, /vb0 = ResourcePosition1/);
    });

    it("scans for children when every listed Merged Mod path is missing", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-missing-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(
            childPath,
            `${childIni}if $\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif\n`,
        );
        const masterPath = path.join(root, "MasterKlee.ini");
        await fse.writeFile(
            masterPath,
            `; Merged Mod: ${path.join(root, "missing", "gone.ini")}
namespace = Klee\\Master
`,
        );

        const discovered = await collectNamespaceChildren(masterPath);
        assert.deepEqual(
            discovered.map((entry) => path.resolve(entry)),
            [path.resolve(childPath)],
        );
    });

    it("unions surviving listed paths with scanned children", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-partial-"));
        tempRoots.push(root);
        const listedChild = path.join(root, "Listed.ini");
        const scannedChild = path.join(root, "Scanned.ini");
        await fse.writeFile(listedChild, childIni);
        await fse.writeFile(
            scannedChild,
            `${childIni}if $\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition\nendif\n`,
        );
        const masterPath = path.join(root, "MasterKlee.ini");
        await fse.writeFile(
            masterPath,
            `; Merged Mod: ${listedChild}
; Merged Mod: ${path.join(root, "missing.ini")}
namespace = Klee\\Master
`,
        );

        const discovered = await collectNamespaceChildren(masterPath);
        assert.deepEqual(
            discovered.map((entry) => path.resolve(entry)).sort(),
            [path.resolve(listedChild), path.resolve(scannedChild)].sort(),
        );
    });

    it("preserves nested if/elif/else/endif blocks when unwrapping namespace wrap", async () => {
        const complexIni = `[TextureOverrideBlackSwanHairBlend]
hash = e2770c9a
match_priority = 0
if $\\BlackswanMerge\\Master\\swapvarZ==0
\thandling = skip
\tvb2 = ResourceBlackSwanHairBlend
\tif DRAW_TYPE == 1
\t\tvb0 = ResourceBlackSwanHairPosition
\t\tdraw = 5376, 0
\tendif
\tResourceBlackSwanHairDrawCS = copy ResourceBlackSwanHairDrawCS
\tif DRAW_TYPE == 8
\t\tResource\\SRMI\\PositionBuffer = ref ResourceBlackSwanHairPositionCS
\t\tResource\\SRMI\\BlendBuffer = ref ResourceBlackSwanHairBlendCS
\t\tResource\\SRMI\\DrawBuffer = ref ResourceBlackSwanHairDrawCS
\t\t$\\SRMI\\vertcount = 5376
\telif DRAW_TYPE != 1
\t\t$_blend_ = 2
\tendif
endif
`;
        const unwrapped = await unwrapNamespace(complexIni);
        assert.doesNotMatch(unwrapped, /\$\\BlackswanMerge\\Master\\swapvarZ/);
        assert.match(unwrapped, /handling = skip/);
        assert.match(
            unwrapped,
            /if DRAW_TYPE == 1\n\tvb0 = ResourceBlackSwanHairPosition\n\tdraw = 5376, 0\nendif/,
        );
        assert.match(unwrapped, /elif DRAW_TYPE != 1\n\t\$_blend_ = 2\nendif/);
    });

    it("selects representative source with position hash and ignores helper files such as ORFix.ini", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-rep-"));
        tempRoots.push(root);
        const orfixPath = path.join(root, "ORFix.ini");
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(
            orfixPath,
            `[TextureOverrideORFix]
hash = helper01
run = CommandList\\global\\ORFix
`,
        );
        await fse.writeFile(childPath, childIni);

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [
                { iniPath: orfixPath, index: 0 },
                { iniPath: childPath, index: 0 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        assert.match(master, /\[TextureOverrideKleePosition\]/);
        assert.match(master, /hash = abcdef01/);
        assert.doesNotMatch(master, /hash = helper01/);
    });

    it("selects representative WWMI source with MarkBoneDataCB hash over helper files", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-wwmi-rep-"));
        tempRoots.push(root);
        const orfixPath = path.join(root, "ORFix.ini");
        const roverPath = path.join(root, "Rover.ini");
        await fse.writeFile(
            orfixPath,
            `[TextureOverrideORFix]
hash = helper01
run = CommandList\\global\\ORFix
`,
        );
        await fse.writeFile(
            roverPath,
            `; WWMI
[TextureOverrideRoverMarkBoneDataCB]
hash = 98765432
vb0 = ResourceRoverPosition
`,
        );

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Rover",
            sources: [
                { iniPath: orfixPath, index: 0 },
                { iniPath: roverPath, index: 0 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        assert.match(master, /\[TextureOverrideRoverMarkBoneDataCB\]/);
        assert.match(master, /hash = 98765432/);
        assert.doesNotMatch(master, /hash = helper01/);
    });
});

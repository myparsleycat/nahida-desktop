import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { extractMergedModPaths, extractPositionSectionHash } from "./ini-text.ts";
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
        assert.match(master, /namespace = Klee\\Master\n; Constants ---------------------------/);
        assert.match(
            master,
            /; Overrides ---------------------------\n\n\[TextureOverrideKleePosition\]\nhash = abcdef01\n\$active = 1/,
        );
        assert.match(
            child,
            /hash = abcdef01\nmatch_priority = 0\nif \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif/,
        );
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

    it("keeps nested DRAW_TYPE branches inside a single swapvar wrap", () => {
        const input = `[TextureOverrideHertaHairBlend]
hash = af0ef73c
handling = skip
vb2 = ResourceHertaHairBlend
if DRAW_TYPE == 1
	vb0 = ResourceHertaHairPosition
	draw = 2981, 0
endif
if DRAW_TYPE == 8
	Resource\\SRMI\\PositionBuffer = ref ResourceHertaHairPositionCS
	$\\SRMI\\vertcount = 2981
endif
`;
        const wrapped = wrapHashedSections(input, "HertaMerge", 0);
        assert.match(
            wrapped,
            /hash = af0ef73c\nmatch_priority = 0\nif \$\\HertaMerge\\Master\\swapvar==0\n\thandling = skip\n\tvb2 = ResourceHertaHairBlend\n\tif DRAW_TYPE == 1\n\t\tvb0 = ResourceHertaHairPosition\n\t\tdraw = 2981, 0\n\tendif\n\tif DRAW_TYPE == 8\n\t\tResource\\SRMI\\PositionBuffer = ref ResourceHertaHairPositionCS\n\t\t\$\\SRMI\\vertcount = 2981\n\tendif\nendif/,
        );
        assert.doesNotMatch(wrapped, /else if \$\\HertaMerge\\Master\\swapvar/);
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

    it("wraps every hashed section as one swapvar branch, including multiple vb0 lines", () => {
        const input = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition0
vb0 = ResourcePosition1
ps-t0 = ResourceTexture
`;
        const wrapped = wrapHashedSections(input, "Klee", 0);
        assert.match(
            wrapped,
            /hash = abcdef01\nmatch_priority = 0\nif \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition0\n\tvb0 = ResourcePosition1\n\tps-t0 = ResourceTexture\nendif/,
        );
        assert.doesNotMatch(wrapped, /else if \$\\Klee\\Master\\swapvar==1/);
    });

    it("keeps EFMI match_index_count outside the swapvar wrap", async () => {
        const input = `[TextureOverride_Component0]
hash = 79a0cd6f
match_priority = 0
match_index_count = 48909
$object_detected = 1
$lod_level = 0
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
`;
        const wrapped = wrapHashedSections(input, "Liino", 1);
        assert.match(
            wrapped,
            /hash = 79a0cd6f\nmatch_priority = 1\nmatch_index_count = 48909\nif \$\\Liino\\Master\\swapvar==1\n\t\$object_detected = 1\n\t\$lod_level = 0\n\tif \$mod_enabled && DRAW_TYPE == 4\n\t    handling = skip\n\t    run = CommandList_Draw_Component0\n\tendif\nendif/,
        );
        assert.doesNotMatch(wrapped, /if \$\\Liino\\Master\\swapvar==1\n\tmatch_index_count/);
        assert.doesNotMatch(wrapped, /match_priority = 0/);

        const unwrapped = await unwrapNamespace(wrapped);
        assert.doesNotMatch(unwrapped, /match_priority/);
        assert.doesNotMatch(unwrapped, /\$\\Liino\\Master\\swapvar/);
        assert.match(unwrapped, /match_index_count = 48909/);
        assert.match(unwrapped, /\$object_detected = 1/);
    });

    it("hoists a match filter that appears after runtime commands", () => {
        const input = `[TextureOverride_Component0]
hash = 79a0cd6f
$object_detected = 1
match_index_count = 48909
handling = skip
`;
        const wrapped = wrapHashedSections(input, "Liino", 1);
        assert.match(
            wrapped,
            /hash = 79a0cd6f\nmatch_priority = 1\nmatch_index_count = 48909\nif \$\\Liino\\Master\\swapvar==1\n\t\$object_detected = 1\n\thandling = skip\nendif/,
        );
    });

    it("wraps EFMI texture overrides that already have match_priority and object_detected", () => {
        const input = `[TextureOverride_Texture0]
hash = 0d62f6d9
match_priority = 0
if $object_detected
    this = Resource_Texture0
endif
`;
        const wrapped = wrapHashedSections(input, "Liino", 1);
        assert.match(
            wrapped,
            /hash = 0d62f6d9\nmatch_priority = 1\nif \$\\Liino\\Master\\swapvar==1\n\tif \$object_detected\n\t    this = Resource_Texture0\n\tendif\nendif/,
        );
        assert.doesNotMatch(wrapped, /match_priority = 0/);
    });

    it("copies match_index_count onto the master active overlay", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-efmi-active-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Liino.ini");
        await fse.writeFile(
            childPath,
            `[TextureOverride_Component0]
hash = 79a0cd6f
match_index_count = 48909
$object_detected = 1
`,
        );

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Liino",
            sources: [{ iniPath: childPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        assert.match(
            master,
            /\[TextureOverrideLiinoComponent0\]\nhash = 79a0cd6f\nmatch_index_count = 48909\n\$active = 1/,
        );
        assert.doesNotMatch(
            master,
            /\[TextureOverrideLiinoComponent0\]\nhash = 79a0cd6f\n\$active = 1/,
        );
    });

    it("does not invent extra swapvar indices from multiple vb0 lines in one child", async () => {
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
        assert.match(master, /\$swapvar = 0\n/);
        assert.doesNotMatch(master, /\$swapvar = 0,1/);
        assert.match(
            child,
            /if \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition0\n\tvb0 = ResourcePosition1\nendif/,
        );
        assert.doesNotMatch(child, /else if \$\\Klee\\Master\\swapvar==1/);
    });

    it("remasters two already-namespaced children into one master", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-nested-"));
        tempRoots.push(root);
        const alphaDir = path.join(root, "Alpha");
        const betaDir = path.join(root, "Beta");
        const alphaPath = path.join(alphaDir, "A.ini");
        const betaPath = path.join(betaDir, "B.ini");
        await fse.ensureDir(alphaDir);
        await fse.ensureDir(betaDir);
        await fse.writeFile(alphaPath, childIni);
        await fse.writeFile(betaPath, childIni);

        await writeNamespaceMerge({
            masterDir: alphaDir,
            name: "Alpha",
            sources: [{ iniPath: alphaPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });
        await writeNamespaceMerge({
            masterDir: betaDir,
            name: "Beta",
            sources: [{ iniPath: betaPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [
                { iniPath: alphaPath, index: 0 },
                { iniPath: betaPath, index: 1 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        const alpha = await fse.readFile(alphaPath, "utf8");
        const beta = await fse.readFile(betaPath, "utf8");
        assert.match(master, /namespace = Klee\\Master\n; Constants ---------------------------/);
        assert.match(master, /\$swapvar = 0,1\n/);
        assert.match(
            master,
            /; Overrides ---------------------------\n\n\[TextureOverrideKleePosition\]\nhash = abcdef01\n\$active = 1/,
        );
        assert.match(
            alpha,
            /hash = abcdef01\nmatch_priority = 0\nif \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif/,
        );
        assert.match(
            beta,
            /hash = abcdef01\nmatch_priority = 1\nif \$\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition\nendif/,
        );
        assert.doesNotMatch(alpha, /\$\\Alpha\\Master\\swapvar/);
        assert.doesNotMatch(beta, /\$\\Beta\\Master\\swapvar/);
        assert.doesNotMatch(alpha, /else if \$\\Klee\\Master\\swapvar/);
        assert.doesNotMatch(beta, /else if \$\\Klee\\Master\\swapvar/);
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
        const headerLines = masterContent
            .split(/\r?\n/)
            .filter((line) => line.startsWith("; Merged Mod:"));
        assert.equal(headerLines.length, 1);
        assert.ok(headerLines[0]?.includes(rel1));
        assert.ok(headerLines[0]?.includes(rel2));
        assert.deepEqual(extractMergedModPaths(masterContent), [`.\\${rel1}`, `.\\${rel2}`]);

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

    it("reads the position hash after a filename-only ResourcePosition section", () => {
        const text = `[ResourcePosition]
filename = KleePosition.buf

[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`;
        assert.equal(extractPositionSectionHash(text), "abcdef01");
    });

    it("keeps the position hash when a later hairblend source is also present", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-resource-pos-"));
        tempRoots.push(root);
        const bodyPath = path.join(root, "Klee.ini");
        const hairPath = path.join(root, "KleeHair.ini");
        await fse.writeFile(
            bodyPath,
            `[ResourcePosition]
filename = KleePosition.buf

[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );
        await fse.writeFile(
            hairPath,
            `[TextureOverrideKleeHairBlend]
hash = hairblend01
vb2 = ResourceHairBlend
`,
        );

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [
                { iniPath: bodyPath, index: 0 },
                { iniPath: hairPath, index: 0 },
            ],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        assert.match(master, /\[TextureOverrideKleePosition\]/);
        assert.match(master, /hash = abcdef01/);
        assert.doesNotMatch(master, /hash = hairblend01/);
    });
});

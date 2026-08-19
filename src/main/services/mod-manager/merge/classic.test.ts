import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { writeClassicMerge } from "./classic.ts";
import { extractMergedModPaths } from "./ini-text.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("writeClassicMerge", () => {
    it("writes merged.ini and disables source inis", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        const bDir = path.join(root, "B");
        await fse.ensureDir(aDir);
        await fse.ensureDir(bDir);
        const aIni = path.join(aDir, "Klee.ini");
        const bIni = path.join(bDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = A.buf
`,
        );
        await fse.writeFile(
            bIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = B.buf
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [
                { iniPath: aIni, groupIndex: 0 },
                { iniPath: bIni, groupIndex: 1 },
            ],
            forwardKey: "vk_right",
            backKey: "vk_left",
        });

        const text = await fse.readFile(output, "utf8");
        const mergedModLines = text
            .split(/\r?\n/)
            .filter((line) => line.startsWith("; Merged Mod:"));
        assert.equal(mergedModLines.length, 1);
        assert.match(mergedModLines[0] ?? "", /A[\\/]Klee\.ini, .*B[\\/]Klee\.ini/);
        assert.match(text, /; Constants ---------------------------/);
        assert.match(text, /; Shader ------------------------------/);
        assert.match(text, /; Overrides ---------------------------/);
        assert.match(text, /; CommandList -------------------------/);
        assert.match(text, /; Resources ---------------------------/);
        assert.match(text, /\$swapvar = 0,1/);
        assert.match(text, /\[KeySwap\]/);
        assert.match(text, /back = vk_left/);
        assert.match(text, /\[CommandListKleePosition\]/);
        assert.match(text, /\[ResourcePosition\.0\]/);
        assert.equal(await fse.pathExists(aIni), false);
        assert.equal(await fse.pathExists(path.join(aDir, "DISABLED_BACKUP_Klee.ini")), true);
    });

    it("does not overwrite an existing DISABLED source ini", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        const bDir = path.join(root, "B");
        await fse.ensureDir(aDir);
        await fse.ensureDir(bDir);
        const aIni = path.join(aDir, "Klee.ini");
        const bIni = path.join(bDir, "Klee.ini");
        const disabledA = path.join(aDir, "DISABLEDKlee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );
        await fse.writeFile(
            bIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );
        await fse.writeFile(disabledA, "user-disabled");

        await writeClassicMerge({
            outputDir: root,
            sources: [
                { iniPath: aIni, groupIndex: 0 },
                { iniPath: bIni, groupIndex: 1 },
            ],
            forwardKey: "vk_right",
        });

        assert.equal(await fse.pathExists(aIni), false);
        assert.equal(await fse.readFile(disabledA, "utf8"), "user-disabled");
        assert.equal(await fse.pathExists(path.join(aDir, "DISABLED_BACKUP_Klee.ini")), true);
    });

    it("formats relative filename in resources and keeps resource references accurate", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        await fse.ensureDir(aDir);
        const aIni = path.join(aDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
handling = skip
ps-t0 = 1
this = CommandListFace

[TextureOverrideKleeVertexLimitRaise]
hash = fedcba98
override_vertex_count = 50000
override_byte_stride = 40

[ResourcePosition]
filename = Relative.buf
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [{ iniPath: aIni, groupIndex: 0 }],
            forwardKey: "vk_right",
        });

        const text = await fse.readFile(output, "utf8");
        assert.match(text, /filename = \.\\A[\\/]Relative\.buf/);
        assert.match(
            text,
            /\[TextureOverrideKleeVertexLimitRaise\]\nhash = fedcba98\noverride_vertex_count = 50000\noverride_byte_stride = 40/,
        );
        assert.match(text, /handling = skip/);
        assert.match(text, /ps-t0 = 1/);
        assert.match(text, /this = CommandListFace/);
        assert.doesNotMatch(text, /ps-t0 = 1\.0/);
        assert.doesNotMatch(text, /this = CommandListFace\.0/);
    });

    it("does not rewrite comparison control-flow lines as key/value pairs", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        await fse.ensureDir(aDir);
        const aIni = path.join(aDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
if DRAW_TYPE == 1
	vb0 = ResourcePosition
endif
[ResourcePosition]
filename = A.buf
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [{ iniPath: aIni, groupIndex: 0 }],
            forwardKey: "vk_right",
        });

        const text = await fse.readFile(output, "utf8");
        assert.match(
            text,
            /\[CommandListKleePosition\]\nif \$swapvar == 0\n\tif DRAW_TYPE == 1\n\t\tvb0 = ResourcePosition\.0\n\tendif\nendif/,
        );
        assert.doesNotMatch(text, /if DRAW_TYPE = = 1/);
        assert.doesNotMatch(text, /if DRAW_TYPE = 1/);
    });

    it("keeps nested DRAW_TYPE branches inside the swapvar command list", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "Hertaf0000Mod");
        await fse.ensureDir(aDir);
        const aIni = path.join(aDir, "Herta.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideHertaHairBlend]
hash = af0ef73c
handling = skip
vb2 = ResourceHertaHairBlend
if DRAW_TYPE == 1
	vb0 = ResourceHertaHairPosition
	draw = 2984, 0
endif
if DRAW_TYPE == 8
	Resource\\SRMI\\PositionBuffer = ref ResourceHertaHairPositionCS
	$\\SRMI\\vertcount = 2984
endif

[ResourceHertaHairBlend]
type = Buffer
stride = 32
filename = HertaHairBlend.buf

[ResourceHertaHairPosition]
type = Buffer
stride = 40
filename = HertaHairPosition.buf

[ResourceHertaHairPositionCS]
type = StructuredBuffer
stride = 40
filename = HertaHairPosition.buf
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [{ iniPath: aIni, groupIndex: 0 }],
            forwardKey: "vk_right",
        });

        const text = await fse.readFile(output, "utf8");
        assert.match(text, /\[CommandListHertaHairBlend\]/);
        assert.match(text, /if \$swapvar == 0/);
        assert.match(text, /\thandling = skip/);
        assert.match(text, /\tvb2 = ResourceHertaHairBlend\.0/);
        assert.match(
            text,
            /\tif DRAW_TYPE == 1\n\t\tvb0 = ResourceHertaHairPosition\.0\n\t\tdraw = 2984, 0\n\tendif/,
        );
        assert.match(
            text,
            /\tif DRAW_TYPE == 8\n\t\tResource\\SRMI\\PositionBuffer = ref ResourceHertaHairPositionCS\.0\n\t\t\$\\SRMI\\vertcount = 2984\n\tendif/,
        );
        assert.match(text, /\[ResourceHertaHairBlend\.0\]/);
        assert.match(text, /\[ResourceHertaHairPositionCS\.0\]/);
        assert.doesNotMatch(text, /if DRAW_TYPE = = 1/);
        assert.doesNotMatch(text, /if DRAW_TYPE = 1/);
    });

    it("adds CommandListCreditInfo to Present when a creditinfo section exists", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        await fse.ensureDir(aDir);
        const aIni = path.join(aDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition

[CommandListCreditInfo]
if $creditinfo == 0
	; credits
endif
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [{ iniPath: aIni, groupIndex: 0 }],
            forwardKey: "vk_right",
        });

        const text = await fse.readFile(output, "utf8");
        assert.match(text, /\[Present\]\npost \$active = 0\nrun = CommandListCreditInfo/);
        assert.match(text, /\[CommandListCreditInfo\]/);
    });

    it("emits one comma-separated Merged Mod line", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "Klee, (Red Dress)");
        const bDir = path.join(root, "Klee, (Blue Dress)");
        await fse.ensureDir(aDir);
        await fse.ensureDir(bDir);
        const aIni = path.join(aDir, "Klee.ini");
        const bIni = path.join(bDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );
        await fse.writeFile(
            bIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [
                { iniPath: aIni, groupIndex: 0 },
                { iniPath: bIni, groupIndex: 1 },
            ],
            forwardKey: "vk_right",
        });

        const text = await fse.readFile(output, "utf8");
        const headerLines = text.split(/\r?\n/).filter((line) => line.startsWith("; Merged Mod:"));
        assert.equal(headerLines.length, 1);
        assert.ok(headerLines[0]?.includes("Klee, (Red Dress)"));
        assert.ok(headerLines[0]?.includes("Klee, (Blue Dress)"));
        assert.deepEqual(extractMergedModPaths(text), [
            `.\\Klee, (Red Dress)\\Klee.ini`,
            `.\\Klee, (Blue Dress)\\Klee.ini`,
        ]);
    });
});

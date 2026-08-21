import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import {
    applyVariableSelection,
    computeIneffectiveValues,
    evaluateViewerState,
} from "@shared/mod-viewer/eval";
import fse from "fs-extra";
import { PNG } from "pngjs";
import sharp from "sharp";
import { afterEach, describe, it } from "vitest";

import { DNF_FALSE, isUnconstrained, sameDnf } from "./dnf";
import { buildDrawGroups, isLikelyWwmiDiffuse, pickWwmiDumpDiffuse } from "./draw-groups";
import { extractResources, parseIniFile, parseIniText } from "./ini";
import { loadModViewerPayload } from "./load";

const tempRoots: string[] = [];
const PNG_1X1 = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
    "base64",
);
const PNG_2X2 = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAE0lEQVR4AWP8z8DwnwEImBigAAAfFwICgH3ifwAAAABJRU5ErkJggg==",
    "base64",
);

function makeColorPng(width = 16, height = 16) {
    const png = new PNG({ width, height });
    for (let y = 0; y < height; y++) {
        for (let x = 0; x < width; x++) {
            const offset = (y * width + x) * 4;
            png.data[offset] = Math.round((x / Math.max(width - 1, 1)) * 255);
            png.data[offset + 1] = Math.round((y / Math.max(height - 1, 1)) * 255);
            png.data[offset + 2] = 90;
            png.data[offset + 3] = 255;
        }
    }
    return PNG.sync.write(png);
}

function makeOversizedPng(width: number, height: number) {
    return sharp({
        create: {
            width,
            height,
            channels: 4,
            background: { r: 32, g: 64, b: 96, alpha: 1 },
        },
    })
        .png()
        .toBuffer();
}

function makeDdsHeader(width: number, height: number) {
    const header = Buffer.alloc(128);
    header.write("DDS ", 0);
    header.writeUInt32LE(124, 4);
    header.writeUInt32LE(height, 12);
    header.writeUInt32LE(width, 16);
    return header;
}

function makeFlatPng(width = 16, height = 16) {
    const png = new PNG({ width, height });
    for (let index = 0; index < width * height; index++) {
        png.data[index * 4] = 240;
        png.data[index * 4 + 1] = 240;
        png.data[index * 4 + 2] = 240;
        png.data[index * 4 + 3] = 255;
    }
    return PNG.sync.write(png);
}

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("loadModViewerPayload", () => {
    it("loads INI buffers and textures without an asset-layout path", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\\GIMI\\Diffuse = ref ResourceDiffuse
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceDiffuse]
filename = diffuse.png
`,
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        const mesh = payload.meshes[0];
        assert.equal(mesh.positions.length, 9);
        assert.deepEqual([...mesh.indices], [0, 1, 2]);
        assert.ok(mesh.texKey);
        assert.ok(payload.textures[mesh.texKey!]);
        assert.equal(payload.textures[mesh.texKey!].role, "diffuse");
        assert.match(await fse.readFile(path.join(root, "mod.ini"), "utf8"), /drawindexed/);
    });

    it("loads mesh INIs from subfolders when the root INI has no geometry", async () => {
        const root = await makeMod({
            ini: `[Constants]
global $swap = 0
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "body"));
                await fse.writeFile(
                    path.join(dir, "body", "body.ini"),
                    `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = ../pos.buf
stride = 40
[ResourceTc]
filename = ../tc.buf
stride = 20
[ResourceBodyIB]
filename = ../body.ib
format = DXGI_FORMAT_R32_UINT
`,
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.equal(payload.meshes[0].positions.length, 9);
        assert.deepEqual([...payload.meshes[0].indices], [0, 1, 2]);
    });

    it("keeps mid-section ib reassignment as two meshes with distinct index sources", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBodyBlend]
ib = ResourceBodyHeadIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
ib = ResourceBodyDressIB
drawindexed = 3, 0, 0
[ResourceBodyHeadIB]
filename = head.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyDressIB]
filename = dress.ib
format = DXGI_FORMAT_R32_UINT
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "head.ib"),
                    Buffer.from(new Uint32Array([10, 11, 12]).buffer),
                );
                await fse.writeFile(
                    path.join(dir, "dress.ib"),
                    Buffer.from(new Uint32Array([20, 21, 22]).buffer),
                );
            },
            vertexCount: 32,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 2);
        const vertexSets = payload.meshes
            .map((mesh) =>
                [...mesh.indices]
                    .map((index) => Math.round(mesh.positions[index * 3] ?? 0))
                    .sort((left, right) => left - right),
            )
            .sort((left, right) => left[0] - right[0]);
        assert.deepEqual(vertexSets, [
            [10, 11, 12],
            [20, 21, 22],
        ]);
    });

    it("hides and shows draws from a cycle variable DNF", async () => {
        const root = await makeMod({
            ini: `[Constants]
global $outfit = 0
[KeyOutfit]
type = cycle
$outfit = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $outfit == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 2);
        const first = evaluateViewerState(payload, { outfit: "0" });
        const second = evaluateViewerState(payload, { outfit: "1" });
        assert.equal(first.meshes[0].visible, true);
        assert.equal(first.meshes[1].visible, false);
        assert.equal(second.meshes[0].visible, false);
        assert.equal(second.meshes[1].visible, true);
    });

    it("resolves later-write-wins diffuse and conditional aux maps", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $color = 0
global $detail = 0
global $metal = 0
[KeyColor]
type = cycle
$color = 0,1,2
[KeyDetail]
type = cycle
$detail = 0,1
[KeyMetal]
type = cycle
$metal = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\\GIMI\\Diffuse = ref ResourceDiffuseA
if $color == 1
Resource\\GIMI\\Diffuse = ref ResourceDiffuseB
endif
if $color == 2
Resource\\GIMI\\Diffuse = ref ResourceDiffuseC
endif
if $detail == 0
Resource\\ZZMI\\NormalMap = ref ResourceNormalA
else
Resource\\ZZMI\\NormalMap = ref ResourceNormalB
endif
Resource\\ZZMI\\LightMap = ref ResourceLight
if $metal == 1
Resource\\ZZMI\\MaterialMap = ref ResourceMaterial
endif
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceDiffuseA]
filename = diffuseA.png
[ResourceDiffuseB]
filename = diffuseB.png
[ResourceDiffuseC]
filename = diffuseC.png
[ResourceNormalA]
filename = normalA.png
[ResourceNormalB]
filename = normalB.png
[ResourceLight]
filename = light.png
[ResourceMaterial]
filename = material.png
`,
            extra: async (dir) => {
                for (const name of [
                    "diffuseA.png",
                    "diffuseB.png",
                    "diffuseC.png",
                    "normalA.png",
                    "normalB.png",
                    "light.png",
                    "material.png",
                ]) {
                    await fse.writeFile(path.join(dir, name), PNG_1X1);
                }
            },
        });

        const payload = await loadModViewerPayload(root);
        const mesh = payload.meshes[0];
        assert.ok(mesh.textureVariants.length >= 3);
        assert.ok(mesh.normalMapVariants.length >= 2);
        assert.equal(mesh.lightMapKey?.endsWith("light.png"), true);
        assert.ok(mesh.materialMapVariants.length >= 1);
        assert.ok(mesh.materialMapVariants[0].conditions.length > 0);

        const color0 = evaluateViewerState(payload, { color: "0", detail: "0", metal: "0" });
        const color1 = evaluateViewerState(payload, { color: "1", detail: "1", metal: "1" });
        const color2 = evaluateViewerState(payload, { color: "2", detail: "0", metal: "0" });
        assert.match(String(color0.meshes[0].texKey), /diffuseA\.png$/);
        assert.match(String(color1.meshes[0].texKey), /diffuseB\.png$/);
        assert.match(String(color2.meshes[0].texKey), /diffuseC\.png$/);
        assert.match(String(color0.meshes[0].normalMapKey), /normalA\.png$/);
        assert.match(String(color1.meshes[0].normalMapKey), /normalB\.png$/);
        assert.equal(color0.meshes[0].materialMapKey, null);
        assert.match(String(color1.meshes[0].materialMapKey), /material\.png$/);
    });

    it("keeps a single remaining conditional diffuse variant", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $color = 0
[KeyColor]
type = cycle
$color = 0,1,2
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\\GIMI\\Diffuse = ref ResourceDiffuseA
if $color == 1
Resource\\GIMI\\Diffuse = ref ResourceDiffuseB
endif
if $color == 2
Resource\\GIMI\\Diffuse = ref ResourceDiffuseC
endif
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceDiffuseA]
filename = missingA.png
[ResourceDiffuseB]
filename = diffuseB.png
[ResourceDiffuseC]
filename = missingC.png
`,
            extra: async (dir) => {
                await fse.writeFile(path.join(dir, "diffuseB.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const mesh = payload.meshes[0];
        assert.equal(mesh.textureVariants.length, 1);
        assert.equal(isUnconstrained(mesh.textureVariants[0].conditions), false);

        const color0 = evaluateViewerState(payload, { color: "0" });
        const color1 = evaluateViewerState(payload, { color: "1" });
        assert.match(String(color1.meshes[0].texKey), /diffuseB\.png$/);
        assert.notEqual(color0.meshes[0].texKey, color1.meshes[0].texKey);
    });

    it("replays Present literal assignments before visibility", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $outfit = 0
global $piece = 0
[KeyOutfit]
type = cycle
$outfit = 0,1
[Present]
if $outfit == 0
    $piece = 0
elif $outfit == 1
    $piece = 1
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $piece == 0
drawindexed = 3, 0, 0
endif
if $piece == 1
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.stateRules.some((rule) => rule.var === "piece"));
        const first = evaluateViewerState(payload, { outfit: "0" });
        const second = evaluateViewerState(payload, { outfit: "1" });
        assert.equal(String(first.state.piece), "0");
        assert.equal(String(second.state.piece), "1");
        assert.equal(first.meshes[0].visible, true);
        assert.equal(first.meshes[1].visible, false);
        assert.equal(second.meshes[0].visible, false);
        assert.equal(second.meshes[1].visible, true);
    });

    it("flips UV V in the mesh builder and keeps TextureLoader default flipY", async () => {
        const payloadSource = await fse.readFile(
            new URL(
                "../../../renderer/src/components/tools/model-viewer/model-viewer-payload.ts",
                import.meta.url,
            ),
            "utf8",
        );
        assert.doesNotMatch(payloadSource, /flipY\s*=\s*false/);
        assert.match(payloadSource, /TextureLoader default flipY=true/);

        const tc = Buffer.alloc(8 * 20);
        const uvs: Array<[number, number]> = [
            [0, 0.25],
            [1, 0.75],
            [0.5, 0.5],
        ];
        for (const [index, [u, v]] of uvs.entries()) {
            tc.writeFloatLE(u, index * 20 + 4);
            tc.writeFloatLE(v, index * 20 + 8);
        }
        const root = await makeMod({
            ini: `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            tc,
        });
        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes[0].uvs);
        assert.equal(payload.meshes[0].uvs![0], 0);
        assert.equal(payload.meshes[0].uvs![1], 0.75);
        assert.equal(payload.meshes[0].uvs![2], 1);
        assert.equal(payload.meshes[0].uvs![3], 0.25);
        assert.equal(payload.meshes[0].uvs![4], 0.5);
        assert.equal(payload.meshes[0].uvs![5], 0.5);
    });

    it("replays menu slot effects when the selected value changes", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $top = 1
global persist $pasties = 0
global persist $glasses = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $top = 1 - $top
    if $top == 0
        $pasties = 1
    endif
elif $clickedSlot == 2
    $glasses = $glasses + 1
    if $glasses > 2
        $glasses = 0
    endif
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1
drawindexed = 3, 0, 0
endif
if $pasties == 1
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        const top = payload.variables.find((variable) => variable.id === "top");
        assert.ok(top);
        assert.ok(top.effects?.some((effect) => effect.var === "pasties"));
        const dialogSource = await fse.readFile(
            new URL(
                "../../../renderer/src/components/tools/model-viewer/model-viewer-dialog.tsx",
                import.meta.url,
            ),
            "utf8",
        );
        assert.match(dialogSource, /applyVariableSelection/);

        const withoutEffects = evaluateViewerState(payload, { top: "0", pasties: "0" });
        assert.equal(withoutEffects.meshes[0].visible, false);
        assert.equal(withoutEffects.meshes[1].visible, false);

        const selected = applyVariableSelection(payload.defaultState, top, "0");
        assert.equal(String(selected.top), "0");
        assert.equal(String(selected.pasties), "1");
        const withEffects = evaluateViewerState(payload, selected);
        assert.equal(withEffects.meshes[0].visible, false);
        assert.equal(withEffects.meshes[1].visible, true);
    });

    it("applies toggle side effects but hides single-value vars from the panel", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $top = 1
global persist $onepiece = 1
[KeyTop]
condition = $active == 1
key = no_modifiers UP
type = cycle
$top = 0,1,2,3
$onepiece = 0
[KeyOnePiece]
condition = $active == 1
key = no_modifiers DOWN
type = cycle
$onepiece = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1 && $onepiece == 0
drawindexed = 3, 0, 0
endif
if $onepiece == 1
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        const top = payload.variables.find((variable) => variable.id === "top");
        assert.ok(top);
        assert.ok(top.effects?.some((effect) => effect.var === "onepiece"));
        assert.ok(
            !payload.variables.some(
                (variable) => variable.id === "onepiece" && variable.label === "Top",
            ),
        );

        const selected = applyVariableSelection(payload.defaultState, top, "2");
        assert.equal(String(selected.top), "2");
        assert.equal(String(selected.onepiece), "0");
        const evaluated = evaluateViewerState(payload, selected);
        assert.equal(evaluated.meshes[0].visible, false);
        assert.equal(evaluated.meshes[1].visible, false);
    });

    it("tracks toggle effect variables as gating vars for draw conditions", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $top = 0
global persist $resetvar = 0
[KeyTop]
condition = $active == 1
key = no_modifiers UP
type = cycle
$top = 0,1,2,3
$resetvar = 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1
if $resetvar == 0
drawindexed = 3, 0, 0
endif
endif
if $resetvar == 1
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);

        // With resetvar=0 (default), top=0 should hide both draws.
        const baseline = evaluateViewerState(payload, { top: "0", resetvar: "0" });
        assert.equal(baseline.meshes[0].visible, false);
        assert.equal(baseline.meshes[1].visible, false);

        // With resetvar=1, the second draw should appear regardless of top.
        const reset = evaluateViewerState(payload, { top: "0", resetvar: "1" });
        assert.equal(reset.meshes[0].visible, false);
        assert.equal(reset.meshes[1].visible, true);

        // With top=1 and resetvar=0, the first draw should appear.
        const active = evaluateViewerState(payload, { top: "1", resetvar: "0" });
        assert.equal(active.meshes[0].visible, true);
        assert.equal(active.meshes[1].visible, false);
    });

    it("disables toggle values that produce no visible mesh change", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $skirt = 0
global persist $bottom = 0
global persist $futa = 0
[KeySkirt]
type = cycle
$skirt = 0,1
[KeyBottom]
type = cycle
$bottom = 0,1
[KeyFutanari]
type = cycle
$futa = 0,1,2,3,4
$bottom = 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $skirt == 1 && ($bottom != 0 || ($bottom == 0 && $futa != 4))
drawindexed = 3, 0, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);

        // futa=4, bottom=0 → skirt=1 is ineffective (condition excludes futa==4)
        const dead = computeIneffectiveValues(payload, { futa: "4", bottom: "0", skirt: "0" });
        const skirtIneffective = dead.get("skirt");
        assert.ok(skirtIneffective);
        const skirt1Entry = skirtIneffective.get("1");
        assert.ok(skirt1Entry);
        assert.ok(skirt1Entry.blockingVars.some((v) => v.label.includes("Futanari")));
        assert.ok(!skirtIneffective.has("0"));

        // futa=0, bottom=0 → skirt=1 is effective
        const live = computeIneffectiveValues(payload, { futa: "0", bottom: "0", skirt: "0" });
        assert.ok(!live.has("skirt") || !live.get("skirt")!.has("1"));
    });

    it("evaluates a second toggle without rebuilding geometry or converting GLB", async () => {
        const loadSource = await fse.readFile(new URL("./load.ts", import.meta.url), "utf8");
        const evalSource = await fse.readFile(
            new URL("../../../shared/mod-viewer/eval.ts", import.meta.url),
            "utf8",
        );
        assert.doesNotMatch(loadSource, /convertModToGlb|resolveVariantStateArtifact|buildModGlb/);
        assert.doesNotMatch(evalSource, /convertModToGlb|resolveVariantStateArtifact|buildModGlb/);
        const openSource = await fse.readFile(
            new URL("../../../renderer/src/hooks/use-mod-actions.tsx", import.meta.url),
            "utf8",
        );
        assert.match(openSource, /tools:loadModViewer/);
        assert.doesNotMatch(openSource, /tools:convertStaticGlbForViewer/);

        const root = await makeMod({
            ini: `[Constants]
global $hat = 0
[KeyHat]
type = cycle
$hat = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $hat == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        const positions = payload.meshes.map((mesh) => mesh.positions);
        const indices = payload.meshes.map((mesh) => mesh.indices);
        const first = evaluateViewerState(payload, { hat: "0" });
        const second = evaluateViewerState(payload, { hat: "1" });
        assert.equal(first.meshes[0].visible, true);
        assert.equal(second.meshes[0].visible, false);
        assert.equal(first.meshes[1].visible, false);
        assert.equal(second.meshes[1].visible, true);
        for (const [index, mesh] of payload.meshes.entries()) {
            assert.equal(mesh.positions, positions[index]);
            assert.equal(mesh.indices, indices[index]);
        }
    });

    it("resolves $var drawindexed args and hides unused auto draws", async () => {
        const root = await makeMod({
            ini: `[Constants]
global $n = 3
global $off = 0
global persist $top = 0
[KeySwap]
type = cycle
$top = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 0
drawindexed = $n, $off, 0
endif
if $top == 1
drawindexed = $n, 3, 0
endif
[TextureOverrideBodyIB]
hash = b03c7e30
handling = skip
drawindexed = auto
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 2);
        assert.ok(payload.variables.some((variable) => variable.id === "top"));
        const first = evaluateViewerState(payload, { top: "0" });
        const second = evaluateViewerState(payload, { top: "1" });
        assert.equal(first.meshes[0].visible, true);
        assert.equal(first.meshes[1].visible, false);
        assert.equal(second.meshes[0].visible, false);
        assert.equal(second.meshes[1].visible, true);
    });

    it("expands $top < 2 against cycle values", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $top = 0
[KeySwap]
type = cycle
$top = 0,1,2
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top < 2
drawindexed = 3, 0, 0
endif
if $top == 2
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 2);
        const low = evaluateViewerState(payload, { top: "0" });
        const high = evaluateViewerState(payload, { top: "2" });
        assert.equal(low.meshes[0].visible, true);
        assert.equal(low.meshes[1].visible, false);
        assert.equal(high.meshes[0].visible, false);
        assert.equal(high.meshes[1].visible, true);
    });

    it("forks GIMI merged CommandList branches instead of using Blend as UV", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $swapvar = 0
[KeySwap]
type = cycle
$swapvar = 0,1
[TextureOverrideAmberPosition]
run = CommandListAmberPosition
[TextureOverrideAmberBlend]
run = CommandListAmberBlend
[TextureOverrideAmberTexcoord]
run = CommandListAmberTexcoord
[TextureOverrideAmberIB]
hash = b03c7e30
run = CommandListAmberIB
[TextureOverrideAmberHead]
hash = b03c7e30
match_first_index = 0
run = CommandListAmberHead
[CommandListAmberPosition]
if $swapvar == 0
vb0 = ResourceAmberPosition.0
else if $swapvar == 1
vb0 = ResourceAmberPosition.1
endif
[CommandListAmberBlend]
if $swapvar == 0
vb1 = ResourceAmberBlend.0
handling = skip
draw = 8,0
else if $swapvar == 1
vb1 = ResourceAmberBlend.1
handling = skip
draw = 8,0
endif
[CommandListAmberTexcoord]
if $swapvar == 0
vb1 = ResourceAmberTexcoord.0
else if $swapvar == 1
vb1 = ResourceAmberTexcoord.1
endif
[CommandListAmberIB]
if $swapvar == 0
handling = skip
drawindexed = auto
else if $swapvar == 1
handling = skip
drawindexed = auto
endif
[CommandListAmberHead]
if $swapvar == 0
ib = ResourceAmberHeadIB.0
ps-t0 = ResourceAmberHeadDiffuse.0
else if $swapvar == 1
ib = ResourceAmberHeadIB.1
ps-t0 = ResourceAmberHeadDiffuse.1
endif
[ResourceAmberPosition.0]
type = Buffer
stride = 40
filename = .\\1. ambermain\\AmberPosition.buf
[ResourceAmberBlend.0]
type = Buffer
stride = 32
filename = .\\1. ambermain\\AmberBlend.buf
[ResourceAmberTexcoord.0]
type = Buffer
stride = 20
filename = .\\1. ambermain\\AmberTexcoord.buf
[ResourceAmberHeadIB.0]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = .\\1. ambermain\\AmberHead.ib
[ResourceAmberHeadDiffuse.0]
filename = .\\1. ambermain\\AmberHeadDiffuse.png
[ResourceAmberPosition.1]
type = Buffer
stride = 40
filename = .\\2. amberbody\\AmberPosition.buf
[ResourceAmberBlend.1]
type = Buffer
stride = 32
filename = .\\2. amberbody\\AmberBlend.buf
[ResourceAmberTexcoord.1]
type = Buffer
stride = 20
filename = .\\2. amberbody\\AmberTexcoord.buf
[ResourceAmberHeadIB.1]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = .\\2. amberbody\\AmberHead.ib
[ResourceAmberHeadDiffuse.1]
filename = .\\2. amberbody\\AmberHeadDiffuse.png
`,
            extra: async (dir) => {
                await writeMergedVariant(dir, "1. ambermain", 1);
                await writeMergedVariant(dir, "2. amberbody", 100);
            },
        });

        const sections = await parseIniFile(path.join(root, "mod.ini"));
        const groups = buildDrawGroups(sections, extractResources(sections));
        assert.equal(groups.length, 1);
        assert.match(groups[0].texcoordFile.replaceAll("\\", "/"), /AmberTexcoord\.buf$/i);
        assert.doesNotMatch(groups[0].texcoordFile.replaceAll("\\", "/"), /Blend/i);
        assert.equal(groups[0].draws.length, 2);
        const files = groups[0].draws.map((draw) => ({
            ib: (draw.ibFile ?? groups[0].ibFile).replaceAll("\\", "/"),
            pos: (draw.positionFile ?? groups[0].positionFile).replaceAll("\\", "/"),
        }));
        assert.ok(files.some((entry) => entry.ib.includes("1. ambermain")));
        assert.ok(files.some((entry) => entry.ib.includes("2. amberbody")));
        assert.ok(files.some((entry) => entry.pos.includes("1. ambermain")));
        assert.ok(files.some((entry) => entry.pos.includes("2. amberbody")));

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.variables.some((variable) => variable.id === "swapvar"));
        assert.equal(payload.meshes.length, 2);
        const xs = payload.meshes
            .map((mesh) => mesh.positions[0])
            .sort((left, right) => left - right);
        assert.equal(xs[0], 1);
        assert.equal(xs[1], 100);
        const first = evaluateViewerState(payload, { swapvar: "0" });
        const second = evaluateViewerState(payload, { swapvar: "1" });
        const visibleAt = (result: ReturnType<typeof evaluateViewerState>, value: number) =>
            result.meshes.filter(
                (mesh, index) => mesh.visible && payload.meshes[index].positions[0] === value,
            ).length;
        assert.equal(visibleAt(first, 1), 1);
        assert.equal(visibleAt(first, 100), 0);
        assert.equal(visibleAt(second, 1), 0);
        assert.equal(visibleAt(second, 100), 1);
    });

    it("binds SRMI this= hash textures from a sibling Diffuse override", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $swapvar = 0
[KeySwap]
type = cycle
$swapvar = 0,1
[TextureOverrideHeadPosition]
vb0 = ResourceHeadPosition
[TextureOverrideHeadTexcoord]
vb1 = ResourceHeadTexcoord
[TextureOverrideHeadA]
hash = 457d09a4
vb0 = ResourceHeadPosition
vb1 = ResourceHeadTexcoord
run = CommandListHeadA
[TextureOverrideHeadADiffuse]
hash = d5539abe
run = CommandListHeadADiffuse
[CommandListHeadA]
if $swapvar == 0
ib = ResourceHeadAIB.0
else if $swapvar == 1
ib = ResourceHeadAIB.0
endif
[CommandListHeadADiffuse]
if $swapvar == 0
this = ResourceHeadADiffuse.0
else if $swapvar == 1
this = ResourceHeadADiffuse.0
endif
[ResourceHeadPosition]
filename = pos.buf
stride = 40
[ResourceHeadTexcoord]
filename = tc.buf
stride = 20
[ResourceHeadAIB.0]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = body.ib
[ResourceHeadADiffuse.0]
filename = head.png
`,
            extra: async (dir) => {
                await fse.writeFile(path.join(dir, "head.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => mesh.texKey));
        for (const mesh of payload.meshes) {
            assert.ok(payload.textures[mesh.texKey!]);
            assert.equal(payload.textures[mesh.texKey!].role, "diffuse");
        }
        const evaluated = evaluateViewerState(payload, { swapvar: "0" });
        assert.ok(evaluated.meshes.some((mesh) => mesh.visible && mesh.texKey));
    });

    it("falls back to IB-stem Diffuse resources when no hash override exists", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBodyB]
ib = ResourceBodyBIB.0
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyBIB.0]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBDiffuse.0]
filename = bodyb.png
`,
            extra: async (dir) => {
                await fse.writeFile(path.join(dir, "bodyb.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes[0]?.texKey);
        assert.ok(payload.textures[payload.meshes[0].texKey!]);
    });

    it("binds SRMI BodyB draws to a sibling BodyADiffuse this= override", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $nine2 = 0
global persist $zero2 = 0
[KeyNine2]
type = cycle
$nine2 = 0,1
[KeyZero2]
type = cycle
$zero2 = 0,1
[TextureOverrideBodyPosition]
vb0 = ResourcePos
[TextureOverrideBodyTexcoord]
vb1 = ResourceTc
[TextureOverrideBodyA]
hash = 6f8c993d
match_first_index = 0
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyB]
hash = 6f8c993d
match_first_index = 62376
ib = ResourceBodyBIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = e88da4d0
if $nine2 == 0
    if $zero2 == 0
this = ResourceBodyADiffuse
    else
this = ResourceDiffWomb
    endif
else
this = ResourceBodyUltADiffuse
endif
[TextureOverrideBodyALightMap]
hash = 1248799e
this = ResourceBodyALightMap
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = body.png
[ResourceDiffWomb]
filename = womb.png
[ResourceBodyUltADiffuse]
filename = ult.png
[ResourceBodyALightMap]
filename = light.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(path.join(dir, "body.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "womb.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "ult.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "light.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const bodyB = payload.meshes.filter((mesh) => mesh.component === "BodyB");
        assert.ok(bodyB.length >= 1);
        assert.ok(bodyB.every((mesh) => mesh.texKey));
        const visibleTex = (result: ReturnType<typeof evaluateViewerState>, component: string) =>
            result.meshes
                .filter(
                    (mesh, index) => mesh.visible && payload.meshes[index].component === component,
                )
                .map((mesh) => String(mesh.texKey));
        const defaultState = evaluateViewerState(payload, { nine2: "0", zero2: "0" });
        const wombState = evaluateViewerState(payload, { nine2: "0", zero2: "1" });
        const ultState = evaluateViewerState(payload, { nine2: "1", zero2: "0" });
        assert.ok(visibleTex(defaultState, "BodyB").some((key) => key.includes("body.png")));
        assert.ok(visibleTex(wombState, "BodyB").some((key) => key.includes("womb.png")));
        assert.ok(visibleTex(ultState, "BodyB").some((key) => key.includes("ult.png")));
        assert.ok(bodyB.every((mesh) => mesh.lightMapKey));
    });

    it("uses the last same-hash Diffuse and LightMap this= like 3DMigoto", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBodyA]
hash = 3b4647d4
match_first_index = 0
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyB]
hash = 3b4647d4
match_first_index = 14598
ib = ResourceBodyBIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyC]
hash = 3b4647d4
match_first_index = 32934
ib = ResourceBodyCIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = f9761bbf
this = ResourceBodyADiffuse
[TextureOverrideBodyBDiffuse]
hash = f9761bbf
this = ResourceBodyBDiffuse
[TextureOverrideBodyCDiffuse]
hash = f9761bbf
this = ResourceBodyCDiffuse
[TextureOverrideBodyALightMap]
hash = 87adc723
this = ResourceBodyALightMap
[TextureOverrideBodyCLightMap]
hash = 87adc723
this = ResourceBodyCLightMap
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyCIB]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = bodya.png
[ResourceBodyBDiffuse]
filename = bodyb.png
[ResourceBodyCDiffuse]
filename = bodyc.png
[ResourceBodyALightMap]
filename = bodya-light.png
[ResourceBodyCLightMap]
filename = bodyc-light.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(
                    path.join(dir, "bodyc.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(path.join(dir, "bodya.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodyb.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodyc.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodya-light.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodyc-light.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const bodyA = payload.meshes.filter((mesh) => mesh.component === "BodyA");
        const bodyB = payload.meshes.filter((mesh) => mesh.component === "BodyB");
        assert.ok(bodyA.length >= 1);
        assert.ok(bodyB.length >= 1);
        assert.ok(bodyA.every((mesh) => String(mesh.texKey).includes("bodyc.png")));
        assert.ok(bodyB.every((mesh) => String(mesh.texKey).includes("bodyc.png")));
        assert.ok(bodyA.every((mesh) => String(mesh.lightMapKey).includes("bodyc-light.png")));
        assert.ok(bodyA.every((mesh) => !String(mesh.texKey).includes("bodya.png")));
    });

    it("keeps name-matched Diffuse when shared-hash siblings use different hashes", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBodyA]
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyC]
ib = ResourceBodyCIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = f9761bbf
this = ResourceBodyADiffuse
[TextureOverrideBodyCDiffuse]
hash = f0226f67
this = ResourceBodyCDiffuse
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyCIB]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = bodya.png
[ResourceBodyCDiffuse]
filename = bodyc.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyc.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(path.join(dir, "bodya.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodyc.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const bodyA = payload.meshes.filter((mesh) => mesh.component === "BodyA");
        const bodyC = payload.meshes.filter((mesh) => mesh.component === "BodyC");
        assert.ok(bodyA.every((mesh) => String(mesh.texKey).includes("bodya.png")));
        assert.ok(bodyC.every((mesh) => String(mesh.texKey).includes("bodyc.png")));
    });

    it("binds SRMI IB-Component Hash_DiffuseMap dumps onto sibling components with one atlas", async () => {
        const root = await makeMod({
            ini: `[TextureOverride_VB_3b4647d4_Position]
hash = 3b4647d4
vb0 = ResourcePos
[TextureOverride_VB_3b4647d4_Texcoord]
hash = 3b4647d4
vb1 = ResourceTc
[TextureOverride_IB_3b4647d4_Component1]
hash = 3b4647d4
match_first_index = 0
ib = Resource_3b4647d4_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_3b4647d4_Component2]
hash = 3b4647d4
match_first_index = 14598
ib = Resource_3b4647d4_Component2
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_3b4647d4_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_3b4647d4_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_f9761bbf]
filename = Texture/3b4647d4_1_f9761bbf_Hash_DiffuseMap.png
[TextureOverride_f9761bbf]
hash = f9761bbf
this = Resource_Texture_f9761bbf
[Resource_Texture_87adc723]
filename = Texture/3b4647d4_1_87adc723_Hash_LightMap.png
[TextureOverride_87adc723]
hash = 87adc723
this = Resource_Texture_87adc723
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.ensureDir(path.join(dir, "Texture"));
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_1_f9761bbf_Hash_DiffuseMap.png"),
                    PNG_1X1,
                );
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_1_87adc723_Hash_LightMap.png"),
                    PNG_1X1,
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        const component1 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component1",
        );
        const component2 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component2",
        );
        assert.ok(component1.length >= 1);
        assert.ok(component2.length >= 1);
        assert.ok(component1.every((mesh) => String(mesh.texKey).includes("Hash_DiffuseMap")));
        assert.ok(component2.every((mesh) => String(mesh.texKey).includes("Hash_DiffuseMap")));
        assert.ok(component2.every((mesh) => String(mesh.lightMapKey).includes("Hash_LightMap")));
    });

    it("does not share distinct IB-Component dump atlases across other components", async () => {
        const root = await makeMod({
            ini: `[TextureOverride_VB_3b4647d4_Position]
hash = 3b4647d4
vb0 = ResourcePos
[TextureOverride_VB_3b4647d4_Texcoord]
hash = 3b4647d4
vb1 = ResourceTc
[TextureOverride_IB_3b4647d4_Component1]
hash = 3b4647d4
ib = Resource_3b4647d4_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_3b4647d4_Component2]
hash = 3b4647d4
ib = Resource_3b4647d4_Component2
drawindexed = 3, 0, 0
[TextureOverride_IB_3b4647d4_Component3]
hash = 3b4647d4
ib = Resource_3b4647d4_Component3
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_3b4647d4_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_3b4647d4_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_3b4647d4_Component3]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_aaaaaaa1]
filename = Texture/3b4647d4_1_aaaaaaa1_Hash_DiffuseMap.png
[Resource_Texture_aaaaaaa2]
filename = Texture/3b4647d4_2_aaaaaaa2_Hash_DiffuseMap.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(
                    path.join(dir, "bodyc.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.ensureDir(path.join(dir, "Texture"));
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_1_aaaaaaa1_Hash_DiffuseMap.png"),
                    PNG_1X1,
                );
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_2_aaaaaaa2_Hash_DiffuseMap.png"),
                    PNG_2X2,
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        const component1 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component1",
        );
        const component2 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component2",
        );
        const component3 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component3",
        );
        assert.ok(component1.length >= 1);
        assert.ok(component2.length >= 1);
        assert.ok(component3.length >= 1);
        assert.ok(component1.every((mesh) => String(mesh.texKey).includes("aaaaaaa1")));
        assert.ok(component2.every((mesh) => String(mesh.texKey).includes("aaaaaaa2")));
        assert.ok(component3.every((mesh) => !mesh.texKey));
    });

    it("keeps per-component dump atlases when a hash-only this= leftover also exists", async () => {
        const root = await makeMod({
            ini: `[TextureOverride_VB_3b4647d4_Position]
hash = 3b4647d4
vb0 = ResourcePos
[TextureOverride_VB_3b4647d4_Texcoord]
hash = 3b4647d4
vb1 = ResourceTc
[TextureOverride_IB_3b4647d4_Component1]
hash = 3b4647d4
ib = Resource_3b4647d4_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_3b4647d4_Component2]
hash = 3b4647d4
ib = Resource_3b4647d4_Component2
drawindexed = 3, 0, 0
[TextureOverride_IB_3b4647d4_Component3]
hash = 3b4647d4
ib = Resource_3b4647d4_Component3
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_3b4647d4_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_3b4647d4_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_3b4647d4_Component3]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_aaaaaaa1]
filename = Texture/3b4647d4_1_aaaaaaa1_Hash_DiffuseMap.png
[Resource_Texture_aaaaaaa2]
filename = Texture/3b4647d4_2_aaaaaaa2_Hash_DiffuseMap.png
[TextureOverride_deadbeef]
hash = deadbeef
this = ResourceOther
[ResourceOther]
filename = other.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(
                    path.join(dir, "bodyc.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.ensureDir(path.join(dir, "Texture"));
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_1_aaaaaaa1_Hash_DiffuseMap.png"),
                    PNG_1X1,
                );
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_2_aaaaaaa2_Hash_DiffuseMap.png"),
                    PNG_2X2,
                );
                await fse.writeFile(path.join(dir, "other.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const component1 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component1",
        );
        const component2 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component2",
        );
        const component3 = payload.meshes.filter(
            (mesh) => mesh.component === "_IB_3b4647d4_Component3",
        );
        assert.ok(component1.length >= 1);
        assert.ok(component2.length >= 1);
        assert.ok(component3.length >= 1);
        assert.ok(component1.every((mesh) => String(mesh.texKey).includes("aaaaaaa1")));
        assert.ok(component2.every((mesh) => String(mesh.texKey).includes("aaaaaaa2")));
        assert.ok(component3.every((mesh) => String(mesh.texKey).includes("other.png")));
    });

    it("does not bind IB-Component dump textures onto BodyA sections", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBodyA]
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_f9761bbf]
filename = Texture/3b4647d4_1_f9761bbf_Hash_DiffuseMap.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Texture"));
                await fse.writeFile(
                    path.join(dir, "Texture", "3b4647d4_1_f9761bbf_Hash_DiffuseMap.png"),
                    PNG_1X1,
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 1);
        assert.equal(payload.meshes[0].texKey, null);
    });

    it("binds hash-only this= images onto IB-named parts using matching toggle vars", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $eyes = 0
global persist $cloth = 0
global persist $clothcolor = 0
[KeyEyes]
type = cycle
$eyes = 0,1
[KeyCloth]
type = cycle
$cloth = 0,1
[KeyClothColor]
type = cycle
$clothcolor = 0,1
[TextureOverride_VB_aaaaaaa1_face_Position]
hash = aaaaaaa1
vb0 = ResourcePos
[TextureOverride_VB_aaaaaaa1_face_Texcoord]
hash = aaaaaaa1
vb1 = ResourceTc
[TextureOverride_VB_bbbbbbbb_body_Position]
hash = bbbbbbbb
vb0 = ResourcePos
[TextureOverride_VB_bbbbbbbb_body_Texcoord]
hash = bbbbbbbb
vb1 = ResourceTc
[TextureOverride_IB_aaaaaaa1_face_Component1]
hash = aaaaaaa1
ib = ResourceFaceIB
if $eyes == 0
drawindexed = 3, 0, 0
endif
[TextureOverride_IB_bbbbbbbb_body_Component2]
hash = bbbbbbbb
ib = ResourceBodyIB
if $cloth == 0
drawindexed = 3, 0, 0
endif
[TextureOverride_7b3e8cd1]
hash = 7b3e8cd1
if $eyes == 0
this = ResourceFaceTex
endif
[TextureOverride_7b6fe593]
hash = 7b6fe593
if $clothcolor == 0
this = ResourceBodyTex
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceFaceIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceFaceTex]
filename = face.png
[ResourceBodyTex]
filename = bodytex.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "bodyb.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(path.join(dir, "face.png"), PNG_1X1);
                await fse.writeFile(path.join(dir, "bodytex.png"), PNG_2X2);
            },
        });

        const payload = await loadModViewerPayload(root);
        const face = payload.meshes.filter((mesh) => mesh.component.includes("face_Component1"));
        const body = payload.meshes.filter((mesh) => mesh.component.includes("body_Component2"));
        assert.ok(face.length >= 1);
        assert.ok(body.length >= 1);
        assert.ok(face.every((mesh) => String(mesh.texKey).includes("face.png")));
        assert.ok(body.every((mesh) => String(mesh.texKey).includes("bodytex.png")));
    });

    it("assigns leftover hash-only images to heavier IB parts first", async () => {
        const root = await makeMod({
            ini: `[TextureOverride_VB_aaaaaaa1_weapon_Position]
hash = aaaaaaa1
vb0 = ResourcePos
[TextureOverride_VB_aaaaaaa1_weapon_Texcoord]
hash = aaaaaaa1
vb1 = ResourceTc
[TextureOverride_VB_bbbbbbbb_fire_Position]
hash = bbbbbbbb
vb0 = ResourcePos
[TextureOverride_VB_bbbbbbbb_fire_Texcoord]
hash = bbbbbbbb
vb1 = ResourceTc
[TextureOverride_IB_aaaaaaa1_weapon_Component1]
hash = aaaaaaa1
ib = ResourceWeaponIB
drawindexed = 3, 0, 0
drawindexed = 3, 0, 0
[TextureOverride_IB_bbbbbbbb_fire_Component1]
hash = bbbbbbbb
ib = ResourceFireIB
drawindexed = 3, 0, 0
[TextureOverride_adac9211]
hash = adac9211
this = ResourceWeaponTex
[TextureOverride_d2a654b2]
hash = d2a654b2
this = ResourceFireTex
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceWeaponIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceFireIB]
filename = fire.ib
format = DXGI_FORMAT_R32_UINT
[ResourceWeaponTex]
filename = weapon.png
[ResourceFireTex]
filename = fire.png
`,
            extra: async (dir) => {
                await fse.writeFile(
                    path.join(dir, "fire.ib"),
                    Buffer.from(new Uint32Array([0, 1, 2]).buffer),
                );
                await fse.writeFile(path.join(dir, "weapon.png"), PNG_2X2);
                await fse.writeFile(path.join(dir, "fire.png"), PNG_1X1);
            },
        });

        const payload = await loadModViewerPayload(root);
        const weapon = payload.meshes.filter((mesh) =>
            mesh.component.includes("weapon_Component1"),
        );
        const fire = payload.meshes.filter((mesh) => mesh.component.includes("fire_Component1"));
        assert.ok(weapon.length >= 1);
        assert.ok(fire.length >= 1);
        assert.ok(weapon.every((mesh) => String(mesh.texKey).includes("weapon.png")));
        assert.ok(fire.every((mesh) => String(mesh.texKey).includes("fire.png")));
    });

    it("prefers sRGB then larger WWMI dump textures", () => {
        assert.equal(
            pickWwmiDumpDiffuse([
                {
                    file: "Textures/Components-3 t=79081b2b.dds",
                    srgb: false,
                    area: 8192 * 8192,
                    bytes: 67_109_012,
                    order: 0,
                },
                {
                    file: "Textures/Components-3 t=f58624fb.dds",
                    srgb: true,
                    area: 8192 * 8192,
                    bytes: 67_109_012,
                    order: 1,
                },
                {
                    file: "Textures/Components-3 t=1944212c.jpg",
                    srgb: true,
                    area: 0,
                    bytes: 1_615_266,
                    order: 2,
                },
            ]),
            "Textures/Components-3 t=f58624fb.dds",
        );
    });

    it("prefers exclusive WWMI dump textures over shared Components-N-M dumps", () => {
        assert.equal(
            pickWwmiDumpDiffuse([
                {
                    file: "Textures/Components-0-1-2-3-4-5 t=e9612536.dds",
                    srgb: true,
                    area: 2048 * 2048,
                    bytes: 4_194_452,
                    order: 0,
                },
                {
                    file: "Textures/Components-3 t=7d8d1ae0.dds",
                    srgb: false,
                    area: 512 * 512,
                    bytes: 262_292,
                    order: 1,
                },
            ]),
            "Textures/Components-3 t=7d8d1ae0.dds",
        );
    });

    it("rejects linear, flat, packed, and normal-looking WWMI dump hints", () => {
        const ok: Parameters<typeof isLikelyWwmiDiffuse>[0] = {
            srgb: true,
            colorSpace: "srgb",
            area: 1024,
            bytes: 100,
            isLikelyFlat: false,
            isLikelyNormal: false,
            isLikelyPacked: false,
        };
        assert.equal(isLikelyWwmiDiffuse(ok), true);
        assert.equal(isLikelyWwmiDiffuse({ ...ok, colorSpace: "unknown" }), true);
        assert.equal(isLikelyWwmiDiffuse({ ...ok, colorSpace: "linear", srgb: false }), false);
        assert.equal(isLikelyWwmiDiffuse({ ...ok, isLikelyFlat: true }), false);
        assert.equal(isLikelyWwmiDiffuse({ ...ok, isLikelyNormal: true }), false);
        assert.equal(isLikelyWwmiDiffuse({ ...ok, isLikelyPacked: true }), false);
    });

    it("binds WWMI Components-N dump textures when hash overrides have no Diffuse name", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent2]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture0]
filename = Textures/Components-2 t=0454fc47.png
[TextureOverrideTexture0]
hash = fcc6992c
this = ResourceTexture0
[ResourceTexture1]
filename = Textures/Components-3 t=1944212c.jpg
[TextureOverrideTexture1]
hash = 1944212c
this = ResourceTexture1
[ResourceTexture4]
filename = Textures/Components-3 t=f58624fb.png
[TextureOverrideTexture4]
hash = 22932116
this = ResourceTexture4
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-2 t=0454fc47.png"),
                    makeColorPng(),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=1944212c.jpg"),
                    PNG_1X1,
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    makeColorPng(32, 32),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        const component2 = payload.meshes.filter((mesh) => mesh.component === "Component2");
        const component3 = payload.meshes.filter((mesh) => mesh.component === "Component3");
        assert.ok(component2.length >= 1);
        assert.ok(component3.length >= 1);
        assert.ok(component2.every((mesh) => String(mesh.texKey).includes("Components-2")));
        assert.ok(component3.every((mesh) => String(mesh.texKey).includes("f58624fb.png")));
        assert.ok(component3.every((mesh) => !String(mesh.texKey).includes("1944212c")));
    });

    it("does not replace RabbitFX diffuse with a WWMI dump filename", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
Resource\\RabbitFX\\Diffuse = ref ResourceTextureKeep
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTextureKeep]
filename = keep.png
[ResourceTexture0]
filename = Textures/Components-3 t=f58624fb.png
[TextureOverrideTexture0]
hash = 22932116
this = ResourceTexture0
`,
            extra: async (dir) => {
                await fse.writeFile(path.join(dir, "keep.png"), PNG_1X1);
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    PNG_2X2,
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("keep.png")));
    });

    it("does not bind WWMI dump textures onto non-Component mesh sections", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture0]
filename = Textures/Components-3 t=f58624fb.png
[TextureOverrideTexture0]
hash = 22932116
this = ResourceTexture0
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 1);
        assert.equal(payload.meshes[0].texKey, null);
    });

    it("binds WWMI ps-t0 resources without Diffuse in the name", async () => {
        const root = await makeMod({
            ini: `[Constants]
global persist $socks = 0
[KeySocks]
type = cycle
$socks = 0, 1
[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
if $socks == 0
ps-t0 = ResourceTexture35
ps-t3 = ResourceTexture32
endif
if $socks == 1
ps-t0 = ResourceTexture35A
ps-t3 = ResourceTexture32
endif
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture32]
filename = Textures/Components-3 t=348c9329.png
[ResourceTexture35]
filename = Textures/Components-3 t=7d8d1ae0.png
[ResourceTexture35A]
filename = Textures/Components-3 t=7d8d1ae0A.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=348c9329.png"),
                    makeColorPng(32, 32),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=7d8d1ae0.png"),
                    makeColorPng(),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=7d8d1ae0A.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("7d8d1ae0")));
        assert.ok(payload.meshes.every((mesh) => !String(mesh.texKey).includes("348c9329")));
        const socksOff = evaluateViewerState(payload, { socks: "0" });
        const socksOn = evaluateViewerState(payload, { socks: "1" });
        assert.ok(String(socksOff.meshes[0]?.texKey).includes("7d8d1ae0.png"));
        assert.ok(String(socksOn.meshes[0]?.texKey).includes("7d8d1ae0A.png"));
    });

    it("binds shared WWMI Components-N-M dumps onto components without exclusive files", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent5]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture11]
filename = Textures/Components-0-1-2-3-4-5 t=e9612536.png
[TextureOverrideTexture11]
hash = e9612536
this = ResourceTexture11
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-0-1-2-3-4-5 t=e9612536.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("e9612536.png")));
    });

    it("does not pick WWMI dump textures assigned to non-diffuse ps-t slots", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent2]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t1 = ResourceTexture31
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture31]
filename = Textures/Components-2 t=e02d9167.png
[ResourceTextureOther]
filename = Textures/Components-2 t=742c5c7b.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-2 t=e02d9167.png"),
                    makeColorPng(32, 32),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-2 t=742c5c7b.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("742c5c7b.png")));
        assert.ok(payload.meshes.every((mesh) => !String(mesh.texKey).includes("e02d9167")));
    });

    it("unions WWMI non-diffuse dump exclusions across shared component indexes", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t1 = ResourceTexture31
drawindexed = 3, 0, 0
[TextureOverridecomponent3]
hash = 8d8097bd
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture31]
filename = Textures/Components-3 t=e02d9167.png
[ResourceTextureOther]
filename = Textures/Components-3 t=742c5c7b.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=e02d9167.png"),
                    makeColorPng(32, 32),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=742c5c7b.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 2);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("742c5c7b.png")));
        assert.ok(payload.meshes.every((mesh) => !String(mesh.texKey).includes("e02d9167")));
    });

    it("skips decoding oversized PNG candidates using IHDR area", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=7d8d1ae0.png
[ResourceTexture4]
filename = Textures/Components-3 t=f58624fb.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=7d8d1ae0.png"),
                    await makeOversizedPng(8192, 8192),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("7d8d1ae0.png")));
        assert.ok(payload.meshes.every((mesh) => !String(mesh.texKey).includes("f58624fb")));
    });

    it("skips decoding oversized DDS candidates using header area", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=7d8d1ae0.dds
[ResourceTexture4]
filename = Textures/Components-3 t=f58624fb.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=7d8d1ae0.dds"),
                    makeDdsHeader(8192, 8192),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.equal(payload.meshes[0]?.texKey, null);
        assert.ok(Object.keys(payload.textures).every((key) => !key.includes("f58624fb")));
    });

    it("replaces unnamed WWMI ps-t0 when it is a flat color map", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideComponent3]
hash = 8d8097bc
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=7d8d1ae0.png
[ResourceTexture4]
filename = Textures/Components-3 t=f58624fb.png
`,
            extra: async (dir) => {
                await fse.ensureDir(path.join(dir, "Textures"));
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=7d8d1ae0.png"),
                    makeFlatPng(),
                );
                await fse.writeFile(
                    path.join(dir, "Textures", "Components-3 t=f58624fb.png"),
                    makeColorPng(),
                );
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.ok(payload.meshes.length >= 1);
        assert.ok(payload.meshes.every((mesh) => String(mesh.texKey).includes("f58624fb.png")));
        assert.ok(payload.meshes.every((mesh) => !String(mesh.texKey).includes("7d8d1ae0")));
    });

    it("plays Present time animations as position variants without a toggle", async () => {
        const root = await makeMod({
            ini: `[Constants]
global $fps = 30
global $frame = 0
[Present]
$frame = time * $fps
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyPosition]
handling = skip
if $frame == 0
    vb0 = ResourcePos
elif $frame == 1
    vb0 = ResourcePos1
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourcePos1]
filename = pos1.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            extra: async (dir) => {
                const pos = Buffer.alloc(8 * 40);
                for (let index = 0; index < 8; index++) {
                    pos.writeFloatLE(100 + index, index * 40);
                    pos.writeFloatLE(index, index * 40 + 4);
                    pos.writeFloatLE(index, index * 40 + 8);
                }
                await fse.writeFile(path.join(dir, "pos1.buf"), pos);
            },
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.animations.length, 1);
        assert.equal(payload.animations[0].id, "frame");
        assert.equal(payload.animations[0].fps, 30);
        assert.equal(
            payload.variables.some((variable) => variable.id === "frame"),
            false,
        );
        assert.equal(payload.meshes.length, 1);
        assert.ok(payload.meshes[0].positionVariants.length >= 2);

        const first = evaluateViewerState(payload, payload.animations[0].frames[0].values);
        const second = evaluateViewerState(payload, payload.animations[0].frames[1].values);
        assert.equal(first.meshes[0].visible, true);
        assert.equal(second.meshes[0].visible, true);
        assert.equal(first.meshes[0].positionVariantIndex, 0);
        assert.equal(second.meshes[0].positionVariantIndex, 1);
        assert.equal(payload.meshes[0].positions[0], 0);
        assert.equal(payload.meshes[0].positionVariants[1].positions[0], 100);

        const dialogSource = await fse.readFile(
            new URL(
                "../../../renderer/src/components/tools/model-viewer/model-viewer-dialog.tsx",
                import.meta.url,
            ),
            "utf8",
        );
        assert.match(dialogSource, /transport\.animations/);
        assert.match(dialogSource, /setAnimationFrame/);
    });

    it("collapses per-frame drawindexed animation into one mesh", async () => {
        const root = await makeMod({
            ini: `[Constants]
global $fps = 15
global $frame = 0
[Present]
post $frame = time * $fps
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 1
drawindexed = 3, 0, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.animations.length, 1);
        assert.equal(payload.meshes.length, 1);
        assert.ok(payload.meshes[0].positionVariants.length >= 2);
        const first = evaluateViewerState(payload, { frame: 0 });
        const second = evaluateViewerState(payload, { frame: 1 });
        const missing = evaluateViewerState(payload, { frame: 2 });
        assert.equal(first.meshes[0].visible, true);
        assert.equal(second.meshes[0].visible, true);
        assert.equal(missing.meshes[0].visible, false);
    });

    it("does not treat excluded else branch draws as visible", async () => {
        const root = await makeMod({
            ini: `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if 1
    drawindexed = 3, 0, 0
else
    drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            ib: Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
            vertexCount: 8,
        });

        const payload = await loadModViewerPayload(root);
        assert.equal(payload.meshes.length, 2);
        const included = payload.meshes.find((mesh) => isUnconstrained(mesh.conditions));
        const excluded = payload.meshes.find((mesh) => sameDnf(mesh.conditions, DNF_FALSE));
        assert.ok(included);
        assert.ok(excluded);

        const evaluated = evaluateViewerState(payload, {});
        assert.equal(evaluated.meshes.find((mesh) => mesh.id === included.id)?.visible, true);
        assert.equal(evaluated.meshes.find((mesh) => mesh.id === excluded.id)?.visible, false);
    });
});

describe("buildDrawGroups run expansion", () => {
    it("rejects negative drawindexed count, start, and base values", () => {
        const sections = parseIniText(
            `[Constants]
global $n = -5
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = -3, 0, 0
drawindexed = 3, -1, 0
drawindexed = 3, 0, -2
drawindexed = $n, 0, 0
drawindexed = 3, $n, 0
drawindexed = 3, 0, $n
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            "mod.ini",
        );
        const groups = buildDrawGroups(sections, extractResources(sections));
        assert.equal(groups.length, 1);
        assert.equal(groups[0]?.draws.length, 1);
        assert.equal(groups[0]?.draws[0]?.count, 3);
        assert.equal(groups[0]?.draws[0]?.start, 0);
        assert.equal(groups[0]?.draws[0]?.base, 0);
    });

    it("ignores if 0 buffer assignments without else before drawindexed", () => {
        const sections = parseIniText(
            `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if 0
ib = ResourceDeadIB
vb0 = ResourceDeadPos
vb1 = ResourceDeadTc
vb2 = ResourceDeadLm
endif
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceDeadPos]
filename = dead-pos.buf
stride = 40
[ResourceDeadTc]
filename = dead-tc.buf
stride = 20
[ResourceDeadLm]
filename = dead-lm.buf
stride = 20
[ResourceDeadIB]
filename = dead.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            "mod.ini",
        );
        const groups = buildDrawGroups(sections, extractResources(sections));
        assert.equal(groups.length, 1);
        assert.equal(groups[0]?.ibFile, "body.ib");
        assert.equal(groups[0]?.positionFile, "pos.buf");
        assert.equal(groups[0]?.texcoordFile, "tc.buf");
        assert.equal(groups[0]?.draws.length, 1);
        assert.equal(groups[0]?.draws[0]?.ibFile, undefined);
        assert.equal(groups[0]?.draws[0]?.positionFile, undefined);
        assert.equal(groups[0]?.draws[0]?.texcoordFile, undefined);
        assert.equal(groups[0]?.draws[0]?.count, 3);
        assert.equal(groups[0]?.draws[0]?.start, 0);
        assert.equal(groups[0]?.draws[0]?.base, 0);
        assert.equal(sameDnf(groups[0]?.draws[0]?.conditions ?? DNF_FALSE, DNF_FALSE), false);
        assert.equal(isUnconstrained(groups[0]?.draws[0]?.conditions), true);
    });

    it("ignores if 0 ib assignments and keeps the reachable else buffers", () => {
        const sections = parseIniText(
            `[Constants]
global persist $swap = 0
[KeySwap]
type = cycle
$swap = 0,1
[TextureOverrideBody]
if 0
ib = ResourceDeadIB
else
if $swap == 0
ib = ResourceBodyIB
else
ib = ResourceAltIB
endif
endif
vb0 = ResourcePos
vb1 = ResourceTc
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceDeadIB]
filename = dead.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceAltIB]
filename = alt.ib
format = DXGI_FORMAT_R32_UINT
`,
            "mod.ini",
        );
        const groups = buildDrawGroups(sections, extractResources(sections));
        assert.equal(groups.length, 1);
        assert.equal(groups[0]?.ibFile, "body.ib");
        assert.equal(groups[0]?.draws.length, 2);
        const ibFiles = groups[0]?.draws.map((draw) => draw.ibFile ?? groups[0]?.ibFile) ?? [];
        assert.deepEqual(ibFiles.sort(), ["alt.ib", "body.ib"]);
        assert.equal(
            groups[0]?.draws.some((draw) => sameDnf(draw.conditions, DNF_FALSE)),
            false,
        );
        assert.equal(
            groups[0]?.draws.every((draw) => !isUnconstrained(draw.conditions)),
            true,
        );
    });

    it("bounds nested run recursion across the full traversal", () => {
        // 12 doubling levels exceed MAX_RUN_EXPANSIONS (4096) but still finish if that bound is removed.
        const depth = 12;
        const chain = Array.from({ length: depth }, (_, index) => {
            const next = `CommandListExp${index + 1}`;
            return `[CommandListExp${index}]\nrun = ${next}\nrun = ${next}\n`;
        }).join("");
        const sections = parseIniText(
            `${chain}[CommandListExp${depth}]
drawindexed = 3, 0, 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
run = CommandListExp0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`,
            "mod.ini",
        );
        const groups = buildDrawGroups(sections, extractResources(sections));
        assert.equal(groups.length, 1);
        assert.equal(groups[0]?.ibFile, "body.ib");
        assert.equal(groups[0]?.positionFile, "pos.buf");
        assert.equal(groups[0]?.texcoordFile, "tc.buf");
        assert.equal(groups[0]?.draws.length, 1);
        assert.equal(groups[0]?.draws[0]?.count, null);
        assert.equal(groups[0]?.draws[0]?.start, 0);
        assert.equal(groups[0]?.draws[0]?.base, 0);
        assert.equal(groups[0]?.draws[0]?.ibFile, undefined);
        assert.equal(isUnconstrained(groups[0]?.draws[0]?.conditions), true);
    });
});

async function makeMod(options: {
    ini: string;
    ib?: Buffer;
    tc?: Buffer;
    vertexCount?: number;
    extra?: (dir: string) => Promise<void>;
}) {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-mod-viewer-"));
    tempRoots.push(root);
    await fse.writeFile(path.join(root, "mod.ini"), options.ini);
    const vertexCount = options.vertexCount ?? 8;
    const pos = Buffer.alloc(vertexCount * 40);
    for (let index = 0; index < vertexCount; index++) {
        pos.writeFloatLE(index, index * 40);
        pos.writeFloatLE(index, index * 40 + 4);
        pos.writeFloatLE(index, index * 40 + 8);
    }
    await fse.writeFile(path.join(root, "pos.buf"), pos);
    await fse.writeFile(path.join(root, "tc.buf"), options.tc ?? Buffer.alloc(vertexCount * 20));
    await fse.writeFile(
        path.join(root, "body.ib"),
        options.ib ?? Buffer.from(new Uint32Array([0, 1, 2]).buffer),
    );
    if (/filename = diffuse\.png/.test(options.ini)) {
        await fse.writeFile(path.join(root, "diffuse.png"), PNG_1X1);
    }
    await options.extra?.(root);
    return root;
}

async function writeMergedVariant(dir: string, folder: string, axis: number) {
    const folderPath = path.join(dir, folder);
    await fse.ensureDir(folderPath);
    const pos = Buffer.alloc(8 * 40);
    for (let index = 0; index < 8; index++) {
        pos.writeFloatLE(axis, index * 40);
        pos.writeFloatLE(index, index * 40 + 4);
        pos.writeFloatLE(index, index * 40 + 8);
    }
    await fse.writeFile(path.join(folderPath, "AmberPosition.buf"), pos);
    await fse.writeFile(path.join(folderPath, "AmberTexcoord.buf"), Buffer.alloc(8 * 20));
    await fse.writeFile(path.join(folderPath, "AmberBlend.buf"), Buffer.alloc(8 * 32));
    await fse.writeFile(
        path.join(folderPath, "AmberHead.ib"),
        Buffer.from(new Uint32Array([0, 1, 2]).buffer),
    );
    await fse.writeFile(path.join(folderPath, "AmberHeadDiffuse.png"), PNG_1X1);
}

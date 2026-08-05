import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import {
    createDefaultTouchZoneSettings,
    TOUCH_PHYSICS_PRESETS,
} from "@shared/touch-profile-settings";
import { describe, it } from "vitest";

import { replaceTouchOutput, touchFolderBaseName } from "./touch-profile";
import { analyzeTouchMod, hashTouchFiles } from "./touch-profile-analyzer";
import {
    bakeSampleOffsets,
    buildVertexMasks,
    extractMaskChannel,
    encodeJiggleParams,
    encodeObjectMap,
    maxVisionActiveRatio,
    maxVisionHeightSpanRatio,
    normalizeVisionMaskTuning,
    selectVerticesFromVisionPolygons,
    zonesFromVisionPolygons,
} from "./touch-profile-assets";
import {
    assertTouchProfileDetectionAllowed,
    inspectTouchProfileInput,
} from "./touch-profile-detection";
import { buildTouchRuntimeZoneOverrides } from "./touch-profile-ini";
import {
    buildAllViewTransforms,
    buildViewTransform,
    TOUCH_VIEW_PROJECTORS,
} from "./touch-profile-projection";
import { normalizeTouchZoneSettings, resolveTouchJiggleParams } from "./touch-profile-settings";
import {
    TOUCH_PROFILE_MANIFEST_FILE,
    TOUCH_PROFILE_MANIFEST_KIND,
    TOUCH_SHADER_FILES,
    type TouchComponentAnalysis,
    type TouchComponentDraft,
} from "./touch-profile-types";
import { validateTouchOutput } from "./touch-profile-validator";

function makeTouchComponent(vertexCount: number, kind: TouchComponentAnalysis["kind"] = "body") {
    const indices = new Uint32Array(
        Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, index) => index),
    );
    return {
        id: `${kind}Position`,
        name: `${kind}Position`,
        kind,
        interactiveCandidate: true,
        supportGrade: "A" as const,
        supportReasons: [],
        positionResourceName: `${kind}Position`,
        positionRelativePath: `${kind}Position.buf`,
        positionPath: `${kind}Position.buf`,
        positionStride: 40,
        vertexCount,
        indexCount: indices.length,
        drawRanges: [{ firstIndex: 0, indexCount: indices.length, baseVertex: 0 }],
        objectMaps: [
            {
                firstIndex: 0,
                indexCount: indices.length,
                objectMode: 7,
                objectId: 1,
                label: "skin",
            },
        ],
    } satisfies TouchComponentAnalysis;
}

function createSettingsDraft(channel: number, preset: "normal" | "firm"): TouchComponentDraft {
    return {
        componentId: `component-${channel}-${preset}`,
        interactive: true,
        objectId: 1,
        zones: [
            {
                id: `zone-${channel}-${preset}`,
                label: preset,
                channel,
                confidence: 0.9,
                center: [0, 0, 0] as [number, number, number],
                radius: [0.1, 0.1, 0.1] as [number, number, number],
                source: "manual",
                settings: {
                    ...createDefaultTouchZoneSettings(),
                    physicsPreset: preset,
                    advanced: { ...TOUCH_PHYSICS_PRESETS[preset] },
                },
            },
        ],
        confidence: 0.9,
        warnings: [],
    };
}

describe("touchFolderBaseName", () => {
    it("appends (Touch) and strips DISABLED prefix", () => {
        assert.equal(touchFolderBaseName("AliceMod"), "AliceMod (Touch)");
        assert.equal(touchFolderBaseName("DISABLED_AliceMod"), "AliceMod (Touch)");
        assert.equal(touchFolderBaseName("DISABLED AliceMod"), "AliceMod (Touch)");
    });
});

describe("touch profile input detection", () => {
    it("detects a generated Touch profile from legacy INI markers", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-detection-"));
        writeTouchDetectionFixture(root, {
            ini: [
                "; Nahida Touch Profile state (nhd_touch_alice)",
                "; ---- Nahida Touch Profile runtime (nhd_touch_alice) ----",
            ].join("\n"),
            runtime: true,
        });

        const detection = await inspectTouchProfileInput(root);

        assert.equal(detection.status, "generated");
        assert.throws(
            () => assertTouchProfileDetectionAllowed(root, detection),
            /TOUCH_PROFILE_INPUT_ALREADY_TOUCH/,
        );
        await assert.rejects(() => analyzeTouchMod(root), /TOUCH_PROFILE_INPUT_ALREADY_TOUCH/);
        fs.rmSync(root, { recursive: true, force: true });
    });

    it("detects a generated Touch profile from its manifest", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-manifest-"));
        writeTouchDetectionFixture(root, { runtime: true });
        fs.writeFileSync(
            path.join(root, TOUCH_PROFILE_MANIFEST_FILE),
            JSON.stringify({ kind: TOUCH_PROFILE_MANIFEST_KIND, runtimeVersion: "1" }),
        );

        const detection = await inspectTouchProfileInput(root);

        assert.equal(detection.status, "generated");
        fs.rmSync(root, { recursive: true, force: true });
    });

    it("marks generated output with missing runtime files as incomplete", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-incomplete-"));
        writeTouchDetectionFixture(root, {
            ini: "; Nahida Touch Profile state (nhd_touch_alice)",
        });

        const detection = await inspectTouchProfileInput(root);

        assert.equal(detection.status, "incomplete");
        assert.ok(detection.reasons.some((reason) => reason.includes("Missing runtime shaders")));
        fs.rmSync(root, { recursive: true, force: true });
    });

    it("does not treat a Touch folder name alone as an existing Touch profile", async () => {
        const parent = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-folder-name-"));
        const root = path.join(parent, "Cosmetic (Touch)");
        fs.mkdirSync(root);
        fs.writeFileSync(path.join(root, "mod.ini"), "[Present]\n");

        const detection = await inspectTouchProfileInput(root);

        assert.equal(detection.status, "none");
        fs.rmSync(parent, { recursive: true, force: true });
    });

    it("suspects an external Touch runtime from its shader footprint", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-external-"));
        writeTouchDetectionFixture(root, {
            ini: [
                "Resources/IM/rzm_gs_probe.hlsl",
                "Resources/IM/rzm_object_detect.hlsl",
                "Resources/IM/rzm_jiggle_interaction.hlsl",
            ].join("\n"),
            runtime: true,
        });

        const detection = await inspectTouchProfileInput(root);

        assert.equal(detection.status, "suspected");
        assert.throws(
            () => assertTouchProfileDetectionAllowed(root, detection),
            /TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH/,
        );
        fs.rmSync(root, { recursive: true, force: true });
    });
});

function writeTouchDetectionFixture(
    root: string,
    options: { ini?: string; runtime?: boolean } = {},
) {
    fs.writeFileSync(path.join(root, "mod.ini"), options.ini ?? "[Present]\n");
    if (!options.runtime) return;

    const runtimeRoot = path.join(root, "Resources", "IM");
    fs.mkdirSync(runtimeRoot, { recursive: true });
    for (const fileName of TOUCH_SHADER_FILES) {
        fs.writeFileSync(path.join(runtimeRoot, fileName), "");
    }
}

describe("touch profile regeneration helpers", () => {
    it("keeps source fingerprints stable when the mod folder is renamed", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-fingerprint-"));
        const activeRoot = path.join(root, "AliceMod");
        const disabledRoot = path.join(root, "DISABLED_AliceMod");
        fs.mkdirSync(activeRoot);
        fs.mkdirSync(disabledRoot);
        fs.writeFileSync(path.join(activeRoot, "mod.ini"), "[Present]\n");
        fs.writeFileSync(path.join(disabledRoot, "mod.ini"), "[Present]\n");
        fs.writeFileSync(path.join(activeRoot, "bodyPosition.buf"), Buffer.from([1, 2, 3]));
        fs.writeFileSync(path.join(disabledRoot, "bodyPosition.buf"), Buffer.from([1, 2, 3]));

        const activeHash = await hashTouchFiles(
            [path.join(activeRoot, "mod.ini"), path.join(activeRoot, "bodyPosition.buf")],
            activeRoot,
        );
        const disabledHash = await hashTouchFiles(
            [path.join(disabledRoot, "mod.ini"), path.join(disabledRoot, "bodyPosition.buf")],
            disabledRoot,
        );

        assert.equal(activeHash, disabledHash);
        fs.rmSync(root, { recursive: true, force: true });
    });

    it("restores the original output when a staged replacement fails", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-replace-"));
        const outputRoot = path.join(root, "output");
        fs.mkdirSync(outputRoot);
        fs.writeFileSync(path.join(outputRoot, "state.txt"), "original");

        await assert.rejects(() => replaceTouchOutput(path.join(root, "missing"), outputRoot));

        assert.equal(fs.readFileSync(path.join(outputRoot, "state.txt"), "utf8"), "original");
        assert.deepEqual(
            fs.readdirSync(root).filter((entry) => entry.includes("backup-")),
            [],
        );
        fs.rmSync(root, { recursive: true, force: true });
    });
});

describe("touch view projection", () => {
    it("treats the negative-Y surface as the front view", () => {
        const positions = new Float32Array([0, -1, 0, 0, 1, 0]);
        const front = buildViewTransform(positions, TOUCH_VIEW_PROJECTORS[0], 100, 0);
        const back = buildViewTransform(positions, TOUCH_VIEW_PROJECTORS[1], 100, 0);

        assert.ok(front.depth[0] < front.depth[1]);
        assert.ok(back.depth[1] < back.depth[0]);
    });

    it("keeps side views upright with height on the vertical axis", () => {
        // +X right, +Y back, +Z up
        const positions = new Float32Array([
            0,
            -1,
            0, // front
            0,
            1,
            0, // back
            0,
            0,
            2, // head
            -1,
            0,
            0, // character left
            1,
            0,
            0, // character right
        ]);
        const left = buildViewTransform(positions, TOUCH_VIEW_PROJECTORS[2], 100, 0);
        const right = buildViewTransform(positions, TOUCH_VIEW_PROJECTORS[3], 100, 0);

        // Image y decreases upward in normalized space; projected Y is height (z).
        assert.ok(left.projected[2 * 2 + 1] > left.projected[0 * 2 + 1]);
        assert.ok(right.projected[2 * 2 + 1] > right.projected[0 * 2 + 1]);
        // From left: front (-Y) is on image-right (higher projected X).
        assert.ok(left.projected[0 * 2] > left.projected[1 * 2]);
        // From right: front (-Y) is on image-left (lower projected X).
        assert.ok(right.projected[0 * 2] < right.projected[1 * 2]);
        assert.ok(left.depth[3] < left.depth[4]);
        assert.ok(right.depth[4] < right.depth[3]);
    });
});

describe("touch asset helpers", () => {
    it("encodes object map, params, and bake offsets", () => {
        const objectMap = encodeObjectMap([
            { firstIndex: 48543, indexCount: 55830, objectMode: 7, objectId: 1, label: "clothed" },
        ]);
        assert.equal(objectMap.length, 8);
        assert.equal(objectMap[0], 1);
        assert.equal(objectMap[4], 48543);
        assert.equal(objectMap[6], 7);

        const params = encodeJiggleParams({
            objectId: 2,
            radius: 0.2,
            strength: 1.15,
            falloff: 1.8,
            dragScale: 1,
            grabDamping: 0.86,
            grabSpring: 0.176,
            releaseDamping: 0.96,
            releaseSpring: 0.055,
            releaseKick: 1.18,
            maxOffset: 0.065,
            targetFollow: 0.12,
            mouseYDirection: 1,
            mouseXDirection: 1,
        });
        assert.equal(params.byteLength, 64);
        assert.equal(params[0], 2);

        assert.deepEqual(bakeSampleOffsets(104373, 55830).slice(0, 2), [104373, 112348]);
    });

    it("applies per-zone mask strength to generated vertex masks", () => {
        const vertexCount = 80;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            const side = i < vertexCount / 2 ? -1 : 1;
            const t = (i % (vertexCount / 2)) / (vertexCount / 2 - 1);
            positions[i * 3] = side * (0.04 + t * 0.1);
            positions[i * 3 + 1] = 0.9 + (t - 0.5) * 0.2;
            positions[i * 3 + 2] = 1.05 + (t - 0.5) * 0.1;
        }
        const indices = new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, i) => i),
        );
        const component = {
            id: "bodyPosition",
            name: "bodyPosition",
            kind: "body" as const,
            interactiveCandidate: true,
            supportGrade: "A" as const,
            supportReasons: [],
            positionResourceName: "bodyPosition",
            positionRelativePath: "bodyPosition.buf",
            positionPath: "bodyPosition.buf",
            positionStride: 40,
            vertexCount,
            indexCount: indices.length,
            drawRanges: [{ firstIndex: 0, indexCount: indices.length, baseVertex: 0 }],
            objectMaps: [
                {
                    firstIndex: 0,
                    indexCount: indices.length,
                    objectMode: 7,
                    objectId: 1,
                    label: "clothed",
                },
            ],
        } satisfies TouchComponentAnalysis;
        const zone = {
            id: "left_breast",
            label: "Left breast",
            channel: 0,
            confidence: 0.9,
            center: [0, 0.9, 1.05] as [number, number, number],
            radius: [0.15, 0.15, 0.15] as [number, number, number],
            source: "vision" as const,
            settings: createDefaultTouchZoneSettings(),
        };
        const halfStrengthZone = {
            ...zone,
            settings: { ...zone.settings, maskStrength: 0.5 },
        };
        const increasedStrengthZone = {
            ...zone,
            settings: { ...zone.settings, maskStrength: 1.5 },
        };
        const flattenedZone = {
            ...zone,
            settings: { ...zone.settings, maskCurve: 0.5 },
        };
        const concentratedZone = {
            ...zone,
            settings: { ...zone.settings, maskCurve: 2 },
        };
        const base = extractMaskChannel(
            buildVertexMasks(vertexCount, positions, indices, component, [zone]),
            vertexCount,
            zone.channel,
        );
        const halfStrength = extractMaskChannel(
            buildVertexMasks(vertexCount, positions, indices, component, [halfStrengthZone]),
            vertexCount,
            zone.channel,
        );
        const increasedStrength = extractMaskChannel(
            buildVertexMasks(vertexCount, positions, indices, component, [increasedStrengthZone]),
            vertexCount,
            zone.channel,
        );
        const flattened = extractMaskChannel(
            buildVertexMasks(vertexCount, positions, indices, component, [flattenedZone]),
            vertexCount,
            zone.channel,
        );
        const concentrated = extractMaskChannel(
            buildVertexMasks(vertexCount, positions, indices, component, [concentratedZone]),
            vertexCount,
            zone.channel,
        );

        assert.ok(base.some((value) => value > 0));
        assert.ok(increasedStrength.some((value, vertex) => value > base[vertex] + 1e-6));
        assert.ok(flattened.some((value, vertex) => value > base[vertex] + 1e-6));
        assert.ok(concentrated.some((value, vertex) => value < base[vertex] - 1e-6));
        for (let vertex = 0; vertex < vertexCount; vertex++) {
            if (base[vertex] <= 1e-6) continue;
            assert.ok(Math.abs(halfStrength[vertex] - base[vertex] * 0.5) < 1e-5);
        }
    });

    it("fades the mask boundary instead of cutting it off abruptly", () => {
        // cutoff d2=2.25, fade starts at 1.7 — sample interior / mid-fade / outside.
        const positions = new Float32Array([
            0,
            0,
            0,
            Math.sqrt(1.2),
            0,
            0,
            Math.sqrt(2.0),
            0,
            0,
            Math.sqrt(2.5),
            0,
            0,
        ]);
        const indices = new Uint32Array([0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3]);
        const component = {
            id: "meshPosition",
            name: "meshPosition",
            kind: "body" as const,
            interactiveCandidate: true,
            supportGrade: "A" as const,
            supportReasons: [],
            positionResourceName: "meshPosition",
            positionRelativePath: "meshPosition.buf",
            positionPath: "meshPosition.buf",
            positionStride: 40,
            vertexCount: 4,
            indexCount: indices.length,
            drawRanges: [{ firstIndex: 0, indexCount: indices.length, baseVertex: 0 }],
            objectMaps: [],
        } satisfies TouchComponentAnalysis;
        const zone = {
            id: "mesh_zone",
            label: "Mesh",
            channel: 0,
            confidence: 1,
            center: [0, 0, 0] as [number, number, number],
            radius: [1, 1, 1] as [number, number, number],
            source: "manual" as const,
            settings: createDefaultTouchZoneSettings(),
        };
        const weights = extractMaskChannel(
            buildVertexMasks(4, positions, indices, component, [zone]),
            4,
            zone.channel,
        );

        assert.ok(weights[1]! > 0);
        assert.ok(weights[2]! > 0);
        assert.ok(weights[2]! < weights[1]! * 0.4);
        assert.equal(weights[3], 0);
    });
});

describe("touch zone settings", () => {
    it("normalizes mask strength within its supported range", () => {
        const settings = normalizeTouchZoneSettings({
            ...createDefaultTouchZoneSettings(),
            maskStrength: 1.5,
        });

        assert.equal(settings.maskStrength, 1.5);
        assert.equal(settings.maskCurve, 1);
        assert.equal(
            normalizeTouchZoneSettings({
                ...createDefaultTouchZoneSettings(),
                maskCurve: 0,
            }).maskCurve,
            0,
        );
        assert.throws(
            () =>
                normalizeTouchZoneSettings({
                    ...createDefaultTouchZoneSettings(),
                    maskStrength: 2.1,
                }),
            /mask strength out of range/,
        );
        assert.throws(
            () =>
                normalizeTouchZoneSettings({
                    ...createDefaultTouchZoneSettings(),
                    maskCurve: 2.1,
                }),
            /mask curve out of range/,
        );
    });

    it("resolves strength and physics settings into runtime overrides", () => {
        const settings = {
            ...createDefaultTouchZoneSettings(),
            strengthPreset: "strong" as const,
            physicsPreset: "firm" as const,
            advanced: { ...TOUCH_PHYSICS_PRESETS.firm },
        };
        const params = resolveTouchJiggleParams(settings, 1);
        const draft = {
            componentId: "bodyPosition",
            interactive: true,
            objectId: 1,
            zones: [
                {
                    id: "left_breast",
                    label: "Left breast",
                    channel: 0,
                    confidence: 0.9,
                    center: [0, 0, 0] as [number, number, number],
                    radius: [0.1, 0.1, 0.1] as [number, number, number],
                    source: "vision" as const,
                    settings,
                },
            ],
            confidence: 0.9,
            warnings: [],
        } satisfies TouchComponentDraft;

        const overrides = buildTouchRuntimeZoneOverrides([draft]);
        assert.equal(params.strength, 1.35 * 1.3);
        assert.equal(overrides.strength[0], params.strength);
        assert.equal(overrides.radius[0], TOUCH_PHYSICS_PRESETS.firm.radius);
        assert.equal(overrides.spring[0], TOUCH_PHYSICS_PRESETS.firm.spring / 0.176);
    });

    it("rejects conflicting settings on one runtime channel", () => {
        const first = createSettingsDraft(0, "normal");
        const second = createSettingsDraft(0, "firm");
        assert.throws(() => buildTouchRuntimeZoneOverrides([first, second]), /conflicting/);
    });

    it("allows mask-only differences on one runtime channel", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-validation-"));
        try {
            const runtimeDir = path.join(root, "Resources", "IM");
            fs.mkdirSync(runtimeDir, { recursive: true });
            for (const shader of TOUCH_SHADER_FILES) {
                fs.writeFileSync(path.join(runtimeDir, shader), "");
            }
            const iniPath = path.join(root, "mod.ini");
            fs.writeFileSync(iniPath, "");

            const first = createSettingsDraft(2, "normal");
            const second = createSettingsDraft(2, "normal");
            first.zones[0]!.settings = {
                ...first.zones[0]!.settings,
                maskStrength: 0.5,
            };
            second.zones[0]!.settings = {
                ...second.zones[0]!.settings,
                maskCurve: 1.5,
                physicsPreset: "custom",
            };

            const validation = await validateTouchOutput({
                outputRoot: root,
                iniPath,
                components: [],
                drafts: [first, second],
                assets: [],
            });

            assert.equal(validation.ok, true);
            assert.deepEqual(validation.issues, []);
        } finally {
            fs.rmSync(root, { recursive: true, force: true });
        }
    });
});

describe("vision mask breadth helpers", () => {
    it("clamps Vision seed influence tuning to the bounded policy", () => {
        assert.equal(normalizeVisionMaskTuning().seedInfluenceScale, 1);
        assert.equal(normalizeVisionMaskTuning({ seedInfluenceScale: 0 }).seedInfluenceScale, 0.65);
        assert.equal(normalizeVisionMaskTuning({ seedInfluenceScale: 4 }).seedInfluenceScale, 1.25);
    });

    it("changes seed falloff without changing the selected seed vertices", () => {
        const component = makeTouchComponent(3);
        const positions = new Float32Array([0, 0, 0, 0.05, 0, 0, 0.1, 0, 0]);
        const indices = new Uint32Array([0, 1, 2]);
        const makeZone = (seedInfluenceScale: number) => ({
            id: "left_breast",
            label: "Left breast",
            channel: 0,
            confidence: 0.9,
            center: [0, 0, 0] as [number, number, number],
            radius: [0.2, 0.2, 0.2] as [number, number, number],
            source: "vision" as const,
            settings: createDefaultTouchZoneSettings(),
            seedVertices: [0],
            visionMaskTuning: { seedInfluenceScale },
        });

        const tight = buildVertexMasks(3, positions, indices, component, [makeZone(0.65)]);
        const wide = buildVertexMasks(3, positions, indices, component, [makeZone(1.25)]);

        assert.ok((wide[12] ?? 0) > (tight[12] ?? 0));
        assert.ok((wide[24] ?? 0) >= (tight[24] ?? 0));
    });

    it("normalizes all explicit correction tuning fields", () => {
        const tuning = {
            adjust: true,
            radiusPadding: 1.1,
            maskCutoffD2: 1.5,
            maskEdgeFadeD2: 0.35,
            seedInfluenceScale: 0.7,
            colocatedLayerRatio: 0.18,
            colocatedLayerMin: 0.008,
            sideHeightPadRatio: 0.06,
            sideHeightPadMin: 0.01,
            seedRadiusScale: 0.96,
        };
        const first = normalizeVisionMaskTuning();
        const corrected = normalizeVisionMaskTuning(tuning, first);

        assert.equal(first.radiusPadding, 1.02);
        assert.equal(first.maskCutoffD2, 2.25);
        assert.deepEqual(corrected, {
            radiusPadding: 1.1,
            maskCutoffD2: 1.5,
            maskEdgeFadeD2: 0.35,
            seedInfluenceScale: 0.7,
            colocatedLayerRatio: 0.18,
            colocatedLayerMin: 0.008,
            sideHeightPadRatio: 0.06,
            sideHeightPadMin: 0.01,
            seedRadiusScale: 0.96,
        });
    });

    it("uses tight production caps for body breast zones on large meshes", () => {
        assert.ok(maxVisionActiveRatio("body", "left_breast", "Left breast", 5000) <= 0.16);
        assert.ok(maxVisionHeightSpanRatio("body", "left_breast", "Left breast", 5000) <= 0.18);
        assert.ok(maxVisionActiveRatio("body", "left_breast", "Left breast", 80) >= 0.9);
    });

    it("drops side-view arm seeds outside the front height band", () => {
        // Front chest cluster + side arm far below in height (z).
        const chestCount = 40;
        const armCount = 40;
        const vertexCount = chestCount + armCount;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < chestCount; i++) {
            const t = i / (chestCount - 1);
            positions[i * 3] = -0.12 + t * 0.08;
            positions[i * 3 + 1] = -0.05 + (t - 0.5) * 0.04;
            positions[i * 3 + 2] = 1.25 + (t - 0.5) * 0.04;
        }
        for (let i = 0; i < armCount; i++) {
            const t = i / (armCount - 1);
            const base = chestCount + i;
            positions[base * 3] = -0.35 + t * 0.05;
            positions[base * 3 + 1] = 0.05 + (t - 0.5) * 0.02;
            positions[base * 3 + 2] = 0.4 + t * 0.15;
        }
        const indices = new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, i) => i),
        );
        const component = makeTouchComponent(vertexCount);
        const transforms = buildAllViewTransforms(positions);
        transforms.front.visibleVertices = new Uint8Array(vertexCount);
        transforms.left.visibleVertices = new Uint8Array(vertexCount);
        for (let i = 0; i < chestCount; i++) transforms.front.visibleVertices[i] = 1;
        for (let i = 0; i < vertexCount; i++) transforms.left.visibleVertices[i] = 1;

        const selections = selectVerticesFromVisionPolygons(
            component,
            positions,
            indices,
            [
                {
                    id: "left_breast",
                    label: "Left breast",
                    confidence: 0.9,
                    include: {
                        front: [
                            [
                                [0, 0],
                                [1, 0],
                                [1, 1],
                                [0, 1],
                            ],
                        ],
                        left: [
                            [
                                [0, 0],
                                [1, 0],
                                [1, 1],
                                [0, 1],
                            ],
                        ],
                    },
                    exclude: {},
                },
            ],
            transforms,
        );

        assert.equal(selections.length, 1);
        const seeds = selections[0].vertices;
        assert.ok(seeds.some((vertex) => vertex < chestCount));
        assert.ok(
            seeds.every((vertex) => vertex < chestCount),
            `arm seeds leaked: ${seeds.filter((vertex) => vertex >= chestCount).join(",")}`,
        );
    });
});

describe("zonesFromVisionPolygons", () => {
    it("maps normalized image polygons through the same transform as previews", () => {
        const vertexCount = 48;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            const t = i / (vertexCount - 1);
            positions[i * 3] = -0.25 + t * 0.5;
            positions[i * 3 + 1] = (i % 6) * 0.02;
            positions[i * 3 + 2] = 0.7 + (i % 8) * 0.03;
        }
        const indices = new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, i) => i),
        );
        const component = {
            id: "bodyPosition",
            name: "bodyPosition",
            kind: "body" as const,
            interactiveCandidate: true,
            supportGrade: "A" as const,
            supportReasons: [],
            positionResourceName: "bodyPosition",
            positionRelativePath: "bodyPosition.buf",
            positionPath: "bodyPosition.buf",
            positionStride: 40,
            vertexCount,
            indexCount: indices.length,
            drawRanges: [{ firstIndex: 0, indexCount: indices.length, baseVertex: 0 }],
            objectMaps: [
                {
                    firstIndex: 0,
                    indexCount: indices.length,
                    objectMode: 7,
                    objectId: 1,
                    label: "clothed",
                },
            ],
        };

        const transforms = buildAllViewTransforms(positions);
        // Cover most of the front silhouette so selected vertices are non-empty.
        const zones = zonesFromVisionPolygons(
            component,
            positions,
            indices,
            [
                {
                    id: "left_breast",
                    label: "Left breast",
                    confidence: 0.9,
                    include: {
                        front: [
                            [
                                [0.05, 0.05],
                                [0.55, 0.05],
                                [0.55, 0.95],
                                [0.05, 0.95],
                            ],
                        ],
                    },
                    exclude: {},
                },
            ],
            transforms,
        );

        assert.ok(zones.length >= 1);
        assert.equal(zones[0].source, "vision");
        assert.ok(zones[0].center[0] <= 0.05);
    });

    it("keeps LLM-selected vertices as mask seeds instead of inventing anatomy bands", () => {
        const vertexCount = 80;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            if (i < 40) {
                const t = i / 39;
                positions[i * 3] = -0.18 + (t % 0.5) * 0.16;
                positions[i * 3 + 1] = -0.2 + (i % 4) * 0.01;
                positions[i * 3 + 2] = 1.2 + (i % 5) * 0.02;
            } else {
                const t = (i - 40) / 39;
                positions[i * 3] = -0.2 + t * 0.4;
                positions[i * 3 + 1] = 0.05 + (t - 0.5) * 0.2;
                positions[i * 3 + 2] = 1.0 + t * 0.15;
            }
        }
        const indices = new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, i) => i),
        );
        const component = makeTouchComponent(vertexCount);
        const transforms = buildAllViewTransforms(positions);
        transforms.front.visibleVertices = new Uint8Array(vertexCount);
        for (let i = 0; i < 40; i++) transforms.front.visibleVertices[i] = 1;

        const zones = zonesFromVisionPolygons(
            component,
            positions,
            indices,
            [
                {
                    id: "left_breast",
                    label: "Left breast",
                    confidence: 0.9,
                    include: {
                        front: [
                            [
                                [0.0, 0.0],
                                [0.55, 0.0],
                                [0.55, 1.0],
                                [0.0, 1.0],
                            ],
                        ],
                    },
                    exclude: {},
                },
            ],
            transforms,
        );

        assert.equal(zones.length, 1);
        const zone = zones[0];
        assert.ok(zone.seedVertices && zone.seedVertices.length >= 3);
        assert.ok(zone.seedVertices.every((vertex) => vertex < 40));
        assert.ok(zone.center[0] < 0);

        const masks = buildVertexMasks(vertexCount, positions, indices, component, zones);
        let seedActive = 0;
        let farActive = 0;
        for (let vertex = 0; vertex < vertexCount; vertex++) {
            const weight = masks[vertex * 12] ?? 0;
            if (weight <= 0.02) continue;
            if (vertex < 40) seedActive += 1;
            else farActive += 1;
        }
        assert.ok(seedActive >= 3, `seed neighborhood empty: ${seedActive}`);
        // Distant torso cloud must not light up from a breast-only seed set.
        assert.ok(farActive < seedActive, `mask leaked to non-seed region: far=${farActive}`);
    });

    it("pulls body-conforming cloth layer vertices into vision seeds", () => {
        // Layer A (0-29): skin, depth-visible. Layer B (30-59): tight cloth, slightly outward, hidden.
        // Layer C (60-89): distant torso that must stay out.
        const skinCount = 30;
        const clothCount = 30;
        const farCount = 30;
        const vertexCount = skinCount + clothCount + farCount;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < skinCount; i++) {
            const t = i / (skinCount - 1);
            positions[i * 3] = -0.16 + (t % 0.5) * 0.12;
            positions[i * 3 + 1] = -0.18 + (i % 5) * 0.012;
            positions[i * 3 + 2] = 1.18 + (i % 4) * 0.015;
        }
        for (let i = 0; i < clothCount; i++) {
            const t = i / (clothCount - 1);
            const base = skinCount + i;
            positions[base * 3] = -0.16 + (t % 0.5) * 0.12 + 0.004;
            positions[base * 3 + 1] = -0.18 + (i % 5) * 0.012 + 0.003;
            positions[base * 3 + 2] = 1.18 + (i % 4) * 0.015 + 0.01;
        }
        for (let i = 0; i < farCount; i++) {
            const t = i / (farCount - 1);
            const base = skinCount + clothCount + i;
            positions[base * 3] = -0.2 + t * 0.4;
            positions[base * 3 + 1] = 0.2 + (t - 0.5) * 0.15;
            positions[base * 3 + 2] = 0.85 + t * 0.1;
        }
        const indices = new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, i) => i),
        );
        const component = makeTouchComponent(vertexCount);
        const transforms = buildAllViewTransforms(positions);
        transforms.front.visibleVertices = new Uint8Array(vertexCount);
        for (let i = 0; i < skinCount; i++) transforms.front.visibleVertices[i] = 1;

        const zones = zonesFromVisionPolygons(
            component,
            positions,
            indices,
            [
                {
                    id: "left_breast",
                    label: "Left breast",
                    confidence: 0.9,
                    include: {
                        front: [
                            [
                                [0.0, 0.0],
                                [0.55, 0.0],
                                [0.55, 1.0],
                                [0.0, 1.0],
                            ],
                        ],
                    },
                    exclude: {},
                },
            ],
            transforms,
        );

        assert.equal(zones.length, 1);
        const seeds = new Set(zones[0].seedVertices ?? []);
        assert.ok(seeds.size >= skinCount, `expected skin seeds, got ${seeds.size}`);
        let clothSeeds = 0;
        let farSeeds = 0;
        for (const vertex of seeds) {
            if (vertex >= skinCount && vertex < skinCount + clothCount) clothSeeds += 1;
            if (vertex >= skinCount + clothCount) farSeeds += 1;
        }
        assert.ok(clothSeeds >= skinCount * 0.5, `cloth layer under-selected: ${clothSeeds}`);
        assert.equal(farSeeds, 0, `distant torso leaked into seeds: ${farSeeds}`);

        const masks = buildVertexMasks(vertexCount, positions, indices, component, zones);
        let clothActive = 0;
        let farActive = 0;
        for (let vertex = skinCount; vertex < vertexCount; vertex++) {
            const weight = masks[vertex * 12] ?? 0;
            if (weight <= 0.02) continue;
            if (vertex < skinCount + clothCount) clothActive += 1;
            else farActive += 1;
        }
        assert.ok(clothActive >= clothCount * 0.5, `cloth mask under-active: ${clothActive}`);
        assert.ok(farActive < 3, `mask leaked to distant torso: ${farActive}`);
    });

    it("masks bra draw-range verts omitted from clothed/nude object maps", () => {
        // Body skin 0-59 in objectMaps; bra 60-89 only in an extra draw range (Pulchra-style).
        const bodyCount = 60;
        const braCount = 30;
        const vertexCount = bodyCount + braCount;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < bodyCount; i++) {
            const side = i < bodyCount / 2 ? -1 : 1;
            const t = (i % (bodyCount / 2)) / (bodyCount / 2 - 1);
            positions[i * 3] = side * (0.06 + t * 0.08);
            positions[i * 3 + 1] = -0.05 + (t - 0.5) * 0.06;
            positions[i * 3 + 2] = 1.22 + (t - 0.5) * 0.05;
        }
        for (let i = 0; i < braCount; i++) {
            const side = i < braCount / 2 ? -1 : 1;
            const t = (i % (braCount / 2)) / (braCount / 2 - 1);
            const base = bodyCount + i;
            positions[base * 3] = side * (0.06 + t * 0.08) + side * 0.003;
            positions[base * 3 + 1] = -0.05 + (t - 0.5) * 0.06 + 0.002;
            positions[base * 3 + 2] = 1.22 + (t - 0.5) * 0.05 + 0.008;
        }
        const bodyIndexCount = bodyCount;
        const braIndexCount = braCount;
        const indices = new Uint32Array(bodyIndexCount + braIndexCount);
        for (let i = 0; i < bodyCount; i++) indices[i] = i;
        for (let i = 0; i < braCount; i++) indices[bodyCount + i] = bodyCount + i;

        const component = {
            ...makeTouchComponent(vertexCount),
            indexCount: indices.length,
            drawRanges: [
                { firstIndex: 0, indexCount: bodyIndexCount, baseVertex: 0 },
                { firstIndex: bodyIndexCount, indexCount: braIndexCount, baseVertex: 0 },
            ],
            objectMaps: [
                {
                    firstIndex: 0,
                    indexCount: bodyIndexCount,
                    objectMode: 7,
                    objectId: 1,
                    label: "clothed",
                },
            ],
        };
        const transforms = buildAllViewTransforms(positions);
        transforms.front.visibleVertices = new Uint8Array(vertexCount);
        for (let i = 0; i < bodyCount / 2; i++) transforms.front.visibleVertices[i] = 1;

        const zones = zonesFromVisionPolygons(
            component,
            positions,
            indices,
            [
                {
                    id: "left_breast",
                    label: "Left breast",
                    confidence: 0.9,
                    include: {
                        front: [
                            [
                                [0.0, 0.0],
                                [0.55, 0.0],
                                [0.55, 1.0],
                                [0.0, 1.0],
                            ],
                        ],
                    },
                    exclude: {},
                },
            ],
            transforms,
        );

        assert.equal(zones.length, 1);
        const seeds = zones[0].seedVertices ?? [];
        const braSeeds = seeds.filter((vertex) => vertex >= bodyCount).length;
        assert.ok(braSeeds >= braCount / 4, `bra draw-range under-seeded: ${braSeeds}`);

        const masks = buildVertexMasks(vertexCount, positions, indices, component, zones);
        let braActive = 0;
        for (let vertex = bodyCount; vertex < bodyCount + braCount / 2; vertex++) {
            if ((masks[vertex * 12] ?? 0) > 0.02) braActive += 1;
        }
        assert.ok(braActive >= braCount / 4, `bra mask under-active: ${braActive}`);
    });

    it("pairs clothed and nude object maps when upper-body draw ranges have unequal index counts", () => {
        const bodyCount = 1800;
        const braCount = 3600;
        const vertexCount = bodyCount + braCount;
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            positions[i * 3] = (i % 2 === 0 ? -1 : 1) * 0.1;
            positions[i * 3 + 1] = 0;
            positions[i * 3 + 2] = 1.2;
        }
        const indices = new Uint32Array(braCount + bodyCount);
        for (let i = 0; i < braCount; i++) indices[i] = bodyCount + (i % braCount);
        for (let i = 0; i < bodyCount; i++) indices[braCount + i] = i;

        const mod = {
            drawRanges: [
                {
                    firstIndex: 0,
                    indexCount: braCount,
                    baseVertex: 0,
                    conditionText: "$Upper == 1",
                },
                { firstIndex: braCount, indexCount: bodyCount, baseVertex: 0 },
            ],
        };
        assert.equal(mod.drawRanges.length, 2);
    });
});

describe("analyzeTouchMod", () => {
    it("loads a minimal stride-40 body/leg style mod", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-"));
        const writePosition = (filePath: string, count: number) => {
            const bytes = Buffer.alloc(count * 40);
            for (let i = 0; i < count; i++) {
                bytes.writeFloatLE(i * 0.01, i * 40);
                bytes.writeFloatLE(0.1, i * 40 + 4);
                bytes.writeFloatLE(1.0, i * 40 + 8);
                bytes.writeFloatLE(0, i * 40 + 12);
                bytes.writeFloatLE(1, i * 40 + 16);
                bytes.writeFloatLE(0, i * 40 + 20);
                bytes.writeFloatLE(1, i * 40 + 24);
                bytes.writeFloatLE(0, i * 40 + 28);
                bytes.writeFloatLE(0, i * 40 + 32);
                bytes.writeFloatLE(1, i * 40 + 36);
            }
            fs.writeFileSync(filePath, bytes);
        };

        writePosition(path.join(root, "bodyPosition.buf"), 9);
        writePosition(path.join(root, "legPosition.buf"), 6);
        fs.writeFileSync(
            path.join(root, "bodyA.ib"),
            Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5, 6, 7, 8]).buffer),
        );
        fs.writeFileSync(
            path.join(root, "legA.ib"),
            Buffer.from(new Uint32Array([0, 1, 2, 3, 4, 5]).buffer),
        );
        fs.writeFileSync(
            path.join(root, "body.ini"),
            [
                "[Constants]",
                "global $active = 0",
                "",
                "[Present]",
                "post $active = 0",
                "",
                "[TextureOverridebodyBlend]",
                "hash = 11111111",
                "handling = skip",
                "vb2 = ResourcebodyBlend",
                "if DRAW_TYPE == 1",
                "\tvb0 = ResourcebodyPosition",
                "\tdraw = 9, 0",
                "\t$active = 1",
                "endif",
                "",
                "[TextureOverridebodyA]",
                "hash = 22222222",
                "ib = ResourcebodyAIB",
                "drawindexed = 9, 0, 0",
                "",
                "[TextureOverridelegBlend]",
                "hash = 33333333",
                "handling = skip",
                "vb2 = ResourcelegBlend",
                "if DRAW_TYPE == 1",
                "\tvb0 = ResourcelegPosition",
                "\tdraw = 6, 0",
                "\t$active = 1",
                "endif",
                "",
                "[TextureOverridelegA]",
                "hash = 44444444",
                "ib = ResourcelegAIB",
                "drawindexed = 6, 0, 0",
                "",
                "[ResourcebodyPosition]",
                "type = Buffer",
                "stride = 40",
                "filename = bodyPosition.buf",
                "",
                "[ResourcebodyBlend]",
                "type = Buffer",
                "stride = 32",
                "filename = bodyPosition.buf",
                "",
                "[ResourcebodyAIB]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = bodyA.ib",
                "",
                "[ResourcelegPosition]",
                "type = Buffer",
                "stride = 40",
                "filename = legPosition.buf",
                "",
                "[ResourcelegBlend]",
                "type = Buffer",
                "stride = 32",
                "filename = legPosition.buf",
                "",
                "[ResourcelegAIB]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = legA.ib",
                "",
            ].join("\n"),
            "utf8",
        );

        const analysis = await analyzeTouchMod(root);
        assert.equal(analysis.supportGrade, "A");
        assert.ok(analysis.components.length >= 2);
        assert.ok(analysis.components.some((component) => component.kind === "body"));
        assert.ok(analysis.components.some((component) => component.kind === "legs"));
        // Tiny fixture meshes stay non-interactive by vertex threshold.
        assert.ok(analysis.components.every((component) => !component.interactiveCandidate));
    });

    it("resolves GIMI merged command lists with auto drawindexed ranges", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-gimi-merged-"));
        const writePosition = (filePath: string) => {
            const bytes = Buffer.alloc(3 * 40);
            for (let index = 0; index < 3; index++) {
                bytes.writeFloatLE(index * 0.01, index * 40);
                bytes.writeFloatLE(0.1, index * 40 + 4);
                bytes.writeFloatLE(1.0, index * 40 + 8);
                bytes.writeFloatLE(0, index * 40 + 12);
                bytes.writeFloatLE(1, index * 40 + 16);
                bytes.writeFloatLE(0, index * 40 + 20);
                bytes.writeFloatLE(1, index * 40 + 24);
                bytes.writeFloatLE(0, index * 40 + 28);
                bytes.writeFloatLE(0, index * 40 + 32);
                bytes.writeFloatLE(1, index * 40 + 36);
            }
            fs.writeFileSync(filePath, bytes);
        };

        writePosition(path.join(root, "AmberPosition.0.buf"));
        writePosition(path.join(root, "AmberPosition.1.buf"));
        const indices = Buffer.from(new Uint32Array([0, 1, 2]).buffer);
        fs.writeFileSync(path.join(root, "AmberBody.0.ib"), indices);
        fs.writeFileSync(path.join(root, "AmberBody.1.ib"), indices);
        fs.writeFileSync(
            path.join(root, "merged.ini"),
            [
                "; Merged Mod: .\\AmberMain\\Amber.ini, .\\AmberBody\\Amber.ini",
                "",
                "[Constants]",
                "global persist $swapvar = 0",
                "",
                "[TextureOverrideAmberPosition]",
                "hash = a2ea4b2d",
                "run = CommandListAmberPosition",
                "",
                "[TextureOverrideAmberIB]",
                "hash = b03c7e30",
                "run = CommandListAmberIB",
                "",
                "[TextureOverrideAmberBody]",
                "hash = b03c7e30",
                "match_first_index = 5670",
                "run = CommandListAmberBody",
                "",
                "[CommandListAmberPosition]",
                "if $swapvar == 0",
                "    vb0 = ResourceAmberPosition.0",
                "else if $swapvar == 1",
                "    vb0 = ResourceAmberPosition.1",
                "endif",
                "",
                "[CommandListAmberIB]",
                "if $swapvar == 0",
                "    handling = skip",
                "    drawindexed = auto",
                "else if $swapvar == 1",
                "    handling = skip",
                "    drawindexed = auto",
                "endif",
                "",
                "[CommandListAmberBody]",
                "if $swapvar == 0",
                "    ib = ResourceAmberBodyIB.0",
                "else if $swapvar == 1",
                "    ib = ResourceAmberBodyIB.1",
                "endif",
                "",
                "[ResourceAmberPosition.0]",
                "type = Buffer",
                "stride = 40",
                "filename = AmberPosition.0.buf",
                "",
                "[ResourceAmberPosition.1]",
                "type = Buffer",
                "stride = 40",
                "filename = AmberPosition.1.buf",
                "",
                "[ResourceAmberBodyIB.0]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = AmberBody.0.ib",
                "",
                "[ResourceAmberBodyIB.1]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = AmberBody.1.ib",
            ].join("\n"),
            "utf8",
        );

        const analysis = await analyzeTouchMod(root);

        assert.equal(analysis.supportGrade, "A");
        assert.equal(analysis.components.length, 2);
        assert.deepEqual(
            analysis.components.map((component) => ({
                variantKey: component.variantKey,
                variantCondition: component.variantCondition,
                indexResourceName: component.indexResourceName,
                ibSectionName: component.ibSectionName,
                drawRanges: component.drawRanges.map((range) => range.indexCount),
            })),
            [
                {
                    variantKey: "0",
                    variantCondition: "$swapvar == 0",
                    indexResourceName: "AmberBodyIB.0",
                    ibSectionName: "AmberBody",
                    drawRanges: [3],
                },
                {
                    variantKey: "1",
                    variantCondition: "!($swapvar == 0) && ($swapvar == 1)",
                    indexResourceName: "AmberBodyIB.1",
                    ibSectionName: "AmberBody",
                    drawRanges: [3],
                },
            ],
        );
    });

    it("uses semantic TextureOverride names for hash-named resources", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-hash-names-"));
        const writePosition = (filePath: string) => {
            const bytes = Buffer.alloc(3 * 40);
            for (let i = 0; i < 3; i++) {
                bytes.writeFloatLE(i * 0.01, i * 40);
                bytes.writeFloatLE(0.1, i * 40 + 4);
                bytes.writeFloatLE(1.0, i * 40 + 8);
                bytes.writeFloatLE(0, i * 40 + 12);
                bytes.writeFloatLE(1, i * 40 + 16);
                bytes.writeFloatLE(0, i * 40 + 20);
                bytes.writeFloatLE(1, i * 40 + 24);
                bytes.writeFloatLE(0, i * 40 + 28);
                bytes.writeFloatLE(0, i * 40 + 32);
                bytes.writeFloatLE(1, i * 40 + 36);
            }
            fs.writeFileSync(filePath, bytes);
        };

        writePosition(path.join(root, "upper-Position.buf"));
        writePosition(path.join(root, "lower-Position.buf"));
        fs.writeFileSync(
            path.join(root, "upper-Component1.buf"),
            Buffer.from(new Uint32Array([0, 1, 2]).buffer),
        );
        fs.writeFileSync(
            path.join(root, "lower-Component1.buf"),
            Buffer.from(new Uint32Array([0, 1, 2]).buffer),
        );
        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[TextureOverride_VB_a1b2c3d4_shangban_Position]",
                "vb0 = Resourcea1b2c3d4Position",
                "",
                "[TextureOverride_IB_a1b2c3d4_shangban_Component1]",
                "ib = Resource_a1b2c3d4_Component1",
                "drawindexed = 3, 0, 0",
                "",
                "[TextureOverride_VB_e5f6a7b8_xiaban_Position]",
                "vb0 = Resourcee5f6a7b8Position",
                "",
                "[TextureOverride_IB_e5f6a7b8_xiaban_Component1]",
                "ib = Resource_e5f6a7b8_Component1",
                "drawindexed = 3, 0, 0",
                "",
                "[Resourcea1b2c3d4Position]",
                "type = Buffer",
                "stride = 40",
                "filename = upper-Position.buf",
                "",
                "[Resource_a1b2c3d4_Component1]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = upper-Component1.buf",
                "",
                "[Resourcee5f6a7b8Position]",
                "type = Buffer",
                "stride = 40",
                "filename = lower-Position.buf",
                "",
                "[Resource_e5f6a7b8_Component1]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = lower-Component1.buf",
            ].join("\n"),
            "utf8",
        );

        const analysis = await analyzeTouchMod(root);

        assert.equal(
            analysis.components.find((component) => /a1b2c3d4/.test(component.id))?.kind,
            "body",
        );
        assert.equal(
            analysis.components.find((component) => /e5f6a7b8/.test(component.id))?.kind,
            "legs",
        );
        assert.ok(!analysis.supportReasons.includes("Component kind is ambiguous"));
    });

    it("expands command lists when drawindexed is indirect", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-command-list-"));
        const positionBytes = Buffer.alloc(3 * 40);
        for (let i = 0; i < 3; i++) {
            positionBytes.writeFloatLE(i * 0.01, i * 40);
            positionBytes.writeFloatLE(0.1, i * 40 + 4);
            positionBytes.writeFloatLE(1.0, i * 40 + 8);
            positionBytes.writeFloatLE(0, i * 40 + 12);
            positionBytes.writeFloatLE(1, i * 40 + 16);
            positionBytes.writeFloatLE(0, i * 40 + 20);
            positionBytes.writeFloatLE(1, i * 40 + 24);
            positionBytes.writeFloatLE(0, i * 40 + 28);
            positionBytes.writeFloatLE(0, i * 40 + 32);
            positionBytes.writeFloatLE(1, i * 40 + 36);
        }
        fs.writeFileSync(path.join(root, "body-Position.buf"), positionBytes);
        fs.writeFileSync(
            path.join(root, "body-A.ib"),
            Buffer.from(new Uint32Array([0, 1, 2]).buffer),
        );
        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[TextureOverrideBodyBlend]",
                "hash = 11111111",
                "run = CommandListBodyBlend",
                "",
                "[TextureOverrideBodyA]",
                "hash = 22222222",
                "run = CommandListBodyA",
                "",
                "[CommandListBodyBlend]",
                "vb2 = ResourceBodyBlend",
                "if DRAW_TYPE == 1",
                "vb0 = ResourceBodyPosition",
                "draw = 3, 0",
                "endif",
                "",
                "[CommandListBodyA]",
                "ib = ResourceBodyAIB",
                "if $active == 1",
                "drawindexed = 3, 0, 0",
                "endif",
                "",
                "[ResourceBodyPosition]",
                "type = Buffer",
                "stride = 40",
                "filename = body-Position.buf",
                "",
                "[ResourceBodyBlend]",
                "type = Buffer",
                "stride = 32",
                "filename = body-Position.buf",
                "",
                "[ResourceBodyAIB]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = body-A.ib",
            ].join("\n"),
            "utf8",
        );

        const analysis = await analyzeTouchMod(root);
        const component = analysis.components[0];

        assert.ok(component);
        assert.equal(analysis.supportGrade, "A");
        assert.equal(component.drawRanges.length, 1);
        assert.equal(component.drawRanges[0]?.indexCount, 3);
        assert.equal(component.ibSectionName, "BodyA");
    });

    it("marks body2-style tiny parts non-interactive even when named body", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-body2-"));
        const count = 2000;
        const bytes = Buffer.alloc(count * 40);
        for (let i = 0; i < count; i++) {
            bytes.writeFloatLE(i * 0.001, i * 40);
            bytes.writeFloatLE(0.1, i * 40 + 4);
            bytes.writeFloatLE(1.0, i * 40 + 8);
            bytes.writeFloatLE(0, i * 40 + 12);
            bytes.writeFloatLE(1, i * 40 + 16);
            bytes.writeFloatLE(0, i * 40 + 20);
            bytes.writeFloatLE(1, i * 40 + 24);
            bytes.writeFloatLE(0, i * 40 + 28);
            bytes.writeFloatLE(0, i * 40 + 32);
            bytes.writeFloatLE(1, i * 40 + 36);
        }
        fs.writeFileSync(path.join(root, "body2Position.buf"), bytes);
        const indices = new Uint32Array(Array.from({ length: 3600 }, (_, i) => i % count));
        fs.writeFileSync(path.join(root, "body2A.ib"), Buffer.from(indices.buffer));
        fs.writeFileSync(
            path.join(root, "body.ini"),
            [
                "[TextureOverridebody2Blend]",
                "vb0 = Resourcebody2Position",
                "[TextureOverridebody2A]",
                "ib = Resourcebody2AIB",
                "drawindexed = 3600, 0, 0",
                "[Resourcebody2Position]",
                "type = Buffer",
                "stride = 40",
                "filename = body2Position.buf",
                "[Resourcebody2AIB]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = body2A.ib",
            ].join("\n"),
            "utf8",
        );

        const analysis = await analyzeTouchMod(root);
        const body2 = analysis.components.find((component) => /body2/i.test(component.name));
        assert.ok(body2);
        assert.equal(body2.kind, "body");
        assert.equal(body2.interactiveCandidate, false);
    });
});

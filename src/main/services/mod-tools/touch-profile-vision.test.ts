import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { createLlmJsonCompletion } from "@main/lib/llm";
import { PNG } from "pngjs";
import { afterEach, beforeEach, describe, it, vi } from "vitest";

import type { TouchComponentAnalysis } from "./touch-profile-types";

import { buildAllViewTransforms, TOUCH_VIEW_NAMES } from "./touch-profile-projection";
import { analyzeComponentVision, renderComponentPreviews } from "./touch-profile-vision";

vi.mock("@main/lib/llm", () => ({
    LLM_MODEL: "test-model",
    createLlmJsonCompletion: vi.fn(),
}));

function makeComponent(vertexCount = 120) {
    const indexCount = Math.floor(vertexCount / 3) * 3;
    return {
        id: "bodyPosition",
        name: "bodyPosition",
        kind: "body",
        interactiveCandidate: true,
        supportGrade: "A",
        supportReasons: [],
        positionResourceName: "bodyPosition",
        positionRelativePath: "bodyPosition.buf",
        positionPath: "bodyPosition.buf",
        positionStride: 40,
        vertexCount,
        indexCount,
        drawRanges: [{ firstIndex: 0, indexCount, baseVertex: 0 }],
        objectMaps: [
            {
                firstIndex: 0,
                indexCount,
                objectMode: 7,
                objectId: 1,
                label: "skin",
            },
        ],
        bones: [],
    } satisfies TouchComponentAnalysis;
}

function makeMesh(vertexCount: number) {
    const positions = new Float32Array(vertexCount * 3);
    for (let i = 0; i < vertexCount; i++) {
        const side = i < vertexCount / 2 ? -1 : 1;
        const t = (i % (vertexCount / 2)) / (vertexCount / 2 - 1);
        positions[i * 3] = 10 + side * (0.08 + t * 0.06);
        positions[i * 3 + 1] = 0.65 + (t - 0.5) * 0.08;
        positions[i * 3 + 2] = 0.7 + (t - 0.5) * 0.08;
    }
    return {
        positions,
        indices: new Uint32Array(
            Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, index) => index),
        ),
    };
}

function makePreviews() {
    return TOUCH_VIEW_NAMES.map((view) => ({
        view,
        absolutePath: `${view}.png`,
        relativePath: `previews/${view}.png`,
        bytes: Buffer.from(view),
    }));
}

function makeVisionInput(component = makeComponent()) {
    const mesh = makeMesh(component.vertexCount);
    return {
        component,
        positions: mesh.positions,
        indices: mesh.indices,
        previews: makePreviews(),
        transforms: buildAllViewTransforms(mesh.positions),
        objectId: 1,
    };
}

describe("renderComponentPreviews", () => {
    const tempDirs: string[] = [];

    afterEach(() => {
        for (const dir of tempDirs.splice(0)) {
            fs.rmSync(dir, { recursive: true, force: true });
        }
    });

    it("shades projections with lighting contrast instead of a flat silhouette", async () => {
        const sessionDir = fs.mkdtempSync(path.join(os.tmpdir(), "touch-preview-shade-"));
        tempDirs.push(sessionDir);

        // Unit cube: faces point different directions so Lambert must vary.
        const positions = new Float32Array([
            0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1,
        ]);
        const indices = new Uint32Array([
            0, 1, 2, 0, 2, 3, 4, 6, 5, 4, 7, 6, 0, 4, 5, 0, 5, 1, 1, 5, 6, 1, 6, 2, 2, 6, 7, 2, 7,
            3, 3, 7, 4, 3, 4, 0,
        ]);
        const { previews } = await renderComponentPreviews({
            sessionDir,
            component: makeComponent(8),
            positions,
            indices,
        });

        const front = previews.find((preview) => preview.view === "front");
        assert.ok(front);
        const png = PNG.sync.read(front.bytes);
        let minL = 255;
        let maxL = 0;
        let covered = 0;
        for (let i = 0; i < png.data.length; i += 4) {
            if (png.data[i] === 0 && png.data[i + 1] === 0 && png.data[i + 2] === 0) continue;
            const l = (png.data[i] + png.data[i + 1] + png.data[i + 2]) / 3;
            minL = Math.min(minL, l);
            maxL = Math.max(maxL, l);
            covered += 1;
        }
        assert.ok(covered > 100);
        assert.ok(maxL - minL >= 25, `expected lighting contrast, got range ${maxL - minL}`);
    });

    it("draws normalized-space reference ticks and value labels on the outer border only", async () => {
        const sessionDir = fs.mkdtempSync(path.join(os.tmpdir(), "touch-preview-ticks-"));
        tempDirs.push(sessionDir);

        const positions = new Float32Array([
            0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1,
        ]);
        const indices = new Uint32Array([
            0, 1, 2, 0, 2, 3, 4, 6, 5, 4, 7, 6, 0, 4, 5, 0, 5, 1, 1, 5, 6, 1, 6, 2, 2, 6, 7, 2, 7,
            3, 3, 7, 4, 3, 4, 0,
        ]);
        const { previews } = await renderComponentPreviews({
            sessionDir,
            component: makeComponent(8),
            positions,
            indices,
        });

        const front = previews.find((preview) => preview.view === "front");
        assert.ok(front);
        const png = PNG.sync.read(front.bytes);
        const size = png.width;
        const pixel = (x: number, y: number) => {
            const offset = (y * size + x) << 2;
            return [png.data[offset], png.data[offset + 1], png.data[offset + 2]];
        };

        // Major tick at normalized 0.5: left border x=0..10, y=size/2.
        const majorPos = Math.round(size / 2);
        assert.deepEqual(pixel(0, majorPos), [220, 220, 230]);
        assert.deepEqual(pixel(10, majorPos), [220, 220, 230]);

        // Minor tick at normalized 0.05: top border x=pos, y=0..4.
        const minorPos = Math.round((size - 64) / 20) + 32;
        assert.deepEqual(pixel(minorPos, 0), [180, 180, 190]);
        assert.deepEqual(pixel(minorPos, 4), [180, 180, 190]);

        // Value label "0.5" starts at x=12; digit "0" row 2 has a pixel at x=12.
        assert.deepEqual(pixel(12, majorPos + 2), [220, 220, 230]);

        // Mesh interior (inside the 32px pad) must not be overwritten by ticks.
        assert.notDeepEqual(pixel(32, 384), [220, 220, 230]);
    });
});

describe("analyzeComponentVision", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("honors an explicit non-interactive vision result", async () => {
        vi.mocked(createLlmJsonCompletion).mockResolvedValue({
            data: {
                componentId: "bodyPosition",
                isHumanBody: true,
                approved: true,
                interactive: false,
                zones: [],
                excludedRegions: ["armor"],
                warnings: [],
            },
            rawText: "{}",
            model: "test-model",
        });
        const component = makeComponent();
        const mesh = makeMesh(component.vertexCount);

        const result = await analyzeComponentVision({
            component,
            positions: mesh.positions,
            indices: mesh.indices,
            previews: makePreviews(),
            transforms: buildAllViewTransforms(mesh.positions),
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.deepEqual(result.zones, []);
        assert.equal(result.vision?.interactive, false);
    });

    it("returns no zones when every vision request fails", async () => {
        vi.mocked(createLlmJsonCompletion).mockRejectedValue(new Error("service unavailable"));
        const component = makeComponent();
        const mesh = makeMesh(component.vertexCount);

        const result = await analyzeComponentVision({
            component,
            positions: mesh.positions,
            indices: mesh.indices,
            previews: makePreviews(),
            transforms: buildAllViewTransforms(mesh.positions),
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.deepEqual(result.zones, []);
        assert.ok(result.warnings.some((warning) => warning.includes("No usable Vision result")));
    });

    it("orders all attached previews by view before sending them to Vision", async () => {
        vi.mocked(createLlmJsonCompletion).mockResolvedValue({
            data: {
                componentId: "bodyPosition",
                isHumanBody: false,
                approved: true,
                interactive: false,
                zones: [],
                excludedRegions: [],
                warnings: [],
            },
            rawText: "{}",
            model: "test-model",
        });

        const result = await analyzeComponentVision({
            ...makeVisionInput(),
            previews: makePreviews().reverse(),
        });

        assert.equal(result.interactive, false);
        assert.deepEqual(
            vi
                .mocked(createLlmJsonCompletion)
                .mock.calls[0]?.[0].images?.map((image) => image.bytes.toString()),
            TOUCH_VIEW_NAMES,
        );
    });

    it("rejects missing or duplicate Vision previews before calling the LLM", async () => {
        for (const previews of [makePreviews().slice(1), [...makePreviews(), makePreviews()[0]!]]) {
            const result = await analyzeComponentVision({
                ...makeVisionInput(),
                previews,
            });

            assert.equal(result.interactive, false);
            assert.deepEqual(result.zones, []);
            assert.ok(result.warnings.some((warning) => warning.includes("Vision previews")));
        }
        assert.equal(vi.mocked(createLlmJsonCompletion).mock.calls.length, 0);
    });

    it("ends vision immediately when the component is not human body anatomy", async () => {
        vi.mocked(createLlmJsonCompletion).mockResolvedValue({
            data: {
                componentId: "bodyPosition",
                isHumanBody: false,
                approved: true,
                interactive: false,
                zones: [],
                excludedRegions: ["accessory"],
                warnings: [],
            },
            rawText: "{}",
            model: "test-model",
        });
        const component = makeComponent();
        const mesh = makeMesh(component.vertexCount);

        const result = await analyzeComponentVision({
            component,
            positions: mesh.positions,
            indices: mesh.indices,
            previews: makePreviews(),
            transforms: buildAllViewTransforms(mesh.positions),
            objectId: 1,
        });

        assert.equal(vi.mocked(createLlmJsonCompletion).mock.calls.length, 1);
        assert.equal(result.vision?.isHumanBody, false);
        assert.equal(result.interactive, false);
        assert.deepEqual(result.zones, []);
    });
});

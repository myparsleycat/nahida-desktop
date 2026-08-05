import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import type { NahidaDesktop } from "@main/index";
import { beforeEach, describe, it, vi } from "vitest";

import type {
    TouchComponentAnalysis,
    TouchComponentDraft,
    TouchModAnalysis,
} from "./touch-profile-types";

import { TouchProfileService } from "./touch-profile";
import { analyzeTouchMod, loadTouchMeshBuffers } from "./touch-profile-analyzer";
import { buildAllViewTransforms } from "./touch-profile-projection";
import { analyzeComponentVision, renderComponentPreviews } from "./touch-profile-vision";

const mockedGetPath = vi.hoisted(() => vi.fn());

vi.mock("electron", () => ({
    app: { getPath: mockedGetPath },
}));

vi.mock("./touch-profile-analyzer", () => ({
    analyzeTouchMod: vi.fn(),
    hashTouchFiles: vi.fn(),
    loadTouchMeshBuffers: vi.fn(),
}));

vi.mock("./touch-profile-vision", () => ({
    analyzeComponentVision: vi.fn(),
    renderComponentPreviews: vi.fn(),
}));

function makeComponent(
    index: number,
    overrides: Partial<TouchComponentAnalysis> = {},
): TouchComponentAnalysis {
    return {
        id: `component-${index}`,
        name: `component-${index}`,
        kind: "body",
        interactiveCandidate: true,
        supportGrade: "A",
        supportReasons: [],
        positionResourceName: `component-${index}`,
        positionRelativePath: `component-${index}.buf`,
        positionPath: `component-${index}.buf`,
        positionStride: 40,
        vertexCount: 3,
        indexCount: 3,
        drawRanges: [{ firstIndex: 0, indexCount: 3, baseVertex: 0 }],
        objectMaps: [],
        ...overrides,
    };
}

function makeDesktop(_sessionRoot: string) {
    return {
        ipc: { broadcast: vi.fn() },
        lib: {
            db: {
                settings: {
                    getValue: vi.fn(async () => null),
                },
                touchProfileVisionCache: {
                    get: vi.fn(async () => null),
                    upsert: vi.fn(async () => {}),
                    deleteAll: vi.fn(async () => {}),
                },
            },
        },
        logger: { error: vi.fn(), warn: vi.fn() },
        setting: {
            get: vi.fn(async () => 7),
            getMany: vi.fn(async () => ({
                "tools.touchProfileLlmProtocol": "openai-response",
                "tools.touchProfileLlmEndpoint": "https://example.com/v1",
                "tools.touchProfileLlmModel": "test-model",
                "tools.touchProfileLlmReasoning": "auto",
            })),
        },
    } as unknown as NahidaDesktop;
}

describe("TouchProfileService position concurrency", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("limits concurrent position analysis and preserves result order", async () => {
        const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-concurrency-"));
        mockedGetPath.mockReturnValue(sessionRoot);

        const components = Array.from({ length: 5 }, (_, index) => makeComponent(index));
        vi.mocked(analyzeTouchMod).mockResolvedValue({
            modRoot: sessionRoot,
            iniPath: path.join(sessionRoot, "mod.ini"),
            iniRelativePath: "mod.ini",
            sourceFilesRelativePaths: ["mod.ini"],
            supportGrade: "A",
            supportReasons: [],
            components,
            meshHash: "mesh-hash",
            iniHash: "ini-hash",
        } satisfies TouchModAnalysis);

        const mesh = {
            positions: new Float32Array(9),
            normals: new Float32Array(9),
            indices: new Uint32Array([0, 1, 2]),
            positionBytes: Buffer.alloc(0),
        };
        vi.mocked(loadTouchMeshBuffers).mockResolvedValue(mesh);
        vi.mocked(renderComponentPreviews).mockImplementation(async (input) => ({
            previews: [],
            transforms: buildAllViewTransforms(input.positions),
        }));

        let active = 0;
        let maxActive = 0;
        vi.mocked(analyzeComponentVision).mockImplementation(async (input) => {
            active += 1;
            maxActive = Math.max(maxActive, active);
            await new Promise((resolve) =>
                setTimeout(resolve, input.component.id === "component-0" ? 20 : 2),
            );
            active -= 1;

            return {
                componentId: input.component.id,
                interactive:
                    input.component.id !== "component-1" && input.component.id !== "component-4",
                objectId: input.objectId,
                zones: [],
                confidence: 0.9,
                warnings: [],
            } satisfies TouchComponentDraft;
        });

        const desktop = makeDesktop(sessionRoot);

        const result = await new TouchProfileService(desktop).loadMod({
            modPath: path.join(sessionRoot, "source"),
        });

        assert.equal(maxActive, 3);
        assert.deepEqual(
            result.components.map((component) => component.componentId),
            components.map((component) => component.id),
        );
        assert.deepEqual(
            result.components.map((component) => component.objectId),
            [1, 2, 2, 3, 4],
        );

        fs.rmSync(sessionRoot, { recursive: true, force: true });
    });

    it("prepareMod returns inspection without running vision and keeps session for later analysis", async () => {
        const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-prepare-"));
        mockedGetPath.mockReturnValue(sessionRoot);

        const components = [
            makeComponent(0, { interactiveCandidate: true }),
            makeComponent(1, { interactiveCandidate: true }),
            makeComponent(2, { interactiveCandidate: false, kind: "accessory" }),
        ];
        vi.mocked(analyzeTouchMod).mockResolvedValue({
            modRoot: sessionRoot,
            iniPath: path.join(sessionRoot, "mod.ini"),
            iniRelativePath: "mod.ini",
            sourceFilesRelativePaths: ["mod.ini"],
            supportGrade: "A",
            supportReasons: [],
            components,
            meshHash: "mesh-hash",
            iniHash: "ini-hash",
        } satisfies TouchModAnalysis);

        const mesh = {
            positions: new Float32Array(9),
            normals: new Float32Array(9),
            indices: new Uint32Array([0, 1, 2]),
            positionBytes: Buffer.alloc(0),
        };
        vi.mocked(loadTouchMeshBuffers).mockResolvedValue(mesh);
        vi.mocked(renderComponentPreviews).mockImplementation(async (input) => ({
            previews: [],
            transforms: buildAllViewTransforms(input.positions),
        }));

        const desktop = makeDesktop(sessionRoot);
        const service = new TouchProfileService(desktop);
        const inspection = await service.prepareMod(path.join(sessionRoot, "source"));

        assert.equal(inspection.components.length, 3);
        assert.deepEqual(
            inspection.components.map((component) => component.id),
            ["component-0", "component-1", "component-2"],
        );
        assert.deepEqual(
            inspection.components.map((component) => component.interactiveCandidate),
            [true, true, false],
        );
        assert.equal(vi.mocked(analyzeComponentVision).mock.calls.length, 0);

        const meshPreview = await service.getMeshPreview({
            sessionId: inspection.sessionId,
            componentId: "component-1",
        });
        assert.equal(meshPreview.positions, mesh.positions);
        assert.equal(meshPreview.indices, mesh.indices);
        assert.equal(vi.mocked(analyzeComponentVision).mock.calls.length, 0);

        fs.rmSync(sessionRoot, { recursive: true, force: true });
    });

    it("analyzeComponents runs vision only on selected components and marks the rest non-interactive", async () => {
        const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "touch-profile-filter-"));
        mockedGetPath.mockReturnValue(sessionRoot);

        const components = [
            makeComponent(0, { interactiveCandidate: true }),
            makeComponent(1, { interactiveCandidate: true }),
            makeComponent(2, { interactiveCandidate: false, kind: "accessory" }),
        ];
        vi.mocked(analyzeTouchMod).mockResolvedValue({
            modRoot: sessionRoot,
            iniPath: path.join(sessionRoot, "mod.ini"),
            iniRelativePath: "mod.ini",
            sourceFilesRelativePaths: ["mod.ini"],
            supportGrade: "A",
            supportReasons: [],
            components,
            meshHash: "mesh-hash",
            iniHash: "ini-hash",
        } satisfies TouchModAnalysis);

        const mesh = {
            positions: new Float32Array(9),
            normals: new Float32Array(9),
            indices: new Uint32Array([0, 1, 2]),
            positionBytes: Buffer.alloc(0),
        };
        vi.mocked(loadTouchMeshBuffers).mockResolvedValue(mesh);
        vi.mocked(renderComponentPreviews).mockImplementation(async (input) => ({
            previews: [],
            transforms: buildAllViewTransforms(input.positions),
        }));
        vi.mocked(analyzeComponentVision).mockImplementation(
            async (input) =>
                ({
                    componentId: input.component.id,
                    interactive: true,
                    objectId: input.objectId,
                    zones: [],
                    confidence: 0.9,
                    warnings: [],
                }) satisfies TouchComponentDraft,
        );

        const desktop = makeDesktop(sessionRoot);
        const service = new TouchProfileService(desktop);
        const inspection = await service.prepareMod(path.join(sessionRoot, "source"));
        const draft = await service.analyzeComponents({
            sessionId: inspection.sessionId,
            componentIds: ["component-0"],
        });

        assert.deepEqual(
            vi.mocked(analyzeComponentVision).mock.calls.map((call) => call[0].component.id),
            ["component-0"],
        );
        assert.deepEqual(
            draft.components.map((component) => component.componentId),
            ["component-0", "component-1", "component-2"],
        );
        assert.equal(draft.components[0]?.interactive, true);
        assert.equal(draft.components[1]?.interactive, false);
        assert.match(draft.components[1]?.warnings[0] ?? "", /not selected/i);
        assert.equal(draft.components[2]?.interactive, false);
        assert.deepEqual(
            draft.components.map((component) => component.objectId),
            [1, 2, 2],
        );

        fs.rmSync(sessionRoot, { recursive: true, force: true });
    });
});

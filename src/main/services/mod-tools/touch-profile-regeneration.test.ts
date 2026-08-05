import assert from "node:assert/strict";
import path from "node:path";

import type { NahidaDesktop } from "@main/index";
import { LLM_MODEL } from "@main/lib/llm";
import { createDefaultTouchZoneSettings } from "@shared/touch-profile-settings";
import fse from "fs-extra";
import { describe, it, vi } from "vitest";

import type { TouchComponentAnalysis, TouchDraft } from "./touch-profile-types";

import { TouchProfileService } from "./touch-profile";
import { hashTouchFiles } from "./touch-profile-analyzer";
import { TOUCH_PROMPT_VERSION } from "./touch-profile-types";

describe("TouchProfileService regeneration", () => {
    it("reuses the analyzed mesh and swaps regenerated output", async () => {
        const root = await fse.mkdtemp(path.join(process.cwd(), "touch-profile-regenerate-"));
        const sourceRoot = path.join(root, "DISABLED_AliceMod");
        const outputRoot = path.join(root, "AliceMod (Touch)");
        const sessionDir = path.join(root, "session");
        await fse.ensureDir(sourceRoot);
        await fse.ensureDir(outputRoot);
        await fse.ensureDir(sessionDir);
        await fse.writeFile(path.join(outputRoot, "old.txt"), "old");

        const positions = new Float32Array([-0.1, 0, 1, 0.1, 0, 1, 0, 0.1, 1]);
        const positionBytes = Buffer.alloc(3 * 40);
        for (let index = 0; index < 3; index++) {
            positionBytes.writeFloatLE(positions[index * 3]!, index * 40);
            positionBytes.writeFloatLE(positions[index * 3 + 1]!, index * 40 + 4);
            positionBytes.writeFloatLE(positions[index * 3 + 2]!, index * 40 + 8);
        }
        const positionPath = path.join(sourceRoot, "bodyPosition.buf");
        const indexPath = path.join(sourceRoot, "bodyIndex.ib");
        const iniPath = path.join(sourceRoot, "mod.ini");
        await fse.writeFile(positionPath, positionBytes);
        await fse.writeFile(indexPath, Buffer.from(new Uint32Array([0, 1, 2]).buffer));
        await fse.writeFile(
            iniPath,
            [
                "[Constants]",
                "global $active = 0",
                "",
                "[Present]",
                "post $active = 0",
                "",
                "[TextureOverridebodyBlend]",
                "vb0 = ResourcebodyPosition",
                "if DRAW_TYPE == 1",
                "    $active = 1",
                "endif",
                "",
                "[TextureOverridebodyIb]",
                "run = CommandListbodyIb",
                "",
                "[CommandListbodyIb]",
                "ib = ResourcebodyIndex",
                "drawindexed = 3, 0, 0",
                "",
                "[ResourcebodyPosition]",
                "type = Buffer",
                "stride = 40",
                "filename = bodyPosition.buf",
                "",
                "[ResourcebodyIndex]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = bodyIndex.ib",
            ].join("\n"),
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
            positionPath,
            positionStride: 40,
            vertexCount: 3,
            indexResourceName: "bodyIndex",
            indexRelativePath: "bodyIndex.ib",
            indexPath,
            indexFormat: "DXGI_FORMAT_R32_UINT",
            indexCount: 3,
            blendSectionName: "bodyBlend",
            ibSectionName: "bodyIb",
            drawRanges: [{ firstIndex: 0, indexCount: 3, baseVertex: 0 }],
            objectMaps: [
                { firstIndex: 0, indexCount: 3, objectMode: 7, objectId: 1, label: "main" },
            ],
            bones: [],
        } satisfies TouchComponentAnalysis;
        const componentDraft = {
            componentId: component.id,
            interactive: true,
            objectId: 1,
            zones: [
                {
                    id: "zone",
                    label: "Zone",
                    channel: 0,
                    confidence: 0.9,
                    center: [0, 0, 1] as [number, number, number],
                    radius: [0.3, 0.3, 0.3] as [number, number, number],
                    source: "manual" as const,
                    settings: createDefaultTouchZoneSettings(),
                },
            ],
            confidence: 0.9,
            warnings: [],
        } satisfies TouchDraft["components"][number];
        const analysis = {
            modRoot: sourceRoot,
            iniPath,
            iniRelativePath: "mod.ini",
            sourceFilesRelativePaths: ["mod.ini", "bodyPosition.buf", "bodyIndex.ib"],
            supportGrade: "A" as const,
            supportReasons: [],
            components: [component],
            meshHash: await hashTouchFiles([positionPath, indexPath], sourceRoot),
            iniHash: await hashTouchFiles([iniPath, positionPath, indexPath], sourceRoot),
        };
        const draft = {
            sessionId: "regenerate-test",
            createdAt: new Date().toISOString(),
            sourceModRoot: sourceRoot,
            analysis,
            components: [componentDraft],
            visionUsed: true,
            modelName: LLM_MODEL,
            llm: {
                protocol: "openai-response",
                endpoint: "https://api.openai.com/v1",
                model: LLM_MODEL,
                reasoning: "auto",
            },
            promptVersion: TOUCH_PROMPT_VERSION,
            runtimeVersion: "1",
            canAutoApply: true,
            warnings: [],
        } satisfies TouchDraft;
        const desktop = {
            logger: { error: vi.fn(), warn: vi.fn() },
            ipc: { broadcast: vi.fn() },
            lib: {
                db: {
                    touchProfileVisionCache: {
                        get: async () => null,
                        upsert: async () => {},
                        deleteAll: async () => {},
                    },
                },
            },
            service: {
                mod: {
                    fn: {
                        enable: async (source: string) => {
                            const target = path.join(
                                path.dirname(source),
                                path.basename(source).replace(/^DISABLED_/, ""),
                            );
                            await fse.move(source, target);
                            return target;
                        },
                    },
                },
            },
        } as unknown as NahidaDesktop;
        const service = new TouchProfileService(desktop);
        (service as unknown as { sessions: Map<string, unknown> }).sessions.set(draft.sessionId, {
            draft,
            analysis,
            sessionDir,
            meshCache: new Map([
                [
                    component.id,
                    {
                        positions,
                        normals: new Float32Array(positions.length),
                        indices: new Uint32Array([0, 1, 2]),
                        positionBytes,
                    },
                ],
            ]),
            previewCache: new Map(),
            applied: {
                outputModRoot: outputRoot,
                sourceModRoot: sourceRoot,
                reenableSourceOnRollback: true,
            },
            operation: null,
        });

        try {
            const first = await service.regenerate({ sessionId: draft.sessionId });
            const paramsPath = path.join(
                outputRoot,
                "Resources",
                "IM",
                "AliceModBodyJiggleParams.buf",
            );
            const before = await fse.readFile(paramsPath);
            await service.updateZoneSettings({
                sessionId: draft.sessionId,
                componentId: component.id,
                zoneId: "zone",
                settings: {
                    ...createDefaultTouchZoneSettings(),
                    physicsPreset: "custom",
                    advanced: {
                        ...createDefaultTouchZoneSettings().advanced,
                        strength: 2,
                    },
                },
            });
            const second = await service.regenerate({ sessionId: draft.sessionId });
            const after = await fse.readFile(paramsPath);

            assert.equal(first.outputModRoot, outputRoot);
            assert.equal(second.outputModRoot, outputRoot);
            assert.notDeepEqual(after, before);
            assert.equal(await fse.pathExists(path.join(outputRoot, "old.txt")), false);

            const patchedIni = await fse.readFile(path.join(outputRoot, "mod.ini"), "utf8");
            assert.ok(
                patchedIni.indexOf("run = CommandListbodyIb") <
                    patchedIni.indexOf("CustomShaderNhdTouchAlicemodBakebodyPosition"),
            );

            const rolledBack = await service.rollback({
                sessionId: draft.sessionId,
                outputModRoot: outputRoot,
                sourceModRoot: sourceRoot,
                reenableSourceOnRollback: true,
            });
            assert.equal(rolledBack.sourceModRoot, path.join(root, "AliceMod"));
            assert.equal(await fse.pathExists(outputRoot), false);
        } finally {
            await fse.remove(root);
        }
    });
});

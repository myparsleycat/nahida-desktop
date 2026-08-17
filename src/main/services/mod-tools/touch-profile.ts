import path from "node:path";
import { fileURLToPath } from "node:url";

import type { NahidaDesktop } from "@main/index";
// vision-llm disabled — LLM imports isolated
// import { LLM_API_KEY_SETTING_KEY, resolveLlmApiKey, type LlmConfig } from "@main/lib/llm";
import touchMaskWorker from "@main/worker/mod-tools/touch-mask.worker?modulePath";
import type { TouchMaskWorkerInput } from "@main/worker/mod-tools/touch-mask.worker";
import { DISABLED_PREFIX_REGEX, stripDisabledPrefix } from "@shared/mod";
import type { TouchProfileLlmSettings } from "@shared/touch-profile-llm";
// vision-llm disabled — protocol/reasoning/normalize only used by vision path
// import type {
//     TouchProfileLlmProtocol,
//     TouchProfileLlmReasoning,
// } from "@shared/touch-profile-llm";
// import { normalizeTouchProfileLlmSettings } from "@shared/touch-profile-llm";
import type {
    TouchProfileAnalyzeComponentsInput,
    TouchProfilePreview,
    TouchProfilePreviewInput,
} from "@shared/touch-profile-preview";
import type { TouchProfileMeshPreview } from "@shared/touch-profile-preview";
import type { TouchZoneSettings } from "@shared/touch-profile-settings";
import type { TouchProfileProgressEvent } from "@shared/types";
import { app } from "electron";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import pLimit from "p-limit";
import Piscina from "piscina";

import { analyzeTouchMod, hashTouchFiles, loadTouchMeshBuffers } from "./touch-profile-analyzer";
import {
    assetPrefixForComponent,
    buildVertexMasks,
    extractMaskChannel,
    writeTouchComponentAssets,
    type TouchGeneratedAssets,
} from "./touch-profile-assets";
import {
    analyzeComponentWithBones,
    DEFAULT_BONE_WEIGHT_THRESHOLD,
    DEFAULT_BONE_WEIGHT_THRESHOLD_MAX,
} from "./touch-profile-bone";
import { assertTouchProfileInputAllowed } from "./touch-profile-detection";
import { compileTouchIni, supportsTouchFrameNumberGuard } from "./touch-profile-ini";
import { normalizeTouchZoneSettings } from "./touch-profile-settings";
import {
    TOUCH_CONFIDENCE_AUTO_APPLY_AVG,
    TOUCH_CONFIDENCE_AUTO_APPLY_MIN,
    TOUCH_PROFILE_MANIFEST_FILE,
    TOUCH_PROFILE_MANIFEST_KIND,
    TOUCH_FOLDER_SUFFIX,
    TOUCH_PROMPT_VERSION,
    TOUCH_RUNTIME_VERSION,
    TOUCH_SHADER_FILES,
    TOUCH_VISION_CONCURRENCY,
    type TouchApplyResult,
    type TouchComponentAnalysis,
    type TouchComponentDraft,
    type TouchDraft,
    type TouchModAnalysis,
    type TouchModInspection,
    type TouchRollbackResult,
    type TouchZoneSpec,
} from "./touch-profile-types";
import { validateTouchOutput } from "./touch-profile-validator";
// vision-llm disabled — vision pipeline imports isolated
// import {
//     analyzeComponentVision,
//     reanalyzeComponentVisionTurn,
//     renderComponentPreviews,
//     type TouchVisionCacheContext,
// } from "./touch-profile-vision";
// import { TouchProfileVisionCache } from "./touch-profile-vision-cache";

export type TouchProfileLoadInput = {
    modPath: string;
};

export type TouchProfileApplyInput = {
    sessionId: string;
    force?: boolean;
};

export type TouchProfileRegenerateInput = {
    sessionId: string;
    force?: boolean;
};

export type TouchProfileUpdateZoneSettingsInput = {
    sessionId: string;
    componentId: string;
    zoneId: string;
    settings: TouchZoneSettings;
};

export type TouchProfileReanalyzeTurnInput = {
    sessionId: string;
    componentId: string;
};

export type TouchProfileSelectTurnInput = {
    sessionId: string;
    componentId: string;
    turn: number;
};

export type TouchProfileLlmApiKeyInput = {
    apiKey: string;
};

export type TouchProfileLlmSettingsView = TouchProfileLlmSettings & {
    apiKeyConfigured: boolean;
};

// vision-llm disabled — LLM setting keys isolated
// const TOUCH_PROFILE_LLM_SETTING_KEYS = [
//     "tools.touchProfileLlmProtocol",
//     "tools.touchProfileLlmEndpoint",
//     "tools.touchProfileLlmModel",
//     "tools.touchProfileLlmReasoning",
// ] as const;

export type TouchProfileRollbackInput = {
    sessionId: string;
    outputModRoot: string;
    sourceModRoot: string;
    reenableSourceOnRollback: boolean;
};

type AppliedTouchProfile = {
    outputModRoot: string;
    sourceModRoot: string;
    reenableSourceOnRollback: boolean;
};

type SessionState = {
    analysis: TouchModAnalysis;
    sessionDir: string;
    meshCache: Map<string, Awaited<ReturnType<typeof loadTouchMeshBuffers>>>;
    previewCache: Map<string, TouchProfilePreview>;
    draft?: TouchDraft;
    applied?: AppliedTouchProfile;
    operation: "apply" | "regenerate" | "rollback" | null;
};

export class TouchProfileService {
    private readonly sessions = new Map<string, SessionState>();
    // vision-llm disabled — vision cache isolated
    // private readonly visionCache: TouchProfileVisionCache;

    /**
     * Worker pool for CPU-heavy mask computation. Mask building (nearest-seed
     * search + adjacency smoothing) is the main cause of main-thread lockups on
     * large meshes (>100k vertices), so it is offloaded here. Structured clone is
     * used (no transferList) so the meshCache originals in the main process stay
     * usable for subsequent preview hits. Created lazily so the Piscina constructor
     * (which needs a resolved worker path) does not run in non-Vite contexts such
     * as unit tests, where `?modulePath` is not available.
     */
    private maskPool: Piscina | null = null;

    private getMaskPool() {
        if (!this.maskPool) {
            this.maskPool = new Piscina({
                filename: touchMaskWorker,
                minThreads: 0,
                maxThreads: 2,
                idleTimeout: 5000,
            });
        }
        return this.maskPool;
    }

    constructor(private readonly desktop: NahidaDesktop) {
        // vision-llm disabled
        // this.visionCache = new TouchProfileVisionCache(desktop.lib.db, desktop.logger);
    }

    // vision-llm disabled — LLM settings methods isolated
    // async getLlmSettings(): Promise<TouchProfileLlmSettingsView> {
    //     const settings = await this.desktop.setting.getMany(TOUCH_PROFILE_LLM_SETTING_KEYS);
    //     const resolved = normalizeTouchProfileLlmSettings({
    //         protocol: settings["tools.touchProfileLlmProtocol"] as TouchProfileLlmProtocol,
    //         endpoint: settings["tools.touchProfileLlmEndpoint"],
    //         model: settings["tools.touchProfileLlmModel"],
    //         reasoning: settings["tools.touchProfileLlmReasoning"] as TouchProfileLlmReasoning,
    //     });
    //     const apiKey = await this.getStoredLlmApiKey();
    //     return {
    //         ...resolved,
    //         apiKeyConfigured: Boolean(resolveLlmApiKey(resolved, apiKey)),
    //     };
    // }
    // async setLlmApiKey(input: TouchProfileLlmApiKeyInput): Promise<TouchProfileLlmSettingsView> {
    //     const apiKey = input.apiKey.trim();
    //     if (!apiKey) throw new Error("LLM API key cannot be empty");
    //     await this.desktop.lib.db.settings.upsert(
    //         LLM_API_KEY_SETTING_KEY,
    //         this.desktop.lib.crypto.encryptString(apiKey),
    //     );
    //     return await this.getLlmSettings();
    // }
    // async clearLlmApiKey(): Promise<TouchProfileLlmSettingsView> {
    //     await this.desktop.lib.db.settings.upsert(LLM_API_KEY_SETTING_KEY, null);
    //     return await this.getLlmSettings();
    // }

    // vision-llm disabled — loadMod used vision default, isolated
    // async loadMod(input: TouchProfileLoadInput | string): Promise<TouchDraft> {
    //     const modPath = typeof input === "string" ? input : input.modPath;
    //     const inspection = await this.prepareMod(modPath);
    //     return await this.analyzeComponents({
    //         sessionId: inspection.sessionId,
    //         componentIds: inspection.components.map((component) => component.id),
    //     });
    // }

    async prepareMod(input: TouchProfileLoadInput | string): Promise<TouchModInspection> {
        const modPath = typeof input === "string" ? input : input.modPath;
        const sessionId = nanoid(12);
        const sessionDir = path.join(app.getPath("userData"), "tools", "touch-profile", sessionId);

        try {
            await fse.ensureDir(sessionDir);
            this.broadcast({
                sessionId,
                stage: "scan",
                progress: 0.05,
                message: "Scanning mod structure",
            });

            const analysis = await analyzeTouchMod(modPath, (message) => {
                this.desktop.logger.warn(message, "TouchProfile");
            });

            this.sessions.set(sessionId, {
                analysis,
                sessionDir,
                meshCache: new Map(),
                previewCache: new Map(),
                operation: null,
            });

            return {
                sessionId,
                modRoot: analysis.modRoot,
                iniRelativePath: analysis.iniRelativePath,
                sourceFilesRelativePaths: analysis.sourceFilesRelativePaths,
                supportGrade: analysis.supportGrade,
                supportReasons: analysis.supportReasons,
                components: analysis.components.map((component) => ({
                    id: component.id,
                    name: component.name,
                    kind: component.kind,
                    supportGrade: component.supportGrade,
                    interactiveCandidate: component.interactiveCandidate,
                    vertexCount: component.vertexCount,
                    indexCount: component.indexCount,
                    variantKey: component.variantKey,
                    variantCondition: component.variantCondition,
                    objectMaps: component.objectMaps,
                    hasBlend: !!component.blendPath,
                    bones: component.bones,
                })),
            };
        } catch (error) {
            await fse.remove(sessionDir).catch(() => {});
            this.desktop.logger.error(error, `TouchProfile:prepareMod:${modPath}`);
            throw error;
        }
    }

    async getMeshPreview(input: TouchProfilePreviewInput): Promise<TouchProfileMeshPreview> {
        try {
            const session = this.requireSession(input.sessionId);
            const cached = session.meshCache.get(input.componentId);
            if (cached) {
                return {
                    sessionId: input.sessionId,
                    componentId: input.componentId,
                    vertexCount: cached.positions.length / 3,
                    positions: cached.positions,
                    indices: cached.indices,
                    bones: cached.bones,
                    blendStride: cached.blendStride,
                    blendBytes: cached.blendBytes,
                };
            }

            const component = session.analysis.components.find(
                (entry) => entry.id === input.componentId,
            );
            if (!component) {
                throw new Error(`Touch component not found: ${input.componentId}`);
            }

            const mesh = await loadTouchMeshBuffers(component);
            session.meshCache.set(component.id, mesh);
            return {
                sessionId: input.sessionId,
                componentId: component.id,
                vertexCount: mesh.positions.length / 3,
                positions: mesh.positions,
                indices: mesh.indices,
                bones: mesh.bones,
                blendStride: mesh.blendStride,
                blendBytes: mesh.blendBytes,
            };
        } catch (error) {
            this.desktop.logger.error(
                error,
                `TouchProfile:getMeshPreview:${input.sessionId}:${input.componentId}`,
            );
            throw error;
        }
    }

    async analyzeComponents(input: TouchProfileAnalyzeComponentsInput): Promise<TouchDraft> {
        const session = this.requireSession(input.sessionId);
        if (!session.analysis) {
            throw new Error(`Touch profile session has no analysis: ${input.sessionId}`);
        }
        const analysis = session.analysis;
        const sessionDir = session.sessionDir;
        const selectedIds = new Set(input.componentIds);
        const selectedComponents = analysis.components.filter((component) =>
            selectedIds.has(component.id),
        );
        const unselectedComponents = analysis.components.filter(
            (component) => !selectedIds.has(component.id),
        );
        const mode = input.mode ?? "bone";

        try {
            const meshCache = new Map<string, Awaited<ReturnType<typeof loadTouchMeshBuffers>>>();
            const totalComponents = Math.max(selectedComponents.length, 1);
            const limit = pLimit(TOUCH_VISION_CONCURRENCY);
            let lastProgress = 0.05;
            const broadcastProgress = (event: TouchProfileProgressEvent) => {
                lastProgress = Math.max(lastProgress, event.progress);
                this.broadcast({ ...event, progress: lastProgress });
            };

            const boneSelectionsByComponent = new Map(
                (input.boneSelections ?? []).map((entry) => [entry.componentId, entry.zones]),
            );
            const weightThreshold = input.weightThreshold ?? [
                DEFAULT_BONE_WEIGHT_THRESHOLD,
                DEFAULT_BONE_WEIGHT_THRESHOLD_MAX,
            ];

            const results = await Promise.allSettled(
                selectedComponents.map((component, index) =>
                    limit(async () => {
                        broadcastProgress({
                            sessionId: input.sessionId,
                            stage: "preview",
                            progress: 0.1 + (index / totalComponents) * 0.25,
                            message:
                                mode === "bone"
                                    ? `Loading mesh for ${component.name}`
                                    : `Preparing previews for ${component.name}`,
                            componentId: component.id,
                        });

                        const mesh = await loadTouchMeshBuffers(component);
                        meshCache.set(component.id, mesh);

                        if (mode === "bone") {
                            broadcastProgress({
                                sessionId: input.sessionId,
                                stage: "vision",
                                progress: 0.35 + (index / totalComponents) * 0.4,
                                message: `Analyzing bone zones for ${component.name}`,
                                componentId: component.id,
                            });
                            const selections = boneSelectionsByComponent.get(component.id) ?? [];
                            return analyzeComponentWithBones({
                                component,
                                positions: mesh.positions,
                                indices: mesh.indices,
                                blendBytes: mesh.blendBytes ?? new Uint8Array(),
                                blendStride: mesh.blendStride ?? 0,
                                bones: mesh.bones,
                                selections,
                                weightThreshold,
                                objectId: 1,
                            });
                        }

                        // vision-llm disabled — vision pipeline isolated
                        // const { previews, transforms } = await renderComponentPreviews({
                        //     sessionDir,
                        //     component,
                        //     positions: mesh.positions,
                        //     indices: mesh.indices,
                        // });
                        // broadcastProgress({
                        //     sessionId: input.sessionId,
                        //     stage: "vision",
                        //     progress: 0.35 + (index / totalComponents) * 0.4,
                        //     message: `Analyzing touch zones for ${component.name}`,
                        //     componentId: component.id,
                        // });
                        // const llm = await this.getResolvedLlmConfig();
                        // return analyzeComponentVision({
                        //     component,
                        //     positions: mesh.positions,
                        //     indices: mesh.indices,
                        //     previews,
                        //     transforms,
                        //     objectId: 1,
                        //     sessionDir,
                        //     llm,
                        //     visionCache: this.visionCache,
                        //     visionCacheContext: {
                        //         meshHash: analysis.meshHash,
                        //         iniHash: analysis.iniHash,
                        //     } satisfies TouchVisionCacheContext,
                        // });

                        // Non-bone mode fallback: empty non-interactive draft
                        return {
                            componentId: component.id,
                            interactive: false,
                            objectId: 1,
                            zones: [],
                            confidence: 0,
                            warnings: ["Vision LLM mode is disabled"],
                        };
                    }),
                ),
            );
            const failed = results.find((result) => result.status === "rejected");
            if (failed?.status === "rejected") throw failed.reason;

            let objectId = 1;
            const analyzedDrafts = results.map((result) => {
                if (result.status === "rejected") throw result.reason;
                const draft = { ...result.value, objectId };
                if (draft.interactive) objectId += 1;
                return draft;
            });
            const unselectedDrafts: TouchComponentDraft[] = unselectedComponents.map(
                (component) => ({
                    componentId: component.id,
                    interactive: false,
                    objectId,
                    zones: [],
                    confidence: 0,
                    warnings: ["Component was not selected for touch analysis"],
                }),
            );

            const components = [...analyzedDrafts, ...unselectedDrafts];
            const interactive = components.filter((draft) => draft.interactive);
            const warnings = [
                ...analysis.supportReasons.filter(
                    (reason) => !/position stride 40 with pn-t layout/i.test(reason),
                ),
                ...interactive.flatMap((entry) => entry.warnings),
            ];
            const confidences = interactive.map((entry) => entry.confidence);
            const minConfidence = confidences.length > 0 ? Math.min(...confidences) : 0;
            const avgConfidence =
                confidences.length > 0
                    ? confidences.reduce((sum, value) => sum + value, 0) / confidences.length
                    : 0;
            // vision-llm disabled — visionUsed/llmConfig simplified
            // const visionUsed =
            //     mode === "vision" && analyzedDrafts.some((entry) => entry.vision !== undefined);
            // const llmConfig = mode === "vision" ? await this.getResolvedLlmConfig() : null;
            const visionUsed = false;

            const draft: TouchDraft = {
                sessionId: input.sessionId,
                createdAt: new Date().toISOString(),
                sourceModRoot: analysis.sourceRoot,
                analysis,
                components,
                visionUsed,
                modelName: mode === "bone" ? "bone-weight" : "",
                // vision-llm disabled — llmConfig always null, ternary simplified to false branch
                // llm: llmConfig
                //     ? {
                //           protocol: llmConfig.protocol,
                //           endpoint: llmConfig.endpoint,
                //           model: llmConfig.model,
                //           reasoning: llmConfig.reasoning,
                //       }
                //     : {
                llm: {
                    protocol: "openai-compatible",
                    endpoint: "",
                    model: "",
                    reasoning: "auto",
                },
                promptVersion: TOUCH_PROMPT_VERSION,
                runtimeVersion: TOUCH_RUNTIME_VERSION,
                canAutoApply: isTouchDraftAutoApplyable(interactive, minConfidence, avgConfidence),
                warnings: [...new Set(warnings)],
            };

            await fse.writeJson(path.join(sessionDir, "draft.json"), draft, { spaces: 2 });
            session.draft = draft;
            session.meshCache = meshCache;

            this.broadcast({
                sessionId: input.sessionId,
                stage: "complete",
                progress: 1,
                message: draft.canAutoApply
                    ? "Draft ready for apply"
                    : "Draft ready (manual review recommended)",
            });

            return draft;
        } catch (error) {
            await fse.remove(sessionDir).catch(() => {});
            this.sessions.delete(input.sessionId);
            this.desktop.logger.error(error, `TouchProfile:analyzeComponents:${input.sessionId}`);
            throw error;
        }
    }

    async saveDraft(draft: TouchDraft): Promise<TouchDraft> {
        const session = this.requireSession(draft.sessionId);
        const interactive = draft.components.filter((component) => component.interactive);
        const confidences = interactive.map((component) => component.confidence);
        const minConfidence = confidences.length > 0 ? Math.min(...confidences) : 0;
        const avgConfidence =
            confidences.length > 0
                ? confidences.reduce((sum, value) => sum + value, 0) / confidences.length
                : 0;
        const next = {
            ...draft,
            canAutoApply: isTouchDraftAutoApplyable(interactive, minConfidence, avgConfidence),
        };
        session.draft = next;
        session.previewCache.clear();
        await fse.writeJson(path.join(session.sessionDir, "draft.json"), next, { spaces: 2 });
        return next;
    }

    // vision-llm disabled — clearVisionCache isolated
    // async clearVisionCache(): Promise<{ ok: true }> {
    //     try {
    //         await this.visionCache.clear();
    //         return { ok: true };
    //     } catch (error) {
    //         this.desktop.logger.error(error, "TouchProfile:clearVisionCache");
    //         throw error;
    //     }
    // }

    async updateZoneSettings(input: TouchProfileUpdateZoneSettingsInput): Promise<TouchDraft> {
        try {
            const session = this.requireSession(input.sessionId);
            if (session.operation) {
                throw new Error(`Touch profile is busy with ${session.operation}`);
            }
            const settings = normalizeTouchZoneSettings(input.settings);
            if (!session.draft) {
                throw new Error(`Touch profile has no draft: ${input.sessionId}`);
            }
            const component = session.draft.components.find(
                (entry) => entry.componentId === input.componentId,
            );
            if (!component) {
                throw new Error(`Touch component draft not found: ${input.componentId}`);
            }
            if (!component.zones.some((zone) => zone.id === input.zoneId)) {
                throw new Error(`Touch zone not found: ${input.componentId}:${input.zoneId}`);
            }

            const next: TouchDraft = {
                ...session.draft,
                components: session.draft.components.map((entry) =>
                    entry.componentId !== input.componentId
                        ? entry
                        : {
                              ...entry,
                              zones: entry.zones.map((zone) =>
                                  zone.id === input.zoneId ? { ...zone, settings } : zone,
                              ),
                          },
                ),
            };
            session.draft = next;
            session.previewCache.clear();
            await fse.writeJson(path.join(session.sessionDir, "draft.json"), next, { spaces: 2 });
            return next;
        } catch (error) {
            this.desktop.logger.error(
                error,
                `TouchProfile:updateZoneSettings:${input.sessionId}:${input.componentId}:${input.zoneId}`,
            );
            throw error;
        }
    }

    // vision-llm disabled — reanalyzeTurn (vision-only) isolated
    // async reanalyzeTurn(input: TouchProfileReanalyzeTurnInput): Promise<TouchDraft> {
    //     try {
    //         const session = this.requireSession(input.sessionId);
    //         if (session.operation) {
    //             throw new Error(`Touch profile is busy with ${session.operation}`);
    //         }
    //         const componentAnalysis = session.analysis.components.find(
    //             (entry) => entry.id === input.componentId,
    //         );
    //         if (!componentAnalysis) {
    //             throw new Error(`Touch component not found: ${input.componentId}`);
    //         }
    //         if (!session.draft) {
    //             throw new Error(`Touch profile has no draft: ${input.sessionId}`);
    //         }
    //         const componentDraft = session.draft.components.find(
    //             (entry) => entry.componentId === input.componentId,
    //         );
    //         if (!componentDraft) {
    //             throw new Error(`Touch component draft not found: ${input.componentId}`);
    //         }
    //         const currentTurn = componentDraft.currentTurn ?? 1;
    //         if (!componentDraft.vision) {
    //             throw new Error(`No previous vision result available for ${input.componentId}`);
    //         }
    //         const mesh =
    //             session.meshCache.get(componentAnalysis.id) ??
    //             (await loadTouchMeshBuffers(componentAnalysis));
    //         session.meshCache.set(componentAnalysis.id, mesh);
    //         const { previews, transforms } = await renderComponentPreviews({
    //             sessionDir: session.sessionDir,
    //             component: componentAnalysis,
    //             positions: mesh.positions,
    //             indices: mesh.indices,
    //         });
    //         const llm = await this.getResolvedLlmConfig();
    //         const history = componentDraft.turnHistory ? [...componentDraft.turnHistory] : [];
    //         const maxTurn = history.reduce((max, entry) => Math.max(max, entry.turn), 0);
    //         const effectiveTurn = Math.max(currentTurn + 1, maxTurn + 1);
    //         this.broadcast({
    //             sessionId: input.sessionId,
    //             stage: "vision",
    //             progress: 0.5,
    //             message: `Re-analyzing turn ${effectiveTurn} for ${componentAnalysis.name}`,
    //             componentId: componentAnalysis.id,
    //         });
    //         const baselineResult =
    //             history.find((entry) => entry.turn === 1)?.vision ?? componentDraft.vision;
    //         const turnResult = await reanalyzeComponentVisionTurn({
    //             component: componentAnalysis,
    //             positions: mesh.positions,
    //             indices: mesh.indices,
    //             previews,
    //             transforms,
    //             objectId: componentDraft.objectId,
    //             turn: effectiveTurn,
    //             previousResult: componentDraft.vision,
    //             baselineResult,
    //             llm,
    //             sessionDir: session.sessionDir,
    //         });
    //         history.push(turnResult.turnRecord);
    //         const updatedComponent: TouchComponentDraft = {
    //             ...componentDraft,
    //             currentTurn: effectiveTurn,
    //             zones: turnResult.zones,
    //             confidence: turnResult.confidence,
    //             warnings: turnResult.warnings,
    //             vision: turnResult.vision,
    //             visionApproved: turnResult.vision.approved,
    //             turnHistory: history,
    //         };
    //         const updatedComponents = session.draft.components.map((entry) =>
    //             entry.componentId === input.componentId ? updatedComponent : entry,
    //         );
    //         const draft = await this.saveDraft({
    //             ...session.draft,
    //             components: updatedComponents,
    //         });
    //         this.broadcast({
    //             sessionId: input.sessionId,
    //             stage: "complete",
    //             progress: 1,
    //             message: `Turn ${effectiveTurn} analysis complete`,
    //             componentId: componentAnalysis.id,
    //         });
    //         return draft;
    //     } catch (error) {
    //         this.desktop.logger.error(
    //             error,
    //             `TouchProfile:reanalyzeTurn:${input.sessionId}:${input.componentId}`,
    //         );
    //         throw error;
    //     }
    // }

    // vision-llm disabled — selectTurn (vision turn history) isolated
    // async selectTurn(input: TouchProfileSelectTurnInput): Promise<TouchDraft> {
    //     try {
    //         const session = this.requireSession(input.sessionId);
    //         if (session.operation) {
    //             throw new Error(`Touch profile is busy with ${session.operation}`);
    //         }
    //         if (!session.draft) {
    //             throw new Error(`Touch profile has no draft: ${input.sessionId}`);
    //         }
    //         const componentDraft = session.draft.components.find(
    //             (entry) => entry.componentId === input.componentId,
    //         );
    //         if (!componentDraft) {
    //             throw new Error(`Touch component draft not found: ${input.componentId}`);
    //         }
    //         const record = componentDraft.turnHistory?.find((entry) => entry.turn === input.turn);
    //         if (!record) {
    //             throw new Error(`Turn ${input.turn} not found in history for ${input.componentId}`);
    //         }
    //         const updatedComponent: TouchComponentDraft = {
    //             ...componentDraft,
    //             currentTurn: input.turn,
    //             zones: record.zones,
    //             confidence: record.confidence,
    //             warnings: record.warnings,
    //             vision: record.vision,
    //             visionApproved: record.approved,
    //         };
    //         const updatedComponents = session.draft.components.map((entry) =>
    //             entry.componentId === input.componentId ? updatedComponent : entry,
    //         );
    //         return await this.saveDraft({
    //             ...session.draft,
    //             components: updatedComponents,
    //         });
    //     } catch (error) {
    //         this.desktop.logger.error(
    //             error,
    //             `TouchProfile:selectTurn:${input.sessionId}:${input.componentId}:${input.turn}`,
    //         );
    //         throw error;
    //     }
    // }

    async getPreview(input: TouchProfilePreviewInput): Promise<TouchProfilePreview> {
        try {
            const session = this.requireSession(input.sessionId);
            const cached = session.previewCache.get(input.componentId);
            if (cached) return cached;

            if (!session.draft) {
                throw new Error(`Touch profile has no draft: ${input.sessionId}`);
            }
            const component = session.analysis.components.find(
                (entry) => entry.id === input.componentId,
            );
            if (!component) {
                throw new Error(`Touch component not found: ${input.componentId}`);
            }

            const componentDraft = session.draft.components.find(
                (entry) => entry.componentId === input.componentId,
            );
            if (!componentDraft) {
                throw new Error(`Touch component draft not found: ${input.componentId}`);
            }

            const mesh =
                session.meshCache.get(component.id) ?? (await loadTouchMeshBuffers(component));
            session.meshCache.set(component.id, mesh);

            this.broadcast({
                sessionId: input.sessionId,
                stage: "preview",
                progress: 0.4,
                message: `Building mask for ${component.name}`,
                componentId: component.id,
            });

            const masks = await this.computeMasks(
                component.vertexCount,
                mesh.positions,
                mesh.indices,
                component,
                componentDraft.zones,
            );
            const preview: TouchProfilePreview = {
                sessionId: input.sessionId,
                componentId: component.id,
                vertexCount: component.vertexCount,
                positions: mesh.positions,
                indices: mesh.indices,
                zones: componentDraft.zones.map((zone) => ({
                    ...zone,
                    weights: extractMaskChannel(masks, component.vertexCount, zone.channel),
                })),
            };

            session.previewCache.set(component.id, preview);
            return preview;
        } catch (error) {
            this.desktop.logger.error(
                error,
                `TouchProfile:getPreview:${input.sessionId}:${input.componentId}`,
            );
            throw error;
        }
    }

    /**
     * Compute vertex masks on a worker thread to avoid blocking the main process
     * event loop on large meshes. Falls back to synchronous buildVertexMasks if the
     * worker pool rejects (e.g. transient worker failure), so correctness is preserved.
     */
    private async computeMasks(
        vertexCount: number,
        positions: Float32Array,
        indices: Uint32Array,
        component: TouchComponentAnalysis,
        zones: TouchZoneSpec[],
    ): Promise<Float32Array> {
        const workerInput: TouchMaskWorkerInput = {
            vertexCount,
            positions,
            indices,
            component,
            zones,
        };
        try {
            const result = await this.getMaskPool().run(workerInput);
            return result.masks;
        } catch (error) {
            this.desktop.logger.warn(
                error,
                `TouchProfile:computeMasks:worker-fallback:${component.id}`,
            );
            return buildVertexMasks(vertexCount, positions, indices, component, zones);
        }
    }

    async discardDraft(sessionId: string): Promise<{ ok: true }> {
        const session = this.sessions.get(sessionId);
        if (session) {
            this.sessions.delete(sessionId);
            await fse.remove(session.sessionDir).catch(() => {});
        }
        return { ok: true };
    }

    async apply(input: TouchProfileApplyInput): Promise<TouchApplyResult> {
        const session = this.requireSession(input.sessionId);
        const draft = session.draft;
        if (!draft) {
            throw new Error(`Touch profile has no draft to apply: ${input.sessionId}`);
        }

        assertTouchDraftCanApply(draft, input.force);
        if (session.applied) {
            throw new Error("Touch profile is already applied. Use regenerate instead.");
        }

        const sourceRoot = path.resolve(draft.sourceModRoot);
        try {
            await assertTouchProfileInputAllowed(sourceRoot);
        } catch (error) {
            this.desktop.logger.error(error, `TouchProfile:apply:input:${sourceRoot}`);
            throw error;
        }
        const parentPath = path.dirname(sourceRoot);
        const existingFolderNames = await this.desktop.lib.fs.listDirectories(parentPath);
        const targetFolderName = this.desktop.lib.fs.getUniqueName(
            touchFolderBaseName(path.basename(sourceRoot)),
            existingFolderNames,
        );
        const targetRoot = path.join(parentPath, targetFolderName);
        this.claimOperation(session, "apply");

        try {
            const validation = await this.generateOutput({
                session,
                sourceRoot,
                targetRoot,
                operation: "apply",
            });

            // Keep the touch clone enabled; disable the source so both are not loaded together.
            const reenableSourceOnRollback = !DISABLED_PREFIX_REGEX.test(path.basename(sourceRoot));
            const disabledSourceRoot = await this.desktop.service.mod.fn.disable(sourceRoot);
            session.applied = {
                outputModRoot: targetRoot,
                sourceModRoot: disabledSourceRoot,
                reenableSourceOnRollback,
            };

            this.broadcast({
                sessionId: draft.sessionId,
                stage: "complete",
                progress: 1,
                message: "Touch mod created",
            });

            return {
                sessionId: draft.sessionId,
                outputModRoot: targetRoot,
                sourceModRoot: disabledSourceRoot,
                reenableSourceOnRollback,
                disabled: false,
                validation,
                warnings: draft.warnings,
            };
        } catch (error) {
            if (await fse.pathExists(targetRoot)) {
                await fse.remove(targetRoot).catch((cleanupError) => {
                    this.desktop.logger.error(
                        cleanupError,
                        `TouchProfile:apply:cleanup:${targetRoot}`,
                    );
                });
            }
            this.desktop.logger.error(error, `TouchProfile:apply:${sourceRoot}`);
            throw error;
        } finally {
            session.operation = null;
        }
    }

    async regenerate(input: TouchProfileRegenerateInput): Promise<TouchApplyResult> {
        const session = this.requireSession(input.sessionId);
        const draft = session.draft;
        const applied = session.applied;
        if (!draft) {
            throw new Error(`Touch profile has no draft to regenerate: ${input.sessionId}`);
        }

        assertTouchDraftCanApply(draft, input.force);
        if (!applied) {
            throw new Error("Touch profile has not been applied yet.");
        }
        this.claimOperation(session, "regenerate");

        const sourceRoot = path.resolve(applied.sourceModRoot);
        const outputRoot = path.resolve(applied.outputModRoot);
        const stagingRoot = path.join(
            path.dirname(outputRoot),
            `.${path.basename(outputRoot)}.regenerating-${nanoid(8)}`,
        );

        try {
            if (!(await fse.pathExists(sourceRoot))) {
                throw new Error(`Touch source mod not found: ${sourceRoot}`);
            }
            if (!(await fse.pathExists(outputRoot))) {
                throw new Error(`Touch output mod not found: ${outputRoot}`);
            }

            await assertTouchProfileInputAllowed(sourceRoot);
            await assertTouchSourceUnchanged(draft.analysis, sourceRoot);
            const validation = await this.generateOutput({
                session,
                sourceRoot,
                targetRoot: stagingRoot,
                operation: "regenerate",
            });

            await replaceTouchOutput(stagingRoot, outputRoot);

            this.broadcast({
                sessionId: draft.sessionId,
                stage: "complete",
                progress: 1,
                message: "Touch mod regenerated",
            });

            return {
                sessionId: draft.sessionId,
                outputModRoot: outputRoot,
                sourceModRoot: sourceRoot,
                reenableSourceOnRollback: applied.reenableSourceOnRollback,
                disabled: false,
                validation,
                warnings: draft.warnings,
            };
        } catch (error) {
            if (await fse.pathExists(stagingRoot)) {
                await fse.remove(stagingRoot).catch((cleanupError) => {
                    this.desktop.logger.error(
                        cleanupError,
                        `TouchProfile:regenerate:cleanup:${stagingRoot}`,
                    );
                });
            }
            this.desktop.logger.error(error, `TouchProfile:regenerate:${sourceRoot}:${outputRoot}`);
            throw error;
        } finally {
            session.operation = null;
        }
    }

    private async generateOutput(input: {
        session: SessionState;
        sourceRoot: string;
        targetRoot: string;
        operation: "apply" | "regenerate";
    }) {
        const draft = input.session.draft;
        if (!draft) {
            throw new Error(`Touch profile has no draft for output generation`);
        }
        const namespaceToken = sanitizeNamespace(input.sourceRoot);
        const varPrefix = `nhd_touch_${namespaceToken.toLowerCase()}`;
        const targetAnalysis = rebaseTouchAnalysis(
            draft.analysis,
            input.sourceRoot,
            input.targetRoot,
        );
        const interactiveComponents = targetAnalysis.components.filter((component) => {
            const componentDraft = draft.components.find(
                (entry) => entry.componentId === component.id,
            );
            return !!componentDraft?.interactive && componentDraft.zones.length > 0;
        });

        if (interactiveComponents.length === 0) {
            throw new Error("No interactive components selected for touch conversion");
        }

        this.broadcast({
            sessionId: draft.sessionId,
            stage: "assets",
            progress: 0.2,
            message:
                input.operation === "regenerate"
                    ? "Preparing regenerated touch output"
                    : "Copying mod to touch output folder",
        });

        await fse.copy(input.sourceRoot, input.targetRoot);

        const assets: TouchGeneratedAssets[] = [];
        for (const [index, component] of interactiveComponents.entries()) {
            const componentDraft = draft.components.find(
                (entry) => entry.componentId === component.id,
            );
            if (!componentDraft) continue;

            this.broadcast({
                sessionId: draft.sessionId,
                stage: "assets",
                progress: 0.3 + (index / interactiveComponents.length) * 0.3,
                message: `Generating touch assets for ${component.name}`,
                componentId: component.id,
            });

            const mesh = input.session.meshCache.get(component.id);
            if (!mesh && input.operation === "regenerate") {
                throw new Error(
                    `Touch mesh cache is missing for ${component.id}; analyze the mod again before regenerating`,
                );
            }
            const resolvedMesh = mesh ?? (await loadTouchMeshBuffers(component));
            assets.push(
                await writeTouchComponentAssets({
                    outputRoot: input.targetRoot,
                    component,
                    draft: componentDraft,
                    positions: resolvedMesh.positions,
                    indices: resolvedMesh.indices,
                    assetPrefix: assetPrefixForComponent(component, namespaceToken),
                }),
            );
        }

        await copyRuntimeShaders(input.targetRoot);

        this.broadcast({
            sessionId: draft.sessionId,
            stage: "ini",
            progress: 0.75,
            message:
                input.operation === "regenerate"
                    ? "Patching regenerated touch INI"
                    : "Patching touch INI",
        });

        const sourceIniPath = resolveRelativePath(
            path.join(input.sourceRoot, draft.analysis.modRootRelativeToSource),
            draft.analysis.iniRelativePath,
        );
        const targetIniPath = rebasePath(sourceIniPath, input.sourceRoot, input.targetRoot);
        await compileTouchIni({
            sourceIniPath,
            targetIniPath,
            analysis: {
                ...targetAnalysis,
                components: interactiveComponents,
            },
            drafts: draft.components,
            assets,
            namespaceToken,
            varPrefix,
            useFrameNumberGuard: supportsTouchFrameNumberGuard(
                this.desktop.service.xxmi.getXXMIConfig()?.Packages.packages.XXMI?.deployed_version,
            ),
        });

        this.broadcast({
            sessionId: draft.sessionId,
            stage: "validate",
            progress: 0.9,
            message:
                input.operation === "regenerate"
                    ? "Validating regenerated touch mod"
                    : "Validating generated touch mod",
        });

        const validation = await validateTouchOutput({
            outputRoot: input.targetRoot,
            iniPath: targetIniPath,
            components: interactiveComponents,
            drafts: draft.components,
            assets,
        });

        if (!validation.ok) {
            throw new Error(
                `Touch validation failed: ${validation.issues
                    .filter((issue) => issue.level === "error")
                    .map((issue) => issue.message)
                    .join("; ")}`,
            );
        }

        await writeTouchProfileManifest(input.targetRoot, draft.runtimeVersion);
        return validation;
    }

    async rollback(input: TouchProfileRollbackInput): Promise<TouchRollbackResult> {
        const session = this.requireSession(input.sessionId);
        const applied = session.applied;
        const outputRoot = path.resolve(input.outputModRoot);
        const sourceRoot = path.resolve(input.sourceModRoot);

        if (!applied) {
            throw new Error("Touch profile has already been rolled back");
        }
        if (
            applied.outputModRoot !== outputRoot ||
            applied.sourceModRoot !== sourceRoot ||
            applied.reenableSourceOnRollback !== input.reenableSourceOnRollback
        ) {
            throw new Error("Touch rollback paths do not match the active touch profile session");
        }
        if (!outputRoot || !sourceRoot) {
            throw new Error("Touch rollback requires output and source mod paths");
        }
        if (outputRoot === sourceRoot) {
            throw new Error("Touch rollback refused: output and source paths are identical");
        }
        if (!(await fse.pathExists(outputRoot))) {
            throw new Error(`Touch output mod not found: ${outputRoot}`);
        }
        if (!(await fse.pathExists(sourceRoot))) {
            throw new Error(`Touch source mod not found: ${sourceRoot}`);
        }

        this.claimOperation(session, "rollback");
        try {
            await fse.remove(outputRoot);

            let restoredSourceRoot = sourceRoot;
            let reenabledSource = false;
            if (input.reenableSourceOnRollback) {
                restoredSourceRoot = await this.desktop.service.mod.fn.enable(sourceRoot);
                reenabledSource = true;
            }

            if (session.draft) {
                session.draft = { ...session.draft, sourceModRoot: restoredSourceRoot };
                await fse.writeJson(path.join(session.sessionDir, "draft.json"), session.draft, {
                    spaces: 2,
                });
            }
            session.applied = undefined;

            return {
                outputModRoot: outputRoot,
                sourceModRoot: restoredSourceRoot,
                removedOutput: true,
                reenabledSource,
            };
        } catch (error) {
            this.desktop.logger.error(error, `TouchProfile:rollback:${outputRoot}:${sourceRoot}`);
            throw error;
        } finally {
            session.operation = null;
        }
    }

    private requireSession(sessionId: string) {
        const session = this.sessions.get(sessionId);
        if (!session) throw new Error(`Touch profile session not found: ${sessionId}`);
        if (!session.analysis) throw new Error(`Touch profile session not analyzed: ${sessionId}`);
        return session;
    }

    // vision-llm disabled — LLM config helpers isolated
    // private async getResolvedLlmConfig(): Promise<LlmConfig> {
    //     const settings = await this.desktop.setting.getMany(TOUCH_PROFILE_LLM_SETTING_KEYS);
    //     const resolved = normalizeTouchProfileLlmSettings({
    //         protocol: settings["tools.touchProfileLlmProtocol"] as TouchProfileLlmProtocol,
    //         endpoint: settings["tools.touchProfileLlmEndpoint"],
    //         model: settings["tools.touchProfileLlmModel"],
    //         reasoning: settings["tools.touchProfileLlmReasoning"] as TouchProfileLlmReasoning,
    //     });
    //     return {
    //         ...resolved,
    //         apiKey: resolveLlmApiKey(resolved, await this.getStoredLlmApiKey()),
    //     };
    // }
    // private async getStoredLlmApiKey() {
    //     const encrypted = await this.desktop.lib.db.settings.getValue(LLM_API_KEY_SETTING_KEY);
    //     if (!encrypted) return undefined;
    //     try {
    //         return this.desktop.lib.crypto.decryptString(encrypted);
    //     } catch (error) {
    //         this.desktop.logger.error(error, "TouchProfile:getLlmApiKey");
    //         return undefined;
    //     }
    // }

    private claimOperation(
        session: SessionState,
        operation: NonNullable<SessionState["operation"]>,
    ) {
        if (session.operation) {
            throw new Error(`Touch profile is busy with ${session.operation}`);
        }
        session.operation = operation;
    }

    private broadcast(event: TouchProfileProgressEvent) {
        this.desktop.ipc.broadcast("tools:touchProfileProgress", event);
    }
}

export function touchFolderBaseName(sourceFolderName: string) {
    return `${stripDisabledPrefix(sourceFolderName)}${TOUCH_FOLDER_SUFFIX}`;
}

function remapPath(filePath: string, sourceRoot: string, targetRoot: string) {
    const absolute = path.resolve(filePath);
    const relative = path.relative(sourceRoot, absolute);
    if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
        throw new Error(`Path is outside mod root: ${filePath}`);
    }
    return path.join(targetRoot, relative);
}

function resolveRelativePath(root: string, relativePath: string) {
    const absolute = path.resolve(root, relativePath);
    const relative = path.relative(root, absolute);
    if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
        throw new Error(`Path is outside mod root: ${relativePath}`);
    }
    return absolute;
}

function rebasePath(filePath: string, sourceRoot: string, targetRoot: string) {
    return remapPath(filePath, sourceRoot, targetRoot);
}

function rebaseTouchAnalysis(
    analysis: TouchDraft["analysis"],
    sourceRoot: string,
    targetRoot: string,
) {
    const analysisRoot = path.join(sourceRoot, analysis.modRootRelativeToSource);
    return {
        ...analysis,
        modRoot: targetRoot,
        iniPath: rebasePath(
            resolveRelativePath(analysisRoot, analysis.iniRelativePath),
            sourceRoot,
            targetRoot,
        ),
        components: analysis.components.map((component) => ({
            ...component,
            positionPath: rebasePath(
                resolveRelativePath(analysisRoot, component.positionRelativePath),
                sourceRoot,
                targetRoot,
            ),
            indexPath: component.indexRelativePath
                ? rebasePath(
                      resolveRelativePath(analysisRoot, component.indexRelativePath),
                      sourceRoot,
                      targetRoot,
                  )
                : undefined,
        })),
    };
}

function assertTouchDraftCanApply(draft: TouchDraft, force = false) {
    if (!force && !draft.canAutoApply) {
        throw new Error(
            "Touch draft confidence is too low for automatic apply. Review zones or pass force=true.",
        );
    }
}

async function assertTouchSourceUnchanged(analysis: TouchDraft["analysis"], sourceRoot: string) {
    const analysisRoot = path.join(sourceRoot, analysis.modRootRelativeToSource);
    const meshPaths = analysis.components.flatMap((component) => [
        resolveRelativePath(analysisRoot, component.positionRelativePath),
        ...(component.indexRelativePath
            ? [resolveRelativePath(analysisRoot, component.indexRelativePath)]
            : []),
    ]);
    const sourcePaths = (
        analysis.sourceFilesRelativePaths?.length
            ? analysis.sourceFilesRelativePaths
            : [analysis.iniRelativePath]
    ).map((relativePath) => resolveRelativePath(analysisRoot, relativePath));
    const [meshHash, iniHash] = await Promise.all([
        hashTouchFiles(meshPaths, analysisRoot),
        hashTouchFiles(sourcePaths, analysisRoot),
    ]);

    if (meshHash !== analysis.meshHash || iniHash !== analysis.iniHash) {
        throw new Error("Touch source mod changed since analysis; analyze the mod again");
    }
}

export async function replaceTouchOutput(stagingRoot: string, outputRoot: string) {
    const backupRoot = `${outputRoot}.backup-${nanoid(8)}`;
    let oldOutputMoved = false;
    let newOutputMoved = false;

    try {
        await fse.move(outputRoot, backupRoot);
        oldOutputMoved = true;
        await fse.move(stagingRoot, outputRoot);
        newOutputMoved = true;
        await fse.remove(backupRoot);
    } catch (error) {
        let recoveryError: unknown;
        if (newOutputMoved && (await fse.pathExists(outputRoot))) {
            try {
                await fse.remove(outputRoot);
            } catch (cleanupError) {
                recoveryError = cleanupError;
            }
        }
        if (oldOutputMoved && (await fse.pathExists(backupRoot))) {
            try {
                await fse.move(backupRoot, outputRoot);
            } catch (restoreError) {
                recoveryError ??= restoreError;
            }
        }
        if (recoveryError) throw new AggregateError([error, recoveryError]);
        throw error;
    } finally {
        if (await fse.pathExists(stagingRoot)) {
            await fse.remove(stagingRoot).catch(() => {});
        }
    }
}

function sanitizeNamespace(modRoot: string) {
    let base = path.basename(modRoot);
    // Users often select a component subfolder (body/). Prefer the parent mod name.
    if (/^(body|face|hair|leg|legs|outfit|parts?)$/i.test(stripDisabledPrefix(base))) {
        base = path.basename(path.dirname(modRoot));
    }
    const stripped = stripDisabledPrefix(base)
        .replace(/[^a-zA-Z0-9]+/g, "")
        .slice(0, 24);
    return stripped || "Mod";
}

function isTouchDraftAutoApplyable(
    interactive: TouchDraft["components"],
    minConfidence: number,
    avgConfidence: number,
) {
    // vision-llm disabled — vision approval check isolated (bone zones have no vision field)
    // const hasUnapprovedVision = interactive.some(
    //     (component) => component.vision !== undefined && component.visionApproved !== true,
    // );
    const hasUnapprovedVision = false;
    return (
        interactive.length > 0 &&
        !hasUnapprovedVision &&
        minConfidence >= TOUCH_CONFIDENCE_AUTO_APPLY_MIN &&
        avgConfidence >= TOUCH_CONFIDENCE_AUTO_APPLY_AVG
    );
}

async function writeTouchProfileManifest(outputRoot: string, runtimeVersion: string) {
    await fse.writeJson(
        path.join(outputRoot, TOUCH_PROFILE_MANIFEST_FILE),
        {
            kind: TOUCH_PROFILE_MANIFEST_KIND,
            runtimeVersion,
            createdAt: new Date().toISOString(),
        },
        { spaces: 2 },
    );
}

async function copyRuntimeShaders(outputRoot: string) {
    const sourceDir = resolveTouchRuntimeDir();
    const targetDir = path.join(outputRoot, "Resources", "IM");
    await fse.ensureDir(targetDir);
    for (const fileName of TOUCH_SHADER_FILES) {
        const sourcePath = path.join(sourceDir, fileName);
        if (!(await fse.pathExists(sourcePath))) {
            throw new Error(`Bundled touch runtime shader missing: ${sourcePath}`);
        }
        await fse.copy(sourcePath, path.join(targetDir, fileName));
    }
}

function resolveTouchRuntimeDir() {
    const appPath = app?.getAppPath?.();
    const candidates = [
        path.resolve(process.cwd(), "resources", "touch-runtime"),
        ...(appPath ? [path.resolve(appPath, "resources", "touch-runtime")] : []),
        path.resolve(
            path.dirname(fileURLToPath(import.meta.url)),
            "../../../../../resources/touch-runtime",
        ),
        process.resourcesPath
            ? path.resolve(process.resourcesPath, "resources", "touch-runtime")
            : "",
        process.resourcesPath ? path.resolve(process.resourcesPath, "touch-runtime") : "",
    ].filter(Boolean);

    for (const candidate of candidates) {
        if (fse.existsSync(path.join(candidate, TOUCH_SHADER_FILES[0]))) return candidate;
    }
    throw new Error("Unable to resolve bundled touch-runtime shaders");
}

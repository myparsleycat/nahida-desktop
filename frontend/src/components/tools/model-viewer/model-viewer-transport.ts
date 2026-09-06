import type {
    ModelViewerDNF,
    ModelViewerTransport as WailsModelViewerTransport,
} from "@bindings/tools";
import type {
    Dnf,
    ModViewerTransport,
    ViewerMaterialProfile,
    ViewerStateValue,
    ViewerTextureRole,
} from "@shared/mod-viewer/types";

import { normalizeAnimationFPS } from "./model-viewer-animation-clock";

function normalizeDNF(value: ModelViewerDNF): Dnf {
    return (value ?? []).map((group) =>
        (group ?? []).map((clause) => ({
            var: clause.var,
            value: clause.value,
            negate: clause.negate,
        })),
    );
}

function normalizeStateValue(value: unknown): ViewerStateValue {
    if (typeof value === "string" || typeof value === "number") {
        return value;
    }
    throw new TypeError(`Invalid model viewer state value: ${String(value)}`);
}

function normalizeState(
    value: { [_ in string]?: unknown } | null,
): Record<string, ViewerStateValue> {
    const state: Record<string, ViewerStateValue> = {};
    for (const [key, entry] of Object.entries(value ?? {})) {
        if (entry !== undefined) {
            state[key] = normalizeStateValue(entry);
        }
    }
    return state;
}

function normalizeTextureRole(value: string): ViewerTextureRole {
    switch (value) {
        case "diffuse":
        case "normal_map":
        case "light_map":
        case "material_map":
            return value;
        default:
            throw new TypeError(`Invalid model viewer texture role: ${value}`);
    }
}

function normalizeMaterialProfile(value?: string): ViewerMaterialProfile | undefined {
    return value === "zzmi" || value === "wuwa:rabbitfx" ? value : undefined;
}

function normalizeComputeSource(source: { url: string; byteLength: number; stride: number }) {
    if (!source.url || source.byteLength <= 0 || source.stride <= 0) {
        throw new TypeError("Invalid model viewer compute source.");
    }
    return { url: source.url, byteLength: source.byteLength, stride: source.stride };
}

export function normalizeModelViewerTransport(
    value: WailsModelViewerTransport,
): ModViewerTransport {
    const textures: ModViewerTransport["textures"] = {};
    for (const [key, texture] of Object.entries(value.textures ?? {})) {
        if (!texture) {
            continue;
        }
        textures[key] = {
            url: texture.url,
            role: normalizeTextureRole(texture.role),
        };
    }

    return {
        memorySessionId: value.memorySessionId,
        iniPath: value.iniPath,
        modPath: value.modPath,
        name: value.name,
        materialProfile: normalizeMaterialProfile(value.materialProfile),
        meshes: (value.meshes ?? []).map((mesh) => ({
            id: mesh.id,
            component: mesh.component,
            positionsUrl: mesh.positionsUrl,
            normalsUrl: mesh.normalsUrl,
            tangentsUrl: mesh.tangentsUrl,
            uvsUrl: mesh.uvsUrl,
            indicesUrl: mesh.indicesUrl,
            sourceIndicesUrl: mesh.sourceIndicesUrl,
            conditions: normalizeDNF(mesh.conditions),
            texKey: mesh.texKey,
            textureVariants: (mesh.textureVariants ?? []).map((variant) => ({
                conditions: normalizeDNF(variant.conditions),
                texKey: variant.texKey,
            })),
            normalMapKey: mesh.normalMapKey,
            normalMapVariants: (mesh.normalMapVariants ?? []).map((variant) => ({
                conditions: normalizeDNF(variant.conditions),
                texKey: variant.texKey,
            })),
            lightMapKey: mesh.lightMapKey,
            lightMapVariants: (mesh.lightMapVariants ?? []).map((variant) => ({
                conditions: normalizeDNF(variant.conditions),
                texKey: variant.texKey,
            })),
            materialMapKey: mesh.materialMapKey,
            materialMapVariants: (mesh.materialMapVariants ?? []).map((variant) => ({
                conditions: normalizeDNF(variant.conditions),
                texKey: variant.texKey,
            })),
            shapeTargets: (mesh.shapeTargets ?? []).map((target) => ({
                var: target.var,
                positionsUrl: target.positionsUrl,
                mode: target.mode === "midpoint_pair" ? target.mode : undefined,
                lowPositionsUrl: target.lowPositionsUrl,
            })),
            positionVariants: (mesh.positionVariants ?? []).map((variant) => ({
                conditions: normalizeDNF(variant.conditions),
                sourceUrl: variant.sourceUrl,
                stride: variant.stride,
                sourceBytes: variant.sourceBytes,
            })),
        })),
        textures,
        variables: (value.variables ?? []).map((variable) => ({
            id: variable.id,
            label: variable.label,
            defaultValue: normalizeStateValue(variable.defaultValue),
            values: (variable.values ?? []).map((entry) => ({
                value: normalizeStateValue(entry.value),
                label: entry.label,
            })),
            order: variable.order,
            slot: variable.slot,
            iconPath: variable.iconPath,
            controlType:
                variable.controlType === "buttons" || variable.controlType === "slider"
                    ? variable.controlType
                    : undefined,
            slider: variable.slider ?? undefined,
            effects: variable.effects?.map((effect) => ({
                when: effect.when,
                var: effect.var,
                value: effect.value,
            })),
        })),
        defaultState: normalizeState(value.defaultState),
        stateRules: (value.stateRules ?? []).map((rule) => ({
            var: rule.var,
            value: rule.value,
            conditions: normalizeDNF(rule.conditions),
        })),
        uiAssets: value.uiAssets,
        animations: (value.animations ?? []).map((clip) => ({
            id: clip.id,
            label: clip.label,
            deformerId: clip.deformerId,
            variableIds: clip.variableIds ?? [],
            fps: normalizeAnimationFPS(clip.fps),
            frameStart: clip.frameStart,
            frameEnd: clip.frameEnd,
            loop: clip.loop,
            frames: (clip.frames ?? []).map((frame) => ({
                index: frame.index,
                time: frame.time,
                values: normalizeState(frame.values),
            })),
        })),
        computeDeformers: (value.computeDeformers ?? []).flatMap((deformer) => {
            if (deformer.kind !== "gimi_shape_pose_v1") {
                return [];
            }
            return [
                {
                    kind: deformer.kind,
                    id: deformer.id,
                    meshIds: deformer.meshIds ?? [],
                    vertexCount: deformer.vertexCount,
                    base: normalizeComputeSource(deformer.base),
                    shapePasses: (deformer.shapePasses ?? []).map((pass) => ({
                        target: normalizeComputeSource(pass.target),
                        phaseRate: pass.phaseRate,
                        wrapAt: pass.wrapAt,
                        phaseOffset: pass.phaseOffset,
                        angularScale: pass.angularScale,
                        amplitude: pass.amplitude,
                        bias: pass.bias,
                    })),
                    pose: deformer.pose
                        ? {
                              blend: normalizeComputeSource(deformer.pose.blend),
                              frames: normalizeComputeSource(deformer.pose.frames),
                              boneCount: deformer.pose.boneCount,
                              frameCount: deformer.pose.frameCount,
                          }
                        : undefined,
                },
            ];
        }),
    };
}

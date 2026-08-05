import path from "node:path";

import {
    createDefaultTouchZoneSettings,
    TOUCH_PROFILE_MASK_CURVE_RANGE,
    TOUCH_PROFILE_MASK_STRENGTH_RANGE,
} from "@shared/touch-profile-settings";
import fse from "fs-extra";
import { PNG } from "pngjs";

import {
    buildAllViewTransforms,
    normalizePolygonPoint,
    TOUCH_VIEW_NAMES,
    vertexToNormalized,
    type TouchViewTransform,
} from "./touch-profile-projection";
import { resolveTouchJiggleParams } from "./touch-profile-settings";
import {
    DEFAULT_TOUCH_VISION_MASK_TUNING,
    TOUCH_MASK_BANDS,
    TOUCH_OBJECT_MODE,
    TOUCH_ZONE_CHANNELS,
    TOUCH_VISION_MASK_TUNING_RANGES,
    TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE,
    type TouchComponentAnalysis,
    type TouchComponentDraft,
    type TouchJiggleParams,
    type TouchObjectMapEntry,
    type TouchVisionMaskTuning,
    type TouchVisionZone,
    type TouchZoneSpec,
} from "./touch-profile-types";

export type TouchGeneratedAssets = {
    componentId: string;
    assetPrefix: string;
    relativeDir: string;
    maskPaths: string[];
    objectMapPaths: Array<{ label: string; relativePath: string; absolutePath: string }>;
    paramsRelativePath: string;
    paramsAbsolutePath: string;
    previewRelativePath: string;
    previewAbsolutePath: string;
    masks: Float32Array;
};

/** How far a vision seed can influence neighbors, relative to the seed-set radius. */
const VISION_SEED_INFLUENCE = 0.32;

export function normalizeVisionMaskTuning(
    tuning?: Partial<TouchVisionMaskTuning> | null,
    fallback?: Partial<TouchVisionMaskTuning> | null,
): TouchVisionMaskTuning {
    const base = { ...DEFAULT_TOUCH_VISION_MASK_TUNING, ...fallback };
    const maskCutoffD2 = boundedNumber(
        tuning?.maskCutoffD2,
        TOUCH_VISION_MASK_TUNING_RANGES.maskCutoffD2,
        base.maskCutoffD2,
    );
    return {
        radiusPadding: boundedNumber(
            tuning?.radiusPadding,
            TOUCH_VISION_MASK_TUNING_RANGES.radiusPadding,
            base.radiusPadding,
        ),
        maskCutoffD2,
        maskEdgeFadeD2: Math.min(
            boundedNumber(
                tuning?.maskEdgeFadeD2,
                TOUCH_VISION_MASK_TUNING_RANGES.maskEdgeFadeD2,
                base.maskEdgeFadeD2,
            ),
            Math.max(TOUCH_VISION_MASK_TUNING_RANGES.maskEdgeFadeD2.min, maskCutoffD2 - 0.01),
        ),
        seedInfluenceScale: boundedNumber(
            tuning?.seedInfluenceScale,
            TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE,
            base.seedInfluenceScale,
        ),
        colocatedLayerRatio: boundedNumber(
            tuning?.colocatedLayerRatio,
            TOUCH_VISION_MASK_TUNING_RANGES.colocatedLayerRatio,
            base.colocatedLayerRatio,
        ),
        colocatedLayerMin: boundedNumber(
            tuning?.colocatedLayerMin,
            TOUCH_VISION_MASK_TUNING_RANGES.colocatedLayerMin,
            base.colocatedLayerMin,
        ),
        sideHeightPadRatio: boundedNumber(
            tuning?.sideHeightPadRatio,
            TOUCH_VISION_MASK_TUNING_RANGES.sideHeightPadRatio,
            base.sideHeightPadRatio,
        ),
        sideHeightPadMin: boundedNumber(
            tuning?.sideHeightPadMin,
            TOUCH_VISION_MASK_TUNING_RANGES.sideHeightPadMin,
            base.sideHeightPadMin,
        ),
        seedRadiusScale: boundedNumber(
            tuning?.seedRadiusScale,
            TOUCH_VISION_MASK_TUNING_RANGES.seedRadiusScale,
            base.seedRadiusScale,
        ),
    };
}

function boundedNumber(value: unknown, range: { min: number; max: number }, fallback: number) {
    return typeof value === "number" && Number.isFinite(value)
        ? Math.max(range.min, Math.min(range.max, value))
        : fallback;
}

/** Apply the soft-mask envelope and retain every zone proposed by Vision. */
export function prepareVisionZones(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    visionZones: TouchZoneSpec[],
): { zones: TouchZoneSpec[]; warnings: string[] } {
    const warnings: string[] = [];
    const zones = visionZones.map((zone) => {
        const visionMaskTuning = normalizeVisionMaskTuning(zone.visionMaskTuning);
        const prepared = {
            ...zone,
            radius: [
                Math.max(zone.radius[0] * visionMaskTuning.radiusPadding, 0.02),
                Math.max(zone.radius[1] * visionMaskTuning.radiusPadding, 0.02),
                Math.max(zone.radius[2] * visionMaskTuning.radiusPadding, 0.02),
            ] as [number, number, number],
            seedVertices: zone.seedVertices?.length ? [...zone.seedVertices] : zone.seedVertices,
            visionMaskTuning: zone.source === "vision" ? visionMaskTuning : zone.visionMaskTuning,
        };
        const quality = evaluateVisionZone(component, positions, indices, prepared);
        if (!quality.valid) {
            warnings.push(`Vision zone ${zone.id} failed geometry checks (${quality.reason})`);
        }
        return prepared;
    });
    return { zones, warnings };
}

export type TouchVisionPolygonSelection = {
    id: string;
    label: string;
    channel: number;
    confidence: number;
    maskTuning: TouchVisionMaskTuning;
    vertices: number[];
    verticesByView: Record<string, number[]>;
};

export function selectVerticesFromVisionPolygons(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zones: TouchVisionZone[],
    viewTransforms?: Record<string, TouchViewTransform>,
): TouchVisionPolygonSelection[] {
    const transforms = viewTransforms ?? buildAllViewTransforms(positions);
    const allowed = allowedVertexMask(component, indices, component.vertexCount);
    return zones.map((zone, index) => {
        const includeViews = Object.keys(zone.include).filter((key) =>
            TOUCH_VIEW_NAMES.includes(key as (typeof TOUCH_VIEW_NAMES)[number]),
        );
        const views = includeViews.length > 0 ? includeViews : ["front"];
        const includePolygons = new Map(
            views.map((view) => [
                view,
                (zone.include[view] ?? zone.include.default ?? []).map((polygon) =>
                    polygon.map((entry) => normalizePolygonPoint(entry)),
                ),
            ]),
        );
        const excludePolygons = new Map(
            views.map((view) => [
                view,
                (zone.exclude[view] ?? zone.exclude.default ?? []).map((polygon) =>
                    polygon.map((entry) => normalizePolygonPoint(entry)),
                ),
            ]),
        );
        const selectedByView: Record<string, number[]> = {};
        const hitsByVertex = new Map<number, string[]>();
        const maskTuning = normalizeVisionMaskTuning(zone.maskTuning);

        for (let vertex = 0; vertex < component.vertexCount; vertex++) {
            if (!allowed[vertex]) continue;

            let excludeHit = false;
            const selectedViews: string[] = [];

            for (const view of views) {
                const transform = transforms[view];
                if (!transform) continue;
                if (transform.visibleVertices && !transform.visibleVertices[vertex]) continue;
                const point = vertexToNormalized(transform, vertex);
                const includePolys = includePolygons.get(view) ?? [];
                const excludePolys = excludePolygons.get(view) ?? [];
                const viewIncludeHit = polygonSetContains(includePolys, point);
                const viewExcludeHit = polygonSetContains(excludePolys, point);
                if (viewExcludeHit) excludeHit = true;
                if (viewIncludeHit && !viewExcludeHit) selectedViews.push(view);
            }

            if (excludeHit || selectedViews.length === 0) continue;
            hitsByVertex.set(vertex, selectedViews);
        }

        // Side picks outside the front height and width bands pull arms and torso into breast masks.
        const frontHits = [...hitsByVertex.entries()].filter(([, hitViews]) =>
            hitViews.includes("front"),
        );
        const frontHeights = frontHits.map(([vertex]) => positions[vertex * 3 + 2]);
        const heightBand =
            frontHeights.length > 0
                ? computeHeightBand(frontHeights, positions, allowed, maskTuning)
                : null;
        const frontXs = frontHits.map(([vertex]) => positions[vertex * 3]);
        const frontXBand = frontXs.length > 0 ? computeXBand(frontXs, positions, allowed) : null;

        const selected: number[] = [];
        for (const [vertex, hitViews] of hitsByVertex) {
            const hasFront = hitViews.includes("front");
            const sideOnly =
                !hasFront && hitViews.some((view) => view === "left" || view === "right");
            if (sideOnly) {
                if (heightBand) {
                    const height = positions[vertex * 3 + 2];
                    if (height < heightBand.min || height > heightBand.max) continue;
                }
                if (frontXBand) {
                    const px = positions[vertex * 3];
                    if (px < frontXBand.min || px > frontXBand.max) continue;
                }
            }
            selected.push(vertex);
            for (const view of hitViews) {
                if ((view === "left" || view === "right") && !hasFront) {
                    const height = positions[vertex * 3 + 2];
                    if (heightBand && (height < heightBand.min || height > heightBand.max))
                        continue;
                    const px = positions[vertex * 3];
                    if (frontXBand && (px < frontXBand.min || px > frontXBand.max)) continue;
                }
                (selectedByView[view] ??= []).push(vertex);
            }
        }

        const expanded = expandColocatedLayerSeeds({
            positions,
            vertexCount: component.vertexCount,
            allowed,
            seeds: selected,
            zoneId: zone.id,
            transforms,
            views,
            excludePolygons,
            heightBand,
            tuning: maskTuning,
        });
        const selectedSet = new Set(selected);
        for (const vertex of expanded) {
            if (selectedSet.has(vertex)) continue;
            selectedSet.add(vertex);
            selected.push(vertex);
        }

        return {
            id: zone.id,
            label: zone.label,
            channel: resolveZoneChannel(zone.id, zone.label, index, component.kind),
            confidence: zone.confidence,
            maskTuning: normalizeVisionMaskTuning(zone.maskTuning),
            vertices: selected,
            verticesByView: selectedByView,
        };
    });
}

function computeHeightBand(
    frontHeights: number[],
    positions: Float32Array,
    allowed: Uint8Array,
    tuning: TouchVisionMaskTuning,
) {
    let min = Number.POSITIVE_INFINITY;
    let max = Number.NEGATIVE_INFINITY;
    for (const height of frontHeights) {
        min = Math.min(min, height);
        max = Math.max(max, height);
    }
    let meshMin = Number.POSITIVE_INFINITY;
    let meshMax = Number.NEGATIVE_INFINITY;
    for (let vertex = 0; vertex < allowed.length; vertex++) {
        if (!allowed[vertex]) continue;
        const height = positions[vertex * 3 + 2];
        meshMin = Math.min(meshMin, height);
        meshMax = Math.max(meshMax, height);
    }
    const meshSpan = Math.max(meshMax - meshMin, 1e-4);
    const pad = Math.max(meshSpan * tuning.sideHeightPadRatio, tuning.sideHeightPadMin);
    return { min: min - pad, max: max + pad };
}

function computeXBand(frontXs: number[], _positions: Float32Array, _allowed: Uint8Array) {
    let min = Number.POSITIVE_INFINITY;
    let max = Number.NEGATIVE_INFINITY;
    for (const x of frontXs) {
        min = Math.min(min, x);
        max = Math.max(max, x);
    }
    const span = Math.max(max - min, 1e-4);
    const pad = Math.max(span * 0.35, 0.03);
    return { min: min - pad, max: max + pad };
}

/** Add allowed vertices that sit on a near-duplicate cloth layer over LLM seeds. */
function expandColocatedLayerSeeds(input: {
    positions: Float32Array;
    vertexCount: number;
    allowed: Uint8Array;
    seeds: number[];
    zoneId: string;
    transforms: Record<string, TouchViewTransform>;
    views: string[];
    excludePolygons: Map<string, Array<Array<[number, number]>>>;
    heightBand?: { min: number; max: number } | null;
    tuning: TouchVisionMaskTuning;
}) {
    if (input.seeds.length === 0) return input.seeds;

    let minX = Number.POSITIVE_INFINITY;
    let minY = Number.POSITIVE_INFINITY;
    let minZ = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let maxY = Number.NEGATIVE_INFINITY;
    let maxZ = Number.NEGATIVE_INFINITY;
    for (const seed of input.seeds) {
        const x = input.positions[seed * 3];
        const y = input.positions[seed * 3 + 1];
        const z = input.positions[seed * 3 + 2];
        minX = Math.min(minX, x);
        minY = Math.min(minY, y);
        minZ = Math.min(minZ, z);
        maxX = Math.max(maxX, x);
        maxY = Math.max(maxY, y);
        maxZ = Math.max(maxZ, z);
    }

    const extentX = maxX - minX;
    const extentY = maxY - minY;
    const extentZ = maxZ - minZ;
    const maxExtent = Math.max(extentX, extentY, extentZ, 1e-4);
    const threshold = Math.min(
        Math.max(maxExtent * input.tuning.colocatedLayerRatio, input.tuning.colocatedLayerMin),
        0.035,
    );
    const threshold2 = threshold * threshold;
    const pad = threshold;
    const isLeft = input.zoneId.includes("left");
    const isRight = input.zoneId.includes("right");
    const isBreast = input.zoneId.includes("breast") || input.zoneId.includes("chest");
    const seedSet = new Set(input.seeds);
    const expanded = [...input.seeds];

    for (let vertex = 0; vertex < input.vertexCount; vertex++) {
        if (seedSet.has(vertex) || !input.allowed[vertex]) continue;
        const px = input.positions[vertex * 3];
        const py = input.positions[vertex * 3 + 1];
        const pz = input.positions[vertex * 3 + 2];
        if (
            px < minX - pad ||
            px > maxX + pad ||
            py < minY - pad ||
            py > maxY + pad ||
            pz < minZ - pad ||
            pz > maxZ + pad
        ) {
            continue;
        }
        if (input.heightBand && (pz < input.heightBand.min || pz > input.heightBand.max)) continue;
        if (isLeft && px > maxX + threshold * 0.35) continue;
        if (isRight && px < minX - threshold * 0.35) continue;
        if (isBreast) {
            if (py > maxY + threshold * 0.5) continue;
            if (pz < minZ - threshold * 0.5) continue;
        }

        let excludeHit = false;
        for (const view of input.views) {
            const transform = input.transforms[view];
            if (!transform) continue;
            const point = vertexToNormalized(transform, vertex);
            if (polygonSetContains(input.excludePolygons.get(view) ?? [], point)) {
                excludeHit = true;
                break;
            }
        }
        if (excludeHit) continue;

        let nearest2 = Number.POSITIVE_INFINITY;
        for (const seed of input.seeds) {
            const sx = input.positions[seed * 3] - px;
            const sy = input.positions[seed * 3 + 1] - py;
            const sz = input.positions[seed * 3 + 2] - pz;
            const d2 = sx * sx + sy * sy + sz * sz;
            if (d2 < nearest2) nearest2 = d2;
        }
        if (nearest2 >= threshold2) continue;
        seedSet.add(vertex);
        expanded.push(vertex);
    }

    return expanded;
}

export function zonesFromVisionPolygons(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zones: TouchVisionZone[],
    viewTransforms?: Record<string, TouchViewTransform>,
): TouchZoneSpec[] {
    const result: TouchZoneSpec[] = [];
    const selectedZones = selectVerticesFromVisionPolygons(
        component,
        positions,
        indices,
        zones,
        viewTransforms,
    );

    for (const selection of selectedZones) {
        const minSelected = Math.min(12, Math.max(3, Math.floor(component.vertexCount * 0.01)));
        if (selection.vertices.length < minSelected) continue;

        // Trust the LLM vertex set: center/radius are only a soft falloff envelope around seeds.
        const center: [number, number, number] = [0, 0, 0];
        for (const vertex of selection.vertices) {
            center[0] += positions[vertex * 3];
            center[1] += positions[vertex * 3 + 1];
            center[2] += positions[vertex * 3 + 2];
        }
        center[0] /= selection.vertices.length;
        center[1] /= selection.vertices.length;
        center[2] /= selection.vertices.length;

        let maxDx = 0;
        let maxDy = 0;
        let maxDz = 0;
        for (const vertex of selection.vertices) {
            maxDx = Math.max(maxDx, Math.abs(positions[vertex * 3] - center[0]));
            maxDy = Math.max(maxDy, Math.abs(positions[vertex * 3 + 1] - center[1]));
            maxDz = Math.max(maxDz, Math.abs(positions[vertex * 3 + 2] - center[2]));
        }

        result.push({
            id: selection.id,
            label: selection.label,
            channel: selection.channel,
            confidence: selection.confidence,
            center,
            radius: [
                Math.max(maxDx * selection.maskTuning.seedRadiusScale, 0.02),
                Math.max(maxDy * selection.maskTuning.seedRadiusScale, 0.02),
                Math.max(maxDz * selection.maskTuning.seedRadiusScale, 0.02),
            ],
            source: "vision",
            settings: createDefaultTouchZoneSettings(),
            seedVertices: selection.vertices,
            visionMaskTuning: selection.maskTuning,
        });
    }

    return result;
}

export function buildVertexMasks(
    vertexCount: number,
    positions: Float32Array,
    indices: Uint32Array,
    component: TouchComponentAnalysis,
    zones: TouchZoneSpec[],
) {
    const masks = new Float32Array(vertexCount * TOUCH_ZONE_CHANNELS);
    const allowed = allowedVertexMask(component, indices, vertexCount);
    const bounds = computeBounds(positions, allowed);
    const midX = (bounds.min[0] + bounds.max[0]) * 0.5;
    const spanX = Math.max(bounds.max[0] - bounds.min[0], 1e-3);
    const clipStart = spanX * 0.03;
    const clipEnd = spanX * 0.08;

    for (const zone of zones) {
        if (zone.channel < 0 || zone.channel >= TOUCH_ZONE_CHANNELS) continue;
        const maskTuning = normalizeVisionMaskTuning(zone.visionMaskTuning);
        const maskStrength = Number.isFinite(zone.settings.maskStrength)
            ? Math.max(
                  TOUCH_PROFILE_MASK_STRENGTH_RANGE.min,
                  Math.min(TOUCH_PROFILE_MASK_STRENGTH_RANGE.max, zone.settings.maskStrength),
              )
            : 1;
        const maskCurve = Number.isFinite(zone.settings.maskCurve)
            ? Math.max(
                  TOUCH_PROFILE_MASK_CURVE_RANGE.min,
                  Math.min(TOUCH_PROFILE_MASK_CURVE_RANGE.max, zone.settings.maskCurve),
              )
            : 1;

        const seeds =
            zone.seedVertices && zone.seedVertices.length > 0
                ? zone.seedVertices.filter((vertex) => vertex >= 0 && vertex < vertexCount)
                : null;
        const seedInfluence = seeds
            ? Math.max(
                  Math.min(zone.radius[0], zone.radius[1], zone.radius[2]) * VISION_SEED_INFLUENCE,
                  Math.max(...zone.radius) * 0.2,
                  0.02,
              ) * maskTuning.seedInfluenceScale
            : 0;
        const seedInfluence2 = seedInfluence * seedInfluence;
        const seedCutoffD2 =
            maskTuning.maskCutoffD2 / DEFAULT_TOUCH_VISION_MASK_TUNING.maskCutoffD2;
        const seedEdgeFadeD2 =
            (0.3 * maskTuning.maskEdgeFadeD2) / DEFAULT_TOUCH_VISION_MASK_TUNING.maskEdgeFadeD2;

        for (let vertex = 0; vertex < vertexCount; vertex++) {
            if (!allowed[vertex]) continue;
            const px = positions[vertex * 3];
            const py = positions[vertex * 3 + 1];
            const pz = positions[vertex * 3 + 2];

            let weight = 0;
            if (seeds) {
                // Vision path: weight from distance to nearest LLM-selected vertex only.
                // Anatomy location comes from the model; code does not invent breast/butt bands.
                let nearest2 = Number.POSITIVE_INFINITY;
                for (const seed of seeds) {
                    const sx = positions[seed * 3] - px;
                    const sy = positions[seed * 3 + 1] - py;
                    const sz = positions[seed * 3 + 2] - pz;
                    const d2 = sx * sx + sy * sy + sz * sz;
                    if (d2 < nearest2) nearest2 = d2;
                }
                if (nearest2 >= seedInfluence2 * seedCutoffD2) continue;
                const t = nearest2 / seedInfluence2;
                weight =
                    Math.pow(1 - t / seedCutoffD2, maskCurve) *
                    (1 - smoothstep(seedCutoffD2 - seedEdgeFadeD2, seedCutoffD2, t));
            } else {
                const dx = (px - zone.center[0]) / zone.radius[0];
                const dy = (py - zone.center[1]) / zone.radius[1];
                const dz = (pz - zone.center[2]) / zone.radius[2];
                const d2 = dx * dx + dy * dy + dz * dz;
                if (d2 >= maskTuning.maskCutoffD2) continue;
                const edgeFade =
                    1 -
                    smoothstep(
                        maskTuning.maskCutoffD2 - maskTuning.maskEdgeFadeD2,
                        maskTuning.maskCutoffD2,
                        d2,
                    );
                weight = Math.pow(Math.exp(-1.35 * d2), maskCurve) * edgeFade;
            }

            // Keep bilateral zones on their side without assuming the model is centered at x=0.
            if (zone.id.includes("left") && px > midX + clipStart) {
                weight *= 1 - smoothstep(0, clipEnd, px - (midX + clipStart));
            }
            if (zone.id.includes("right") && px < midX - clipStart) {
                weight *= 1 - smoothstep(0, clipEnd, midX - clipStart - px);
            }
            weight *= maskStrength;
            const offset = vertex * TOUCH_ZONE_CHANNELS + zone.channel;
            masks[offset] = Math.max(masks[offset], weight);
        }
    }

    // Single adjacency pass — a second pass walks along limbs and bloates breast masks.
    smoothMasks(masks, vertexCount, indices);
    clampMasks(masks);
    return masks;
}

export function extractMaskChannel(masks: Float32Array, vertexCount: number, channel: number) {
    const weights = new Float32Array(vertexCount);
    if (channel < 0 || channel >= TOUCH_ZONE_CHANNELS) return weights;

    for (let vertex = 0; vertex < vertexCount; vertex++) {
        weights[vertex] = masks[vertex * TOUCH_ZONE_CHANNELS + channel] ?? 0;
    }
    return weights;
}

export async function writeTouchComponentAssets(input: {
    outputRoot: string;
    component: TouchComponentAnalysis;
    draft: TouchComponentDraft;
    positions: Float32Array;
    indices: Uint32Array;
    assetPrefix: string;
}): Promise<TouchGeneratedAssets> {
    const relativeDir = path.join("Resources", "IM");
    const absoluteDir = path.join(input.outputRoot, relativeDir);
    await fse.ensureDir(absoluteDir);

    const masks = buildVertexMasks(
        input.component.vertexCount,
        input.positions,
        input.indices,
        input.component,
        input.draft.zones,
    );

    const maskPaths: string[] = [];
    for (let band = 0; band < TOUCH_MASK_BANDS; band++) {
        const fileName = `${input.assetPrefix}JiggleMasks${band}.buf`;
        const absolutePath = path.join(absoluteDir, fileName);
        await fse.writeFile(
            absolutePath,
            Buffer.from(maskBandBytes(masks, input.component.vertexCount, band).buffer),
        );
        maskPaths.push(path.join(relativeDir, fileName).replaceAll("\\", "/"));
    }

    const objectMapPaths: TouchGeneratedAssets["objectMapPaths"] = [];
    for (const entry of resolveObjectMaps(input.component, input.draft.objectId)) {
        const fileName = objectMapFileName(input.assetPrefix, entry.label);
        const absolutePath = path.join(absoluteDir, fileName);
        await fse.writeFile(absolutePath, Buffer.from(encodeObjectMap([entry]).buffer));
        objectMapPaths.push({
            label: entry.label,
            relativePath: path.join(relativeDir, fileName).replaceAll("\\", "/"),
            absolutePath,
        });
    }

    const paramsFileName = `${input.assetPrefix}JiggleParams.buf`;
    const paramsAbsolutePath = path.join(absoluteDir, paramsFileName);
    await fse.writeFile(
        paramsAbsolutePath,
        Buffer.from(
            encodeJiggleParams(
                resolveTouchJiggleParams(
                    input.draft.zones[0]?.settings ?? createDefaultTouchZoneSettings(),
                    input.draft.objectId,
                ),
            ).buffer,
        ),
    );

    const previewFileName = `${input.assetPrefix}TouchMaskPreview.png`;
    const previewAbsolutePath = path.join(absoluteDir, previewFileName);
    await writeMaskPreview(
        previewAbsolutePath,
        input.positions,
        masks,
        `${input.assetPrefix} touch masks`,
    );

    return {
        componentId: input.component.id,
        assetPrefix: input.assetPrefix,
        relativeDir: relativeDir.replaceAll("\\", "/"),
        maskPaths,
        objectMapPaths,
        paramsRelativePath: path.join(relativeDir, paramsFileName).replaceAll("\\", "/"),
        paramsAbsolutePath,
        previewRelativePath: path.join(relativeDir, previewFileName).replaceAll("\\", "/"),
        previewAbsolutePath,
        masks,
    };
}

export function encodeObjectMap(entries: TouchObjectMapEntry[]) {
    const values = new Float32Array((1 + entries.length) * 4);
    values[0] = entries.length;
    entries.forEach((entry, index) => {
        const offset = (index + 1) * 4;
        values[offset] = entry.firstIndex;
        values[offset + 1] = entry.indexCount;
        values[offset + 2] = entry.objectMode || TOUCH_OBJECT_MODE;
        values[offset + 3] = entry.objectId;
    });
    return values;
}

export function encodeJiggleParams(params: TouchJiggleParams) {
    return new Float32Array([
        params.objectId,
        params.radius,
        params.strength,
        params.falloff,
        params.dragScale,
        params.grabDamping,
        params.grabSpring,
        params.releaseDamping,
        params.releaseSpring,
        params.releaseKick,
        params.maxOffset,
        params.targetFollow,
        params.mouseYDirection,
        params.mouseXDirection,
        0,
        0,
    ]);
}

export function bakeSampleOffsets(firstIndex: number, indexCount: number, sampleCount = 8) {
    if (indexCount <= 1) return Array.from({ length: sampleCount }, () => firstIndex);
    return Array.from({ length: sampleCount }, (_, index) => {
        return firstIndex + Math.floor((index * (indexCount - 1)) / (sampleCount - 1));
    });
}

export function assetPrefixForComponent(component: TouchComponentAnalysis, namespaceToken: string) {
    const kind =
        component.kind === "body"
            ? "Body"
            : component.kind === "legs"
              ? "Leg"
              : component.name.replace(/position/i, "").replace(/[^a-zA-Z0-9]+/g, "") || "Mesh";
    // Avoid bodyBody / legLeg when the selected folder itself is a component folder.
    const ns = namespaceToken.replace(new RegExp(`${kind}$`, "i"), "");
    const prefix = ns || "Nhd";
    return `${prefix}${kind}${component.variantKey ? `V${component.variantKey}` : ""}`;
}

function resolveObjectMaps(
    component: TouchComponentAnalysis,
    objectId: number,
): TouchObjectMapEntry[] {
    if (component.objectMaps.length > 0) {
        return component.objectMaps.map((entry) => ({
            ...entry,
            objectId,
            objectMode: TOUCH_OBJECT_MODE,
        }));
    }
    const fallback = component.drawRanges[0];
    return [
        {
            firstIndex: fallback?.firstIndex ?? 0,
            indexCount: fallback?.indexCount ?? component.indexCount,
            objectMode: TOUCH_OBJECT_MODE,
            objectId,
            label: "main",
        },
    ];
}

function objectMapFileName(prefix: string, label: string) {
    const normalized = label.charAt(0).toUpperCase() + label.slice(1);
    if (label === "main" || label === "skin") return `${prefix}ObjectMap.buf`;
    return `${prefix}${normalized}ObjectMap.buf`;
}

function maskBandBytes(masks: Float32Array, vertexCount: number, band: number) {
    const out = new Float32Array(vertexCount * 4);
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const src = vertex * TOUCH_ZONE_CHANNELS + band * 4;
        const dst = vertex * 4;
        out[dst] = masks[src];
        out[dst + 1] = masks[src + 1];
        out[dst + 2] = masks[src + 2];
        out[dst + 3] = masks[src + 3];
    }
    return out;
}

function evaluateVisionZone(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
) {
    const masks = buildVertexMasks(component.vertexCount, positions, indices, component, [zone]);
    const activeVertices = countActiveZoneVertices(masks, component.vertexCount, zone.channel);
    const activeRatio = activeVertices / Math.max(component.vertexCount, 1);
    const minActiveVertices = Math.min(12, Math.max(3, Math.floor(component.vertexCount * 0.01)));
    const seedCount = zone.seedVertices?.length ?? 0;
    const finiteRadius = zone.radius.every((value) => Number.isFinite(value) && value > 0);
    const maxActiveRatio = maxVisionActiveRatio(
        component.kind,
        zone.id,
        zone.label,
        component.vertexCount,
    );
    const maxHeightSpan = maxVisionHeightSpanRatio(
        component.kind,
        zone.id,
        zone.label,
        component.vertexCount,
    );
    const radiusTooBroad = visionRadiusTooBroad(component, positions, indices, zone);
    const heightSpanRatio = measureZoneHeightSpanRatio(component, positions, indices, zone);

    // Geometry-only gates. Anatomy correctness is the LLM's job via the review loop.
    const reason = !finiteRadius
        ? "non-finite radius"
        : seedCount > 0 && seedCount < minActiveVertices
          ? `${seedCount} seed verts, need ≥${minActiveVertices}`
          : activeVertices < minActiveVertices
            ? `${activeVertices} active verts, need ≥${minActiveVertices}`
            : radiusTooBroad
              ? "vision radius is too broad for the component"
              : activeRatio > maxActiveRatio
                ? `${Math.round(activeRatio * 100)}% coverage (too broad; max ${Math.round(maxActiveRatio * 100)}%)`
                : heightSpanRatio > maxHeightSpan
                  ? `vertical span ${Math.round(heightSpanRatio * 100)}% of body (too tall; tighten polygons off arms/torso)`
                  : "";

    return {
        activeVertices,
        activeRatio,
        heightSpanRatio,
        reason,
        valid: reason === "",
    };
}

function visionRadiusTooBroad(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
) {
    const allowed = allowedVertexMask(component, indices, component.vertexCount);
    let minX = Number.POSITIVE_INFINITY;
    let minY = Number.POSITIVE_INFINITY;
    let minZ = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let maxY = Number.NEGATIVE_INFINITY;
    let maxZ = Number.NEGATIVE_INFINITY;
    for (let vertex = 0; vertex < component.vertexCount; vertex++) {
        if (!allowed[vertex]) continue;
        const x = positions[vertex * 3];
        const y = positions[vertex * 3 + 1];
        const z = positions[vertex * 3 + 2];
        minX = Math.min(minX, x);
        minY = Math.min(minY, y);
        minZ = Math.min(minZ, z);
        maxX = Math.max(maxX, x);
        maxY = Math.max(maxY, y);
        maxZ = Math.max(maxZ, z);
    }
    const meshSpan = [maxX - minX, maxY - minY, maxZ - minZ];
    return zone.radius.some((radius, axis) => radius > Math.max(meshSpan[axis] * 2, 0.25));
}

export function maxVisionActiveRatio(
    kind: TouchComponentAnalysis["kind"],
    zoneId: string,
    zoneLabel: string,
    vertexCount = Number.POSITIVE_INFINITY,
) {
    // Tiny meshes (unit tests / stubs) are the zone itself — skip production breadth caps.
    if (vertexCount < 256) return 1;
    const text = `${zoneId} ${zoneLabel}`.toLowerCase();
    if (kind === "body" && /(breast|chest)/.test(text)) return 0.16;
    if (kind === "body" && /belly/.test(text)) return 0.22;
    if (kind === "legs" && /(butt|buttock|hip)/.test(text)) return 0.28;
    if (kind === "legs") return 0.32;
    return 0.35;
}

export function maxVisionHeightSpanRatio(
    kind: TouchComponentAnalysis["kind"],
    zoneId: string,
    zoneLabel: string,
    vertexCount = Number.POSITIVE_INFINITY,
) {
    if (vertexCount < 256) return 1;
    const text = `${zoneId} ${zoneLabel}`.toLowerCase();
    if (kind === "body" && /(breast|chest)/.test(text)) return 0.18;
    if (kind === "body" && /belly/.test(text)) return 0.28;
    if (kind === "legs" && /(butt|buttock|hip)/.test(text)) return 0.35;
    if (kind === "legs") return 0.45;
    return 0.5;
}

function measureZoneHeightSpanRatio(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
) {
    const allowed = allowedVertexMask(component, indices, component.vertexCount);
    let meshMin = Number.POSITIVE_INFINITY;
    let meshMax = Number.NEGATIVE_INFINITY;
    for (let vertex = 0; vertex < component.vertexCount; vertex++) {
        if (!allowed[vertex]) continue;
        const height = positions[vertex * 3 + 2];
        meshMin = Math.min(meshMin, height);
        meshMax = Math.max(meshMax, height);
    }
    const meshSpan = Math.max(meshMax - meshMin, 1e-4);

    const seeds =
        zone.seedVertices && zone.seedVertices.length > 0
            ? zone.seedVertices
            : Array.from({ length: component.vertexCount }, (_, vertex) => vertex).filter(
                  (vertex) => allowed[vertex],
              );
    if (seeds.length === 0) return 0;

    let zoneMin = Number.POSITIVE_INFINITY;
    let zoneMax = Number.NEGATIVE_INFINITY;
    for (const vertex of seeds) {
        if (vertex < 0 || vertex >= component.vertexCount) continue;
        const height = positions[vertex * 3 + 2];
        zoneMin = Math.min(zoneMin, height);
        zoneMax = Math.max(zoneMax, height);
    }
    if (!Number.isFinite(zoneMin) || !Number.isFinite(zoneMax)) return 0;
    return (zoneMax - zoneMin) / meshSpan;
}

function countActiveZoneVertices(masks: Float32Array, vertexCount: number, channel: number) {
    let count = 0;
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        if (masks[vertex * TOUCH_ZONE_CHANNELS + channel] > 0.02) count += 1;
    }
    return count;
}

export function constrainMaskEnvelope(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zones: TouchZoneSpec[],
): { zones: TouchZoneSpec[]; adjustments: Map<string, string[]> } {
    const adjustments = new Map<string, string[]>();
    const result = zones.map((zone) => {
        const cap = maxVisionActiveRatio(
            component.kind,
            zone.id,
            zone.label,
            component.vertexCount,
        );
        // Expand zones that are below 95% of the cap so the mask is not stuck
        // at an over-shrunk value from a previous turn. Shrinking remains
        // the dominant direction for zones that exceed the cap.
        const targetCap = component.vertexCount < 256 ? cap : cap * 0.95;
        const currentRatio = measureZoneActiveRatio(component, positions, indices, zone);
        if (currentRatio <= cap && currentRatio >= targetCap) return zone;

        const tuning = normalizeVisionMaskTuning(zone.visionMaskTuning);
        const changes: string[] = [];

        if (currentRatio > cap) {
            shrinkToCap(component, positions, indices, zone, tuning, cap, changes);
        } else {
            expandToTargetCap(component, positions, indices, zone, tuning, targetCap, changes);
        }

        if (changes.length > 0) adjustments.set(zone.id, changes);
        return { ...zone, visionMaskTuning: tuning };
    });
    return { zones: result, adjustments };
}

function shrinkToCap(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
    tuning: TouchVisionMaskTuning,
    cap: number,
    changes: string[],
) {
    let lo: number = TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE.min;
    let hi: number = tuning.seedInfluenceScale;
    for (let i = 0; i < 8; i++) {
        const mid = (lo + hi) / 2;
        const ratio = measureZoneActiveRatio(component, positions, indices, {
            ...zone,
            visionMaskTuning: { ...tuning, seedInfluenceScale: mid },
        });
        if (ratio > cap) hi = mid;
        else lo = mid;
    }
    if (hi !== tuning.seedInfluenceScale) {
        changes.push(`seedInfluenceScale ${tuning.seedInfluenceScale.toFixed(3)}→${hi.toFixed(3)}`);
        tuning.seedInfluenceScale = hi;
    }

    if (
        measureZoneActiveRatio(component, positions, indices, {
            ...zone,
            visionMaskTuning: tuning,
        }) > cap
    ) {
        lo = TOUCH_VISION_MASK_TUNING_RANGES.maskCutoffD2.min;
        hi = tuning.maskCutoffD2;
        for (let i = 0; i < 8; i++) {
            const mid = (lo + hi) / 2;
            const ratio = measureZoneActiveRatio(component, positions, indices, {
                ...zone,
                visionMaskTuning: { ...tuning, maskCutoffD2: mid },
            });
            if (ratio > cap) hi = mid;
            else lo = mid;
        }
        if (hi !== tuning.maskCutoffD2) {
            changes.push(`maskCutoffD2 ${tuning.maskCutoffD2.toFixed(3)}→${hi.toFixed(3)}`);
            tuning.maskCutoffD2 = hi;
        }
    }
}

function expandToTargetCap(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
    tuning: TouchVisionMaskTuning,
    targetCap: number,
    changes: string[],
) {
    let lo: number = tuning.seedInfluenceScale;
    let hi: number = TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE.max;
    for (let i = 0; i < 8; i++) {
        const mid = (lo + hi) / 2;
        const ratio = measureZoneActiveRatio(component, positions, indices, {
            ...zone,
            visionMaskTuning: { ...tuning, seedInfluenceScale: mid },
        });
        if (ratio > targetCap) hi = mid;
        else lo = mid;
    }
    if (lo !== tuning.seedInfluenceScale) {
        changes.push(`seedInfluenceScale ${tuning.seedInfluenceScale.toFixed(3)}→${lo.toFixed(3)}`);
        tuning.seedInfluenceScale = lo;
    }

    if (
        measureZoneActiveRatio(component, positions, indices, {
            ...zone,
            visionMaskTuning: tuning,
        }) < targetCap
    ) {
        lo = tuning.maskCutoffD2;
        hi = TOUCH_VISION_MASK_TUNING_RANGES.maskCutoffD2.max;
        for (let i = 0; i < 8; i++) {
            const mid = (lo + hi) / 2;
            const ratio = measureZoneActiveRatio(component, positions, indices, {
                ...zone,
                visionMaskTuning: { ...tuning, maskCutoffD2: mid },
            });
            if (ratio > targetCap) hi = mid;
            else lo = mid;
        }
        if (lo !== tuning.maskCutoffD2) {
            changes.push(`maskCutoffD2 ${tuning.maskCutoffD2.toFixed(3)}→${lo.toFixed(3)}`);
            tuning.maskCutoffD2 = lo;
        }
    }
}

function measureZoneActiveRatio(
    component: TouchComponentAnalysis,
    positions: Float32Array,
    indices: Uint32Array,
    zone: TouchZoneSpec,
) {
    const masks = buildVertexMasks(component.vertexCount, positions, indices, component, [zone]);
    return (
        countActiveZoneVertices(masks, component.vertexCount, zone.channel) /
        Math.max(component.vertexCount, 1)
    );
}

function allowedVertexMask(
    component: TouchComponentAnalysis,
    indices: Uint32Array,
    vertexCount: number,
) {
    const allowed = new Uint8Array(vertexCount);
    // Prefer full draw ranges so body-conforming cloth (bra, tights) on the same
    // component can receive mask weights. objectMaps are only the clothed/nude
    // detection pair and intentionally omit accessory draw spans.
    const ranges =
        component.drawRanges.length > 0
            ? component.drawRanges
            : component.objectMaps.map((range) => ({
                  firstIndex: range.firstIndex,
                  indexCount: range.indexCount,
              }));

    for (const range of ranges) {
        const end = Math.min(indices.length, range.firstIndex + range.indexCount);
        for (let i = range.firstIndex; i < end; i++) {
            const vertex = indices[i];
            if (vertex < vertexCount) allowed[vertex] = 1;
        }
    }

    if (!allowed.some(Boolean)) {
        for (const range of component.objectMaps) {
            const end = Math.min(indices.length, range.firstIndex + range.indexCount);
            for (let i = range.firstIndex; i < end; i++) {
                const vertex = indices[i];
                if (vertex < vertexCount) allowed[vertex] = 1;
            }
        }
    }

    if (!allowed.some(Boolean)) allowed.fill(1);
    return allowed;
}

function smoothMasks(masks: Float32Array, vertexCount: number, indices: Uint32Array) {
    const adjacency = Array.from({ length: vertexCount }, () => new Set<number>());
    for (let i = 0; i + 2 < indices.length; i += 3) {
        const a = indices[i];
        const b = indices[i + 1];
        const c = indices[i + 2];
        adjacency[a]?.add(b);
        adjacency[a]?.add(c);
        adjacency[b]?.add(a);
        adjacency[b]?.add(c);
        adjacency[c]?.add(a);
        adjacency[c]?.add(b);
    }

    const next = new Float32Array(masks);
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const neighbors = adjacency[vertex];
        if (!neighbors || neighbors.size === 0) continue;
        for (let channel = 0; channel < TOUCH_ZONE_CHANNELS; channel++) {
            let sum = masks[vertex * TOUCH_ZONE_CHANNELS + channel];
            let count = 1;
            for (const neighbor of neighbors) {
                sum += masks[neighbor * TOUCH_ZONE_CHANNELS + channel];
                count += 1;
            }
            next[vertex * TOUCH_ZONE_CHANNELS + channel] = sum / count;
        }
    }
    masks.set(next);
}

function clampMasks(masks: Float32Array) {
    for (let i = 0; i < masks.length; i++) {
        const value = masks[i];
        if (!Number.isFinite(value) || value < 0) masks[i] = 0;
        else if (value > 1) masks[i] = 1;
    }
}

function smoothstep(edge0: number, edge1: number, value: number) {
    const normalized = Math.max(0, Math.min(1, (value - edge0) / (edge1 - edge0)));
    return normalized * normalized * (3 - 2 * normalized);
}

function computeBounds(positions: Float32Array, allowed: Uint8Array) {
    const min: [number, number, number] = [
        Number.POSITIVE_INFINITY,
        Number.POSITIVE_INFINITY,
        Number.POSITIVE_INFINITY,
    ];
    const max: [number, number, number] = [
        Number.NEGATIVE_INFINITY,
        Number.NEGATIVE_INFINITY,
        Number.NEGATIVE_INFINITY,
    ];
    for (let i = 0; i < allowed.length; i++) {
        if (!allowed[i]) continue;
        const x = positions[i * 3];
        const y = positions[i * 3 + 1];
        const z = positions[i * 3 + 2];
        min[0] = Math.min(min[0], x);
        min[1] = Math.min(min[1], y);
        min[2] = Math.min(min[2], z);
        max[0] = Math.max(max[0], x);
        max[1] = Math.max(max[1], y);
        max[2] = Math.max(max[2], z);
    }
    if (!Number.isFinite(min[0])) {
        return {
            min: [-1, -1, -1] as [number, number, number],
            max: [1, 1, 1] as [number, number, number],
        };
    }
    return { min, max };
}

function polygonSetContains(polygons: Array<Array<[number, number]>>, point: [number, number]) {
    return polygons.some((polygon) => pointInPolygon(point, polygon));
}

function pointInPolygon(point: [number, number], polygon: Array<[number, number]>) {
    if (polygon.length < 3) return false;
    let inside = false;
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
        const xi = polygon[i][0];
        const yi = polygon[i][1];
        const xj = polygon[j][0];
        const yj = polygon[j][1];
        const intersect =
            yi > point[1] !== yj > point[1] &&
            point[0] < ((xj - xi) * (point[1] - yi)) / (yj - yi + Number.EPSILON) + xi;
        if (intersect) inside = !inside;
    }
    return inside;
}

function resolveZoneChannel(
    id: string,
    label: string,
    index: number,
    kind: TouchComponentAnalysis["kind"],
) {
    const text = `${id} ${label}`.toLowerCase();
    if (text.includes("left") && (text.includes("breast") || text.includes("chest"))) return 0;
    if (text.includes("right") && (text.includes("breast") || text.includes("chest"))) return 1;
    if (
        text.includes("left") &&
        (text.includes("butt") || text.includes("hip") || text.includes("thigh"))
    ) {
        return 2;
    }
    if (
        text.includes("right") &&
        (text.includes("butt") || text.includes("hip") || text.includes("thigh"))
    ) {
        return 3;
    }
    if (kind === "legs") return Math.min(2 + (index % 2), TOUCH_ZONE_CHANNELS - 1);
    return Math.min(index, TOUCH_ZONE_CHANNELS - 1);
}

async function writeMaskPreview(
    filePath: string,
    positions: Float32Array,
    masks: Float32Array,
    title: string,
) {
    const size = 768;
    const png = new PNG({ width: size, height: size });
    png.data.fill(0);
    for (let i = 3; i < png.data.length; i += 4) png.data[i] = 255;

    const vertexCount = positions.length / 3;
    let minX = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let minZ = Number.POSITIVE_INFINITY;
    let maxZ = Number.NEGATIVE_INFINITY;
    for (let i = 0; i < vertexCount; i++) {
        const x = positions[i * 3];
        const z = positions[i * 3 + 2];
        minX = Math.min(minX, x);
        maxX = Math.max(maxX, x);
        minZ = Math.min(minZ, z);
        maxZ = Math.max(maxZ, z);
    }

    const scale = Math.min(
        (size - 40) / Math.max(maxX - minX, 1e-6),
        (size - 40) / Math.max(maxZ - minZ, 1e-6),
    );
    const centerX = (minX + maxX) * 0.5;
    const centerZ = (minZ + maxZ) * 0.5;
    const palette = [
        [255, 70, 70],
        [70, 130, 255],
        [255, 190, 50],
        [180, 70, 255],
    ];

    for (let i = 0; i < vertexCount; i++) {
        const x = Math.round((positions[i * 3] - centerX) * scale + size / 2);
        const y = Math.round(size / 2 - (positions[i * 3 + 2] - centerZ) * scale);
        if (x < 0 || y < 0 || x >= size || y >= size) continue;

        let bestChannel = 0;
        let bestWeight = 0;
        for (let channel = 0; channel < 4; channel++) {
            const weight = masks[i * TOUCH_ZONE_CHANNELS + channel];
            if (weight > bestWeight) {
                bestWeight = weight;
                bestChannel = channel;
            }
        }

        const offset = (size * y + x) << 2;
        if (bestWeight <= 0) {
            png.data[offset] = 45;
            png.data[offset + 1] = 45;
            png.data[offset + 2] = 50;
        } else {
            const color = palette[bestChannel] ?? palette[0];
            const gain = 0.25 + 0.75 * bestWeight;
            png.data[offset] = Math.min(255, Math.round(color[0] * gain));
            png.data[offset + 1] = Math.min(255, Math.round(color[1] * gain));
            png.data[offset + 2] = Math.min(255, Math.round(color[2] * gain));
        }
        png.data[offset + 3] = 255;
    }

    // Title is omitted from pixels; filename carries identity.
    void title;
    await fse.writeFile(filePath, PNG.sync.write(png));
}

import path from "node:path";

import { prepareTextureForMaterial } from "@native/static-glb";
import type {
    TextureVariant,
    ViewerMesh,
    ViewerShapeTarget,
    ViewerTexture,
    ViewerTextureRole,
} from "@shared/mod-viewer/types";
import fse from "fs-extra";

import type { DrawGroup, DrawRecord } from "./draw-groups";
import type { ShapeSlider } from "./shapes";

import { DNF_TRUE, isUnconstrained } from "./dnf";
import { safeResourcePath } from "./ini";

const POSITION_STRIDE = 40;
const POSITION_OFFSET = 0;
const INDEX_SIZE = 4;
const MAX_DRAWS = 10_000;
const MAX_BUFFER_FILE_BYTES = 512 * 1024 * 1024;
const MAX_TOTAL_BUFFER_BYTES = 2 * 1024 * 1024 * 1024;
const MIN_AXIS_SPREAD = 1e-4;
const MIN_IN_RANGE = 0.95;

export type MeshBuildResult = {
    meshes: ViewerMesh[];
    textures: Record<string, ViewerTexture>;
};

export async function buildMeshResult(
    groups: DrawGroup[],
    modDir: string,
): Promise<MeshBuildResult> {
    const meshes: ViewerMesh[] = [];
    const textures: Record<string, ViewerTexture> = {};
    const ibCache = new Map<string, Buffer>();
    const rawBufCache = new Map<string, Buffer>();
    const bufCache = new Map<
        string,
        {
            posData: Buffer;
            posStride: number;
            tcData: Buffer;
            tcStride: number;
            uvOff: number;
            uvFmt: "f16" | "f32";
        }
    >();
    const sparseShapeCache = new Map<string, Map<number, [number, number, number]>>();
    let totalBufferBytes = 0;

    const readBuffer = async (filePath: string) => {
        const size = (await fse.stat(filePath)).size;
        if (size > MAX_BUFFER_FILE_BYTES) {
            throw new Error(`Buffer file is too large (${(size / 1048576).toFixed(1)} MiB).`);
        }
        if (totalBufferBytes + size > MAX_TOTAL_BUFFER_BYTES) {
            throw new Error("Mod buffer data exceeds the 2 GiB safety limit.");
        }
        const data = await fse.readFile(filePath);
        totalBufferBytes += data.length;
        return data;
    };

    const drawTotal = groups.reduce((sum, group) => sum + group.draws.length, 0);
    if (drawTotal > MAX_DRAWS) {
        throw new Error(`Mod has too many draws (${drawTotal}; limit ${MAX_DRAWS}).`);
    }

    const loadBuf = async (
        posPath: string,
        posStride: number,
        tcPath: string,
        tcStride: number,
    ) => {
        const key = `${posPath}|${posStride}|${tcPath}|${tcStride}`;
        const cached = bufCache.get(key);
        if (cached) {
            return cached;
        }
        if (!rawBufCache.has(posPath)) {
            rawBufCache.set(posPath, await readBuffer(posPath));
        }
        if (!rawBufCache.has(tcPath)) {
            rawBufCache.set(tcPath, await readBuffer(tcPath));
        }
        const posData = rawBufCache.get(posPath)!;
        const tcData = rawBufCache.get(tcPath)!;
        const [uvOff, uvFmt] = detectUvBest(tcData, tcStride);
        const entry = { posData, posStride, tcData, tcStride, uvOff, uvFmt };
        bufCache.set(key, entry);
        return entry;
    };

    for (const group of groups) {
        const posPath = safeResourcePath(modDir, group.positionFile);
        const tcPath = safeResourcePath(modDir, group.texcoordFile);
        const ibPath = safeResourcePath(modDir, group.ibFile);
        if (
            !posPath ||
            !tcPath ||
            !ibPath ||
            !(await fse.pathExists(posPath)) ||
            !(await fse.pathExists(tcPath)) ||
            !(await fse.pathExists(ibPath))
        ) {
            continue;
        }

        const buffers = await loadBuf(posPath, group.positionStride, tcPath, group.texcoordStride);
        if (!ibCache.has(ibPath)) {
            ibCache.set(ibPath, await readBuffer(ibPath));
        }
        const unique = deduplicateDraws(group);

        const texKeyFor = (relative: string | undefined, role: ViewerTextureRole = "diffuse") => {
            if (!relative) {
                return null;
            }
            const resolved = safeResourcePath(modDir, relative);
            if (!resolved) {
                return null;
            }
            return textureKey(path.relative(modDir, resolved).replaceAll("\\", "/"), role);
        };

        const ensureTexture = async (
            relative: string | undefined,
            role: ViewerTextureRole = "diffuse",
        ) => {
            const resolved = safeResourcePath(modDir, relative);
            if (!resolved || !(await fse.pathExists(resolved))) {
                return null;
            }
            const key = textureKey(path.relative(modDir, resolved).replaceAll("\\", "/"), role);
            if (!textures[key]) {
                const encoded = await encodeTexture(resolved, role);
                if (encoded) {
                    textures[key] = {
                        texKey: key,
                        role,
                        bytes: encoded.bytes,
                        mimeType: encoded.mimeType,
                        relativePath: path.relative(modDir, resolved).replaceAll("\\", "/"),
                    };
                }
            }
            return key;
        };

        for (const draw of unique) {
            let drawIbPath = ibPath;
            if (draw.ibFile) {
                const nextIb = safeResourcePath(modDir, draw.ibFile);
                if (!nextIb || !(await fse.pathExists(nextIb))) {
                    continue;
                }
                drawIbPath = nextIb;
                if (!ibCache.has(drawIbPath)) {
                    ibCache.set(drawIbPath, await readBuffer(drawIbPath));
                }
            }
            if (draw.start < 0 || (draw.count !== null && draw.count < 0) || draw.base < 0) {
                continue;
            }
            const raw = readIndices(
                ibCache.get(drawIbPath)!,
                draw.start,
                draw.count,
                draw.indexSize ?? group.indexSize ?? INDEX_SIZE,
            );
            if (raw.length === 0) {
                continue;
            }
            const base = draw.base || 0;
            const shifted = base ? raw.map((value) => value + base) : raw;
            const used = [...new Set(shifted)].sort((left, right) => left - right);
            if (used.length === 0 || used[0] < 0) {
                continue;
            }
            const remap = new Map(used.map((value, index) => [value, index]));

            let drawBuffers = buffers;
            let effectivePosPath = posPath;
            if (draw.positionFile && draw.texcoordFile) {
                const drawPosPath = safeResourcePath(modDir, draw.positionFile);
                const drawTcPath = safeResourcePath(modDir, draw.texcoordFile);
                if (
                    !drawPosPath ||
                    !drawTcPath ||
                    !(await fse.pathExists(drawPosPath)) ||
                    !(await fse.pathExists(drawTcPath))
                ) {
                    continue;
                }
                drawBuffers = await loadBuf(
                    drawPosPath,
                    draw.positionStride ?? group.positionStride,
                    drawTcPath,
                    draw.texcoordStride ?? group.texcoordStride,
                );
                effectivePosPath = drawPosPath;
            }

            const positions = new Float32Array(used.length * 3);
            const uvs = new Float32Array(used.length * 2);
            const uvSize = drawBuffers.uvFmt === "f32" ? 8 : 4;
            const shapeBuffers = await buildShapeBuffers(
                group.shapeSliders,
                modDir,
                effectivePosPath,
                used,
                rawBufCache,
                sparseShapeCache,
                readBuffer,
            );

            for (const [outIndex, vertex] of used.entries()) {
                const posOff = vertex * drawBuffers.posStride + POSITION_OFFSET;
                const x =
                    posOff >= 0 && posOff + 12 <= drawBuffers.posData.length
                        ? drawBuffers.posData.readFloatLE(posOff)
                        : 0;
                const y =
                    posOff >= 0 && posOff + 12 <= drawBuffers.posData.length
                        ? drawBuffers.posData.readFloatLE(posOff + 4)
                        : 0;
                const z =
                    posOff >= 0 && posOff + 12 <= drawBuffers.posData.length
                        ? drawBuffers.posData.readFloatLE(posOff + 8)
                        : 0;
                positions[outIndex * 3] = x;
                positions[outIndex * 3 + 1] = y;
                positions[outIndex * 3 + 2] = z;
                for (const shape of shapeBuffers) {
                    if (shape.sparse) {
                        const delta = shape.sparse.get(vertex) ?? [0, 0, 0];
                        shape.positions[outIndex * 3] = x + delta[0];
                        shape.positions[outIndex * 3 + 1] = y + delta[1];
                        shape.positions[outIndex * 3 + 2] = z + delta[2];
                    } else {
                        const targetOff = vertex * shape.stride + POSITION_OFFSET;
                        shape.positions[outIndex * 3] =
                            targetOff >= 0 && targetOff + 12 <= shape.data.length
                                ? shape.data.readFloatLE(targetOff)
                                : x;
                        shape.positions[outIndex * 3 + 1] =
                            targetOff >= 0 && targetOff + 12 <= shape.data.length
                                ? shape.data.readFloatLE(targetOff + 4)
                                : y;
                        shape.positions[outIndex * 3 + 2] =
                            targetOff >= 0 && targetOff + 12 <= shape.data.length
                                ? shape.data.readFloatLE(targetOff + 8)
                                : z;
                    }
                    if (shape.lowData && shape.lowPositions) {
                        const lowOff = vertex * shape.stride + POSITION_OFFSET;
                        shape.lowPositions[outIndex * 3] =
                            lowOff >= 0 && lowOff + 12 <= shape.lowData.length
                                ? shape.lowData.readFloatLE(lowOff)
                                : x;
                        shape.lowPositions[outIndex * 3 + 1] =
                            lowOff >= 0 && lowOff + 12 <= shape.lowData.length
                                ? shape.lowData.readFloatLE(lowOff + 4)
                                : y;
                        shape.lowPositions[outIndex * 3 + 2] =
                            lowOff >= 0 && lowOff + 12 <= shape.lowData.length
                                ? shape.lowData.readFloatLE(lowOff + 8)
                                : z;
                    }
                }
                const tcOff = vertex * drawBuffers.tcStride + drawBuffers.uvOff;
                if (tcOff >= 0 && tcOff + uvSize <= drawBuffers.tcData.length) {
                    const u =
                        drawBuffers.uvFmt === "f32"
                            ? drawBuffers.tcData.readFloatLE(tcOff)
                            : float16(drawBuffers.tcData.readUInt16LE(tcOff));
                    const v =
                        drawBuffers.uvFmt === "f32"
                            ? drawBuffers.tcData.readFloatLE(tcOff + 4)
                            : float16(drawBuffers.tcData.readUInt16LE(tcOff + 2));
                    uvs[outIndex * 2] = u;
                    uvs[outIndex * 2 + 1] = 1 - v;
                }
            }

            const indices = Uint32Array.from(shifted, (value) => remap.get(value) ?? 0);
            const texKey = await ensureTexture(draw.textureDefaultFile);
            const shapeTargets: ViewerShapeTarget[] = shapeBuffers.map((shape) => ({
                var: shape.slider.var,
                positions: shape.positions,
                mode: shape.slider.mode,
                lowPositions: shape.lowPositions,
            }));
            const variantCache = new Map<string, Float32Array>();
            variantCache.set(`${effectivePosPath}|${drawBuffers.posStride}`, positions);
            const positionVariants: ViewerMesh["positionVariants"] = [];
            for (const variant of draw.positionVariants ?? []) {
                const variantPath = safeResourcePath(modDir, variant.file);
                if (!variantPath || !(await fse.pathExists(variantPath))) {
                    continue;
                }
                const stride = variant.stride ?? group.positionStride;
                const cacheKey = `${variantPath}|${stride}`;
                let variantPositions = variantCache.get(cacheKey);
                if (!variantPositions) {
                    if (!rawBufCache.has(variantPath)) {
                        rawBufCache.set(variantPath, await readBuffer(variantPath));
                    }
                    const data = rawBufCache.get(variantPath)!;
                    variantPositions = new Float32Array(used.length * 3);
                    for (const [outIndex, vertex] of used.entries()) {
                        const posOff = vertex * stride + POSITION_OFFSET;
                        variantPositions[outIndex * 3] =
                            posOff >= 0 && posOff + 12 <= data.length
                                ? data.readFloatLE(posOff)
                                : 0;
                        variantPositions[outIndex * 3 + 1] =
                            posOff >= 0 && posOff + 12 <= data.length
                                ? data.readFloatLE(posOff + 4)
                                : 0;
                        variantPositions[outIndex * 3 + 2] =
                            posOff >= 0 && posOff + 12 <= data.length
                                ? data.readFloatLE(posOff + 8)
                                : 0;
                    }
                    variantCache.set(cacheKey, variantPositions);
                }
                positionVariants.push({
                    conditions: variant.conditions,
                    positions: variantPositions,
                });
            }

            const textureRules = draw.textureAssignments ?? draw.textureVariants ?? [];
            const textureVariants: TextureVariant[] = [];
            for (const variant of textureRules) {
                const key = await ensureTexture(variant.file);
                if (key) {
                    textureVariants.push({ conditions: variant.conditions, texKey: key });
                }
            }

            const aux = async (
                channel: ViewerTextureRole,
                defaultFile?: string,
                variants?: DrawRecord["normalMapVariants"],
            ) => {
                const key = await ensureTexture(defaultFile, channel);
                const resolved: TextureVariant[] = [];
                for (const variant of variants ?? []) {
                    const variantKey = await ensureTexture(variant.file, channel);
                    if (variantKey) {
                        resolved.push({ conditions: variant.conditions, texKey: variantKey });
                    }
                }
                return { key, variants: resolved };
            };
            const normal = await aux(
                "normal_map",
                draw.normalMapDefaultFile,
                draw.normalMapVariants,
            );
            const light = await aux("light_map", draw.lightMapDefaultFile, draw.lightMapVariants);
            const material = await aux(
                "material_map",
                draw.materialMapDefaultFile,
                draw.materialMapVariants,
            );

            meshes.push({
                id: draw.label,
                component: group.displayName || group.name,
                positions,
                uvs,
                indices,
                conditions: draw.conditions,
                texKey: texKey ?? texKeyFor(draw.textureDefaultFile),
                textureVariants,
                normalMapKey: normal.key,
                normalMapVariants: normal.variants,
                lightMapKey: light.key,
                lightMapVariants: light.variants,
                materialMapKey: material.key,
                materialMapVariants: material.variants,
                shapeTargets,
                positionVariants,
            });
        }
    }

    return {
        meshes: meshes.map((mesh) => ({
            ...mesh,
            textureVariants:
                mesh.textureVariants.length > 1 ||
                (mesh.textureVariants[0] && !isUnconstrained(mesh.textureVariants[0].conditions))
                    ? mesh.textureVariants
                    : [],
        })),
        textures,
    };
}

function textureKey(relativePath: string, role: ViewerTextureRole): string {
    return `${role}::${relativePath.replaceAll("\\", "/")}`;
}

function deduplicateDraws(group: DrawGroup): DrawRecord[] {
    const merged = new Map<
        string,
        { draw: DrawRecord; alts: typeof group.draws; sources: DrawRecord["sources"] }
    >();
    const order: string[] = [];
    for (const draw of group.draws) {
        const key = `${draw.ibFile ?? ""}|${draw.positionFile ?? ""}|${draw.texcoordFile ?? ""}|${draw.start}|${draw.count}`;
        if (!merged.has(key)) {
            merged.set(key, { draw, alts: [], sources: [] });
            order.push(key);
        }
        const entry = merged.get(key)!;
        for (const source of draw.sources) {
            if (!entry.sources.includes(source)) {
                entry.sources.push(source);
            }
        }
        const condGroups = draw.conditions ?? DNF_TRUE;
        if (isUnconstrained(condGroups)) {
            if (!entry.alts.some((alt) => isUnconstrained(alt.conditions))) {
                entry.alts.push(draw);
            }
        } else {
            entry.alts.push(draw);
        }
    }
    return order.map((key) => {
        const entry = merged.get(key)!;
        const alternatives = entry.alts.flatMap((draw) =>
            isUnconstrained(draw.conditions) ? [[]] : draw.conditions,
        );
        entry.draw.conditions = alternatives.some((group) => group.length === 0)
            ? DNF_TRUE
            : alternatives;
        entry.draw.sources = entry.sources;
        return entry.draw;
    });
}

function readIndices(
    data: Buffer,
    startIndex: number,
    count: number | null,
    indexSize: number,
): number[] {
    if (startIndex < 0 || (count !== null && count < 0) || indexSize <= 0) {
        return [];
    }
    const total = Math.floor(data.length / indexSize);
    const length = count === null ? total - startIndex : count;
    const end = Math.min(startIndex + length, total);
    if (end <= startIndex) {
        return [];
    }
    const values: number[] = [];
    for (let index = startIndex; index < end; index++) {
        values.push(indexSize === 2 ? data.readUInt16LE(index * 2) : data.readUInt32LE(index * 4));
    }
    return values;
}

function detectUvBest(data: Buffer, stride: number): [number, "f16" | "f32"] {
    const total = stride ? Math.floor(data.length / stride) : 0;
    if (!total) {
        return [4, "f16"];
    }
    const step = Math.max(1, Math.floor(total / 4096));
    const scored: Array<{
        bothLive: boolean;
        inRange: number;
        spread: number;
        off: number;
        fmt: "f16" | "f32";
    }> = [];
    for (const uvOff of [0, 4]) {
        for (const fmt of ["f16", "f32"] as const) {
            const fmtSize = fmt === "f32" ? 8 : 4;
            if (uvOff + fmtSize > stride) {
                continue;
            }
            const us: number[] = [];
            const vs: number[] = [];
            let sampled = 0;
            for (let vertex = 0; vertex < total; vertex += step) {
                const off = vertex * stride + uvOff;
                if (off + fmtSize > data.length) {
                    break;
                }
                sampled += 1;
                const u = fmt === "f32" ? data.readFloatLE(off) : float16(data.readUInt16LE(off));
                const v =
                    fmt === "f32" ? data.readFloatLE(off + 4) : float16(data.readUInt16LE(off + 2));
                if (u >= -0.01 && u <= 2 && v >= -0.01 && v <= 2) {
                    us.push(u);
                    vs.push(v);
                }
            }
            if (!sampled || us.length === 0) {
                continue;
            }
            const inRange = us.length / sampled;
            if (inRange < MIN_IN_RANGE) {
                continue;
            }
            const du = Math.max(...us) - Math.min(...us);
            const dv = Math.max(...vs) - Math.min(...vs);
            scored.push({
                bothLive: du >= MIN_AXIS_SPREAD && dv >= MIN_AXIS_SPREAD,
                inRange: Math.round(inRange * 1000) / 1000,
                spread: Math.round((du + dv) * 1000) / 1000,
                off: uvOff,
                fmt,
            });
        }
    }
    scored.sort(
        (left, right) =>
            Number(right.bothLive) - Number(left.bothLive) ||
            right.inRange - left.inRange ||
            right.spread - left.spread,
    );
    if (scored[0]) {
        return [scored[0].off, scored[0].fmt];
    }
    if (4 + 4 <= stride) {
        return [4, "f16"];
    }
    return [0, "f16"];
}

function float16(value: number): number {
    const sign = (value & 0x8000) >> 15;
    const exponent = (value & 0x7c00) >> 10;
    const fraction = value & 0x03ff;
    if (exponent === 0) {
        return (sign ? -1 : 1) * 2 ** -14 * (fraction / 1024);
    }
    if (exponent === 31) {
        return fraction ? Number.NaN : sign ? Number.NEGATIVE_INFINITY : Number.POSITIVE_INFINITY;
    }
    return (sign ? -1 : 1) * 2 ** (exponent - 15) * (1 + fraction / 1024);
}

async function buildShapeBuffers(
    sliders: ShapeSlider[] | undefined,
    modDir: string,
    effectivePosPath: string,
    used: number[],
    rawBufCache: Map<string, Buffer>,
    sparseShapeCache: Map<string, Map<number, [number, number, number]>>,
    readBuffer: (filePath: string) => Promise<Buffer>,
) {
    const result: Array<{
        slider: ShapeSlider;
        data: Buffer;
        positions: Float32Array;
        sparse?: Map<number, [number, number, number]>;
        stride: number;
        lowData?: Buffer;
        lowPositions?: Float32Array;
    }> = [];
    for (const slider of sliders ?? []) {
        const shapeBasePath = safeResourcePath(modDir, slider.baseFile);
        if (
            !shapeBasePath ||
            path.normalize(shapeBasePath).toLowerCase() !==
                path.normalize(effectivePosPath).toLowerCase()
        ) {
            continue;
        }
        if (slider.shapeId !== undefined) {
            const paths = [slider.offsetFile, slider.vertexIdFile, slider.vertexOffsetFile].map(
                (file) => safeResourcePath(modDir, file),
            );
            if (paths.some((entry) => !entry)) {
                continue;
            }
            const [offsetPath, vertexIdPath, vertexOffsetPath] = paths as string[];
            if (
                !(await fse.pathExists(offsetPath)) ||
                !(await fse.pathExists(vertexIdPath)) ||
                !(await fse.pathExists(vertexOffsetPath))
            ) {
                continue;
            }
            const keyId = slider.bufferShapeId ?? slider.shapeId + Math.floor(slider.shapeId / 127);
            const cacheKey = `${offsetPath}|${vertexIdPath}|${vertexOffsetPath}|${keyId}`;
            if (!sparseShapeCache.has(cacheKey)) {
                for (const filePath of [offsetPath, vertexIdPath, vertexOffsetPath]) {
                    if (!rawBufCache.has(filePath)) {
                        rawBufCache.set(filePath, await readBuffer(filePath));
                    }
                }
                const offsets = rawBufCache.get(offsetPath)!;
                const vertexIds = rawBufCache.get(vertexIdPath)!;
                const deltas = rawBufCache.get(vertexOffsetPath)!;
                if ((keyId + 2) * 4 > offsets.length) {
                    continue;
                }
                let begin = offsets.readUInt32LE(keyId * 4) + (slider.sparseEntryOffset ?? 0);
                const end = offsets.readUInt32LE((keyId + 1) * 4) + (slider.sparseEntryOffset ?? 0);
                const limit = Math.min(
                    end,
                    Math.floor(vertexIds.length / 4),
                    Math.floor(deltas.length / 12),
                );
                const sparse = new Map<number, [number, number, number]>();
                for (let index = begin; index < limit; index++) {
                    const vertexId = vertexIds.readUInt32LE(index * 4);
                    const dx = float16(deltas.readUInt16LE(index * 12));
                    const dy = float16(deltas.readUInt16LE(index * 12 + 2));
                    const dz = float16(deltas.readUInt16LE(index * 12 + 4));
                    const prior = sparse.get(vertexId) ?? [0, 0, 0];
                    sparse.set(vertexId, [prior[0] + dx, prior[1] + dy, prior[2] + dz]);
                }
                sparseShapeCache.set(cacheKey, sparse);
            }
            result.push({
                slider,
                data: Buffer.alloc(0),
                positions: new Float32Array(used.length * 3),
                sparse: sparseShapeCache.get(cacheKey),
                stride: slider.stride ?? POSITION_STRIDE,
            });
            continue;
        }
        const targetPath = safeResourcePath(modDir, slider.targetFile);
        if (!targetPath || !(await fse.pathExists(targetPath))) {
            continue;
        }
        if (!rawBufCache.has(targetPath)) {
            rawBufCache.set(targetPath, await readBuffer(targetPath));
        }
        let lowData: Buffer | undefined;
        let lowPositions: Float32Array | undefined;
        if (slider.lowFile) {
            const lowPath = safeResourcePath(modDir, slider.lowFile);
            if (!lowPath || !(await fse.pathExists(lowPath))) {
                continue;
            }
            if (!rawBufCache.has(lowPath)) {
                rawBufCache.set(lowPath, await readBuffer(lowPath));
            }
            lowData = rawBufCache.get(lowPath);
            lowPositions = new Float32Array(used.length * 3);
        }
        result.push({
            slider,
            data: rawBufCache.get(targetPath)!,
            positions: new Float32Array(used.length * 3),
            stride: slider.stride ?? POSITION_STRIDE,
            lowData,
            lowPositions,
        });
    }
    return result;
}

async function encodeTexture(
    filePath: string,
    role: ViewerTextureRole,
): Promise<{ bytes: Buffer; mimeType: "image/png" | "image/jpeg" } | null> {
    const ext = path.extname(filePath).toLowerCase();
    if (ext === ".png") {
        return { bytes: await fse.readFile(filePath), mimeType: "image/png" };
    }
    if (ext === ".jpg" || ext === ".jpeg") {
        return { bytes: await fse.readFile(filePath), mimeType: "image/jpeg" };
    }
    if (ext !== ".dds") {
        return null;
    }

    const prepared = await prepareTextureForMaterial({
        texturePath: filePath,
        resourceName: role,
        textureFormat: "png",
        jpegQuality: 85,
        allowCacheReuse: false,
        cacheDir: "",
    }).catch(() => null);
    if (!prepared?.image) {
        return null;
    }
    return {
        bytes: prepared.image,
        mimeType: prepared.mimeType === "image/jpeg" ? "image/jpeg" : "image/png",
    };
}

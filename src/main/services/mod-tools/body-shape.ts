import path from "node:path";

import type { NahidaDesktop } from "@main/index";
import { loadIniBundle } from "@main/lib/mod-static-glb/ini-loader";
import {
    applySnorm8VectorCorrection,
    DEFAULT_BLEND_STRIDE,
    detectSnorm8VectorLayout,
    extractPositions,
    listBlendBones,
    validateBlendBuffer,
    validatePositionBuffer,
    writePositionsIntoBuffer,
    type BlendBoneInfo,
} from "@shared/body-shape";
import { stripDisabledPrefix } from "@shared/mod";
import fse from "fs-extra";

import {
    collectBlendResources,
    collectIndexResources,
    collectPositionResources,
    collectResources,
    collectVectorResources,
    matchCompanionResource,
    matchIndexResources,
    readIndexBuffer,
    resourceKey,
} from "./mod-buffer-parser";

export type BodyShapeMeshCandidate = {
    id: string;
    name: string;
    positionPath: string;
    positionRelativePath: string;
    positionStride: number;
    vertexCount: number;
    positions: Float32Array;
    indices?: Uint32Array;
    indexPath?: string;
    indexRelativePath?: string;
    vectorPath?: string;
    vectorRelativePath?: string;
    vectorStride?: number;
    vectorLayout?: "snorm8-tangent-normal" | null;
    /** GLB mesh/node names (IB stems) that share this position buffer group. */
    glbMeshNames: string[];
    blendPath?: string;
    blendRelativePath?: string;
    blendStride?: number;
    /** Raw Blend.buf bytes (4 bone indices + 4 weights per vertex). */
    blendBytes?: Uint8Array;
    bones: BlendBoneInfo[];
};

export type BodyShapeLoadResult = {
    modRoot: string;
    iniPath: string;
    meshes: BodyShapeMeshCandidate[];
};

export type BodyShapeExportInput = {
    modRoot: string;
    positionPath: string;
    positionStride: number;
    positions: Float32Array;
    vectorPath?: string;
    vectorLayout?: "snorm8-tangent-normal" | null;
    weights?: Float32Array;
    amount?: number;
    axisScale?: [number, number, number];
    writeChangeLog?: boolean;
    changeSummary?: {
        amount: number;
        axisScale: [number, number, number];
        movedVertices: number;
        maxDisplacement: number;
    };
};

export type BodyShapeExportResult = {
    positionPath: string;
    positionBytes: number;
    vectorPath?: string;
    vectorBytes?: number;
    changeLogPath?: string;
    /** New enabled mod folder created for the body-shaped variant. */
    modRoot?: string;
    /** Source mod path after disable (may be renamed with DISABLED prefix). */
    sourceModPath?: string;
};

export const BODY_SHAPED_SUFFIX = " (Body Shaped)";
const SHADER_FIXES_MOD_MARKER_FILE = ".nahida-shader-fixes.json";

export function bodyShapedFolderBaseName(sourceFolderName: string): string {
    return `${stripDisabledPrefix(sourceFolderName)}${BODY_SHAPED_SUFFIX}`;
}

export class BodyShapeEditor {
    constructor(private readonly desktop: NahidaDesktop) {}

    async loadMod(modPath: string): Promise<BodyShapeLoadResult> {
        return loadBodyShapeMod(modPath, (message) => {
            this.desktop.logger.warn(message, "BodyShapeEditor");
        });
    }

    async exportMesh(input: BodyShapeExportInput): Promise<BodyShapeExportResult> {
        const warn = (message: string) => {
            this.desktop.logger.warn(message, "BodyShapeEditor");
        };

        const sourceRoot = path.resolve(input.modRoot);
        if (!(await fse.pathExists(sourceRoot))) {
            throw new Error(`Mod path does not exist: ${sourceRoot}`);
        }

        const parentPath = path.dirname(sourceRoot);
        const existingFolderNames = await this.desktop.lib.fs.listDirectories(parentPath);
        const targetFolderName = this.desktop.lib.fs.getUniqueName(
            bodyShapedFolderBaseName(path.basename(sourceRoot)),
            existingFolderNames,
        );
        const targetRoot = path.join(parentPath, targetFolderName);

        let copied = false;
        try {
            await fse.copy(sourceRoot, targetRoot);
            copied = true;
            await fse.remove(path.join(targetRoot, SHADER_FIXES_MOD_MARKER_FILE));

            const result = await exportBodyShapeMesh(
                {
                    ...input,
                    modRoot: targetRoot,
                    positionPath: remapPathIntoRoot(input.positionPath, sourceRoot, targetRoot),
                    vectorPath: input.vectorPath
                        ? remapPathIntoRoot(input.vectorPath, sourceRoot, targetRoot)
                        : undefined,
                },
                warn,
            );

            const sourceModPath = await this.desktop.service.mod.fn.disable(sourceRoot);

            return {
                ...result,
                modRoot: targetRoot,
                sourceModPath,
            };
        } catch (error) {
            if (copied && (await fse.pathExists(targetRoot))) {
                try {
                    await fse.remove(targetRoot);
                } catch (cleanupError) {
                    this.desktop.logger.error(
                        cleanupError,
                        `BodyShapeEditor:exportMesh:cleanup:${targetRoot}`,
                    );
                }
            }
            this.desktop.logger.error(error, `BodyShapeEditor:exportMesh:${sourceRoot}`);
            throw error;
        }
    }
}

function remapPathIntoRoot(filePath: string, sourceRoot: string, targetRoot: string): string {
    const absolute = path.resolve(filePath);
    const relative = path.relative(sourceRoot, absolute);
    if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
        throw new Error(`Path is outside mod root: ${filePath}`);
    }
    return path.join(targetRoot, relative);
}

/** Pure-ish load entry used by the service and unit tests. */
export async function loadBodyShapeMod(
    modPath: string,
    warn: (message: string) => void = () => {},
): Promise<BodyShapeLoadResult> {
    const resolved = path.resolve(modPath);
    if (!(await fse.pathExists(resolved))) {
        throw new Error(`Path does not exist: ${resolved}`);
    }

    const { iniPath, sections } = await loadIniBundle(resolved);
    const modRoot = path.dirname(iniPath);
    const resources = collectResources(sections);
    const positionResources = collectPositionResources(resources);
    const indexResources = collectIndexResources(resources);
    const vectorResources = collectVectorResources(resources);
    const blendResources = collectBlendResources(resources);
    const indexResourcesByPosition = matchIndexResources(
        positionResources,
        indexResources,
        sections,
    );

    if (positionResources.length === 0) {
        throw new Error("No position buffer resources found in mod.ini");
    }

    const meshes: BodyShapeMeshCandidate[] = [];

    for (const position of positionResources) {
        if (!position.filename || !position.stride) continue;
        const positionPath = path.resolve(modRoot, position.filename);
        if (!(await fse.pathExists(positionPath))) {
            warn(`Missing position buffer: ${positionPath}`);
            continue;
        }

        const bytes = await fse.readFile(positionPath);
        const validation = validatePositionBuffer(bytes.byteLength, position.stride);
        if (!validation.ok) {
            warn(`Skipping position buffer ${positionPath}: ${validation.reason}`);
            continue;
        }

        const positions = extractPositions(new Uint8Array(bytes), position.stride);
        const indexMatches = indexResourcesByPosition.get(resourceKey(position)) ?? [];
        const vectorMatch = matchCompanionResource(position, vectorResources);
        const blendMatch = matchCompanionResource(position, blendResources);
        const indexData = (
            await Promise.all(
                indexMatches.map(async (index) => {
                    if (!index.filename) return null;
                    const indexPath = path.resolve(modRoot, index.filename);
                    if (!(await fse.pathExists(indexPath))) {
                        warn(`Missing index buffer: ${indexPath}`);
                        return null;
                    }
                    const indices = await readIndexBuffer(indexPath, index.format);
                    if (indices.some((value) => value >= validation.vertexCount)) {
                        warn(
                            `Skipping index buffer ${indexPath}: index exceeds ${validation.vertexCount - 1}`,
                        );
                        return null;
                    }
                    return { indices, indexPath, relativePath: index.filename };
                }),
            )
        ).filter((entry): entry is NonNullable<typeof entry> => !!entry);
        const indices = combineIndexBuffers(indexData.map((entry) => entry.indices));
        const indexPath = indexData[0]?.indexPath;
        const indexRelativePath = indexData[0]?.relativePath;

        let vectorPath: string | undefined;
        let vectorRelativePath: string | undefined;
        let vectorStride: number | undefined;
        let vectorLayout: "snorm8-tangent-normal" | null = null;

        if (vectorMatch?.filename) {
            vectorPath = path.resolve(modRoot, vectorMatch.filename);
            if (await fse.pathExists(vectorPath)) {
                const vectorStat = await fse.stat(vectorPath);
                vectorStride = vectorMatch.stride ?? 8;
                vectorLayout = detectSnorm8VectorLayout(
                    vectorStat.size,
                    validation.vertexCount,
                    vectorStride,
                );
                vectorRelativePath = vectorMatch.filename;
            }
        }

        let blendPath: string | undefined;
        let blendRelativePath: string | undefined;
        let blendStride: number | undefined;
        let blendBytes: Uint8Array | undefined;
        let bones: BlendBoneInfo[] = [];

        if (blendMatch?.filename) {
            blendPath = path.resolve(modRoot, blendMatch.filename);
            if (await fse.pathExists(blendPath)) {
                const raw = await fse.readFile(blendPath);
                blendStride = blendMatch.stride ?? DEFAULT_BLEND_STRIDE;
                const blendValidation = validateBlendBuffer(
                    raw.byteLength,
                    validation.vertexCount,
                    blendStride,
                );
                if (!blendValidation.ok) {
                    warn(`Skipping blend buffer ${blendPath}: ${blendValidation.reason}`);
                    blendPath = undefined;
                    blendStride = undefined;
                } else {
                    blendBytes = new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength);
                    blendRelativePath = blendMatch.filename;
                    bones = listBlendBones(blendBytes, validation.vertexCount, blendStride);
                }
            } else {
                blendPath = undefined;
            }
        }

        meshes.push({
            id: position.name,
            name: position.name,
            positionPath,
            positionRelativePath: position.filename,
            positionStride: position.stride,
            vertexCount: validation.vertexCount,
            positions,
            indices,
            indexPath,
            indexRelativePath,
            vectorPath,
            vectorRelativePath,
            vectorStride,
            vectorLayout,
            glbMeshNames: indexMatches.flatMap((index) =>
                index.filename ? [path.basename(index.filename, path.extname(index.filename))] : [],
            ),
            blendPath,
            blendRelativePath,
            blendStride,
            blendBytes,
            bones,
        });
    }

    if (meshes.length === 0) {
        throw new Error("No readable position buffers found in the selected mod");
    }

    return { modRoot, iniPath, meshes };
}

/**
 * Write-back: only position (and validated SNORM8 vectors when non-uniform scale).
 * Index/Blend/TexCoord/Color stay unchanged.
 */
export async function exportBodyShapeMesh(
    input: BodyShapeExportInput,
    warn: (message: string) => void = () => {},
): Promise<BodyShapeExportResult> {
    const positionPath = path.resolve(input.positionPath);
    if (!(await fse.pathExists(positionPath))) {
        throw new Error(`Position buffer not found: ${positionPath}`);
    }

    const originalBytes = await fse.readFile(positionPath);
    const sizeBefore = originalBytes.byteLength;
    const validation = validatePositionBuffer(
        sizeBefore,
        input.positionStride,
        input.positions.length / 3,
    );
    if (!validation.ok) {
        throw new Error(validation.reason);
    }

    const written = writePositionsIntoBuffer(
        new Uint8Array(originalBytes),
        input.positionStride,
        input.positions,
    );

    if (written.byteLength !== sizeBefore) {
        throw new Error(
            `Refusing to write position buffer: size would change from ${sizeBefore} to ${written.byteLength}`,
        );
    }

    await fse.writeFile(positionPath, Buffer.from(written));

    const result: BodyShapeExportResult = {
        positionPath,
        positionBytes: written.byteLength,
    };

    if (
        input.vectorPath &&
        input.vectorLayout === "snorm8-tangent-normal" &&
        input.weights &&
        input.amount !== undefined &&
        input.axisScale
    ) {
        const vectorPath = path.resolve(input.vectorPath);
        const originalVectors = await fse.readFile(vectorPath);
        if (originalVectors.byteLength !== (input.positions.length / 3) * 8) {
            warn(`Skipping vector rewrite for ${vectorPath}: size mismatch`);
        } else {
            const corrected = applySnorm8VectorCorrection({
                originalVectors: new Int8Array(
                    originalVectors.buffer,
                    originalVectors.byteOffset,
                    originalVectors.byteLength,
                ),
                weights: input.weights,
                amount: input.amount,
                axisScale: input.axisScale,
            });
            if (corrected.byteLength !== originalVectors.byteLength) {
                throw new Error("Vector buffer size would change");
            }
            await fse.writeFile(vectorPath, Buffer.from(corrected));
            result.vectorPath = vectorPath;
            result.vectorBytes = corrected.byteLength;
        }
    }

    if (input.writeChangeLog !== false) {
        const changeLogPath = path.join(path.resolve(input.modRoot), "변경사항.txt");
        const summary = input.changeSummary;
        const lines = [
            "[체형 수정 내역]",
            "",
            `- 수정 대상 파일: ${path.relative(input.modRoot, positionPath) || path.basename(positionPath)}`,
            result.vectorPath
                ? `- 방향 버퍼: ${path.relative(input.modRoot, result.vectorPath) || path.basename(result.vectorPath)}`
                : "- 방향 버퍼: 수정하지 않음 (레이아웃 미검증 또는 균일 스케일)",
            "- 유지한 파일: Index, Blend, UV, Color 및 기타 원본 파일",
            "- 변경 방식: 본/부위 가중치 + 피벗 기준 축별 스케일 (원본 정점 기준 재계산)",
        ];
        if (summary) {
            lines.push(
                `- 강도: ${summary.amount}`,
                `- 축 스케일: X=${summary.axisScale[0]}, Y=${summary.axisScale[1]}, Z=${summary.axisScale[2]}`,
                `- 이동 정점 수: ${summary.movedVertices}`,
                `- 최대 이동 거리: ${summary.maxDisplacement.toFixed(6)}`,
            );
        }
        lines.push("- 참고: 실제 애니메이션과 모든 의상 조합에서 미세 클리핑이 발생할 수 있음", "");
        await fse.writeFile(changeLogPath, lines.join("\n"), "utf8");
        result.changeLogPath = changeLogPath;
    }

    return result;
}

function combineIndexBuffers(buffers: Uint32Array[]): Uint32Array | undefined {
    if (buffers.length === 0) return undefined;
    if (buffers.length === 1) return buffers[0];

    const indices = new Uint32Array(buffers.reduce((length, buffer) => length + buffer.length, 0));
    buffers.reduce((offset, buffer) => {
        indices.set(buffer, offset);
        return offset + buffer.length;
    }, 0);
    return indices;
}

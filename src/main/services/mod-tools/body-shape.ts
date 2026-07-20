import path from "node:path";

import type { NahidaDesktop } from "@main/index";
import { loadIniBundle } from "@main/lib/mod-static-glb/ini-loader";
import type { IniSection, Resource } from "@main/lib/mod-static-glb/types";
import {
    applySnorm8VectorCorrection,
    detectSnorm8VectorLayout,
    extractPositions,
    validatePositionBuffer,
    writePositionsIntoBuffer,
} from "@shared/body-shape";
import fse from "fs-extra";

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
};

export class BodyShapeEditor {
    constructor(private readonly desktop: NahidaDesktop) {}

    async loadMod(modPath: string): Promise<BodyShapeLoadResult> {
        return loadBodyShapeMod(modPath, (message) => {
            this.desktop.logger.warn(message, "BodyShapeEditor");
        });
    }

    async exportMesh(input: BodyShapeExportInput): Promise<BodyShapeExportResult> {
        return exportBodyShapeMesh(input, (message) => {
            this.desktop.logger.warn(message, "BodyShapeEditor");
        });
    }
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
        const indexMatch = matchCompanionResource(position, indexResources);
        const vectorMatch = matchCompanionResource(position, vectorResources);

        let indices: Uint32Array | undefined;
        let indexPath: string | undefined;
        let indexRelativePath: string | undefined;

        if (indexMatch?.filename) {
            indexPath = path.resolve(modRoot, indexMatch.filename);
            if (await fse.pathExists(indexPath)) {
                indices = await readIndexBuffer(indexPath, indexMatch.format);
                indexRelativePath = indexMatch.filename;
            }
        }

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
            "- 변경 방식: 사용자 페인트 가중치 + 피벗 기준 축별 스케일 (원본 정점 기준 재계산)",
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

function collectResources(sections: IniSection[]): Resource[] {
    return sections
        .filter((section) => section.header === "Resource")
        .map((section) => ({
            name: section.name,
            filename: section.values.filename,
            stride: section.values.stride ? Number(section.values.stride) : undefined,
            format: section.values.format,
            values: section.values,
        }));
}

function collectPositionResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename || !resource.stride) return false;
        if (/position/i.test(resource.name) && resource.stride >= 12) return true;
        return false;
    });
}

function collectIndexResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename) return false;
        if (/index/i.test(resource.name)) return true;
        if (resource.format && /R\d+_UINT/i.test(resource.format)) return true;
        return false;
    });
}

function collectVectorResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename) return false;
        return /vector/i.test(resource.name);
    });
}

function matchCompanionResource(position: Resource, candidates: Resource[]): Resource | undefined {
    const positionKey = companionKey(position.name);
    const exact = candidates.find((candidate) => companionKey(candidate.name) === positionKey);
    if (exact) return exact;
    if (candidates.length === 1) return candidates[0];
    return undefined;
}

function companionKey(name: string): string {
    return name
        .replace(/(Position|Vector|Index|Blend|TexCoord|Color)Buffer/gi, "")
        .replace(/(Position|Vector|Index|Blend|Texcoord)/gi, "")
        .toLowerCase();
}

async function readIndexBuffer(filePath: string, format?: string): Promise<Uint32Array> {
    const bytes = await fse.readFile(filePath);
    const use16 =
        format?.toUpperCase().includes("R16") ||
        (bytes.byteLength % 4 !== 0 && bytes.byteLength % 2 === 0);

    if (use16) {
        const src = new Uint16Array(
            bytes.buffer,
            bytes.byteOffset,
            Math.floor(bytes.byteLength / 2),
        );
        return Uint32Array.from(src);
    }

    return new Uint32Array(bytes.buffer, bytes.byteOffset, Math.floor(bytes.byteLength / 4));
}

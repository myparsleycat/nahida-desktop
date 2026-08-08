import crypto from "node:crypto";
import path from "node:path";

import { loadIniBundle } from "@main/lib/mod-static-glb/ini-loader";
import type { IniSection, Resource } from "@main/lib/mod-static-glb/types";
import {
    DEFAULT_BLEND_STRIDE,
    extractPositions,
    listBlendBones,
    validateBlendBuffer,
    validatePositionBuffer,
    type BlendBoneInfo,
} from "@shared/body-shape";
import fse from "fs-extra";

import {
    collectBlendResources,
    collectCommandLists,
    collectIndexResources,
    collectPositionResources,
    collectResources,
    combineIndexBuffers,
    expandCommandListLines,
    matchCompanionResource,
    matchIndexResources,
    readIndexBuffer,
    resourceForReference,
    resourceKey,
    sectionValueFromLines,
} from "./mod-buffer-parser";
import {
    assertTouchProfileDetectionAllowed,
    inspectTouchProfileBundle,
} from "./touch-profile-detection";
import {
    TOUCH_OBJECT_MODE,
    TOUCH_POSITION_STRIDE,
    type TouchComponentAnalysis,
    type TouchComponentKind,
    type TouchDrawRange,
    type TouchModAnalysis,
    type TouchObjectMapEntry,
    type TouchSupportGrade,
} from "./touch-profile-types";

export async function analyzeTouchMod(
    modPath: string,
    warn: (message: string) => void = () => {},
): Promise<TouchModAnalysis> {
    const resolved = path.resolve(modPath);
    if (!(await fse.pathExists(resolved))) {
        throw new Error(`Path does not exist: ${resolved}`);
    }

    const { iniPath, sections, sourcePaths } = await loadIniBundle(resolved);
    const modRoot = path.dirname(iniPath);
    assertTouchProfileDetectionAllowed(
        resolved,
        await inspectTouchProfileBundle(modRoot, sourcePaths),
    );
    const resources = collectResources(sections);
    const positionResources = collectPositionResources(resources);
    const indexResources = collectIndexResources(resources);
    const blendResources = collectBlendResources(resources);
    const commandLists = collectCommandLists(sections);
    const indexByPosition = matchIndexResources(
        positionResources,
        indexResources,
        sections,
        commandLists,
    );

    if (positionResources.length === 0) {
        throw new Error("No position buffer resources found in mod.ini");
    }

    const components: TouchComponentAnalysis[] = [];

    for (const position of positionResources) {
        if (!position.filename || !position.stride) continue;

        const positionPath = path.resolve(modRoot, position.filename);
        if (!(await fse.pathExists(positionPath))) {
            warn(`Missing position buffer: ${positionPath}`);
            continue;
        }

        const positionBytes = await fse.readFile(positionPath);
        const validation = validatePositionBuffer(positionBytes.byteLength, position.stride);
        if (!validation.ok) {
            warn(`Skipping position buffer ${positionPath}: ${validation.reason}`);
            continue;
        }

        const indexMatches = indexByPosition.get(resourceKey(position)) ?? [];
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
                    return {
                        resource: index,
                        indices,
                        indexPath,
                        relativePath: index.filename,
                    };
                }),
            )
        ).filter((entry): entry is NonNullable<typeof entry> => !!entry);

        if (indexData.length === 0) {
            warn(`No usable index buffer for ${position.name}`);
            continue;
        }

        const combinedIndices = combineIndexBuffers(indexData.map((entry) => entry.indices));
        const primaryIndex = indexData[0];
        const indexPaths = indexData.map((entry) => entry.indexPath);
        const indexRelativePaths = indexData.map((entry) => entry.relativePath);
        const indexFormats = indexData.map((entry) => entry.resource.format);
        let combinedOffset = 0;
        const indexInfos = indexData.map((entry) => {
            const info = {
                resource: entry.resource,
                offset: combinedOffset,
                count: entry.indices.length,
            };
            combinedOffset += entry.indices.length;
            return info;
        });
        const drawContext = findDrawContext(
            sections,
            commandLists,
            position,
            indexInfos,
            combinedIndices.length,
        );
        const drawRanges = uniqueDrawRanges(drawContext.ranges);
        const kind = classifyComponentKind(
            position.name,
            primaryIndex.resource.name,
            drawContext.blendSectionName,
            drawContext.ibSectionName,
        );
        const support = gradeComponent({
            positionStride: position.stride,
            positionBytes,
            vertexCount: validation.vertexCount,
            indices: combinedIndices,
            drawRanges,
            kind,
        });
        const interactiveCandidate = isInteractiveCandidate(
            kind,
            validation.vertexCount,
            combinedIndices.length,
            position.name,
        );

        const positions = extractPositions(new Uint8Array(positionBytes), position.stride);
        const blendMatch = matchCompanionResource(position, blendResources);
        let blendRelativePath: string | undefined;
        let blendPath: string | undefined;
        let blendStride: number | undefined;
        let bones: BlendBoneInfo[] = [];
        if (blendMatch?.filename) {
            const resolvedBlendPath = path.resolve(modRoot, blendMatch.filename);
            if (await fse.pathExists(resolvedBlendPath)) {
                const raw = await fse.readFile(resolvedBlendPath);
                const resolvedStride = blendMatch.stride ?? DEFAULT_BLEND_STRIDE;
                const blendValidation = validateBlendBuffer(
                    raw.byteLength,
                    validation.vertexCount,
                    resolvedStride,
                );
                if (!blendValidation.ok) {
                    warn(`Skipping blend buffer ${resolvedBlendPath}: ${blendValidation.reason}`);
                } else {
                    blendRelativePath = blendMatch.filename;
                    blendPath = resolvedBlendPath;
                    blendStride = resolvedStride;
                    bones = listBlendBones(
                        new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength),
                        validation.vertexCount,
                        resolvedStride,
                    );
                }
            }
        }
        const objectMaps = buildObjectMapCandidates(
            drawRanges,
            kind,
            components.length + 1,
            positions,
            combinedIndices,
        );
        components.push({
            id: sanitizeId(position.name),
            name: position.name,
            kind,
            interactiveCandidate,
            supportGrade: support.grade,
            supportReasons: support.reasons,
            positionResourceName: position.name,
            positionRelativePath: position.filename,
            positionPath,
            positionStride: position.stride,
            vertexCount: validation.vertexCount,
            indexResourceName: primaryIndex.resource.name,
            indexRelativePath: primaryIndex.relativePath,
            indexPath: primaryIndex.indexPath,
            indexRelativePaths,
            indexPaths,
            indexFormats,
            indexFormat: primaryIndex.resource.format,
            indexCount: combinedIndices.length,
            blendSectionName: drawContext.blendSectionName,
            ibSectionName: drawContext.ibSectionName,
            ibHash: drawContext.ibHash,
            variantKey: variantKeyForResource(position.name),
            variantCondition: drawContext.variantCondition,
            drawRanges,
            objectMaps,
            blendRelativePath,
            blendPath,
            blendStride,
            bones,
        });
    }

    if (components.length === 0) {
        throw new Error("No readable touch-compatible mesh components found");
    }

    const supportGrade = worstGrade(components.map((component) => component.supportGrade));
    const supportReasons = [
        ...new Set(components.flatMap((component) => component.supportReasons)),
    ];
    const meshHash = await hashTouchFiles(
        components.flatMap((component) =>
            [
                component.positionPath,
                ...(component.indexPaths ?? (component.indexPath ? [component.indexPath] : [])),
                component.blendPath,
            ].filter((entry): entry is string => !!entry),
        ),
        modRoot,
    );
    const iniHash = await hashTouchFiles(sourcePaths, modRoot);

    return {
        modRoot,
        sourceRoot: resolved,
        modRootRelativeToSource: path.relative(resolved, modRoot) || ".",
        iniPath,
        iniRelativePath: path.relative(modRoot, iniPath) || path.basename(iniPath),
        sourceFilesRelativePaths: sourcePaths.map((sourcePath) =>
            path.relative(modRoot, sourcePath),
        ),
        supportGrade,
        supportReasons,
        components,
        meshHash,
        iniHash,
    };
}

export async function loadTouchMeshBuffers(component: TouchComponentAnalysis) {
    const positionBytes = await fse.readFile(component.positionPath);
    const positions = extractPositions(new Uint8Array(positionBytes), component.positionStride);
    const normals = extractNormals(new Uint8Array(positionBytes), component.positionStride);
    const indices = component.indexPaths?.length
        ? combineIndexBuffers(
              await Promise.all(
                  component.indexPaths.map((indexPath, index) =>
                      readIndexBuffer(
                          indexPath,
                          component.indexFormats?.[index] ?? component.indexFormat,
                      ),
                  ),
              ),
          )
        : component.indexPath
          ? await readIndexBuffer(
                component.indexPath,
                component.indexFormats?.[0] ?? component.indexFormat,
            )
          : new Uint32Array();
    let blendBytes: Uint8Array | undefined;
    let bones: BlendBoneInfo[] = component.bones;
    if (component.blendPath && component.blendStride) {
        const raw = await fse.readFile(component.blendPath);
        blendBytes = new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength);
        if (bones.length === 0) {
            bones = listBlendBones(blendBytes, component.vertexCount, component.blendStride);
        }
    }
    return {
        positions,
        normals,
        indices,
        positionBytes,
        blendBytes,
        blendStride: component.blendStride,
        bones,
    };
}

function gradeComponent(input: {
    positionStride: number;
    positionBytes: Buffer;
    vertexCount: number;
    indices: Uint32Array;
    drawRanges: TouchDrawRange[];
    kind: TouchComponentKind;
}): { grade: TouchSupportGrade; reasons: string[] } {
    const reasons: string[] = [];

    if (input.indices.length === 0 || input.indices.length % 3 !== 0) {
        return { grade: "C", reasons: ["Mesh is not a triangle list"] };
    }
    if (input.indices.some((value) => value >= input.vertexCount)) {
        return { grade: "C", reasons: ["Index buffer exceeds position vertex count"] };
    }
    if (input.drawRanges.length === 0) {
        reasons.push("No drawindexed ranges found in INI");
        return { grade: "C", reasons };
    }

    if (input.positionStride === TOUCH_POSITION_STRIDE) {
        const layoutOk = hasPositionNormalTangent(input.positionBytes, input.positionStride);
        if (!layoutOk) {
            reasons.push("Stride 40 but normal/tangent layout looks incomplete");
            return { grade: "B", reasons };
        }
        if (input.kind === "unknown") reasons.push("Component kind is ambiguous");
        return {
            grade: "A",
            reasons: reasons.length > 0 ? reasons : ["Position stride 40 with PN-T layout"],
        };
    }

    if (input.positionStride >= 12) {
        reasons.push(`Position stride ${input.positionStride} requires touch VB rebuild`);
        return { grade: "B", reasons };
    }

    return { grade: "C", reasons: [`Unsupported position stride ${input.positionStride}`] };
}

function buildObjectMapCandidates(
    ranges: TouchDrawRange[],
    kind: TouchComponentKind,
    defaultObjectId: number,
    positions?: Float32Array,
    indices?: Uint32Array,
): TouchObjectMapEntry[] {
    const uniqueBySpan = new Map<string, TouchDrawRange>();
    for (const range of ranges) {
        if (range.indexCount < 300) continue;
        const key = `${range.firstIndex}:${range.indexCount}`;
        if (!uniqueBySpan.has(key)) uniqueBySpan.set(key, range);
    }

    const meaningful = [...uniqueBySpan.values()].sort(
        (left, right) => right.indexCount - left.indexCount || left.firstIndex - right.firstIndex,
    );

    if (meaningful.length === 0) {
        return ranges.slice(0, 1).map((range) => ({
            firstIndex: range.firstIndex,
            indexCount: range.indexCount,
            objectMode: TOUCH_OBJECT_MODE,
            objectId: defaultObjectId,
            label: range.label || `${kind}-main`,
        }));
    }

    if (kind === "body") {
        const pair = pickBodyObjectMapPair(meaningful, positions, indices);
        if (pair) {
            return pair.map((range, index) => ({
                firstIndex: range.firstIndex,
                indexCount: range.indexCount,
                objectMode: TOUCH_OBJECT_MODE,
                objectId: defaultObjectId,
                label: index === 0 ? "clothed" : "nude",
            }));
        }
    }

    return [
        {
            firstIndex: meaningful[0].firstIndex,
            indexCount: meaningful[0].indexCount,
            objectMode: TOUCH_OBJECT_MODE,
            objectId: defaultObjectId,
            label: kind === "legs" ? "skin" : "main",
        },
    ];
}

/** Prefer equal-sized draw pairs that cover the upper torso (breasts), not just the largest pair. */
function pickBodyObjectMapPair(
    ranges: TouchDrawRange[],
    positions?: Float32Array,
    indices?: Uint32Array,
) {
    const byCount = new Map<number, TouchDrawRange[]>();
    for (const range of ranges) {
        const group = byCount.get(range.indexCount) ?? [];
        group.push(range);
        byCount.set(range.indexCount, group);
    }

    let bestEqual: [TouchDrawRange, TouchDrawRange] | undefined;
    let bestEqualScore = Number.NEGATIVE_INFINITY;

    for (const group of byCount.values()) {
        if (group.length < 2) continue;
        const ordered = [...group].sort((left, right) => left.firstIndex - right.firstIndex);
        for (let i = 0; i < ordered.length; i++) {
            for (let j = i + 1; j < ordered.length; j++) {
                const left = ordered[i];
                const right = ordered[j];
                const geometryScore =
                    positions && indices
                        ? scoreDrawRangeUpperBody(left, positions, indices) +
                          scoreDrawRangeUpperBody(right, positions, indices)
                        : 0;
                const score = geometryScore * 10_000 + left.indexCount;
                if (score > bestEqualScore) {
                    bestEqualScore = score;
                    bestEqual = [left, right];
                }
            }
        }
    }

    if (bestEqual && bestEqualScore > 0) return bestEqual;

    if (positions && indices && ranges.length >= 2) {
        let bestUnequal: [TouchDrawRange, TouchDrawRange] | undefined;
        let bestUnequalScore = Number.NEGATIVE_INFINITY;

        for (let i = 0; i < ranges.length; i++) {
            for (let j = i + 1; j < ranges.length; j++) {
                const left = ranges[i];
                const right = ranges[j];
                const scoreLeft = scoreDrawRangeUpperBody(left, positions, indices);
                const scoreRight = scoreDrawRangeUpperBody(right, positions, indices);
                if (scoreLeft > 0.4 && scoreRight > 0.4) {
                    const geometryScore = scoreLeft + scoreRight;
                    const score = geometryScore * 10_000 + (left.indexCount + right.indexCount);
                    if (score > bestUnequalScore) {
                        bestUnequalScore = score;
                        bestUnequal =
                            left.indexCount >= right.indexCount ? [left, right] : [right, left];
                    }
                }
            }
        }
        if (bestUnequal) return bestUnequal;
    }

    return bestEqual;
}

function scoreDrawRangeUpperBody(
    range: TouchDrawRange,
    positions: Float32Array,
    indices: Uint32Array,
) {
    const vertexCount = positions.length / 3;
    let minZ = Number.POSITIVE_INFINITY;
    let maxZ = Number.NEGATIVE_INFINITY;
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const z = positions[vertex * 3 + 2];
        minZ = Math.min(minZ, z);
        maxZ = Math.max(maxZ, z);
    }
    if (!Number.isFinite(minZ) || !Number.isFinite(maxZ)) return 0;
    const spanZ = Math.max(maxZ - minZ, 1e-6);
    // Chest/breast band sits in the upper-middle of the body mesh, not the head tip.
    const bandLow = minZ + spanZ * 0.55;
    const bandHigh = minZ + spanZ * 0.85;

    const end = Math.min(indices.length, range.firstIndex + range.indexCount);
    const seen = new Uint8Array(vertexCount);
    let unique = 0;
    let upper = 0;
    let zSum = 0;
    for (let i = range.firstIndex; i < end; i++) {
        const vertex = indices[i];
        if (vertex >= vertexCount || seen[vertex]) continue;
        seen[vertex] = 1;
        unique += 1;
        const z = positions[vertex * 3 + 2];
        zSum += z;
        if (z >= bandLow && z <= bandHigh) upper += 1;
    }
    if (unique === 0) return 0;
    const upperRatio = upper / unique;
    const meanZNorm = (zSum / unique - minZ) / spanZ;
    return upperRatio * 2 + meanZNorm;
}

function findDrawContext(
    sections: IniSection[],
    commandLists: Map<string, IniSection>,
    position: Resource,
    indexInput:
        | Resource
        | Resource[]
        | Array<{ resource: Resource; offset: number; count: number }>,
    indexCount: number,
): {
    ranges: TouchDrawRange[];
    blendSectionName?: string;
    ibSectionName?: string;
    ibHash?: string;
    variantCondition?: string;
} {
    const normalizedInput = Array.isArray(indexInput) ? indexInput : [indexInput];
    const indexInfos = normalizedInput.map((item) =>
        (item as { resource: Resource }).resource
            ? (item as { resource: Resource; offset: number; count: number })
            : { resource: item as Resource, offset: 0, count: indexCount },
    );
    const indexMap = new Map(indexInfos.map((info) => [resourceKey(info.resource), info]));
    const indexKeys = new Set(indexMap.keys());
    const resourcesByName = new Map(
        collectResources(sections).map((resource) => [resourceKey(resource), resource]),
    );

    const contexts = sections
        .filter((section) => section.header === "TextureOverride")
        .map((section) => {
            const lines = expandCommandListLines(section.lines, commandLists);
            return {
                section,
                lines,
                hash: sectionValueFromLines(lines, "hash"),
                positionAssignments: conditionalResourceAssignments(lines, "vb0", resourcesByName),
                indexAssignments: conditionalResourceAssignments(lines, "ib", resourcesByName),
                ranges: extractDrawRanges(lines),
                autoConditions: extractAutoDrawConditions(lines),
            };
        });

    let blendSectionName: string | undefined;
    let ibSectionName: string | undefined;
    let ibHash: string | undefined;
    let variantCondition: string | undefined;
    const ranges: TouchDrawRange[] = [];

    for (const context of contexts) {
        const positionAssignment = context.positionAssignments.find(
            (assignment) => resourceKey(assignment.resource) === resourceKey(position),
        );
        if (positionAssignment) {
            blendSectionName ??= context.section.name;
        }

        const indexAssignment = context.indexAssignments.find((assignment) =>
            indexKeys.has(resourceKey(assignment.resource)),
        );
        if (!indexAssignment) continue;

        const matched = indexMap.get(resourceKey(indexAssignment.resource));
        const resourceOffset = matched?.offset ?? 0;
        const resourceCount = matched?.count ?? indexCount;

        ibSectionName ??= context.section.name;
        ibHash ??= context.hash;
        variantCondition ??= indexAssignment.conditionText;
        ranges.push(
            ...context.ranges
                .filter((range) =>
                    rangeMatchesCondition(range.conditionText, indexAssignment.conditionText),
                )
                .map((range) => ({ ...range, firstIndex: range.firstIndex + resourceOffset })),
        );

        if (
            context.autoConditions.some((condition) =>
                sameCondition(condition, indexAssignment.conditionText),
            )
        ) {
            ranges.push({
                firstIndex: resourceOffset,
                indexCount: resourceCount,
                baseVertex: 0,
                conditionText: indexAssignment.conditionText,
            });
        }
    }

    if (ranges.length === 0 && ibHash) {
        const autoContext = contexts.some(
            (context) =>
                context.hash === ibHash &&
                context.autoConditions.some((condition) =>
                    sameCondition(condition, variantCondition),
                ),
        );
        if (autoContext) {
            // GIMI's shared auto draw uses the replacement IB's local range.
            ranges.push({
                firstIndex: 0,
                indexCount,
                baseVertex: 0,
                conditionText: variantCondition,
            });
        }
    }

    return { ranges, blendSectionName, ibSectionName, ibHash, variantCondition };
}

function extractDrawRanges(lines: string[]): TouchDrawRange[] {
    const ranges: TouchDrawRange[] = [];
    for (const entry of linesWithConditions(lines)) {
        const line = entry.line;
        if (!line || line.startsWith(";")) continue;

        const match = line.match(/^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$/i);
        if (!match) continue;

        const indexCount = Number(match[1].trim());
        const firstIndex = Number(match[2].trim());
        const baseVertex = Number(match[3].trim());
        if (![indexCount, firstIndex, baseVertex].every(Number.isFinite)) continue;
        if (indexCount <= 0) continue;

        ranges.push({
            firstIndex,
            indexCount,
            baseVertex,
            conditionText: entry.conditionText,
        });
    }

    return ranges;
}

function conditionalResourceAssignments(
    lines: string[],
    key: string,
    resourcesByName: Map<string, Resource>,
) {
    return linesWithConditions(lines).flatMap((entry) => {
        const separator = entry.line.indexOf("=");
        if (separator < 0 || entry.line.slice(0, separator).trim().toLowerCase() !== key) {
            return [];
        }

        const resource = resourceForReference(
            entry.line.slice(separator + 1).trim(),
            resourcesByName,
        );
        return resource ? [{ resource, conditionText: entry.conditionText }] : [];
    });
}

function extractAutoDrawConditions(lines: string[]) {
    return linesWithConditions(lines)
        .filter((entry) => /^drawindexed\s*=\s*auto$/i.test(entry.line))
        .map((entry) => entry.conditionText);
}

function linesWithConditions(lines: string[]) {
    const result: Array<{ line: string; conditionText?: string }> = [];
    const stack: Array<{ branches: string[]; condition: string }> = [];

    for (const rawLine of lines) {
        const line = rawLine.trim();
        if (!line || line.startsWith(";")) {
            result.push({ line });
            continue;
        }

        const lower = line.toLowerCase();
        if (lower.startsWith("if ")) {
            const expression = line.slice(3).trim();
            stack.push({ branches: [expression], condition: expression });
            continue;
        }
        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            const expression = lower.startsWith("elif ")
                ? line.slice(5).trim()
                : line.slice(8).trim();
            const frame = stack.at(-1);
            if (!frame) {
                stack.push({ branches: [expression], condition: expression });
                continue;
            }
            frame.condition = branchCondition([...frame.branches, expression]);
            frame.branches.push(expression);
            continue;
        }
        if (lower === "else") {
            const frame = stack.at(-1);
            if (frame) frame.condition = branchCondition(frame.branches, true);
            continue;
        }
        if (lower === "endif") {
            stack.pop();
            continue;
        }

        result.push({
            line,
            conditionText:
                stack.length > 0 ? stack.map((frame) => frame.condition).join(" && ") : undefined,
        });
    }

    return result;
}

function branchCondition(branches: string[], isElse = false) {
    if (isElse) return branches.map((branch) => `!(${branch})`).join(" && ");
    const current = branches.at(-1);
    if (!current) return "";
    return [...branches.slice(0, -1).map((branch) => `!(${branch})`), `(${current})`].join(" && ");
}

function sameCondition(left?: string, right?: string) {
    return normalizeCondition(left) === normalizeCondition(right);
}

function rangeMatchesCondition(left?: string, right?: string) {
    return !left || !right || sameCondition(left, right);
}

function normalizeCondition(condition?: string) {
    return condition?.replace(/\s+/g, " ").trim() || "";
}

function variantKeyForResource(name: string) {
    return name.match(/\.(\d+)$/)?.[1];
}

function uniqueDrawRanges(ranges: TouchDrawRange[]) {
    const seen = new Set<string>();
    return ranges.filter((range) => {
        const key = `${range.firstIndex}:${range.indexCount}:${range.baseVertex}:${range.conditionText ?? ""}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

function classifyComponentKind(...names: Array<string | undefined>): TouchComponentKind {
    const text = names
        .filter((name): name is string => !!name)
        .join(" ")
        .toLowerCase();
    if (/(leg|thigh|butt|hip|lower[_-]?body|xiaban|tuibu)/.test(text)) return "legs";
    if (/(hair|tail|toufa)/.test(text)) return "hair";
    if (/(body|torso|chest|breast|upper[_-]?body|shangban|shenti)/.test(text)) return "body";
    if (/(back|cloth|dress|coat|acc|weapon|face|head|zhuangshi|pifuzhuangshi)/.test(text)) {
        return "accessory";
    }
    return "unknown";
}

const MIN_TOUCH_VERTEX_COUNT = 1500;
const MIN_TOUCH_INDEX_COUNT = 3000;

function isInteractiveCandidate(
    kind: TouchComponentKind,
    vertexCount: number,
    indexCount: number,
    name: string,
) {
    if (kind !== "body" && kind !== "legs") return false;
    if (vertexCount < MIN_TOUCH_VERTEX_COUNT) return false;
    if (indexCount < MIN_TOUCH_INDEX_COUNT) return false;
    // Tiny secondary parts like body2/body3 are not full torso meshes.
    if (/(body|leg)\d+/i.test(name)) return false;
    return true;
}

function hasPositionNormalTangent(bytes: Buffer, stride: number) {
    if (stride < 40 || bytes.byteLength < stride * 3) return false;
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    for (let i = 0; i < Math.min(8, bytes.byteLength / stride); i++) {
        const base = i * stride;
        const nx = view.getFloat32(base + 12, true);
        const ny = view.getFloat32(base + 16, true);
        const nz = view.getFloat32(base + 20, true);
        const nLen = Math.hypot(nx, ny, nz);
        if (!Number.isFinite(nLen) || nLen < 0.1 || nLen > 2.5) return false;
    }
    return true;
}

function extractNormals(bytes: Uint8Array, stride: number) {
    const vertexCount = Math.floor(bytes.byteLength / stride);
    const normals = new Float32Array(vertexCount * 3);
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    for (let i = 0; i < vertexCount; i++) {
        const base = i * stride;
        const o = i * 3;
        if (stride >= 24) {
            normals[o] = view.getFloat32(base + 12, true);
            normals[o + 1] = view.getFloat32(base + 16, true);
            normals[o + 2] = view.getFloat32(base + 20, true);
        }
    }
    return normals;
}

export async function hashTouchFiles(filePaths: string[], root: string) {
    const hash = crypto.createHash("sha256");
    for (const filePath of [...filePaths].sort()) {
        hash.update(path.relative(root, filePath).replaceAll("\\", "/"));
        hash.update(await fse.readFile(filePath));
    }
    return hash.digest("hex");
}

function worstGrade(grades: TouchSupportGrade[]): TouchSupportGrade {
    if (grades.includes("C")) return "C";
    if (grades.includes("B")) return "B";
    return "A";
}

function sanitizeId(name: string) {
    return name.replace(/[^a-zA-Z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "component";
}

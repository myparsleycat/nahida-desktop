import crypto from "node:crypto";
import path from "node:path";
import fse from "fs-extra";
import pLimit from "p-limit";
import { collectIbResources } from "./build";
import { float32ArrayToBuffer, uint32ArrayToBuffer } from "./dxgi-utils";
import { loadFmtForIbCached, loadIndicesForIbCached } from "./fmt-loader";
import { extractPrimitiveGeometry } from "./geometry";
import { evaluateIniCondition, evaluateIniNumericExpression } from "./ini-expression";
import { keyMatchesIb, strictKeyMatchesIb } from "./mesh-key";
import * as overrideAnalysis from "./override-analysis";
import {
    createTimedStageLogger,
    getStaticGlbWorkConcurrency,
    humanizeVariableLabel,
    normalizeKey,
    sanitizeArtifactName,
} from "./shared";
import type {
    BufferGroup,
    ConvertModToGlbBufferOptions,
    ConvertModToGlbOptions,
    FmtLayout,
    IbResource,
    IniSection,
    MeshFrameGeometry,
    PreparedAnimationClip,
    PresentAnimationPattern,
    SlotVariableBinding,
    StaticGlbAnimationBufferWriter,
    StaticGlbAnimationClip,
    StaticGlbAnimationFrame,
    StaticGlbAnimationSharedBuffer,
    StaticGlbBuildContext,
    TextureOverrideBinding,
    VariableStateMap,
} from "./types";

export function detectPresentAnimations(
    sections: IniSection[],
    defaultVariables: Map<string, number | string>,
    slotBindings: SlotVariableBinding[],
): PreparedAnimationClip[] {
    const presentSections = sections.filter((section) => section.header === "Present");
    if (presentSections.length === 0) {
        return [];
    }

    const manualVariables = new Set(slotBindings.map((binding) => normalizeKey(binding.variable)));
    const discovered = new Map<string, PreparedAnimationClip>();

    for (const section of presentSections) {
        for (const rawLine of section.lines) {
            const assignmentMatch = rawLine.match(/^\$([\w.]+)\s*=\s*(.+)$/);
            if (!assignmentMatch) {
                continue;
            }

            const variableId = normalizeKey(assignmentMatch[1]);
            const expression = assignmentMatch[2].trim();
            if (!/\btime\b/i.test(expression) || manualVariables.has(variableId)) {
                continue;
            }

            const branchValues = collectDiscreteBranchValues(sections, variableId);
            if (branchValues.length < 2) {
                continue;
            }

            const fps = resolveAnimationFps(defaultVariables, expression);
            if (!Number.isFinite(fps) || fps <= 0) {
                continue;
            }

            const frameStart = branchValues[0] ?? 0;
            const frameEnd = branchValues[branchValues.length - 1] ?? frameStart;
            const frames: PreparedAnimationClip["frames"] = [];
            let valid = true;

            for (let frameIndex = frameStart; frameIndex <= frameEnd; frameIndex += 1) {
                const time = frameIndex / fps;
                const value = evaluateIniNumericExpression(
                    expression,
                    defaultVariables,
                    normalizeKey,
                    { time },
                );
                if (value === null || !Number.isFinite(value)) {
                    valid = false;
                    break;
                }

                frames.push({
                    index: frameIndex,
                    time,
                    values: { [variableId]: value },
                });
            }

            if (!valid || frames.length !== frameEnd - frameStart + 1) {
                continue;
            }

            const clipId = variableId;
            discovered.set(clipId, {
                id: clipId,
                label: humanizeVariableLabel(variableId),
                variableIds: [variableId],
                fps,
                frameStart,
                frameEnd,
                loop: true,
                frames,
            });
        }
    }

    for (const clip of detectIncrementalPresentAnimations(
        presentSections,
        sections,
        defaultVariables,
        manualVariables,
    )) {
        discovered.set(clip.id, clip);
    }

    return Array.from(discovered.values()).sort((left, right) => left.id.localeCompare(right.id));
}

function detectIncrementalPresentAnimations(
    presentSections: IniSection[],
    sections: IniSection[],
    defaultVariables: Map<string, number | string>,
    manualVariables: Set<string>,
): PreparedAnimationClip[] {
    const discovered = new Map<string, PreparedAnimationClip>();

    for (const section of presentSections) {
        const trimmedLines = section.lines.map((line) => line.trim()).filter(Boolean);
        const patterns = [
            ...collectPresentModuloPatterns(trimmedLines, defaultVariables),
            ...collectPresentAccumulatorPatterns(trimmedLines, defaultVariables),
        ];
        for (const pattern of patterns) {
            const variableId = pattern.variableId;
            if (manualVariables.has(variableId) || discovered.has(variableId)) {
                continue;
            }

            const branchValues = collectDiscreteBranchValues(sections, variableId);
            if (branchValues.length < 2) {
                continue;
            }

            const frameStart =
                resolveNumericTokenValue(pattern.frameStartToken, defaultVariables) ??
                branchValues[0] ??
                0;
            const frameEnd =
                resolveNumericTokenValue(pattern.frameEndToken, defaultVariables) ??
                branchValues[branchValues.length - 1] ??
                frameStart;
            if (
                !Number.isInteger(frameStart) ||
                !Number.isInteger(frameEnd) ||
                frameEnd <= frameStart
            ) {
                continue;
            }

            const speed = resolveNumericTokenValue(pattern.speedToken, defaultVariables);
            if (speed === null || !Number.isFinite(speed) || speed <= 0) {
                continue;
            }

            const fps = 60 / speed;
            if (!Number.isFinite(fps) || fps <= 0) {
                continue;
            }

            const frames: PreparedAnimationClip["frames"] = [];
            for (let frameIndex = frameStart; frameIndex <= frameEnd; frameIndex += 1) {
                frames.push({
                    index: frameIndex,
                    time: (frameIndex - frameStart) / fps,
                    values: { [variableId]: frameIndex },
                });
            }

            discovered.set(variableId, {
                id: variableId,
                label: humanizeVariableLabel(variableId),
                variableIds: [variableId],
                fps,
                frameStart,
                frameEnd,
                loop: true,
                frames,
            });
        }
    }

    return Array.from(discovered.values());
}

function collectDiscreteBranchValues(sections: IniSection[], variableId: string): number[] {
    const values = new Set<number>();
    const pattern = new RegExp(
        `^(?:if|elif|else if)\\s+\\$${escapeRegex(variableId)}\\s*==\\s*(-?\\d+(?:\\.\\d+)?)$`,
        "i",
    );

    for (const section of sections) {
        for (const rawLine of section.lines) {
            const match = rawLine.trim().match(pattern);
            if (!match) {
                continue;
            }

            const value = Number(match[1]);
            if (Number.isInteger(value)) {
                values.add(value);
            }
        }
    }

    return Array.from(values).sort((left, right) => left - right);
}

function resolveAnimationFps(
    defaultVariables: Map<string, number | string>,
    expression: string,
): number {
    const explicitFps = Number(
        defaultVariables.get(normalizeKey("$fps")) ?? defaultVariables.get("fps"),
    );
    if (Number.isFinite(explicitFps) && explicitFps > 0) {
        return explicitFps;
    }

    const referencedFps = expression.match(/\$([\w.]*fps[\w.]*)/i)?.[1];
    if (!referencedFps) {
        return Number.NaN;
    }

    const fpsValue = Number(defaultVariables.get(normalizeKey(referencedFps)));
    return Number.isFinite(fpsValue) && fpsValue > 0 ? fpsValue : Number.NaN;
}

function collectPresentModuloPatterns(
    lines: string[],
    defaultVariables: Map<string, number | string>,
): PresentAnimationPattern[] {
    const patterns = new Map<string, PresentAnimationPattern>();

    for (let index = 0; index < lines.length; index += 1) {
        const moduloMatch = lines[index]?.match(/^if\s+\$([\w.]+)\s*%\s*(\$?[\w.-]+)\s*==\s*0$/i);
        if (!moduloMatch) {
            continue;
        }

        const auxVariableToken = moduloMatch[1];
        const speedToken = moduloMatch[2];
        if (!hasIncrementingAuxVariable(lines, auxVariableToken)) {
            continue;
        }

        for (let probe = index + 1; probe < lines.length; probe += 1) {
            const compareMatch = lines[probe]?.match(
                /^(?:if|elif|else if)\s+\$([\w.]+)\s*<\s*(\$?[\w.-]+)\s*$/i,
            );
            if (!compareMatch) {
                continue;
            }

            const variableId = normalizeKey(compareMatch[1]);
            const frameEndToken = compareMatch[2];
            const incrementPattern = new RegExp(
                `^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*\\$${escapeRegex(compareMatch[1])}\\s*\\+\\s*1$`,
                "i",
            );
            const resetMatch = lines
                .slice(probe + 1)
                .find(
                    (line) =>
                        new RegExp(
                            `^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*(\\$?[\\w.-]+)$`,
                            "i",
                        ).test(line) && !incrementPattern.test(line),
                )
                ?.match(
                    new RegExp(`^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*(\\$?[\\w.-]+)$`, "i"),
                );

            if (
                !lines.slice(probe + 1).some((line) => incrementPattern.test(line)) ||
                !resetMatch
            ) {
                continue;
            }

            patterns.set(variableId, {
                variableId,
                speedToken,
                frameStartToken: resetMatch[1],
                frameEndToken,
            });
            break;
        }
    }

    return Array.from(patterns.values()).filter((pattern) => {
        const speed = resolveNumericTokenValue(pattern.speedToken, defaultVariables);
        return speed !== null && Number.isFinite(speed) && speed > 0;
    });
}

function collectPresentAccumulatorPatterns(
    lines: string[],
    defaultVariables: Map<string, number | string>,
): PresentAnimationPattern[] {
    const patterns = new Map<string, PresentAnimationPattern>();

    for (let index = 0; index < lines.length; index += 1) {
        const accumulatorMatch = lines[index]?.match(
            /^if\s+\(\s*\$([\w.]+)\s*\+\s*\(?\s*1\s*\/\s*(\$?[\w.-]+)\s*\)?\s*\)\s*<\s*(\$?[\w.-]+)\s*$/i,
        );
        if (!accumulatorMatch) {
            continue;
        }

        const auxVariableToken = accumulatorMatch[1];
        const speedToken = accumulatorMatch[2];
        const frameEndToken = accumulatorMatch[3];
        const incrementPattern = new RegExp(
            `^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*\\+\\s*\\(?\\s*1\\s*\\/\\s*${escapeRegex(speedToken)}\\s*\\)?$`,
            "i",
        );
        const assignmentPattern = new RegExp(
            `^\\$([\\w.]+)\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*//\\s*1$`,
            "i",
        );
        const resetMatch = lines
            .slice(index + 1)
            .find(
                (line) =>
                    new RegExp(
                        `^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*(\\$?[\\w.-]+)$`,
                        "i",
                    ).test(line) && !incrementPattern.test(line),
            )
            ?.match(
                new RegExp(`^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*(\\$?[\\w.-]+)$`, "i"),
            );
        const assignmentMatch = lines.slice(index + 1).find((line) => assignmentPattern.test(line));

        if (
            !lines.slice(index + 1).some((line) => incrementPattern.test(line)) ||
            !resetMatch ||
            !assignmentMatch
        ) {
            continue;
        }

        const variableMatch = assignmentMatch.match(assignmentPattern);
        const variableId = normalizeKey(variableMatch?.[1] ?? "");
        if (!variableId) {
            continue;
        }

        patterns.set(variableId, {
            variableId,
            speedToken,
            frameStartToken: resetMatch[1],
            frameEndToken,
        });
    }

    return Array.from(patterns.values()).filter((pattern) => {
        const speed = resolveNumericTokenValue(pattern.speedToken, defaultVariables);
        return speed !== null && Number.isFinite(speed) && speed > 0;
    });
}

function hasIncrementingAuxVariable(lines: string[], auxVariableToken: string): boolean {
    return lines.some((line) =>
        new RegExp(
            `^(?:post\\s+)?\\$${escapeRegex(auxVariableToken)}\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*\\+\\s*1$`,
            "i",
        ).test(line),
    );
}

export function resolveNumericTokenValue(
    token: string | undefined,
    defaultVariables: Map<string, number | string>,
): number | null {
    if (!token) {
        return null;
    }

    const trimmed = token.trim();
    if (/^-?\d+(?:\.\d+)?$/.test(trimmed)) {
        const value = Number(trimmed);
        return Number.isFinite(value) ? value : null;
    }

    const key = normalizeKey(trimmed.startsWith("$") ? trimmed : `$${trimmed}`);
    const value = Number(defaultVariables.get(key));
    return Number.isFinite(value) ? value : null;
}

function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export async function materializeAnimationClips(
    clips: PreparedAnimationClip[],
    artifactRoot: string,
    options: Omit<ConvertModToGlbOptions, "outputPath">,
    defaultState: VariableStateMap,
    warn: (message: string) => void,
    prepareContext: (
        options: Pick<ConvertModToGlbBufferOptions, "modPath" | "assetPath">,
        warn: (message: string) => void,
    ) => Promise<StaticGlbBuildContext>,
    getDrawBindingsForIb: (
        context: StaticGlbBuildContext,
        ibName: string,
    ) => TextureOverrideBinding[],
    writeAnimationBuffer?: StaticGlbAnimationBufferWriter,
): Promise<StaticGlbAnimationClip[]> {
    const logTiming = createTimedStageLogger(
        options.logger,
        "mod-static-glb.materializeAnimationClips",
    );
    const output: StaticGlbAnimationClip[] = [];
    const contextStartedAt = Date.now();
    const context = await prepareContext(options, warn);
    logTiming("Prepared animation build context", contextStartedAt);
    const geometryLimit = pLimit(getStaticGlbWorkConcurrency(6));

    for (const clip of clips) {
        const clipStartedAt = Date.now();
        const clipRoot = path.join(artifactRoot, "animation", sanitizeArtifactName(clip.id));
        const sharedBufferDir = path.join(clipRoot, "shared");
        const sharedBuffersByKey = new Map<string, StaticGlbAnimationSharedBuffer>();
        const sharedBuffersById = new Map<string, StaticGlbAnimationSharedBuffer>();

        const planStartedAt = Date.now();
        const plan = await planClipGeometry(
            context,
            clip,
            defaultState,
            options,
            warn,
            getDrawBindingsForIb,
        );
        logTiming(
            `Planned clip geometry (uniqueJobs=${plan.uniqueJobs.size}, totalMeshInstances=${plan.totalMeshCount})`,
            planStartedAt,
        );

        const extractStartedAt = Date.now();
        const geometryResults = new Map<string, MeshFrameGeometry | null>();
        let warningCount = 0;
        await Promise.all(
            Array.from(plan.uniqueJobs.entries()).map(([key, job]) =>
                geometryLimit(async () => {
                    const jobStartedAt = Date.now();
                    const indices = await loadIndicesForIbCached(
                        context,
                        job.ib,
                        job.ib.format || job.fmt.indexFormat,
                    );
                    const activeIndices = overrideAnalysis.buildIndicesForState(
                        getDrawBindingsForIb(context, job.ib.name),
                        indices,
                        job.resolvedVariables,
                        warn,
                    );
                    const result = extractAnimationFrameGeometry(
                        job.ib,
                        job.group,
                        job.fmt,
                        activeIndices,
                        warn,
                    );
                    geometryResults.set(
                        key,
                        result
                            ? {
                                  name: job.meshName,
                                  indices: result.indices,
                                  position: result.position,
                                  normal: result.normal,
                                  tangent: result.tangent,
                                  texcoord0: result.texcoord0,
                                  vertexCount: result.vertexCount,
                              }
                            : null,
                    );
                    logTiming(
                        `Extracted unique geometry ${job.meshName} (vertices=${result?.vertexCount ?? 0}, indices=${result?.indices.length ?? 0})`,
                        jobStartedAt,
                    );
                }),
            ),
        );
        logTiming(
            `Extracted unique geometries (${plan.uniqueJobs.size}/${plan.totalMeshCount})`,
            extractStartedAt,
        );

        const assembleStartedAt = Date.now();
        const frames: StaticGlbAnimationFrame[] = [];
        for (const framePlan of plan.framePlans) {
            const meshes: StaticGlbAnimationFrame["meshes"] = [];
            for (const entry of framePlan.meshEntries) {
                const geometry = geometryResults.get(entry.geometryKey);
                if (!geometry) {
                    warningCount += 1;
                    continue;
                }
                const meshStem = sanitizeArtifactName(geometry.name);
                const indicesBuf = await writeSharedAnimationBuffer(
                    sharedBufferDir,
                    sharedBuffersByKey,
                    sharedBuffersById,
                    `${meshStem}.indices`,
                    uint32ArrayToBuffer(geometry.indices),
                    writeAnimationBuffer,
                );
                const positionBuf = await writeSharedAnimationBuffer(
                    sharedBufferDir,
                    sharedBuffersByKey,
                    sharedBuffersById,
                    `${meshStem}.position`,
                    float32ArrayToBuffer(geometry.position),
                    writeAnimationBuffer,
                );

                let normalBufferId: string | undefined;
                if (geometry.normal) {
                    normalBufferId = (
                        await writeSharedAnimationBuffer(
                            sharedBufferDir,
                            sharedBuffersByKey,
                            sharedBuffersById,
                            `${meshStem}.normal`,
                            float32ArrayToBuffer(geometry.normal),
                            writeAnimationBuffer,
                        )
                    ).id;
                }

                let tangentBufferId: string | undefined;
                if (geometry.tangent) {
                    tangentBufferId = (
                        await writeSharedAnimationBuffer(
                            sharedBufferDir,
                            sharedBuffersByKey,
                            sharedBuffersById,
                            `${meshStem}.tangent`,
                            float32ArrayToBuffer(geometry.tangent),
                            writeAnimationBuffer,
                        )
                    ).id;
                }

                let texcoord0BufferId: string | undefined;
                if (geometry.texcoord0) {
                    texcoord0BufferId = (
                        await writeSharedAnimationBuffer(
                            sharedBufferDir,
                            sharedBuffersByKey,
                            sharedBuffersById,
                            `${meshStem}.texcoord0`,
                            float32ArrayToBuffer(geometry.texcoord0),
                            writeAnimationBuffer,
                        )
                    ).id;
                }

                meshes.push({
                    meshName: geometry.name,
                    indicesBufferId: indicesBuf.id,
                    positionBufferId: positionBuf.id,
                    normalBufferId,
                    tangentBufferId,
                    texcoord0BufferId,
                });
            }

            if (warningCount > 0) {
                warn(
                    `Animation frame ${framePlan.index} for ${clip.id} generated with ${warningCount} warnings`,
                );
            }

            frames.push({
                index: framePlan.index,
                time: framePlan.time,
                values: framePlan.values,
                meshes,
            });
        }
        logTiming(
            `Assembled ${frames.length} frames from pre-computed geometry`,
            assembleStartedAt,
        );

        logTiming(
            `Completed animation clip ${clip.id} (frames=${frames.length}, sharedBuffers=${sharedBuffersById.size}, uniqueGeometries=${plan.uniqueJobs.size})`,
            clipStartedAt,
        );

        output.push({
            ...clip,
            sharedBuffers: Array.from(sharedBuffersById.values()),
            frames,
        });
    }

    return output;
}

type AnimationGeometryJob = {
    ib: IbResource;
    group: BufferGroup;
    fmt: FmtLayout;
    meshName: string;
    resolvedVariables: Map<string, number | string>;
};

type AnimationFramePlan = {
    index: number;
    time: number;
    values: VariableStateMap;
    meshEntries: Array<{ meshName: string; geometryKey: string }>;
};

type ClipGeometryPlan = {
    framePlans: AnimationFramePlan[];
    uniqueJobs: Map<string, AnimationGeometryJob>;
    totalMeshCount: number;
};

async function planClipGeometry(
    context: StaticGlbBuildContext,
    clip: PreparedAnimationClip,
    defaultState: VariableStateMap,
    options: Omit<ConvertModToGlbOptions, "outputPath">,
    warn: (message: string) => void,
    getDrawBindingsForIb: (
        context: StaticGlbBuildContext,
        ibName: string,
    ) => TextureOverrideBinding[],
): Promise<ClipGeometryPlan> {
    const uniqueJobs = new Map<string, AnimationGeometryJob>();
    const framePlans: AnimationFramePlan[] = [];
    let totalMeshCount = 0;

    for (const frame of clip.frames) {
        const resolvedVariables = overrideAnalysis.mergeVariableState(context.defaultVariables, {
            ...defaultState,
            ...frame.values,
        });
        const textureBindings = overrideAnalysis.collectTextureBindings(
            context.sections,
            context.sectionByFullName,
            resolvedVariables,
        );
        const ibResources = collectIbResources(
            context.sections,
            context.resources,
            context.bufferGroups,
            context.sectionByFullName,
            resolvedVariables,
            textureBindings,
            context.drawBindings,
        );

        const meshEntries: AnimationFramePlan["meshEntries"] = [];
        for (const ib of ibResources) {
            const group =
                context.bufferGroups.find((c) => strictKeyMatchesIb(c.key, ib.key)) ||
                context.bufferGroups.find((c) => keyMatchesIb(c.key, ib.key));
            if (!group) {
                warn(`No matching vertex buffer found for ${ib.filename}`);
                continue;
            }

            let fmt: FmtLayout;
            try {
                fmt = await loadFmtForIbCached(context, options.assetPath, ib, group.stride);
            } catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                warn(`Skipping ${ib.filename}: ${message}`);
                continue;
            }

            const ibPath = path.resolve(context.modDir, ib.filename);
            if (!(await fse.pathExists(ibPath))) {
                warn(`Missing IB file: ${ibPath}`);
                continue;
            }

            const indices = await loadIndicesForIbCached(context, ib, ib.format || fmt.indexFormat);
            if (indices.length === 0) {
                warn(`Empty IB file: ${ibPath}`);
                continue;
            }

            const meshName = path.basename(ib.filename, path.extname(ib.filename));
            const drawSignature = computeActiveDrawSignature(
                getDrawBindingsForIb(context, ib.name),
                resolvedVariables,
            );
            const geometryKey = `${normalizeKey(ib.filename)}:${normalizeKey(ib.key)}:${drawSignature}`;

            if (!uniqueJobs.has(geometryKey)) {
                uniqueJobs.set(geometryKey, { ib, group, fmt, meshName, resolvedVariables });
            }

            meshEntries.push({ meshName, geometryKey });
            totalMeshCount += 1;
        }

        framePlans.push({
            index: frame.index,
            time: frame.time,
            values: frame.values,
            meshEntries,
        });
    }

    return { framePlans, uniqueJobs, totalMeshCount };
}

function computeActiveDrawSignature(
    bindings: TextureOverrideBinding[],
    variables: Map<string, number | string>,
): string {
    const parts: string[] = [];
    for (let bi = 0; bi < bindings.length; bi++) {
        const binding = bindings[bi];
        for (let di = 0; di < binding.draws.length; di++) {
            const draw = binding.draws[di];
            const active =
                !draw.condition ||
                draw.condition.every(
                    (clause) =>
                        evaluateIniCondition(clause.expression, variables, normalizeKey) ===
                        clause.expected,
                );
            if (active) {
                parts.push(`${bi}:${di}`);
            }
        }
    }
    return parts.join("|");
}

function extractAnimationFrameGeometry(
    ib: IbResource,
    group: BufferGroup,
    fmt: FmtLayout,
    activeIndices: Uint32Array,
    warn: (message: string) => void,
): MeshFrameGeometry | null {
    const mesh = extractPrimitiveGeometry(
        group.vbBytes,
        group.stride,
        fmt,
        activeIndices,
        { includeTangents: true, compactVertices: true },
        warn,
    );
    if (!mesh) {
        return null;
    }

    return {
        name: path.basename(ib.filename, path.extname(ib.filename)),
        indices: mesh.indices,
        position: mesh.position,
        normal: mesh.normal,
        tangent: mesh.tangent,
        texcoord0: mesh.texcoord0,
        vertexCount: mesh.vertexCount,
    };
}

async function writeSharedAnimationBuffer(
    outputDir: string,
    byKey: Map<string, StaticGlbAnimationSharedBuffer>,
    byId: Map<string, StaticGlbAnimationSharedBuffer>,
    name: string,
    buffer: Buffer,
    writeAnimationBuffer?: StaticGlbAnimationBufferWriter,
): Promise<StaticGlbAnimationSharedBuffer> {
    const digest = crypto.createHash("sha256").update(buffer).digest("hex").slice(0, 16);
    const key = `${buffer.byteLength}:${digest}`;
    const existing = byKey.get(key);
    if (existing) {
        return existing;
    }

    const baseName = sanitizeArtifactName(name).replace(/\.bin$/i, "");
    const id = `${baseName}-${digest}`;
    const outputPath = writeAnimationBuffer
        ? await writeAnimationBuffer(id, buffer)
        : path.join(outputDir, `${id}.bin`);
    if (!writeAnimationBuffer) {
        await fse.ensureDir(outputDir);
        await fse.writeFile(outputPath, buffer);
    }

    const entry = { id, path: outputPath };
    byKey.set(key, entry);
    byId.set(id, entry);
    return entry;
}

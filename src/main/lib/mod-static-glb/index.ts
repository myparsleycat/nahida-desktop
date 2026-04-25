import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { convertDdsToPng } from "@native/native-util";
import {
    decodeIndices as decodeIndicesNative,
    ensureVec4 as ensureVec4Native,
    interleaveVertexBuffers,
    mergeDrawIndices,
    normalizeTangentArray as normalizeTangentArrayNative,
    normalizeVec3Array as normalizeVec3ArrayNative,
    prepareTextureForMaterial,
    readFloatAttribute as readFloatAttributeNative,
    removeDegenerateTriangles as removeDegenerateTrianglesNative,
} from "@native/static-glb";
import { decodeImage, parseDDSHeader } from "dds-ktx-parser";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import pLimit from "p-limit";
import { PNG } from "pngjs";
import writeFileAtomic from "write-file-atomic";
import type { Logger } from "../../internal/logger";
import { GlbBuilder } from "./builder";
import { evaluateIniCondition, evaluateIniNumericExpression } from "./ini-expression";
import {
    type PreparedTexture,
    type StaticGlbTextureFormat,
    textureNamePriority,
} from "./texture-utils";

export type { StaticGlbTextureFormat } from "./texture-utils";

type IniSection = {
    header: string;
    name: string;
    lines: string[];
    values: Record<string, string>;
};

type Resource = {
    name: string;
    filename?: string;
    stride?: number;
    format?: string;
    values: Record<string, string>;
};

type FmtElement = {
    semanticName: string;
    semanticIndex: number;
    format: string;
    inputSlot: number;
    alignedByteOffset: number;
    inputSlotClass: string;
    instanceDataStepRate: number;
};

type FmtLayout = {
    stride: number;
    topology: string;
    indexFormat: string;
    elements: FmtElement[];
};

type BufferGroup = {
    key: string;
    vbFilename: string;
    vbBytes: Buffer;
    stride: number;
};

type BufferResourceGroup = {
    position?: Resource;
    blend?: Resource;
    texcoord?: Resource;
    single?: Resource;
};

type IbResource = {
    name: string;
    filename: string;
    format: string;
    key: string;
    overrideHash?: string;
    overrideHashes?: string[];
};

type TextureBinding = {
    ibResourceName: string;
    diffuseResourceName?: string;
    textureResourceNames?: string[];
    overrideHash?: string;
};

type MaterialBinding = {
    materialIndex: number;
    textureResourceName: string;
    imagePath: string;
    mimeType: "image/png" | "image/jpeg";
};

export type VariableStateValue = number | string;
export type VariableStateMap = Record<string, VariableStateValue>;

export type StaticGlbVariantValue = {
    value: VariableStateValue;
    label: string;
};

export type StaticGlbVariantSlider = {
    min: number;
    max: number;
    step: number;
};

export type StaticGlbRealtimeShapeKeyDimension = {
    variableId: string;
    smallerPath: string;
    biggerPath: string;
};

export type StaticGlbRealtimeShapeKey = {
    shaderPath: string;
    targetMeshPrefixes: string[];
    basePath: string;
    vertexStride: number;
    positionOffset: number;
    normalOffset: number;
    tangentOffset: number;
    dimensions: StaticGlbRealtimeShapeKeyDimension[];
};

export type StaticGlbVariantVariable = {
    id: string;
    label: string;
    defaultValue: VariableStateValue;
    values: StaticGlbVariantValue[];
    order: number;
    slot?: number;
    iconPath?: string;
    controlType?: "buttons" | "slider";
    slider?: StaticGlbVariantSlider;
};

export type StaticGlbViewerUiAssets = {
    backgroundPath?: string;
    slotPath?: string;
    slotHoverPath?: string;
    slotActivePath?: string;
};

export type StaticGlbVariantManifest = {
    version: 1;
    name: string;
    modPath: string;
    iniPath: string;
    defaultState: VariableStateMap;
    variables: StaticGlbVariantVariable[];
    uiAssets: StaticGlbViewerUiAssets;
    shapeKeys?: StaticGlbRealtimeShapeKey[];
    states: Array<{
        key: string;
        values: VariableStateMap;
        glbPath: string;
    }>;
};

export type ConvertModVariantArtifactsResult = {
    iniPath: string;
    artifactRoot: string;
    defaultGlbPath: string;
    meshCount: number;
    warningCount: number;
    manifestPath: string;
    manifest: StaticGlbVariantManifest;
};

export type ConvertModToGlbOptions = {
    modPath: string;
    assetPath: string;
    outputPath: string;
    textureFormat?: StaticGlbTextureFormat;
    jpegQuality?: number;
    includeTangents?: boolean;
    debug?: boolean;
    logger?: Logger;
    onWarning?: (message: string) => void;
};

export type ConvertModToGlbBufferOptions = Omit<ConvertModToGlbOptions, "outputPath"> & {
    textureCacheDir: string;
    variableState?: VariableStateMap;
};

export type ConvertModToGlbResult = {
    iniPath: string;
    outputPath: string;
    meshCount: number;
    warningCount: number;
};

export type ConvertModToGlbBufferResult = {
    iniPath: string;
    glb: Buffer;
    meshCount: number;
    warningCount: number;
};

type DrawInstruction = {
    ibResourceName?: string;
    indexCount: number;
    startIndex: number;
    baseVertex: number;
    condition?: IniConditionClause[];
};

type IniConditionClause = {
    expression: string;
    expected: boolean;
};

type IniBranchFrame = {
    activeClauses: IniConditionClause[];
    inverseClauses: IniConditionClause[];
};

type TextureOverrideBinding = TextureBinding & {
    sectionName: string;
    draws: DrawInstruction[];
};

type SlotVariableBinding = {
    slot: number;
    variable: string;
    values: VariableStateValue[];
};

const variantArtifactManifestLocks = new Map<string, Promise<void>>();
const normalizeKeyCache = new Map<string, string>();
const MAX_NORMALIZE_KEY_CACHE = 4096;
const DEFAULT_TEXTURE_FORMAT: StaticGlbTextureFormat = "jpeg-safe";
const DEFAULT_JPEG_QUALITY = 85;

async function withVariantArtifactManifestLock<T>(
    artifactRoot: string,
    operation: () => Promise<T>,
): Promise<T> {
    const previous = variantArtifactManifestLocks.get(artifactRoot) ?? Promise.resolve();
    let release!: () => void;
    const current = new Promise<void>((resolve) => {
        release = resolve;
    });
    // Store the tail promise so later callers queue behind the currently scheduled operation.
    const queued = previous.catch(() => undefined).then(() => current);
    variantArtifactManifestLocks.set(artifactRoot, queued);

    await previous.catch(() => undefined);
    try {
        return await operation();
    } finally {
        release();
        if (variantArtifactManifestLocks.get(artifactRoot) === queued) {
            variantArtifactManifestLocks.delete(artifactRoot);
        }
    }
}

async function writeVariantManifestAtomic(
    manifestPath: string,
    manifest: StaticGlbVariantManifest,
): Promise<void> {
    await writeFileAtomic(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, {
        encoding: "utf8",
    });
}

export async function convertModToGlb(
    options: ConvertModToGlbOptions,
): Promise<ConvertModToGlbResult> {
    const isDebug = !!options.debug;
    const outputDir = path.dirname(path.resolve(options.outputPath));
    const textureCacheDir = isDebug
        ? path.resolve(outputDir, "texture-cache")
        : path.resolve(outputDir, `.texture-cache-${nanoid()}`);

    try {
        const glbResult = await buildModGlb({
            ...options,
            textureCacheDir,
        });

        await fse.ensureDir(outputDir);
        await fse.writeFile(options.outputPath, glbResult.glb);

        return {
            iniPath: glbResult.iniPath,
            outputPath: path.resolve(options.outputPath),
            meshCount: glbResult.meshCount,
            warningCount: glbResult.warningCount,
        };
    } finally {
        if (!isDebug && (await fse.pathExists(textureCacheDir))) {
            await fse.rm(textureCacheDir, { recursive: true, force: true });
        }
    }
}

export async function convertModToGlbBuffer(
    options: ConvertModToGlbBufferOptions,
): Promise<ConvertModToGlbBufferResult> {
    return buildModGlb(options);
}

export async function convertModToVariantArtifacts(
    options: Omit<ConvertModToGlbOptions, "outputPath"> & {
        artifactRoot: string;
        preGenerateVariableStates?: boolean;
    },
): Promise<ConvertModVariantArtifactsResult | null> {
    const analysis = await analyzeModVariants(options);
    if (analysis.variables.length === 0) {
        return null;
    }

    const artifactRoot = path.resolve(options.artifactRoot);
    const glbDir = path.join(artifactRoot, "glb");
    const uiDir = path.join(artifactRoot, "ui");
    const textureCacheDir = path.join(artifactRoot, ".texture-cache");
    await fse.ensureDir(glbDir);

    const statesToGenerate = new Map<string, VariableStateMap>();
    statesToGenerate.set(createStateKey(analysis.defaultState), analysis.defaultState);

    if (options.preGenerateVariableStates !== false) {
        for (const variable of analysis.variables) {
            for (const entry of variable.values) {
                const nextState = {
                    ...analysis.defaultState,
                    [variable.id]: entry.value,
                };
                statesToGenerate.set(createStateKey(nextState), nextState);
            }
        }
    }

    const warning = createWarningCollector(options.onWarning);
    const states: StaticGlbVariantManifest["states"] = [];
    let defaultGlbPath = "";
    let meshCount = 0;

    try {
        for (const [key, state] of statesToGenerate) {
            const glbName = createStateArtifactFileName(key);
            const glbPath = path.join(glbDir, glbName);
            const result = await buildModGlb({
                ...options,
                textureCacheDir,
                variableState: state,
            });
            await fse.writeFile(glbPath, result.glb);
            meshCount = Math.max(meshCount, result.meshCount);
            states.push({
                key,
                values: state,
                glbPath,
            });
            if (key === createStateKey(analysis.defaultState)) {
                defaultGlbPath = glbPath;
            }
        }

        const uiAssets = await materializeViewerUiAssets(
            analysis.uiAssets,
            path.dirname(analysis.iniPath),
            uiDir,
            options,
            warning.warn,
        );
        const variables = await Promise.all(
            analysis.variables.map(async (variable) => ({
                ...variable,
                iconPath: variable.iconPath
                    ? await materializeUiAsset(
                          variable.iconPath,
                          uiDir,
                          `item-${variable.slot ?? variable.order}`,
                          warning.warn,
                          options,
                      )
                    : undefined,
            })),
        );
        const manifest: StaticGlbVariantManifest = {
            version: 1,
            name: path.basename(path.dirname(analysis.iniPath)),
            modPath: path.dirname(analysis.iniPath),
            iniPath: analysis.iniPath,
            defaultState: analysis.defaultState,
            variables,
            uiAssets,
            shapeKeys: analysis.shapeKeys,
            states,
        };
        const manifestPath = path.join(artifactRoot, "manifest.json");
        await writeVariantManifestAtomic(manifestPath, manifest);

        return {
            iniPath: analysis.iniPath,
            artifactRoot,
            defaultGlbPath,
            meshCount,
            warningCount: warning.count,
            manifestPath,
            manifest,
        };
    } finally {
        if (!options.debug && (await fse.pathExists(textureCacheDir))) {
            await fse.rm(textureCacheDir, { recursive: true, force: true });
        }
    }
}

export async function resolveVariantStateArtifact(
    options: Omit<ConvertModToGlbOptions, "outputPath"> & {
        artifactRoot: string;
        state: VariableStateMap;
        manifestPath?: string;
    },
): Promise<{
    glbPath: string;
    manifestPath: string;
    manifest: StaticGlbVariantManifest;
    meshCount: number;
    warningCount: number;
}> {
    return withVariantArtifactManifestLock(path.resolve(options.artifactRoot), async () => {
        const artifactRoot = path.resolve(options.artifactRoot);
        const manifestPath = options.manifestPath
            ? path.resolve(options.manifestPath)
            : path.join(artifactRoot, "manifest.json");
        let manifest: StaticGlbVariantManifest;
        try {
            manifest = (await fse.readJson(manifestPath)) as StaticGlbVariantManifest;
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            throw new Error(`Failed to read variant manifest at ${manifestPath}: ${message}`);
        }

        const key = createStateKey(options.state);
        const existing = manifest.states.find((entry) => entry.key === key);
        if (existing && (await fse.pathExists(existing.glbPath))) {
            return {
                glbPath: existing.glbPath,
                manifestPath,
                manifest,
                meshCount: 0,
                warningCount: 0,
            };
        }

        const textureCacheDir = path.join(artifactRoot, ".texture-cache");
        try {
            const result = await buildModGlb({
                ...options,
                modPath: manifest.modPath,
                textureCacheDir,
                variableState: options.state,
            });
            const glbDir = path.join(artifactRoot, "glb");
            await fse.ensureDir(glbDir);
            const glbPath = path.join(glbDir, createStateArtifactFileName(key));
            await fse.writeFile(glbPath, result.glb);

            const current = manifest.states.find((entry) => entry.key === key);
            if (!current) {
                const entry = {
                    key,
                    values: options.state,
                    glbPath,
                };
                manifest.states.push(entry);
                await writeVariantManifestAtomic(manifestPath, manifest);
                return {
                    glbPath,
                    manifestPath,
                    manifest,
                    meshCount: result.meshCount,
                    warningCount: result.warningCount,
                };
            }

            if (current.glbPath !== glbPath) {
                current.glbPath = glbPath;
                await writeVariantManifestAtomic(manifestPath, manifest);
            }

            return {
                glbPath,
                manifestPath,
                manifest,
                meshCount: result.meshCount,
                warningCount: result.warningCount,
            };
        } finally {
            await fse.rm(textureCacheDir, { recursive: true, force: true }).catch(() => {});
        }
    });
}

async function buildModGlb(
    options: ConvertModToGlbBufferOptions,
): Promise<ConvertModToGlbBufferResult> {
    const warning = createWarningCollector(options.onWarning);
    const { iniPath, sections } = await loadIniBundle(options.modPath);
    const modDir = path.dirname(iniPath);
    const defaultVariables = collectDefaultIniVariables(sections);
    const resolvedVariables = mergeVariableState(defaultVariables, options.variableState);
    const resources = collectResources(sections);
    const sectionByFullName = new Map(
        sections.map((section) => [normalizeKey(getSectionFullName(section)), section]),
    );
    const bufferGroups = await collectBufferGroups(modDir, resources, warning.warn);
    const textureBindings = collectTextureBindings(sections, sectionByFullName, resolvedVariables);
    const drawBindings = collectTextureOverrideDrawBindings(sections);
    const ibResources = collectIbResources(
        sections,
        resources,
        bufferGroups,
        sectionByFullName,
        resolvedVariables,
        textureBindings,
        drawBindings,
    );

    options.logger?.debug(
        `Found ${resources.length} resources, ${bufferGroups.length} buffer groups, ${ibResources.length} IB resources, ${textureBindings.length} texture bindings`,
        "StaticGLB",
    );

    if (ibResources.length === 0) {
        throw new Error(`No index buffer Resource sections were found in ${iniPath}`);
    }

    const builder = new GlbBuilder();
    const materialBindings = await buildMaterials(
        builder,
        options,
        modDir,
        options.textureCacheDir,
        resources,
        textureBindings,
        warning.warn,
    );

    for (const ib of ibResources) {
        const group =
            bufferGroups.find((candidate) => strictKeyMatchesIb(candidate.key, ib.key)) ||
            bufferGroups.find((candidate) => keyMatchesIb(candidate.key, ib.key));
        if (!group) {
            warning.warn(`No matching vertex buffer found for ${ib.filename}`);
            continue;
        }

        const fmt = await loadFmtForIb(modDir, options.assetPath, ib, group.stride);
        const ibPath = path.resolve(modDir, ib.filename);
        if (!(await fse.pathExists(ibPath))) {
            warning.warn(`Missing IB file: ${ibPath}`);
            continue;
        }

        const indices = decodeIndices(await fse.readFile(ibPath), ib.format || fmt.indexFormat);
        if (indices.length === 0) {
            warning.warn(`Empty IB file: ${ibPath}`);
            continue;
        }
        const activeIndices = buildIndicesForState(
            drawBindings.filter(
                (binding) => normalizeKey(binding.ibResourceName) === normalizeKey(ib.name),
            ),
            indices,
            resolvedVariables,
            warning.warn,
        );

        const material = materialBindings.get(normalizeKey(ib.name));
        const primitive = buildPrimitive(
            builder,
            group.vbBytes,
            group.stride,
            fmt,
            activeIndices,
            {
                includeTangents: !!options.includeTangents,
                includeVertexColors: !material,
            },
            warning.warn,
        );
        if (!primitive) {
            warning.warn(`Could not build primitive for ${ib.filename}`);
            continue;
        }

        if (material) {
            primitive.material = material.materialIndex;
        }

        builder.addMesh(path.basename(ib.filename, path.extname(ib.filename)), primitive);
    }

    if (builder.meshCount() === 0) {
        throw new Error(
            "No mesh primitives were written. Check that mod resources match asset layout files.",
        );
    }

    return {
        iniPath,
        glb: builder.toGlb(),
        meshCount: builder.meshCount(),
        warningCount: warning.count,
    };
}

function createWarningCollector(onWarning?: (message: string) => void) {
    let count = 0;
    return {
        get count() {
            return count;
        },
        warn(message: string) {
            count += 1;
            onWarning?.(message);
        },
    };
}

async function loadIniBundle(input: string): Promise<{ iniPath: string; sections: IniSection[] }> {
    const iniPath = await findIni(input);
    const iniText = await fse.readFile(iniPath, "utf8");
    const sections = parseIni(iniText);
    const mergedRefs = extractMergedIniRefs(iniText, path.dirname(iniPath));

    if (mergedRefs.length === 0) {
        return { iniPath, sections };
    }

    const extraSections = (
        await Promise.all(
            mergedRefs
                .filter((refPath) => path.resolve(refPath) !== path.resolve(iniPath))
                .map(async (refPath) => {
                    if (!(await fse.pathExists(refPath))) {
                        return [];
                    }
                    const refText = await fse.readFile(refPath, "utf8");
                    return parseIni(refText);
                }),
        )
    ).flat();

    return {
        iniPath,
        sections: [...sections, ...extraSections],
    };
}

async function findIni(input: string): Promise<string> {
    const resolved = path.resolve(input);
    const stat = await fse.stat(resolved);
    if (stat.isFile()) return resolved;

    const candidates = await fg("**/*.ini", {
        cwd: resolved,
        absolute: true,
        onlyFiles: true,
        ignore: ["**/disabled*.ini"],
        caseSensitiveMatch: false,
    });

    if (candidates.length === 0) {
        throw new Error(`No .ini found in ${input}`);
    }

    const scored = await Promise.all(
        candidates.map(async (candidate) => ({
            path: candidate,
            score: scoreIniCandidate(candidate, await fse.readFile(candidate, "utf8")),
        })),
    );
    scored.sort((left, right) => right.score - left.score || left.path.localeCompare(right.path));
    return scored[0].path;
}

function scoreIniCandidate(candidatePath: string, text: string): number {
    const basename = path.basename(candidatePath).toLowerCase();
    let score = 0;

    if (basename === "merged.ini") score += 120;
    if (basename.startsWith("master") && basename.endsWith(".ini")) score += 140;
    if (text.includes("; Merged Mod:")) score += 80;
    if (/^\s*namespace\s*=.+$/im.test(text)) score += 60;

    const persistCount = (text.match(/^\s*global\s+persist\s+\$/gim) || []).length;
    const cycleCount = (text.match(/^\s*type\s*=\s*cycle\s*$/gim) || []).length;
    const overrideCount = (text.match(/^\s*\[TextureOverride/gim) || []).length;
    const resourceCount = (text.match(/^\s*\[Resource/gim) || []).length;

    score += persistCount * 15;
    score += cycleCount * 10;
    score += Math.min(overrideCount, 50);
    score += Math.min(resourceCount, 50);

    if (/^\s*\[KeyHelp\]/im.test(text)) score -= 25;
    if (basename.startsWith("disabled") && !text.includes("; Merged Mod:")) score -= 10;

    return score;
}

function extractMergedIniRefs(text: string, baseDir: string): string[] {
    const firstLine = text.split(/\r?\n/, 1)[0] || "";
    const match = firstLine.match(/;\s*Merged Mod:\s*(.+)$/i);
    if (!match) {
        return [];
    }

    return match[1]
        .split(",")
        .map((entry) => entry.trim())
        .filter(Boolean)
        .map((entry) => path.resolve(baseDir, entry));
}

function parseIni(text: string): IniSection[] {
    const sections: IniSection[] = [];
    let current: IniSection | null = null;

    for (const rawLine of text.split(/\r?\n/)) {
        const line = rawLine.trim();
        if (!line || line.startsWith(";")) continue;

        const headerMatch = line.match(/^\[([^\]]+)\]$/);
        if (headerMatch) {
            const full = headerMatch[1].trim();
            const kindMatch = full.match(
                /^(TextureOverride|ShaderOverride|Resource|Constants|Present|CommandList|CustomShader)(.*)$/,
            );
            current = {
                header: kindMatch ? kindMatch[1] : full,
                name: kindMatch ? kindMatch[2] : full,
                lines: [],
                values: {},
            };
            sections.push(current);
            continue;
        }

        if (!current) continue;
        current.lines.push(stripInlineComment(line));
        const eq = line.indexOf("=");
        if (eq < 0) continue;
        const key = line.slice(0, eq).trim();
        const value = stripInlineComment(line.slice(eq + 1).trim());
        current.values[key] = value;
    }

    return sections;
}

function stripInlineComment(value: string): string {
    let quote: '"' | "'" | null = null;
    for (let index = 0; index < value.length; index++) {
        const current = value[index];
        if ((current === '"' || current === "'") && value[index - 1] !== "\\") {
            quote = quote === current ? null : quote ? quote : (current as '"' | "'");
            continue;
        }
        if (quote) {
            continue;
        }
        if (current === ";" && index > 0 && /\s/.test(value[index - 1])) {
            return value.slice(0, index).trim();
        }
    }
    return value;
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

async function collectBufferGroups(
    modDir: string,
    resources: Resource[],
    warn: (message: string) => void,
): Promise<BufferGroup[]> {
    const byKey = new Map<string, BufferResourceGroup>();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const typedResource = parseBufferGroupResourceName(resource.name);
        if (typedResource) {
            const { key, kind } = typedResource;
            if (kind === "position") {
                ensureBufferResourceGroup(byKey, key).position = resource;
            } else if (kind === "blend") {
                ensureBufferResourceGroup(byKey, key).blend = resource;
            } else {
                ensureBufferResourceGroup(byKey, key).texcoord = resource;
            }
        } else if (
            !isShapeKeyPositionVariantResource(resource.name) &&
            (resource.filename.toLowerCase().endsWith(".buf") ||
                resource.filename.toLowerCase().endsWith(".vb"))
        ) {
            ensureBufferResourceGroup(byKey, resource.name).single = resource;
        }
    }

    const groups: BufferGroup[] = [];
    for (const [key, group] of byKey) {
        if (group.single?.filename && group.single.stride) {
            const filePath = path.resolve(modDir, group.single.filename);
            if (!(await fse.pathExists(filePath))) {
                warn(`Missing vertex buffer file: ${filePath}`);
                continue;
            }
            groups.push({
                key,
                vbFilename: group.single.filename,
                vbBytes: await fse.readFile(filePath),
                stride: group.single.stride,
            });
            continue;
        }

        if (group.position?.filename && group.blend?.filename && group.texcoord?.filename) {
            const positionResource = group.position;
            const blendResource = group.blend;
            const texcoordResource = group.texcoord;
            const [position, blend, texcoord] = await Promise.all([
                readResourceBytes(modDir, positionResource),
                readResourceBytes(modDir, blendResource),
                readResourceBytes(modDir, texcoordResource),
            ]);
            const positionStride = positionResource.stride;
            const blendStride = blendResource.stride;
            const texcoordStride = texcoordResource.stride;
            if (
                positionStride === undefined ||
                blendStride === undefined ||
                texcoordStride === undefined
            ) {
                warn(`Skipping incomplete interleaved buffer group: ${key}`);
                continue;
            }
            const stride = positionStride + blendStride + texcoordStride;
            const vertexCount = Math.min(
                Math.floor(position.length / positionStride),
                Math.floor(blend.length / blendStride),
                Math.floor(texcoord.length / texcoordStride),
            );
            const vb = interleaveVertexBuffers(
                position,
                positionStride,
                blend,
                blendStride,
                texcoord,
                texcoordStride,
            );
            if (vb.length !== vertexCount * stride) {
                throw new Error(`Unexpected interleaved buffer length for ${key}`);
            }
            groups.push({ key, vbFilename: `${key}.vb`, vbBytes: vb, stride });
        }
    }

    return groups;
}

function parseBufferGroupResourceName(
    resourceName: string,
): { key: string; kind: "position" | "blend" | "texcoord" } | null {
    const typedMatch = resourceName.match(/^(.*?)(Position|Blend|Texcoord)(\.\d+)?$/i);
    if (typedMatch) {
        const [, prefix, kind, suffix = ""] = typedMatch;
        return {
            key: `${prefix}${suffix}`,
            kind: kind.toLowerCase() as "position" | "blend" | "texcoord",
        };
    }

    const basePositionMatch = resourceName.match(/^(.*?)PositionBase(\.\d+)?$/i);
    if (basePositionMatch) {
        const [, prefix, suffix = ""] = basePositionMatch;
        return { key: `${prefix}${suffix}`, kind: "position" };
    }

    return null;
}

function isShapeKeyPositionVariantResource(resourceName: string): boolean {
    return /Position(?!Base(?:\.|$))[\w.-]+$/i.test(resourceName);
}

function ensureBufferResourceGroup(
    map: Map<string, BufferResourceGroup>,
    key: string,
): BufferResourceGroup {
    let value = map.get(key);
    if (!value) {
        value = {};
        map.set(key, value);
    }
    return value;
}

async function readResourceBytes(modDir: string, resource: Resource): Promise<Buffer> {
    const filePath = path.resolve(modDir, resource.filename!);
    if (!(await fse.pathExists(filePath))) throw new Error(`Missing resource file: ${filePath}`);
    return await fse.readFile(filePath);
}

function collectIbResources(
    sections: IniSection[],
    resources: Resource[],
    bufferGroups: BufferGroup[],
    sectionByFullName: Map<string, IniSection>,
    resolvedVariables: Map<string, number | string>,
    textureBindings: TextureBinding[],
    drawBindings: TextureOverrideBinding[],
): IbResource[] {
    const bufferKeys = bufferGroups.map((group) => group.key);
    const bindingsByIbName = new Map<string, TextureBinding[]>();
    for (const binding of textureBindings) {
        const key = normalizeKey(binding.ibResourceName);
        const group = bindingsByIbName.get(key) ?? [];
        group.push(binding);
        bindingsByIbName.set(key, group);
    }
    const referencedIbNames = new Set([
        ...bindingsByIbName.keys(),
        ...drawBindings.map((binding) => normalizeKey(binding.ibResourceName)),
    ]);
    const activeIbNames = new Set(
        sections
            .filter((section) => section.header === "TextureOverride")
            .map((section) =>
                resolveAssignmentFromSection(
                    section,
                    ["ib"],
                    sectionByFullName,
                    resolvedVariables,
                ).get("ib"),
            )
            .filter((value): value is string => !!value)
            .map(trimResourcePrefix)
            .map(normalizeKey),
    );

    return resources
        .filter((resource) => {
            if (!resource.filename || !resource.format) return false;

            const lowerFilename = resource.filename.toLowerCase();
            if (lowerFilename.endsWith(".ib")) {
                return activeIbNames.size === 0 || activeIbNames.has(normalizeKey(resource.name));
            }

            return referencedIbNames.has(normalizeKey(resource.name));
        })
        .map((resource) => {
            const stem = path.basename(resource.filename!, path.extname(resource.filename!));
            const key = bestKeyForIb(stem, resource.name, bufferKeys);
            const bindings = bindingsByIbName.get(normalizeKey(resource.name)) ?? [];
            const overrideHashes = Array.from(
                new Set(
                    bindings
                        .map((binding) => binding.overrideHash?.trim())
                        .filter((value): value is string => !!value),
                ),
            );
            return {
                name: resource.name,
                filename: resource.filename!,
                format: resource.format!,
                key,
                overrideHash: overrideHashes[0],
                overrideHashes,
            };
        });
}

function collectTextureBindings(
    sections: IniSection[],
    sectionByFullName: Map<string, IniSection>,
    resolvedVariables: Map<string, number | string>,
): TextureBinding[] {
    const bindings: TextureBinding[] = [];
    const overrideTextureResources = sections
        .filter(
            (section) => section.header === "TextureOverride" || section.header === "CommandList",
        )
        .map((section) => {
            const resourceName = resolveSectionResourceName(
                section,
                sectionByFullName,
                resolvedVariables,
            );
            if (!resourceName) return null;
            return {
                sectionName: section.name,
                resourceName,
            };
        })
        .filter((entry): entry is { sectionName: string; resourceName: string } => !!entry);

    for (const section of sections) {
        if (section.header !== "TextureOverride") continue;

        const assignments = resolveAssignmentFromSection(
            section,
            [
                "ib",
                "this",
                "run",
                "ps-t0",
                "ps-t1",
                "ps-t2",
                "ps-t3",
                "ps-t4",
                "ps-t5",
                "ps-t6",
                "ps-t7",
                "ps-t8",
                "ps-t9",
                "ps-t10",
                "Resource\\ZZMI\\Diffuse",
            ],
            sectionByFullName,
            resolvedVariables,
        );
        const ibValues = collectSectionIbResourceNames(section);
        const resolvedIbValue = assignments.get("ib") || section.values.ib;
        if (ibValues.length === 0 && resolvedIbValue) {
            ibValues.push(trimResourcePrefix(resolvedIbValue));
        }
        if (ibValues.length === 0) continue;

        const textureResourceNames = Array.from(assignments.entries())
            .filter(([key, value]) => {
                if (!value) return false;
                const lowerKey = key.toLowerCase();
                const lowerValue = value.toLowerCase();
                return (
                    (/^pst\d+$/.test(lowerKey) ||
                        lowerKey === normalizeKey("Resource\\ZZMI\\Diffuse")) &&
                    (lowerValue.startsWith("resource") || lowerValue.startsWith("ref resource"))
                );
            })
            .map(([, value]) =>
                resolveTextureResourceReference(
                    trimResourcePrefix(value.replace(/^ref\s+/i, "")),
                    section,
                    sectionByFullName,
                    resolvedVariables,
                ),
            )
            .filter((name): name is string => !!name);

        for (const ibValue of ibValues) {
            const diffuseResourceName = resolveTextureResourceReference(
                textureResourceNames.find((name) => name.toLowerCase().includes("diffuse")) ||
                    textureResourceNames.find((name) => {
                        const lower = name.toLowerCase();
                        return !lower.includes("normal") && !lower.includes("light");
                    }) ||
                    resolveSectionResourceName(section, sectionByFullName, resolvedVariables) ||
                    resolveOverrideDiffuseResource(
                        section.name,
                        trimResourcePrefix(ibValue),
                        overrideTextureResources,
                    ),
                section,
                sectionByFullName,
                resolvedVariables,
            );

            bindings.push({
                ibResourceName: trimResourcePrefix(ibValue),
                diffuseResourceName,
                textureResourceNames,
                overrideHash: section.values.hash?.trim(),
            });
        }
    }
    return bindings;
}

function collectSectionIbResourceNames(section: IniSection): string[] {
    const names = new Set<string>();
    for (const line of section.lines) {
        const match = line.trim().match(/^([^=]+?)\s*=\s*(.+)$/);
        if (!match || normalizeKey(match[1].trim()) !== "ib") continue;
        const name = trimResourcePrefix(match[2].trim());
        if (name) {
            names.add(name);
        }
    }

    if (names.size === 0 && section.values.ib) {
        names.add(trimResourcePrefix(section.values.ib));
    }

    return Array.from(names);
}

function collectTextureOverrideDrawBindings(sections: IniSection[]): TextureOverrideBinding[] {
    const variables = collectDefaultIniVariables(sections);
    const sectionByFullName = new Map(
        sections.map((section) => [normalizeKey(getSectionFullName(section)), section]),
    );

    return sections
        .filter((section) => section.header === "TextureOverride")
        .flatMap((section) => {
            const draws = collectSectionDrawInstructions(section, variables, sectionByFullName);
            const byIb = new Map<string, DrawInstruction[]>();
            for (const draw of draws) {
                if (!draw.ibResourceName) continue;
                const key = normalizeKey(draw.ibResourceName);
                const group = byIb.get(key) ?? [];
                group.push(draw);
                byIb.set(key, group);
            }

            return Array.from(byIb.values()).map((group) => ({
                sectionName: section.name,
                ibResourceName: group[0].ibResourceName!,
                diffuseResourceName: undefined,
                overrideHash: section.values.hash?.trim(),
                draws: group,
            }));
        });
}

function collectSectionDrawInstructions(
    section: IniSection,
    variables: Map<string, number | string>,
    sectionByFullName: Map<string, IniSection>,
    inheritedClauses: IniConditionClause[] = [],
    inheritedIbResourceName?: string,
    visited = new Set<string>(),
): DrawInstruction[] {
    const instructions: DrawInstruction[] = [];
    const stack: IniBranchFrame[] = [];
    let currentIbResourceName = inheritedIbResourceName;
    const normalizedName = normalizeKey(getSectionFullName(section));
    if (visited.has(normalizedName)) {
        return instructions;
    }
    visited.add(normalizedName);

    for (const rawLine of section.lines) {
        const trimmed = rawLine.trim();
        const lower = trimmed.toLowerCase();

        if (lower.startsWith("if ")) {
            const expression = trimmed.slice(3).trim();
            stack.push({
                // Each frame carries the clauses required for the current branch and the
                // accumulated inverse used by later `elif` / `else` branches.
                activeClauses: [{ expression, expected: true }],
                inverseClauses: [{ expression, expected: false }],
            });
            continue;
        }

        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            const previous = stack.pop();
            const expression = (
                lower.startsWith("elif ") ? trimmed.slice(5) : trimmed.slice(8)
            ).trim();
            stack.push({
                activeClauses: [
                    ...(previous?.inverseClauses ?? []),
                    { expression, expected: true },
                ],
                inverseClauses: [
                    ...(previous?.inverseClauses ?? []),
                    { expression, expected: false },
                ],
            });
            continue;
        }

        if (lower === "else") {
            const previous = stack.pop();
            if (!previous) continue;
            stack.push({
                activeClauses: previous.inverseClauses,
                inverseClauses: [],
            });
            continue;
        }

        if (lower === "endif") {
            stack.pop();
            continue;
        }

        const runMatch = trimmed.match(/^run\s*=\s*(.+)$/i);
        if (runMatch) {
            const nestedSection = sectionByFullName.get(normalizeKey(runMatch[1].trim()));
            if (!nestedSection) {
                continue;
            }

            const activeConditions = [
                ...inheritedClauses,
                ...stack.flatMap((entry) => entry.activeClauses),
            ];
            instructions.push(
                ...collectSectionDrawInstructions(
                    nestedSection,
                    variables,
                    sectionByFullName,
                    activeConditions,
                    currentIbResourceName,
                    new Set(visited),
                ),
            );
            continue;
        }

        const assignmentMatch = trimmed.match(/^([^=]+?)\s*=\s*(.+)$/);
        if (assignmentMatch && normalizeKey(assignmentMatch[1].trim()) === "ib") {
            currentIbResourceName = trimResourcePrefix(assignmentMatch[2].trim());
            continue;
        }

        const drawMatch = trimmed.match(/^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$/i);
        if (!drawMatch) continue;

        const indexCount = evaluateIniNumericExpression(drawMatch[1], variables, normalizeKey);
        const startIndex = evaluateIniNumericExpression(drawMatch[2], variables, normalizeKey);
        const baseVertex = evaluateIniNumericExpression(drawMatch[3], variables, normalizeKey);
        if (indexCount === null || startIndex === null || baseVertex === null) {
            continue;
        }

        const activeConditions = [
            ...inheritedClauses,
            ...stack.flatMap((entry) => entry.activeClauses),
        ];
        instructions.push({
            ibResourceName: currentIbResourceName,
            indexCount,
            startIndex,
            baseVertex,
            condition: activeConditions.length > 0 ? activeConditions : undefined,
        });
    }

    return instructions;
}

function buildIndicesForState(
    bindings: TextureOverrideBinding[],
    indices: Uint32Array,
    variables: Map<string, number | string>,
    warn: (message: string) => void,
): Uint32Array {
    const activeDraws = bindings.flatMap((binding) =>
        binding.draws.filter(
            (draw) =>
                !draw.condition ||
                draw.condition.every(
                    (clause) =>
                        evaluateIniCondition(clause.expression, variables, normalizeKey) ===
                        clause.expected,
                ),
        ),
    );

    if (activeDraws.length === 0) {
        return indices;
    }

    const result = mergeDrawIndices(uint32ArrayToBuffer(indices), activeDraws);
    for (const message of result.invalidRanges) {
        warn(message);
    }
    return bufferToUint32Array(result.indices);
}

function resolveOverrideDiffuseResource(
    sectionName: string,
    ibResourceName: string,
    overrideTextureResources: Array<{ sectionName: string; resourceName: string }>,
): string | undefined {
    const exactSection = `${sectionName}Diffuse`.toLowerCase();
    const ibStem = ibResourceName.replace(/IB$/i, "");
    const exactResource = `${ibStem}Diffuse`.toLowerCase();
    const familyStem = ibStem.replace(/(?<=[a-z0-9])[A-Z]$/g, "");
    const sectionAliases = buildDiffuseLookupAliases(sectionName);
    const ibAliases = buildDiffuseLookupAliases(ibStem);
    const familyAliases = buildDiffuseLookupAliases(familyStem);

    const preferred = overrideTextureResources.filter((entry) => {
        const sectionLower = entry.sectionName.toLowerCase();
        const resourceLower = entry.resourceName.toLowerCase();
        return sectionLower.includes("diffuse") || resourceLower.includes("diffuse");
    });

    return (
        preferred.find((entry) => entry.sectionName.toLowerCase() === exactSection)?.resourceName ||
        preferred.find((entry) =>
            sectionAliases.some((alias) => entry.sectionName.toLowerCase() === `${alias}diffuse`),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase() === exactResource)
            ?.resourceName ||
        preferred.find((entry) =>
            ibAliases.some((alias) => entry.resourceName.toLowerCase() === `${alias}diffuse`),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(sectionName.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            sectionAliases.some((alias) => entry.sectionName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            familyAliases.some((alias) => entry.sectionName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase().startsWith(ibStem.toLowerCase()))
            ?.resourceName ||
        preferred.find((entry) =>
            ibAliases.some((alias) => entry.resourceName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.resourceName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            familyAliases.some((alias) => entry.resourceName.toLowerCase().startsWith(alias)),
        )?.resourceName
    );
}

function buildDiffuseLookupAliases(value: string): string[] {
    const aliases = new Set<string>();
    const trimmed = value.trim();
    if (!trimmed) {
        return [];
    }

    aliases.add(trimmed.toLowerCase());

    const resourceTrimmed = trimmed.replace(/^resource/i, "");
    if (resourceTrimmed && resourceTrimmed !== trimmed) {
        aliases.add(resourceTrimmed.toLowerCase());
    }

    const meshSuffixMatch = resourceTrimmed.match(
        /(head|body|dress|hair|face|weapon|glasses|cloth|skirt|shoe|arm|leg|hand|foot)[a-z0-9]*$/i,
    );
    if (meshSuffixMatch) {
        aliases.add(meshSuffixMatch[0].toLowerCase());
        const familyAlias = meshSuffixMatch[1];
        if (familyAlias) {
            aliases.add(familyAlias.toLowerCase());
        }
    }

    return Array.from(aliases).filter(Boolean);
}

function getSectionFullName(section: IniSection): string {
    return `${section.header}${section.name}`;
}

function collectDefaultIniVariables(sections: IniSection[]): Map<string, number | string> {
    const variables = new Map<string, number | string>();
    for (const section of sections) {
        if (section.header !== "Constants") continue;
        for (const line of section.lines) {
            const match = line.match(
                /^(?:global|local)(?:\s+persist)?\s+([$\w\\.\\]+)(?:\s*=\s*(.+))?$/i,
            );
            if (!match) continue;
            variables.set(normalizeKey(match[1]), parseIniScalar(match[2]));
        }
    }
    return variables;
}

function parseIniScalar(value?: string): number | string {
    if (!value) return 0;
    const trimmed = value.trim();
    if (/^-?\d+(?:\.\d+)?$/.test(trimmed)) {
        return Number(trimmed);
    }
    return trimmed;
}

function resolveSectionResourceName(
    section: IniSection,
    sectionByFullName: Map<string, IniSection>,
    defaultVariables: Map<string, number | string>,
    visited = new Set<string>(),
): string | undefined {
    const assignment = resolveAssignmentFromSection(
        section,
        ["this", "resource\\zzmi\\diffuse"],
        sectionByFullName,
        defaultVariables,
        visited,
    );
    const directThis =
        assignment.get("this") || assignment.get(normalizeKey("Resource\\ZZMI\\Diffuse"));
    return directThis?.toLowerCase().includes("resource")
        ? trimResourcePrefix(directThis.replace(/^ref\s+/i, ""))
        : undefined;
}

function resolveTextureResourceReference(
    resourceName: string | undefined,
    section: IniSection,
    sectionByFullName: Map<string, IniSection>,
    variables: Map<string, number | string>,
    visited = new Set<string>(),
): string | undefined {
    if (!resourceName) {
        return undefined;
    }

    const normalizedName = normalizeKey(resourceName);
    if (!normalizedName || visited.has(normalizedName)) {
        return resourceName;
    }
    visited.add(normalizedName);

    const lookupKeys = buildResourceAssignmentLookupKeys(resourceName);
    const assignments = resolveAssignmentFromSection(
        section,
        lookupKeys,
        sectionByFullName,
        variables,
    );
    const nextValue = lookupKeys
        .map((key) => assignments.get(normalizeKey(key)))
        .find((value) => !!value);
    if (!nextValue) {
        return resourceName;
    }

    const nextResourceName = trimResourcePrefix(nextValue.replace(/^ref\s+/i, ""));
    if (!nextResourceName || normalizeKey(nextResourceName) === normalizedName) {
        return resourceName;
    }

    return resolveTextureResourceReference(
        nextResourceName,
        section,
        sectionByFullName,
        variables,
        visited,
    );
}

function buildResourceAssignmentLookupKeys(resourceName: string): string[] {
    const keys = new Set<string>();
    const trimmed = resourceName.trim();
    if (!trimmed) {
        return [];
    }

    keys.add(trimmed);
    if (!/^resource/i.test(trimmed)) {
        keys.add(`Resource${trimmed}`);
    }
    return Array.from(keys);
}

function resolveAssignmentFromSection(
    section: IniSection,
    targetKeys: string[],
    sectionByFullName: Map<string, IniSection>,
    variables: Map<string, number | string>,
    visited = new Set<string>(),
): Map<string, string> {
    const wanted = new Set(targetKeys.map(normalizeKey));
    const normalizedName = normalizeKey(getSectionFullName(section));
    if (visited.has(normalizedName)) return new Map<string, string>();
    visited.add(normalizedName);

    const branchActive: boolean[] = [];
    const branchMatched: boolean[] = [];
    const isActive = () => branchActive.every(Boolean);
    const assignments = new Map<string, string>();

    for (const line of section.lines) {
        const trimmed = line.trim();
        const lower = trimmed.toLowerCase();

        if (lower.startsWith("if ")) {
            const parentActive = isActive();
            const matched = parentActive
                ? evaluateIniCondition(trimmed.slice(3), variables, normalizeKey)
                : false;
            branchActive.push(matched);
            branchMatched.push(matched);
            continue;
        }

        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            if (branchActive.length === 0) continue;
            const depth = branchActive.length - 1;
            const parentActive = branchActive.slice(0, depth).every(Boolean);
            const expression = lower.startsWith("elif ") ? trimmed.slice(5) : trimmed.slice(8);
            const matched =
                parentActive && !branchMatched[depth]
                    ? evaluateIniCondition(expression, variables, normalizeKey)
                    : false;
            branchActive[depth] = matched;
            branchMatched[depth] = branchMatched[depth] || matched;
            continue;
        }

        if (lower === "else") {
            if (branchActive.length === 0) continue;
            const depth = branchActive.length - 1;
            const parentActive = branchActive.slice(0, depth).every(Boolean);
            branchActive[depth] = parentActive && !branchMatched[depth];
            branchMatched[depth] = true;
            continue;
        }

        if (lower === "endif") {
            branchActive.pop();
            branchMatched.pop();
            continue;
        }

        if (!isActive()) continue;

        const runMatch = trimmed.match(/^run\s*=\s*(.+)$/i);
        if (runMatch) {
            const nestedSection = sectionByFullName.get(normalizeKey(runMatch[1].trim()));
            if (!nestedSection) {
                continue;
            }
            const nested = resolveAssignmentFromSection(
                nestedSection,
                targetKeys,
                sectionByFullName,
                variables,
                new Set(visited),
            );
            if (nested) {
                for (const [key, value] of nested) {
                    if (wanted.has(key)) {
                        assignments.set(key, value);
                    }
                }
            }
        }

        const assignmentMatch = trimmed.match(/^([^=]+?)\s*=\s*(.+)$/);
        if (!assignmentMatch) {
            continue;
        }

        const key = normalizeKey(assignmentMatch[1].trim());
        const value = assignmentMatch[2].trim();
        if (wanted.has(key)) {
            assignments.set(key, value);
        }
    }

    return assignments;
}

function mergeVariableState(
    base: Map<string, number | string>,
    overrides?: VariableStateMap,
): Map<string, number | string> {
    const merged = new Map(base);
    if (!overrides) {
        return merged;
    }

    for (const [key, value] of Object.entries(overrides)) {
        merged.set(normalizeKey(key), value);
    }

    return merged;
}

async function analyzeModVariants(options: {
    modPath: string;
    assetPath?: string;
    logger?: Logger;
    onWarning?: (message: string) => void;
}) {
    const { iniPath, sections } = await loadIniBundle(options.modPath);
    const modDir = path.dirname(iniPath);
    const defaultVariables = collectDefaultIniVariables(sections);
    const slotBindings = collectSlotVariableBindings(sections, defaultVariables);
    const resources = collectResources(sections);
    const shapeKeys = collectRealtimeShapeKeys(sections, resources, modDir);
    const realtimeShapeKeyVariableIds = new Set(
        shapeKeys.flatMap((shapeKey) =>
            shapeKey.dimensions.map((dimension) => normalizeKey(dimension.variableId)),
        ),
    );
    const variables = (
        await buildVariantVariables(slotBindings, sections, modDir, {
            ...options,
            realtimeShapeKeyVariableIds,
        })
    ).map((variable) => ({
        ...variable,
        defaultValue: defaultVariables.get(normalizeKey(variable.id)) ?? 0,
    }));

    return {
        iniPath,
        defaultState: mapToRecord(
            defaultVariables,
            variables.map((variable) => variable.id),
        ),
        variables,
        uiAssets: collectViewerUiAssetPaths(sections),
        shapeKeys,
    };
}

function collectSlotVariableBindings(
    sections: IniSection[],
    defaultVariables: Map<string, number | string>,
): SlotVariableBinding[] {
    const bindings: SlotVariableBinding[] = [
        ...collectKeyCycleBindings(sections),
        ...collectButtonAmountBindings(sections),
    ];
    const clickedSection = sections.find(
        (section) =>
            section.header === "CommandList" &&
            normalizeKey(section.name) === normalizeKey("ClickedSlot"),
    );
    if (!clickedSection) {
        return dedupeSlotBindings(bindings);
    }

    let currentSlot: number | null = null;

    for (const rawLine of clickedSection.lines) {
        const trimmed = rawLine.trim();
        const slotMatch = trimmed.match(/^(?:if|elif)\s+\$clickedslot\s*==\s*(\d+)$/i);
        if (slotMatch) {
            currentSlot = Number(slotMatch[1]);
            continue;
        }

        if (trimmed.toLowerCase() === "endif") {
            currentSlot = null;
            continue;
        }

        if (currentSlot === null) continue;

        const toggleMatch = trimmed.match(/^\$([\w.]+)\s*=\s*1\s*-\s*\$\1$/i);
        if (toggleMatch) {
            bindings.push({
                slot: currentSlot,
                variable: normalizeKey(toggleMatch[1]),
                values: [0, 1],
            });
            currentSlot = null;
            continue;
        }

        const incrementMatch = trimmed.match(/^\$([\w.]+)\s*=\s*\$\1\s*\+\s*1$/i);
        if (!incrementMatch) continue;

        const variable = normalizeKey(incrementMatch[1]);
        const defaultValue = Number(defaultVariables.get(variable) ?? 0);
        const variablePattern = escapeRegex(variable);
        const assignmentPattern = new RegExp(`^\\s*\\$${variablePattern}\\s*=\\s*(\\d+)\\s*$`, "i");
        const conditionPattern = new RegExp(
            `^\\s*(?:if|elif)\\s+\\$${variablePattern}\\s*>\\s*(\\d+)\\s*$`,
            "i",
        );
        const assignedValues = clickedSection.lines
            .map((line) => line.match(assignmentPattern))
            .filter((match): match is RegExpMatchArray => match !== null)
            .map((match) => Number(match[1]));
        const conditionalValues = clickedSection.lines
            .map((line) => line.match(conditionPattern))
            .filter((match): match is RegExpMatchArray => match !== null)
            .map((match) => Number(match[1]));
        const maxValue = Math.max(defaultValue, ...assignedValues, ...conditionalValues);
        const values = Array.from({ length: maxValue + 1 }, (_, index) => index);
        bindings.push({
            slot: currentSlot,
            variable,
            values,
        });
        currentSlot = null;
    }

    return dedupeSlotBindings(bindings);
}

function collectButtonAmountBindings(sections: IniSection[]): SlotVariableBinding[] {
    const bindings: SlotVariableBinding[] = [];

    for (const section of sections) {
        if (section.header !== "CommandList") {
            continue;
        }

        let currentSlot: number | null = null;
        for (const rawLine of section.lines) {
            const trimmed = rawLine.trim();
            const slotMatch = trimmed.match(/^if\s+\$button_amount\s*>=\s*(\d+)$/i);
            if (slotMatch) {
                currentSlot = Number(slotMatch[1]);
                continue;
            }

            if (currentSlot === null) {
                continue;
            }

            const cycleMatch = trimmed.match(/^if\s+\$([\w.]+)\s*<\s*(-?\d+(?:\.\d+)?)$/i);
            if (!cycleMatch) {
                continue;
            }

            const maxValue = Number(cycleMatch[2]);
            if (!Number.isFinite(maxValue)) {
                continue;
            }

            bindings.push({
                slot: currentSlot,
                variable: normalizeKey(cycleMatch[1]),
                values: Array.from(
                    { length: Math.max(0, Math.floor(maxValue)) + 1 },
                    (_, index) => index,
                ),
            });
            currentSlot = null;
        }
    }

    return bindings;
}

function collectKeyCycleBindings(sections: IniSection[]): SlotVariableBinding[] {
    return sections
        .filter((section) => normalizeKey(section.header).startsWith("key"))
        .flatMap((section, index) => {
            const type = section.values.type?.toLowerCase();
            if (type !== "cycle") {
                return [];
            }

            return Object.entries(section.values)
                .filter(([key, value]) => key.startsWith("$") && value.includes(","))
                .map(([key, value]) => ({
                    slot: index + 1,
                    variable: normalizeKey(key),
                    values: value
                        .split(",")
                        .map((entry) => parseIniScalar(entry.trim()))
                        .filter((entry) => entry !== ""),
                }));
        });
}

function dedupeSlotBindings(bindings: SlotVariableBinding[]): SlotVariableBinding[] {
    const seen = new Set<string>();
    return bindings.filter((binding) => {
        const key = `${binding.slot}:${binding.variable}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

async function buildVariantVariables(
    bindings: SlotVariableBinding[],
    sections: IniSection[],
    modDir: string,
    options: {
        logger?: Logger;
        onWarning?: (message: string) => void;
        realtimeShapeKeyVariableIds?: Set<string>;
    },
): Promise<StaticGlbVariantVariable[]> {
    const resourceMap = new Map(
        sections
            .filter((section) => section.header === "Resource" && !!section.values.filename)
            .map((section) => [normalizeKey(section.name), section.values.filename]),
    );

    const variables: StaticGlbVariantVariable[] = [];

    for (const binding of mergeBindingsByVariable(bindings)) {
        const iconResource = findFirstResourcePath(resourceMap, [
            `MenuItem.${binding.slot}`,
            `MenuItem.${deriveVariableUiToken(binding.variable)}`,
            `Button_${binding.slot - 1}`,
            `Button_${binding.slot}`,
        ]);
        const slider = inferSliderConfig(
            binding.variable,
            binding.values,
            options.realtimeShapeKeyVariableIds?.has(normalizeKey(binding.variable)) ?? false,
        );
        variables.push({
            id: binding.variable,
            label: resolveVariantVariableLabel(binding.variable, iconResource),
            defaultValue: 0,
            values: binding.values.map((value) => ({
                value,
                label: String(value),
            })),
            order: binding.slot,
            slot: binding.slot,
            iconPath: iconResource ? path.resolve(modDir, iconResource) : undefined,
            controlType: slider ? "slider" : "buttons",
            slider,
        });
    }

    return variables;
}

function resolveVariantVariableLabel(variableId: string, iconResource?: string): string {
    if (iconResource) {
        const stem = path.basename(iconResource, path.extname(iconResource));
        if (!/^(?:button|icon|item)[._-]?\d+$/i.test(stem)) {
            return humanizeVariableLabel(stem);
        }
    }

    return humanizeVariableLabel(variableId);
}

function mergeBindingsByVariable(bindings: SlotVariableBinding[]): SlotVariableBinding[] {
    const merged = new Map<string, SlotVariableBinding>();

    for (const binding of bindings.sort((a, b) => a.slot - b.slot)) {
        const existing = merged.get(binding.variable);
        if (!existing) {
            merged.set(binding.variable, {
                slot: binding.slot,
                variable: binding.variable,
                values: [...binding.values],
            });
            continue;
        }

        existing.slot = Math.min(existing.slot, binding.slot);
        existing.values = mergeVariableValues(existing.values, binding.values);
    }

    return Array.from(merged.values()).sort((a, b) => a.slot - b.slot);
}

function mergeVariableValues(
    left: VariableStateValue[],
    right: VariableStateValue[],
): VariableStateValue[] {
    const merged: VariableStateValue[] = [];
    const seen = new Set<string>();

    for (const value of [...left, ...right]) {
        const key = String(value);
        if (seen.has(key)) {
            continue;
        }
        seen.add(key);
        merged.push(value);
    }

    if (merged.every((value) => typeof value === "number")) {
        return [...merged].sort((a, b) => Number(a) - Number(b));
    }

    return merged;
}

function inferSliderConfig(
    variableId: string,
    values: VariableStateValue[],
    forceNumericSlider = false,
): StaticGlbVariantSlider | undefined {
    const token = deriveVariableUiToken(variableId).toLowerCase();
    if (!forceNumericSlider && !token.startsWith("slider")) {
        return undefined;
    }

    const numericValues = values.filter((value): value is number => typeof value === "number");
    if (numericValues.length < 3 || numericValues.length !== values.length) {
        return undefined;
    }

    const sorted = [...numericValues].sort((a, b) => a - b);
    const steps = sorted
        .slice(1)
        .map((value, index) => Number((value - sorted[index]).toFixed(6)))
        .filter((step) => step > 0);
    const step = steps.length > 0 ? Math.min(...steps) : 1;

    return {
        min: sorted[0] ?? 0,
        max: sorted[sorted.length - 1] ?? 0,
        step,
    };
}

function collectViewerUiAssetPaths(sections: IniSection[]): StaticGlbViewerUiAssets {
    const resourceMap = new Map(
        sections
            .filter((section) => section.header === "Resource" && !!section.values.filename)
            .map((section) => [normalizeKey(section.name), section.values.filename]),
    );

    const slotHoverPath = findFirstResourcePath(resourceMap, [
        "ItemSlotHover.1",
        "ItemSlotHover.SlotHover",
        "UIButtonSelect",
        "ButtonPush",
    ]);
    const slotActivePath = findFirstResourcePath(resourceMap, [
        "ItemSlotHover.2",
        "ItemSlotHover.SlotClicked",
        "UIButtonSelect",
        "ButtonPush",
    ]);

    return {
        backgroundPath: findFirstResourcePath(resourceMap, [
            "MenuBG",
            "MenuBack",
            "MenuPlate",
            "UIBackground",
        ]),
        slotPath: findFirstResourcePath(resourceMap, ["ItemSlot", "ItemSlotBack", "OutlineButton"]),
        slotHoverPath,
        slotActivePath: slotActivePath ?? slotHoverPath,
    };
}

function collectRealtimeShapeKeys(
    sections: IniSection[],
    resources: Resource[],
    modDir: string,
): StaticGlbRealtimeShapeKey[] {
    const sectionByFullName = new Map(
        sections.map((section) => [normalizeKey(getSectionFullName(section)), section]),
    );
    const resourceMap = new Map(
        resources.map((resource) => [normalizeKey(resource.name), resource]),
    );
    const customShaders = sections.filter(
        (section) =>
            section.header === "CustomShader" &&
            section.values.cs &&
            path.basename(section.values.cs).toLowerCase() === "shapekey.hlsl",
    );
    const shapeKeys: StaticGlbRealtimeShapeKey[] = [];

    for (const shaderSection of customShaders) {
        const shaderSectionName = getSectionFullName(shaderSection);
        const outputEntry = shaderSection.lines
            .map((line) => line.match(/^([^=]+?)\s*=\s*copy\s+ref\s+cs-u5\s*$/i))
            .find((match): match is RegExpMatchArray => match !== null);
        const baseEntry = shaderSection.lines
            .map((line) => line.match(/^cs-u5\s*=\s*copy\s+(.+)$/i))
            .find((match): match is RegExpMatchArray => match !== null);
        if (!outputEntry || !baseEntry) {
            continue;
        }

        const outputResource = resourceMap.get(
            normalizeKey(trimResourcePrefix(outputEntry[1].trim())),
        );
        const baseResource = resourceMap.get(normalizeKey(trimResourcePrefix(baseEntry[1].trim())));
        if (!outputResource || !baseResource?.filename) {
            continue;
        }

        const targetMeshPrefix = deriveBufferGroupKey(outputResource.name);
        if (!targetMeshPrefix) {
            continue;
        }

        const callers = sections.filter((section) =>
            section.lines.some(
                (line) => normalizeKey(line) === normalizeKey(`run = ${shaderSectionName}`),
            ),
        );
        const dimensions = new Map<string, StaticGlbRealtimeShapeKeyDimension>();

        for (const caller of callers) {
            const assignments = resolveAssignmentFromSection(
                caller,
                ["x88", "x89", "cs-t51", "cs-t52", "cs-t53", "cs-t54"],
                sectionByFullName,
                new Map(),
            );
            const bottomVariableId = parseVariableToken(assignments.get(normalizeKey("x88")));
            const chestVariableId = parseVariableToken(assignments.get(normalizeKey("x89")));
            const smallerBottom = resourceMap.get(
                normalizeKey(
                    trimResourcePrefix(stripCopyPrefix(assignments.get(normalizeKey("cs-t52")))),
                ),
            );
            const biggerBottom = resourceMap.get(
                normalizeKey(
                    trimResourcePrefix(stripCopyPrefix(assignments.get(normalizeKey("cs-t51")))),
                ),
            );
            const smallerChest = resourceMap.get(
                normalizeKey(
                    trimResourcePrefix(stripCopyPrefix(assignments.get(normalizeKey("cs-t54")))),
                ),
            );
            const biggerChest = resourceMap.get(
                normalizeKey(
                    trimResourcePrefix(stripCopyPrefix(assignments.get(normalizeKey("cs-t53")))),
                ),
            );

            if (bottomVariableId && smallerBottom?.filename && biggerBottom?.filename) {
                dimensions.set(bottomVariableId, {
                    variableId: bottomVariableId,
                    smallerPath: path.resolve(modDir, smallerBottom.filename),
                    biggerPath: path.resolve(modDir, biggerBottom.filename),
                });
            }

            if (chestVariableId && smallerChest?.filename && biggerChest?.filename) {
                dimensions.set(chestVariableId, {
                    variableId: chestVariableId,
                    smallerPath: path.resolve(modDir, smallerChest.filename),
                    biggerPath: path.resolve(modDir, biggerChest.filename),
                });
            }
        }

        if (dimensions.size === 0) {
            continue;
        }

        shapeKeys.push({
            shaderPath: path.resolve(modDir, shaderSection.values.cs),
            targetMeshPrefixes: [targetMeshPrefix],
            basePath: path.resolve(modDir, baseResource.filename),
            vertexStride: baseResource.stride ?? 40,
            positionOffset: 0,
            normalOffset: 12,
            tangentOffset: 24,
            dimensions: Array.from(dimensions.values()),
        });
    }

    return shapeKeys;
}

function deriveBufferGroupKey(resourceName: string): string | undefined {
    return parseBufferGroupResourceName(resourceName)?.key;
}

function stripCopyPrefix(value: string | undefined): string {
    return (
        value
            ?.replace(/^copy\s+/i, "")
            .replace(/^ref\s+/i, "")
            .trim() ?? ""
    );
}

function parseVariableToken(value: string | undefined): string | undefined {
    const match = value?.match(/\$([\w.]+)/);
    return match ? normalizeKey(match[1]) : undefined;
}

function findFirstResourcePath(
    resourceMap: Map<string, string>,
    candidates: string[],
): string | undefined {
    for (const candidate of candidates) {
        const value = resourceMap.get(normalizeKey(candidate));
        if (value) {
            return value;
        }
    }
    return undefined;
}

function deriveVariableUiToken(variableId: string): string {
    const raw = variableId.replace(/^\$+/, "");
    const trimmed = raw.replace(/^swapvar/i, "");
    return trimmed || raw;
}

function mapToRecord(
    input: Map<string, number | string>,
    keys: string[],
): Record<string, VariableStateValue> {
    const record: Record<string, VariableStateValue> = {};
    for (const key of keys) {
        record[key] = input.get(normalizeKey(key)) ?? 0;
    }
    return record;
}

function humanizeVariableLabel(id: string): string {
    return id
        .replace(/^\$+/, "")
        .split(/[._-]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ");
}

export function createStateKey(state: VariableStateMap): string {
    return Object.entries(state)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${normalizeKey(key)}=${String(value)}`)
        .join("&");
}

function sanitizeStateKey(stateKey: string): string {
    return stateKey.replace(/[^a-z0-9=&_-]+/gi, "_").replace(/[=&]/g, "_");
}

function createStateArtifactFileName(stateKey: string): string {
    const sanitized = sanitizeStateKey(stateKey).replace(/^_+|_+$/g, "");
    const digest = crypto.createHash("sha256").update(stateKey).digest("hex").slice(0, 12);
    const prefix = sanitized.slice(0, 80) || "state";
    return `${prefix}-${digest}.glb`;
}

function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function createTextureCacheBaseName(texturePath: string): string {
    const extensionless = path.basename(texturePath, path.extname(texturePath));
    const digest = crypto
        .createHash("sha256")
        .update(path.resolve(texturePath))
        .digest("hex")
        .slice(0, 12);
    return `${extensionless}-${digest}`;
}

async function buildMaterials(
    builder: GlbBuilder,
    options: ConvertModToGlbBufferOptions,
    modDir: string,
    textureCacheDir: string,
    resources: Resource[],
    textureBindings: TextureBinding[],
    warn: (message: string) => void,
): Promise<Map<string, MaterialBinding>> {
    const buildStartedAt = Date.now();
    const resourcesByName = new Map(
        resources.map((resource) => [normalizeKey(resource.name), resource]),
    );
    const materialByIb = new Map<string, MaterialBinding>();
    const textureCache = new Map<string, MaterialBinding>();
    const textureOutDir = path.resolve(textureCacheDir);

    options.logger?.debug(`Building materials. Cache dir: ${textureOutDir}`, "StaticGLB");

    const candidates: Array<{
        binding: TextureBinding;
        diffuseResourceName: string;
        texturePath: string;
    }> = (
        await Promise.all(
            textureBindings.map(async (binding) => {
                const resourceNames = collectMaterialTextureCandidateNames(binding);
                if (resourceNames.length === 0) {
                    options.logger?.debug(
                        `Binding for ${binding.ibResourceName} has no texture candidates`,
                        "StaticGLB",
                    );
                    return [];
                }

                const resolved = await Promise.all(
                    resourceNames.map(async (diffuseResourceName) => {
                        const textureResource = resourcesByName.get(
                            normalizeKey(diffuseResourceName),
                        );
                        if (!textureResource?.filename) {
                            options.logger?.debug(
                                `Texture resource ${diffuseResourceName} not found or has no filename`,
                                "StaticGLB",
                            );
                            return null;
                        }

                        const texturePath = path.resolve(modDir, textureResource.filename);
                        if (!(await fse.pathExists(texturePath))) {
                            warn(`Texture file not found: ${texturePath}`);
                            return null;
                        }

                        return {
                            binding,
                            diffuseResourceName,
                            texturePath,
                        };
                    }),
                );

                return resolved.filter(
                    (
                        candidate,
                    ): candidate is {
                        binding: TextureBinding;
                        diffuseResourceName: string;
                        texturePath: string;
                    } => candidate !== null,
                );
            }),
        )
    ).flat();
    options.logger?.debug(
        `Resolved ${candidates.length} texture candidates across ${textureBindings.length} bindings in ${Date.now() - buildStartedAt}ms`,
        "StaticGLB",
    );

    const texturePrepareConcurrency = Math.max(1, Math.min(os.availableParallelism(), 8));
    const limitTexturePreparation = pLimit(texturePrepareConcurrency);
    const prepareTasks = new Map<string, Promise<PreparedTexture | null>>();
    const prepareScheduleStartedAt = Date.now();
    for (const candidate of candidates) {
        if (!prepareTasks.has(candidate.texturePath)) {
            prepareTasks.set(
                candidate.texturePath,
                limitTexturePreparation(() =>
                    prepareTextureImage(
                        options,
                        candidate.texturePath,
                        textureOutDir,
                        candidate.diffuseResourceName,
                        warn,
                    ),
                ),
            );
        }
    }
    options.logger?.debug(
        `Scheduled ${prepareTasks.size} unique texture preparation tasks with concurrency ${texturePrepareConcurrency} in ${Date.now() - prepareScheduleStartedAt}ms`,
        "StaticGLB",
    );

    const candidatesByIb = new Map<string, typeof candidates>();
    for (const candidate of candidates) {
        const key = normalizeKey(candidate.binding.ibResourceName);
        const current = candidatesByIb.get(key);
        if (current) {
            current.push(candidate);
        } else {
            candidatesByIb.set(key, [candidate]);
        }
    }
    options.logger?.debug(
        `Grouped texture candidates into ${candidatesByIb.size} IB buckets in ${Date.now() - buildStartedAt}ms`,
        "StaticGLB",
    );

    for (const [ibKey, bindingCandidates] of candidatesByIb) {
        const ibStartedAt = Date.now();
        const preparedCandidates: Array<{
            candidate: (typeof candidates)[number];
            texture: PreparedTexture;
        }> = [];
        for (const candidate of bindingCandidates) {
            const prepareTask = prepareTasks.get(candidate.texturePath);
            if (!prepareTask) {
                continue;
            }

            const texture = await prepareTask;
            if (!texture) {
                options.logger?.debug(
                    `Failed to prepare texture ${candidate.texturePath}`,
                    "StaticGLB",
                );
                continue;
            }

            preparedCandidates.push({ candidate, texture });
        }
        options.logger?.debug(
            `Prepared ${preparedCandidates.length}/${bindingCandidates.length} texture candidates for ${ibKey} in ${Date.now() - ibStartedAt}ms`,
            "StaticGLB",
        );

        const selected = preparedCandidates.sort((left, right) => {
            if (right.texture.selectionScore !== left.texture.selectionScore) {
                return right.texture.selectionScore - left.texture.selectionScore;
            }
            return (
                textureNamePriority(right.candidate.diffuseResourceName, normalizeKey) -
                textureNamePriority(left.candidate.diffuseResourceName, normalizeKey)
            );
        })[0];

        if (!selected) {
            continue;
        }

        options.logger?.debug(
            `Texture candidates for ${ibKey}: ${preparedCandidates
                .map(
                    ({ candidate, texture }) =>
                        `${candidate.diffuseResourceName}=${texture.selectionScore}[${texture.srgbConfidence}]`,
                )
                .join(", ")} | selected=${selected.candidate.diffuseResourceName}`,
            "StaticGLB",
        );

        let cached = textureCache.get(selected.candidate.texturePath);
        if (!cached) {
            const texture = selected.texture;
            const materialCreateStartedAt = Date.now();

            options.logger?.debug(
                `Prepared texture: ${texture.imagePath} (${texture.mimeType}, alpha: ${texture.usesAlpha}, inverted: ${texture.invertedAlpha}, score: ${texture.selectionScore})`,
                "StaticGLB",
            );

            const imageIndex = builder.addImage(
                await fse.readFile(texture.imagePath),
                texture.mimeType,
                path.basename(texture.imagePath),
            );
            const textureIndex = builder.addTexture(imageIndex);
            const materialIndex = builder.addMaterial({
                name: selected.candidate.diffuseResourceName,
                pbrMetallicRoughness: {
                    baseColorTexture: { index: textureIndex },
                    metallicFactor: 0,
                    roughnessFactor: 1,
                },
                ...(texture.alphaMode
                    ? {
                          alphaMode: texture.alphaMode,
                          alphaCutoff: texture.alphaCutoff,
                          doubleSided: true,
                      }
                    : {}),
            });

            cached = {
                materialIndex,
                textureResourceName: selected.candidate.diffuseResourceName,
                imagePath: texture.imagePath,
                mimeType: texture.mimeType,
            };
            textureCache.set(selected.candidate.texturePath, cached);
            options.logger?.debug(
                `Created GLB material for ${selected.candidate.diffuseResourceName} in ${Date.now() - materialCreateStartedAt}ms`,
                "StaticGLB",
            );
        } else {
            options.logger?.debug(
                `Reused cached GLB material for ${selected.candidate.diffuseResourceName}`,
                "StaticGLB",
            );
        }

        if (cached) {
            materialByIb.set(ibKey, cached);
        }
    }

    options.logger?.debug(
        `Built ${materialByIb.size} materials in ${Date.now() - buildStartedAt}ms`,
        "StaticGLB",
    );

    return materialByIb;
}

async function prepareTextureImage(
    options: ConvertModToGlbBufferOptions,
    texturePath: string,
    textureOutDir: string,
    resourceName: string,
    warn: (message: string) => void,
): Promise<PreparedTexture | null> {
    const startedAt = Date.now();
    try {
        const nativeStartedAt = Date.now();
        const prepared = await prepareTextureForMaterial({
            texturePath,
            resourceName,
            textureFormat: resolveTextureFormatOption(options.textureFormat),
            jpegQuality: normalizeJpegQualityOption(options.jpegQuality),
            allowCacheReuse: true,
            cacheDir: textureOutDir,
        });
        const nativeElapsedMs = Date.now() - nativeStartedAt;
        const outputStartedAt = Date.now();
        const imagePath = await writePreparedTextureImage(
            texturePath,
            textureOutDir,
            prepared.imagePath,
            prepared.image,
            prepared.imageExtension,
            prepared.mimeType,
            normalizeJpegQualityOption(options.jpegQuality),
        );
        const outputElapsedMs = Date.now() - outputStartedAt;
        options.logger?.debug(
            `Prepared texture pipeline for ${resourceName} in ${Date.now() - startedAt}ms (native=${nativeElapsedMs}ms, output=${outputElapsedMs}ms)`,
            "StaticGLB",
        );

        return {
            imagePath,
            mimeType: prepared.mimeType as PreparedTexture["mimeType"],
            alphaMode: prepared.alphaMode === "MASK" ? "MASK" : undefined,
            alphaCutoff: prepared.alphaCutoff ?? undefined,
            usesAlpha: prepared.usesAlpha,
            invertedAlpha: prepared.invertedAlpha,
            selectionScore: prepared.selectionScore,
            srgbConfidence: normalizeSrgbConfidence(prepared.srgbConfidence),
        };
    } catch (error) {
        warn(
            `Failed to prepare texture ${texturePath}: ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        options.logger?.debug(
            `Prepared texture pipeline failed for ${resourceName} after ${Date.now() - startedAt}ms`,
            "StaticGLB",
        );
        return null;
    }
}

function collectMaterialTextureCandidateNames(binding: TextureBinding): string[] {
    const ordered = new Set<string>();
    for (const resourceName of binding.textureResourceNames ?? []) {
        ordered.add(resourceName);
    }
    if (binding.diffuseResourceName) {
        ordered.add(binding.diffuseResourceName);
    }
    return Array.from(ordered);
}

function resolveTextureFormatOption(format?: StaticGlbTextureFormat): StaticGlbTextureFormat {
    if (format === "png" || format === "jpeg-safe" || format === "jpeg-force") {
        return format;
    }

    return DEFAULT_TEXTURE_FORMAT;
}

function normalizeJpegQualityOption(quality?: number): number {
    if (quality === undefined || !Number.isFinite(quality)) {
        return DEFAULT_JPEG_QUALITY;
    }

    return Math.max(1, Math.min(100, Math.round(quality)));
}

async function writePreparedTextureImage(
    texturePath: string,
    textureOutDir: string,
    preparedImagePath: string | undefined,
    image: Buffer | undefined,
    imageExtension: string,
    mimeType: PreparedTexture["mimeType"] | string,
    jpegQuality: number,
): Promise<string> {
    if (preparedImagePath) {
        return preparedImagePath;
    }

    const fileName =
        mimeType === "image/png"
            ? `${createTextureCacheBaseName(texturePath)}-prepared.${imageExtension}`
            : `${createTextureCacheBaseName(texturePath)}-q${jpegQuality}.${imageExtension}`;
    const outputPath = path.join(textureOutDir, fileName);
    if (!image) {
        throw new Error(`Missing prepared texture bytes for ${texturePath}`);
    }
    await fse.ensureDir(textureOutDir);
    await fse.writeFile(outputPath, image);
    return outputPath;
}

async function convertDdsToPngFallback(texturePath: string, pngPath: string): Promise<void> {
    const png = await decodeDdsToPngObject(texturePath);
    await writePngBuffer(png, pngPath);
}

async function decodeDdsToPngObject(texturePath: string): Promise<PNG> {
    const dds = await fse.readFile(texturePath);
    const info = parseDDSHeader(dds);
    if (!info || !info.layers[0]) {
        throw new Error("DDS header could not be parsed");
    }

    const rgba = decodeImage(dds, info.format, info.layers[0]);
    const png = new PNG({ width: info.shape.width, height: info.shape.height });
    png.data.set(rgba);
    return png;
}

async function writePngBuffer(png: PNG, pngPath: string): Promise<void> {
    await pipeline(png.pack(), fse.createWriteStream(pngPath));
}

function normalizeSrgbConfidence(value: string): PreparedTexture["srgbConfidence"] {
    return value === "srgb" || value === "linear" || value === "unknown" ? value : "unknown";
}

async function materializeViewerUiAssets(
    uiAssets: StaticGlbViewerUiAssets,
    modDir: string,
    uiDir: string,
    options: { logger?: Logger },
    warn: (message: string) => void,
): Promise<StaticGlbViewerUiAssets> {
    await fse.ensureDir(uiDir);

    return {
        backgroundPath: await materializeUiAssetPath(
            uiAssets.backgroundPath,
            modDir,
            uiDir,
            "menu-bg",
            options,
            warn,
        ),
        slotPath: await materializeUiAssetPath(
            uiAssets.slotPath,
            modDir,
            uiDir,
            "slot",
            options,
            warn,
        ),
        slotHoverPath: await materializeUiAssetPath(
            uiAssets.slotHoverPath,
            modDir,
            uiDir,
            "slot-hover",
            options,
            warn,
        ),
        slotActivePath: await materializeUiAssetPath(
            uiAssets.slotActivePath,
            modDir,
            uiDir,
            "slot-active",
            options,
            warn,
        ),
    };
}

async function materializeUiAssetPath(
    assetPath: string | undefined,
    modDir: string,
    uiDir: string,
    outputName: string,
    options: { logger?: Logger },
    warn: (message: string) => void,
): Promise<string | undefined> {
    if (!assetPath) {
        return undefined;
    }

    return materializeUiAsset(path.resolve(modDir, assetPath), uiDir, outputName, warn, options);
}

async function materializeUiAsset(
    sourcePath: string,
    outputDir: string,
    outputName: string,
    warn?: (message: string) => void,
    options?: { logger?: Logger },
): Promise<string | undefined> {
    if (!(await fse.pathExists(sourcePath))) {
        warn?.(`Missing UI asset: ${sourcePath}`);
        return undefined;
    }

    const extension = path.extname(sourcePath).toLowerCase();
    const outputPath = path.join(outputDir, `${outputName}.png`);

    if (extension === ".png") {
        await fse.copyFile(sourcePath, outputPath);
        return outputPath;
    }

    if (extension === ".dds") {
        try {
            await convertDdsToPng(sourcePath, outputPath);
            return outputPath;
        } catch {
            try {
                await convertDdsToPngFallback(sourcePath, outputPath);
                return outputPath;
            } catch (error) {
                warn?.(
                    `Failed to convert UI DDS ${sourcePath}: ${
                        error instanceof Error ? error.message : String(error)
                    }`,
                );
                return undefined;
            }
        }
    }

    options?.logger?.debug(`Skipping unsupported UI asset type: ${sourcePath}`, "StaticGLB");
    warn?.(`Unsupported UI asset type: ${sourcePath}`);
    return undefined;
}

function trimResourcePrefix(value: string): string {
    return value
        .trim()
        .replace(/^ref\s+/i, "")
        .replace(/^Resource/i, "");
}

function bestKeyForIb(stem: string, resourceName: string, keys: string[]): string {
    const normalizedStem = normalizeKey(stem);
    const normalizedName = normalizeKey(stripIbResourceSuffix(resourceName));
    const sorted = [...keys].sort((a, b) => b.length - a.length);
    const suffix = extractNumericSuffix(resourceName) ?? extractNumericSuffix(stem);
    const sameSuffixKeys =
        suffix !== null ? sorted.filter((key) => extractNumericSuffix(key) === suffix) : [];

    const exactKeyMatch =
        sameSuffixKeys.find((key) => normalizeKey(key) === normalizedName) ||
        sorted.find((key) => normalizeKey(key) === normalizedName);
    if (exactKeyMatch) {
        return exactKeyMatch;
    }

    if (sameSuffixKeys.length === 1) {
        return sameSuffixKeys[0];
    }

    return (
        sameSuffixKeys.find((key) => normalizedStem.includes(normalizeKey(key))) ||
        sameSuffixKeys.find((key) => normalizedName.includes(normalizeKey(key))) ||
        sameSuffixKeys.find((key) => normalizeKey(key).includes(normalizedName)) ||
        sorted.find((key) => normalizedStem.includes(normalizeKey(key))) ||
        sorted.find((key) => normalizedName.includes(normalizeKey(key))) ||
        sorted.find((key) => normalizeKey(key).includes(normalizedName)) ||
        stem
    );
}

function stripIbResourceSuffix(value: string): string {
    const withSuffixRemoved = value.replace(/(?:\.\d+)?$/i, "");
    const ibRemoved = withSuffixRemoved.replace(/(?:head|body|dress|hair|face|weapon)?a?ib$/i, "");
    const meshRemoved = ibRemoved.replace(
        /(?:head|body|dress|hair|face|weapon|cloth|skirt|shoe|arm|leg|hand|foot)$/i,
        "",
    );
    const numericSuffix = extractNumericSuffix(value);
    return `${meshRemoved}${numericSuffix !== null ? `.${suffixToString(numericSuffix)}` : ""}`;
}

function extractNumericSuffix(value: string): number | null {
    const match = value.match(/\.([0-9]+)$/);
    return match ? Number(match[1]) : null;
}

function suffixToString(value: number): string {
    return String(value);
}

function keyMatchesIb(groupKey: string, ibKey: string): boolean {
    const a = normalizeKey(groupKey);
    const b = normalizeKey(ibKey);
    if (a === b) {
        return true;
    }

    const groupSuffix = extractNumericSuffix(groupKey);
    const ibSuffix = extractNumericSuffix(ibKey);
    if (groupSuffix !== null || ibSuffix !== null) {
        if (groupSuffix !== ibSuffix) {
            return false;
        }

        const groupBase = normalizeKey(groupKey.replace(/\.\d+$/i, ""));
        const ibBase = normalizeKey(ibKey.replace(/\.\d+$/i, ""));
        return groupBase === ibBase || groupBase.includes(ibBase) || ibBase.includes(groupBase);
    }

    return a.includes(b) || b.includes(a);
}

function strictKeyMatchesIb(groupKey: string, ibKey: string): boolean {
    const a = normalizeKey(groupKey);
    const b = normalizeKey(ibKey);
    if (a === b) {
        return true;
    }

    const groupSuffix = extractNumericSuffix(groupKey);
    const ibSuffix = extractNumericSuffix(ibKey);
    if (groupSuffix !== null || ibSuffix !== null) {
        if (groupSuffix !== ibSuffix) {
            return false;
        }

        const groupBase = normalizeKey(groupKey.replace(/\.\d+$/i, ""));
        const ibBase = normalizeKey(ibKey.replace(/\.\d+$/i, ""));
        return groupBase === ibBase;
    }

    return false;
}

function normalizeKey(value: string): string {
    const cached = normalizeKeyCache.get(value);
    if (cached) {
        return cached;
    }
    const normalized = value.toLowerCase().replace(/[^a-z0-9]/g, "");
    if (normalizeKeyCache.size >= MAX_NORMALIZE_KEY_CACHE) {
        normalizeKeyCache.clear();
    }
    normalizeKeyCache.set(value, normalized);
    return normalized;
}

async function loadFmtForIb(
    modDir: string,
    assetDir: string,
    ib: IbResource,
    stride: number,
): Promise<FmtLayout> {
    const stem = path.basename(ib.filename, path.extname(ib.filename));
    const localFmt = path.resolve(modDir, `${stem}.fmt`);
    if (await fse.pathExists(localFmt)) {
        return parseFmt(await fse.readFile(localFmt, "utf8"), stride, ib.format);
    }

    const assetFmt = await findRecursive(assetDir, "**/*.fmt", (file) => {
        const lower = path.basename(file).toLowerCase();
        return normalizeKey(lower).includes(normalizeKey(stem));
    });
    if (assetFmt) {
        return parseFmt(await fse.readFile(assetFmt, "utf8"), stride, ib.format);
    }

    let vb0Txt = await findRecursive(assetDir, "**/*.txt", (file) => {
        const lower = path.basename(file).toLowerCase();
        return lower.includes("vb0") && normalizeKey(lower).includes(normalizeKey(stem));
    });

    if (!vb0Txt) {
        const hashCandidates = Array.from(
            new Set(
                [...(ib.overrideHashes ?? []), ib.overrideHash, ib.key]
                    .map((value) => normalizeKey(value || ""))
                    .filter(Boolean),
            ),
        );

        for (const ibHash of hashCandidates) {
            const ibTxt = await findRecursive(assetDir, "**/*.txt", (file) => {
                const lower = path.basename(file).toLowerCase();
                return lower.includes("-ib=") && normalizeKey(lower).includes(ibHash);
            });

            if (!ibTxt) {
                continue;
            }

            const ibBase = path.basename(ibTxt).replace(/-ib=.*$/i, "");
            vb0Txt = await findRecursive(assetDir, "**/*.txt", (file) => {
                const lower = path.basename(file).toLowerCase();
                return lower.includes("vb0") && lower.startsWith(ibBase.toLowerCase());
            });

            if (vb0Txt) {
                break;
            }
        }
    }

    if (!vb0Txt) {
        throw new Error(`No matching .fmt or *-vb0.txt found for ${ib.filename} under ${assetDir}`);
    }

    return parseFmt(
        extractFmtFromVb0(await fse.readFile(vb0Txt, "utf8"), stride, ib.format),
        stride,
        ib.format,
    );
}

async function findRecursive(
    root: string,
    pattern: string | string[],
    predicate: (file: string) => boolean,
): Promise<string | null> {
    const matches = await fg(pattern, {
        cwd: path.resolve(root),
        absolute: true,
        onlyFiles: true,
        caseSensitiveMatch: false,
    });

    return matches.find((file) => predicate(file)) ?? null;
}

function extractFmtFromVb0(text: string, stride: number, indexFormat: string): string {
    const lines = text.split(/\r?\n/).map((line) => line.trim());
    const out = [`stride: ${stride}`, "topology: trianglelist", `format: ${indexFormat}`];
    for (let i = 0; i < lines.length; i++) {
        if (!lines[i].startsWith("element[")) continue;
        out.push(lines[i]);
        for (let j = 1; j <= 7 && i + j < lines.length; j++) {
            out.push(`  ${lines[i + j]}`);
        }
    }
    return out.join("\n");
}

function parseFmt(text: string, fallbackStride: number, fallbackIndexFormat: string): FmtLayout {
    const lines = text.split(/\r?\n/);
    const layout: FmtLayout = {
        stride: fallbackStride,
        topology: "trianglelist",
        indexFormat: fallbackIndexFormat || "DXGI_FORMAT_R32_UINT",
        elements: [],
    };
    let current: Partial<FmtElement> | null = null;
    let appendOffset = 0;

    for (const raw of lines) {
        const line = raw.trim();
        if (!line) continue;
        if (line.startsWith("stride:")) layout.stride = Number(line.slice("stride:".length).trim());
        else if (line.startsWith("topology:"))
            layout.topology = line.slice("topology:".length).trim();
        else if (line.startsWith("format:"))
            layout.indexFormat = line.slice("format:".length).trim();
        else if (line.startsWith("element[")) {
            if (current) {
                const element = completeElement(current);
                layout.elements.push(element);
                appendOffset = element.alignedByteOffset + formatByteSize(element.format);
            }
            current = {};
        } else if (current) {
            const sep = line.indexOf(":");
            if (sep < 0) continue;
            const key = line.slice(0, sep).trim();
            const value = line.slice(sep + 1).trim();
            switch (key) {
                case "SemanticName":
                    current.semanticName = value;
                    break;
                case "SemanticIndex":
                    current.semanticIndex = Number(value);
                    break;
                case "Format":
                    current.format = value;
                    break;
                case "InputSlot":
                    current.inputSlot = Number(value);
                    break;
                case "AlignedByteOffset":
                    current.alignedByteOffset = value === "append" ? appendOffset : Number(value);
                    break;
                case "InputSlotClass":
                    current.inputSlotClass = value;
                    break;
                case "InstanceDataStepRate":
                    current.instanceDataStepRate = Number(value);
                    break;
            }
        }
    }
    if (current) layout.elements.push(completeElement(current));
    layout.elements = layout.elements.filter(
        (element) => element.inputSlotClass !== "per-instance",
    );
    return layout;
}

function completeElement(value: Partial<FmtElement>): FmtElement {
    return {
        semanticName: value.semanticName || "",
        semanticIndex: value.semanticIndex || 0,
        format: value.format || "DXGI_FORMAT_UNKNOWN",
        inputSlot: value.inputSlot || 0,
        alignedByteOffset: value.alignedByteOffset || 0,
        inputSlotClass: value.inputSlotClass || "per-vertex",
        instanceDataStepRate: value.instanceDataStepRate || 0,
    };
}

function decodeIndices(bytes: Buffer, format: string): Uint32Array {
    return bufferToUint32Array(decodeIndicesNative(bytes, format));
}

function buildPrimitive(
    builder: GlbBuilder,
    vb: Buffer,
    stride: number,
    fmt: FmtLayout,
    indices: Uint32Array,
    options: { includeTangents: boolean; includeVertexColors: boolean },
    warn: (message: string) => void,
): Record<string, unknown> | null {
    if (fmt.topology.toLowerCase() !== "trianglelist") {
        throw new Error(`Unsupported topology: ${fmt.topology}`);
    }

    const vertexCount = Math.floor(vb.length / stride);
    const attributes: Record<string, number> = {};

    const position = findElement(fmt, "POSITION");
    if (!position) return null;

    const positions = readFloatAttribute(vb, stride, vertexCount, position, 3);
    attributes.POSITION = builder.addAccessorFromFloat32(positions, "VEC3", true);

    const normal = findElement(fmt, "NORMAL");
    if (normal) {
        attributes.NORMAL = builder.addAccessorFromFloat32(
            normalizeVec3Array(readFloatAttribute(vb, stride, vertexCount, normal, 3)),
            "VEC3",
            false,
        );
    }

    const tangent = findElement(fmt, "TANGENT");
    if (tangent && options.includeTangents) {
        const width = formatComponentCount(tangent.format);
        const tangentData = readFloatAttribute(
            vb,
            stride,
            vertexCount,
            tangent,
            Math.min(width, 4),
        );
        attributes.TANGENT = builder.addAccessorFromFloat32(
            normalizeTangentArray(ensureVec4(tangentData, vertexCount, width)),
            "VEC4",
            false,
        );
    }

    const texcoord0 = findElement(fmt, "TEXCOORD", 0);
    if (texcoord0) {
        attributes.TEXCOORD_0 = builder.addAccessorFromFloat32(
            readFloatAttribute(vb, stride, vertexCount, texcoord0, 2),
            "VEC2",
            false,
        );
    }

    const color0 = findElement(fmt, "COLOR", 0);
    if (color0 && options.includeVertexColors) {
        const colorWidth = Math.min(formatComponentCount(color0.format), 4);
        attributes.COLOR_0 = builder.addAccessorFromFloat32(
            ensureVec4(
                readFloatAttribute(vb, stride, vertexCount, color0, colorWidth),
                vertexCount,
                colorWidth,
                1,
            ),
            "VEC4",
            false,
        );
    }

    return {
        attributes,
        indices: builder.addAccessorFromIndices(
            removeDegenerateTriangles(indices, warn),
            vertexCount,
        ),
        mode: 4,
    };
}

function findElement(fmt: FmtLayout, semantic: string, index?: number): FmtElement | undefined {
    return fmt.elements.find((element) => {
        if (element.semanticName.toUpperCase() !== semantic) return false;
        return index === undefined || element.semanticIndex === index;
    });
}

function readFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    element: FmtElement,
    width: number,
): Float32Array {
    return bufferToFloat32Array(
        readFloatAttributeNative(
            bytes,
            stride,
            vertexCount,
            element.alignedByteOffset,
            element.format,
            width,
        ),
    );
}

function bufferToUint32Array(buffer: Buffer): Uint32Array {
    return new Uint32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}

function uint32ArrayToBuffer(values: Uint32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

function bufferToFloat32Array(buffer: Buffer): Float32Array {
    return new Float32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}

function float32ArrayToBuffer(values: Float32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

function ensureVec4(
    data: Float32Array,
    vertexCount: number,
    width: number,
    fillW = 1,
): Float32Array {
    if (width === 4) return data;
    return bufferToFloat32Array(
        ensureVec4Native(float32ArrayToBuffer(data), vertexCount, width, fillW),
    );
}

function normalizeVec3Array(data: Float32Array): Float32Array {
    return bufferToFloat32Array(normalizeVec3ArrayNative(float32ArrayToBuffer(data)));
}

function normalizeTangentArray(data: Float32Array): Float32Array {
    return bufferToFloat32Array(normalizeTangentArrayNative(float32ArrayToBuffer(data)));
}

function removeDegenerateTriangles(
    indices: Uint32Array,
    warn: (message: string) => void,
): Uint32Array {
    const result = removeDegenerateTrianglesNative(uint32ArrayToBuffer(indices));
    const removed = result.removed;
    if (removed > 0) {
        warn(`Removed ${removed} degenerate triangles`);
    }
    return removed === 0 ? indices : bufferToUint32Array(result.indices);
}

// oxlint-disable-next-line no-unused-vars
function readDxgiValues(bytes: Buffer, offset: number, format: string): number[] {
    const upper = format.toUpperCase();
    const count = formatComponentCount(upper);

    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") {
        const value = bytes.readUInt32LE(offset);
        return [
            (value & 0x3ff) / 1023,
            ((value >> 10) & 0x3ff) / 1023,
            ((value >> 20) & 0x3ff) / 1023,
            ((value >> 30) & 0x3) / 3,
        ];
    }

    if (upper.includes("_FLOAT")) {
        if (upper.includes("32")) return range(count).map((i) => bytes.readFloatLE(offset + i * 4));
        if (upper.includes("16"))
            return range(count).map((i) => halfToFloat(bytes.readUInt16LE(offset + i * 2)));
    }

    if (upper.includes("_UNORM")) {
        if (upper.includes("16"))
            return range(count).map((i) => bytes.readUInt16LE(offset + i * 2) / 65535);
        if (upper.includes("8")) return range(count).map((i) => bytes.readUInt8(offset + i) / 255);
    }

    if (upper.includes("_SNORM")) {
        if (upper.includes("16"))
            return range(count).map((i) => Math.max(-1, bytes.readInt16LE(offset + i * 2) / 32767));
        if (upper.includes("8"))
            return range(count).map((i) => Math.max(-1, bytes.readInt8(offset + i) / 127));
    }

    if (upper.includes("_UINT")) {
        if (upper.includes("32"))
            return range(count).map((i) => bytes.readUInt32LE(offset + i * 4));
        if (upper.includes("16"))
            return range(count).map((i) => bytes.readUInt16LE(offset + i * 2));
        if (upper.includes("8")) return range(count).map((i) => bytes.readUInt8(offset + i));
    }

    if (upper.includes("_SINT")) {
        if (upper.includes("32")) return range(count).map((i) => bytes.readInt32LE(offset + i * 4));
        if (upper.includes("16")) return range(count).map((i) => bytes.readInt16LE(offset + i * 2));
        if (upper.includes("8")) return range(count).map((i) => bytes.readInt8(offset + i));
    }

    throw new Error(`Unsupported DXGI format: ${format}`);
}

function formatComponentCount(format: string): number {
    const normalized = format.toUpperCase().replace(/^DXGI_FORMAT_/, "");
    const channels = normalized.match(/[RGBA]\d+/g);
    return channels ? channels.length : 1;
}

function formatByteSize(format: string): number {
    const upper = format.toUpperCase();
    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") return 4;
    const count = formatComponentCount(upper);
    if (upper.includes("32")) return count * 4;
    if (upper.includes("16")) return count * 2;
    if (upper.includes("8")) return count;
    return 0;
}

function halfToFloat(h: number): number {
    const sign = h & 0x8000 ? -1 : 1;
    const exponent = (h >> 10) & 0x1f;
    const fraction = h & 0x03ff;
    if (exponent === 0) return sign * 2 ** -14 * (fraction / 1024);
    if (exponent === 31) return fraction ? Number.NaN : sign * Number.POSITIVE_INFINITY;
    return sign * 2 ** (exponent - 15) * (1 + fraction / 1024);
}

function range(count: number): number[] {
    return Array.from({ length: count }, (_, i) => i);
}

import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { convertDdsToPng } from "@native/native-util";
import { decodeImage, parseDDSHeader } from "dds-ktx-parser";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import pLimit from "p-limit";
import { PNG } from "pngjs";
import writeFileAtomic from "write-file-atomic";
import type { Logger } from "../internal/logger";

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

type IbResource = {
    name: string;
    filename: string;
    format: string;
    key: string;
    overrideHash?: string;
};

type TextureBinding = {
    ibResourceName: string;
    diffuseResourceName?: string;
    overrideHash?: string;
};

type MaterialBinding = {
    materialIndex: number;
    textureResourceName: string;
    pngPath: string;
};

export type VariableStateValue = number | string;
export type VariableStateMap = Record<string, VariableStateValue>;

export type StaticGlbVariantValue = {
    value: VariableStateValue;
    label: string;
};

export type StaticGlbVariantVariable = {
    id: string;
    label: string;
    defaultValue: VariableStateValue;
    values: StaticGlbVariantValue[];
    order: number;
    slot?: number;
    iconPath?: string;
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

type PreparedTexture = {
    pngPath: string;
    alphaMode?: "MASK";
    alphaCutoff?: number;
    invertedAlpha: boolean;
};

type GlTf = {
    asset: { version: "2.0"; generator: string };
    scene: number;
    scenes: Array<{ nodes: number[] }>;
    nodes: Array<{ mesh: number; name: string }>;
    meshes: Array<{ name: string; primitives: unknown[] }>;
    buffers: Array<{ byteLength: number }>;
    bufferViews: Array<Record<string, unknown>>;
    accessors: Array<Record<string, unknown>>;
    materials?: Array<Record<string, unknown>>;
    images?: Array<Record<string, unknown>>;
    textures?: Array<Record<string, unknown>>;
};

export type ConvertModToGlbOptions = {
    modPath: string;
    assetPath: string;
    outputPath: string;
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
    indexCount: number;
    startIndex: number;
    baseVertex: number;
    condition?: string;
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

const GL_COMPONENT = {
    BYTE: 5120,
    UNSIGNED_BYTE: 5121,
    SHORT: 5122,
    UNSIGNED_SHORT: 5123,
    UNSIGNED_INT: 5125,
    FLOAT: 5126,
} as const;

const variantArtifactManifestLocks = new Map<string, Promise<void>>();

async function withVariantArtifactManifestLock<T>(
    artifactRoot: string,
    operation: () => Promise<T>,
): Promise<T> {
    const previous = variantArtifactManifestLocks.get(artifactRoot) ?? Promise.resolve();
    let release!: () => void;
    const current = new Promise<void>((resolve) => {
        release = resolve;
    });
    const queued = previous.then(() => current);
    variantArtifactManifestLocks.set(artifactRoot, queued);

    await previous;
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
            const glbName = `${sanitizeStateKey(key)}.glb`;
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
            states,
        };
        const manifestPath = path.join(artifactRoot, "manifest.json");
        await fse.writeJson(manifestPath, manifest, { spaces: 2 });

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
        if (await fse.pathExists(textureCacheDir)) {
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
            const glbPath = path.join(glbDir, `${sanitizeStateKey(key)}.glb`);
            await fse.writeFile(glbPath, result.glb);

            manifest = (await fse.readJson(manifestPath)) as StaticGlbVariantManifest;
            const current = manifest.states.find((entry) => entry.key === key);
            if (!current) {
                manifest.states.push({
                    key,
                    values: options.state,
                    glbPath,
                });
                await writeVariantManifestAtomic(manifestPath, manifest);
            }

            return {
                glbPath: current?.glbPath ?? glbPath,
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
    const bufferGroups = await collectBufferGroups(modDir, resources, warning.warn);
    const sectionByFullName = new Map(
        sections.map((section) => [normalizeKey(getSectionFullName(section)), section]),
    );
    const textureBindings = collectTextureBindings(sections, sectionByFullName, resolvedVariables);
    const ibResources = collectIbResources(
        sections,
        resources,
        bufferGroups,
        sectionByFullName,
        resolvedVariables,
        textureBindings,
    );
    const drawBindings = collectTextureOverrideDrawBindings(sections);

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
        const group = bufferGroups.find((candidate) => keyMatchesIb(candidate.key, ib.key));
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
    const semicolon = value.indexOf(" ;");
    if (semicolon >= 0) return value.slice(0, semicolon).trim();
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
    const byKey = new Map<
        string,
        { position?: Resource; blend?: Resource; texcoord?: Resource; single?: Resource }
    >();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const typedMatch = resource.name.match(/^(.*?)(Position|Blend|Texcoord)(\.\d+)?$/i);
        if (typedMatch) {
            const [, prefix, kind, suffix = ""] = typedMatch;
            const key = `${prefix}${suffix}`;
            if (/position/i.test(kind)) {
                ensureGroup(byKey, key).position = resource;
            } else if (/blend/i.test(kind)) {
                ensureGroup(byKey, key).blend = resource;
            } else {
                ensureGroup(byKey, key).texcoord = resource;
            }
        } else if (
            resource.filename.toLowerCase().endsWith(".buf") ||
            resource.filename.toLowerCase().endsWith(".vb")
        ) {
            ensureGroup(byKey, resource.name).single = resource;
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
            const position = await readResourceBytes(modDir, group.position);
            const blend = await readResourceBytes(modDir, group.blend);
            const texcoord = await readResourceBytes(modDir, group.texcoord);
            const stride = group.position.stride! + group.blend.stride! + group.texcoord.stride!;
            const vertexCount = Math.min(
                Math.floor(position.length / group.position.stride!),
                Math.floor(blend.length / group.blend.stride!),
                Math.floor(texcoord.length / group.texcoord.stride!),
            );
            const vb = Buffer.alloc(vertexCount * stride);
            for (let i = 0; i < vertexCount; i++) {
                let offset = i * stride;
                position.copy(
                    vb,
                    offset,
                    i * group.position.stride!,
                    (i + 1) * group.position.stride!,
                );
                offset += group.position.stride!;
                blend.copy(vb, offset, i * group.blend.stride!, (i + 1) * group.blend.stride!);
                offset += group.blend.stride!;
                texcoord.copy(
                    vb,
                    offset,
                    i * group.texcoord.stride!,
                    (i + 1) * group.texcoord.stride!,
                );
            }
            groups.push({ key, vbFilename: `${key}.vb`, vbBytes: vb, stride });
        }
    }

    return groups;
}

function ensureGroup<T>(map: Map<string, T>, key: string): T {
    let value = map.get(key);
    if (!value) {
        value = {} as T;
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
): IbResource[] {
    const bufferKeys = bufferGroups.map((group) => group.key);
    const bindingsByIbName = new Map(
        textureBindings.map((binding) => [normalizeKey(binding.ibResourceName), binding]),
    );
    const referencedIbNames = new Set(bindingsByIbName.keys());
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
            const binding = bindingsByIbName.get(normalizeKey(resource.name));
            return {
                name: resource.name,
                filename: resource.filename!,
                format: resource.format!,
                key,
                overrideHash: binding?.overrideHash,
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
        .filter((section) => section.header === "TextureOverride")
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
        const ibValue = assignments.get("ib") || section.values.ib;
        if (!ibValue) continue;

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
            .map(([, value]) => trimResourcePrefix(value.replace(/^ref\s+/i, "")));

        const diffuseResourceName =
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
            );

        bindings.push({
            ibResourceName: trimResourcePrefix(ibValue),
            diffuseResourceName,
            overrideHash: section.values.hash?.trim(),
        });
    }
    return bindings;
}

function collectTextureOverrideDrawBindings(sections: IniSection[]): TextureOverrideBinding[] {
    const variables = collectDefaultIniVariables(sections);

    return sections
        .filter((section) => section.header === "TextureOverride")
        .map((section) => ({
            sectionName: section.name,
            ibResourceName: trimResourcePrefix(section.values.ib || ""),
            diffuseResourceName: undefined,
            overrideHash: section.values.hash?.trim(),
            draws: collectSectionDrawInstructions(section.lines, variables),
        }))
        .filter((binding) => !!binding.ibResourceName);
}

function collectSectionDrawInstructions(
    lines: string[],
    variables: Map<string, number | string>,
): DrawInstruction[] {
    const instructions: DrawInstruction[] = [];
    const stack: Array<{ active: string; inverse?: string }> = [];

    for (const rawLine of lines) {
        const trimmed = rawLine.trim();
        const lower = trimmed.toLowerCase();

        if (lower.startsWith("if ")) {
            stack.push({ active: trimmed.slice(3).trim() });
            continue;
        }

        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            const previous = stack.pop();
            const current = (
                lower.startsWith("elif ") ? trimmed.slice(5) : trimmed.slice(8)
            ).trim();
            const inverse = previous?.active ? `!(${previous.active})` : undefined;
            stack.push({
                active: inverse ? `${inverse} && (${current})` : current,
                inverse: previous?.inverse
                    ? `${previous.inverse} && !(${current})`
                    : `!(${current})`,
            });
            continue;
        }

        if (lower === "else") {
            const previous = stack.pop();
            if (!previous) continue;
            stack.push({
                active: previous.inverse || `!(${previous.active})`,
            });
            continue;
        }

        if (lower === "endif") {
            stack.pop();
            continue;
        }

        const drawMatch = trimmed.match(/^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$/i);
        if (!drawMatch) continue;

        const indexCount = evaluateIniNumericExpression(drawMatch[1], variables);
        const startIndex = evaluateIniNumericExpression(drawMatch[2], variables);
        const baseVertex = evaluateIniNumericExpression(drawMatch[3], variables);
        if (indexCount === null || startIndex === null || baseVertex === null) {
            continue;
        }

        const activeConditions = stack.map((entry) => entry.active).filter(Boolean);
        instructions.push({
            indexCount,
            startIndex,
            baseVertex,
            condition: activeConditions.length > 0 ? activeConditions.join(" && ") : undefined,
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
            (draw) => !draw.condition || evaluateIniCondition(draw.condition, variables),
        ),
    );

    if (activeDraws.length === 0) {
        return indices;
    }

    const merged: number[] = [];
    for (const draw of activeDraws) {
        const endIndex = draw.startIndex + draw.indexCount;
        if (draw.startIndex < 0 || endIndex > indices.length) {
            warn(`Skipping invalid draw range start=${draw.startIndex} count=${draw.indexCount}`);
            continue;
        }

        for (let index = draw.startIndex; index < endIndex; index++) {
            merged.push(indices[index] + draw.baseVertex);
        }
    }

    return merged.length > 0 ? Uint32Array.from(merged) : indices;
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

    const preferred = overrideTextureResources.filter((entry) => {
        const sectionLower = entry.sectionName.toLowerCase();
        const resourceLower = entry.resourceName.toLowerCase();
        return sectionLower.includes("diffuse") || resourceLower.includes("diffuse");
    });

    return (
        preferred.find((entry) => entry.sectionName.toLowerCase() === exactSection)?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase() === exactResource)
            ?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(sectionName.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase().startsWith(ibStem.toLowerCase()))
            ?.resourceName ||
        preferred.find((entry) =>
            entry.resourceName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName
    );
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
                ? evaluateIniCondition(trimmed.slice(3), variables)
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
                    ? evaluateIniCondition(expression, variables)
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

function evaluateIniCondition(
    expression: string,
    variables: Map<string, number | string>,
): boolean {
    const jsExpression = substituteIniExpressionTokens(expression, variables);

    if (!/^[\d\s()+\-*/%<>=!&|."'\\]+$/.test(jsExpression)) {
        return false;
    }

    try {
        return !!Function(`"use strict"; return (${jsExpression});`)();
    } catch {
        return false;
    }
}

function evaluateIniNumericExpression(
    expression: string,
    variables: Map<string, number | string>,
): number | null {
    const jsExpression = substituteIniExpressionTokens(expression, variables);
    if (!/^[\d\s()+\-*/%<>=!&|.]+$/.test(jsExpression)) {
        return null;
    }

    try {
        const value = Function(`"use strict"; return (${jsExpression});`)();
        return typeof value === "number" && Number.isFinite(value) ? value : null;
    } catch {
        return null;
    }
}

function substituteIniExpressionTokens(
    expression: string,
    variables: Map<string, number | string>,
): string {
    return expression.replace(/\$?[A-Za-z_\\][A-Za-z0-9_\\.\\]*/g, (token) => {
        const lower = token.toLowerCase();
        if (["true", "false"].includes(lower)) return lower;
        if (/^\d/.test(token)) return token;
        const value = variables.get(normalizeKey(token)) ?? 0;
        return typeof value === "number" ? String(value) : JSON.stringify(String(value));
    });
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
    const variables = (await buildVariantVariables(slotBindings, sections, modDir, options)).map(
        (variable) => ({
            ...variable,
            defaultValue: defaultVariables.get(normalizeKey(variable.id)) ?? 0,
        }),
    );

    return {
        iniPath,
        defaultState: mapToRecord(
            defaultVariables,
            variables.map((variable) => variable.id),
        ),
        variables,
        uiAssets: collectViewerUiAssetPaths(sections),
    };
}

function collectSlotVariableBindings(
    sections: IniSection[],
    defaultVariables: Map<string, number | string>,
): SlotVariableBinding[] {
    const bindings: SlotVariableBinding[] = collectKeyCycleBindings(sections);
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
        const maxMatch = clickedSection.lines.find((line) =>
            new RegExp(`^if\\s+\\$${escapeRegex(variable)}\\s*>\\s*(\\d+)$`, "i").test(line.trim()),
        );
        const maxValueMatch = maxMatch?.trim().match(/>\s*(\d+)$/);
        const maxValue = maxValueMatch ? Number(maxValueMatch[1]) : defaultValue;
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
    _options: { logger?: Logger; onWarning?: (message: string) => void },
): Promise<StaticGlbVariantVariable[]> {
    const resourceMap = new Map(
        sections
            .filter((section) => section.header === "Resource" && !!section.values.filename)
            .map((section) => [normalizeKey(section.name), section.values.filename]),
    );

    const variables: StaticGlbVariantVariable[] = [];

    for (const binding of bindings.sort((a, b) => a.slot - b.slot)) {
        const iconResource = resourceMap.get(normalizeKey(`MenuItem.${binding.slot}`));
        variables.push({
            id: binding.variable,
            label: humanizeVariableLabel(binding.variable),
            defaultValue: 0,
            values: binding.values.map((value) => ({
                value,
                label: String(value),
            })),
            order: binding.slot,
            slot: binding.slot,
            iconPath: iconResource ? path.resolve(modDir, iconResource) : undefined,
        });
    }

    return variables;
}

function collectViewerUiAssetPaths(sections: IniSection[]): StaticGlbViewerUiAssets {
    const resourceMap = new Map(
        sections
            .filter((section) => section.header === "Resource" && !!section.values.filename)
            .map((section) => [normalizeKey(section.name), section.values.filename]),
    );

    return {
        backgroundPath: resourceMap.get(normalizeKey("MenuBG")),
        slotPath: resourceMap.get(normalizeKey("ItemSlot")),
        slotHoverPath: resourceMap.get(normalizeKey("ItemSlotHover.1")),
        slotActivePath: resourceMap.get(normalizeKey("ItemSlotHover.2")),
    };
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

function createStateKey(state: VariableStateMap): string {
    return Object.entries(state)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${normalizeKey(key)}=${String(value)}`)
        .join("&");
}

function sanitizeStateKey(stateKey: string): string {
    return stateKey.replace(/[^a-z0-9=&_-]+/gi, "_").replace(/[=&]/g, "_");
}

function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
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
                const diffuseResourceName = binding.diffuseResourceName;
                if (!diffuseResourceName) {
                    options.logger?.debug(
                        `Binding for ${binding.ibResourceName} has no diffuse resource name`,
                        "StaticGLB",
                    );
                    return null;
                }

                const textureResource = resourcesByName.get(normalizeKey(diffuseResourceName));
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
        )
    ).filter(
        (
            candidate,
        ): candidate is {
            binding: TextureBinding;
            diffuseResourceName: string;
            texturePath: string;
        } => candidate !== null,
    );

    const texturePrepareConcurrency = Math.max(1, Math.min(os.availableParallelism(), 8));
    const limitTexturePreparation = pLimit(texturePrepareConcurrency);
    const prepareTasks = new Map<string, Promise<PreparedTexture | null>>();
    for (const candidate of candidates) {
        if (!prepareTasks.has(candidate.texturePath)) {
            prepareTasks.set(
                candidate.texturePath,
                limitTexturePreparation(() =>
                    prepareTexturePng(
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

    for (const candidate of candidates) {
        let cached = textureCache.get(candidate.texturePath);
        if (!cached) {
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

            options.logger?.debug(
                `Prepared texture: ${texture.pngPath} (inverted: ${texture.invertedAlpha})`,
                "StaticGLB",
            );

            const imageIndex = builder.addImage(
                await fse.readFile(texture.pngPath),
                "image/png",
                path.basename(texture.pngPath),
            );
            const textureIndex = builder.addTexture(imageIndex);
            const materialIndex = builder.addMaterial({
                name: candidate.diffuseResourceName,
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
                textureResourceName: candidate.diffuseResourceName,
                pngPath: texture.pngPath,
            };
            textureCache.set(candidate.texturePath, cached);
        }

        if (cached) {
            materialByIb.set(normalizeKey(candidate.binding.ibResourceName), cached);
        }
    }

    return materialByIb;
}

async function prepareTexturePng(
    options: ConvertModToGlbBufferOptions,
    texturePath: string,
    textureOutDir: string,
    resourceName: string,
    warn: (message: string) => void,
): Promise<PreparedTexture | null> {
    const pngPath = await convertTextureToPng(options, texturePath, textureOutDir, warn);
    if (!pngPath) return null;

    try {
        const png = await readPngAsync(pngPath);
        const alpha = analyzeAlpha(png);
        if (!alpha.hasAlpha) {
            return { pngPath, invertedAlpha: false };
        }

        if (shouldInvertAlpha(resourceName, texturePath, alpha)) {
            invertPngAlpha(png);
            const invertedPath = path.join(
                textureOutDir,
                `${path.basename(pngPath, path.extname(pngPath))}-alpha-inverted.png`,
            );
            await fse.ensureDir(textureOutDir);
            await writePngAsync(png, invertedPath);
            const correctedAlpha = analyzeAlpha(png);
            return {
                pngPath: invertedPath,
                ...materialAlphaMode(correctedAlpha),
                invertedAlpha: true,
            };
        }

        return {
            pngPath,
            ...materialAlphaMode(alpha),
            invertedAlpha: false,
        };
    } catch (error) {
        warn(
            `Could not inspect texture alpha ${pngPath}: ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        return { pngPath, invertedAlpha: false };
    }
}

function analyzeAlpha(png: PNG): {
    hasAlpha: boolean;
    lowRatio: number;
    highRatio: number;
    partialRatio: number;
    lowAlphaRgbMean: number;
} {
    const pixelCount = png.width * png.height;
    let low = 0;
    let high = 0;
    let partial = 0;
    let lowAlphaRgbTotal = 0;

    for (let offset = 0; offset < png.data.length; offset += 4) {
        const alpha = png.data[offset + 3];
        if (alpha <= 16) {
            low++;
            lowAlphaRgbTotal +=
                (png.data[offset] + png.data[offset + 1] + png.data[offset + 2]) / 3;
        } else if (alpha >= 239) {
            high++;
        } else {
            partial++;
        }
    }

    return {
        hasAlpha: low > 0 || partial > 0,
        lowRatio: pixelCount > 0 ? low / pixelCount : 0,
        highRatio: pixelCount > 0 ? high / pixelCount : 0,
        partialRatio: pixelCount > 0 ? partial / pixelCount : 0,
        lowAlphaRgbMean: low > 0 ? lowAlphaRgbTotal / low : 0,
    };
}

function materialAlphaMode(
    alpha: ReturnType<typeof analyzeAlpha>,
): Pick<PreparedTexture, "alphaMode" | "alphaCutoff"> {
    if (isCutoutAlpha(alpha)) {
        return { alphaMode: "MASK", alphaCutoff: 0.5 };
    }

    return {};
}

function isCutoutAlpha(alpha: ReturnType<typeof analyzeAlpha>): boolean {
    return alpha.lowRatio >= 0.005 && alpha.highRatio >= 0.5 && alpha.partialRatio <= 0.02;
}

function shouldInvertAlpha(
    resourceName: string,
    texturePath: string,
    alpha: ReturnType<typeof analyzeAlpha>,
): boolean {
    const key = normalizeKey(`${resourceName} ${path.basename(texturePath)}`);
    if (key.includes("invertalpha") || key.includes("alphainvert")) {
        return true;
    }

    return alpha.lowRatio >= 0.95 && alpha.highRatio <= 0.03 && alpha.lowAlphaRgbMean >= 8;
}

function invertPngAlpha(png: PNG): void {
    for (let offset = 3; offset < png.data.length; offset += 4) {
        png.data[offset] = 255 - png.data[offset];
    }
}

async function readPngAsync(pngPath: string): Promise<PNG> {
    const buffer = await fse.readFile(pngPath);
    return await new Promise((resolve, reject) => {
        new PNG().parse(buffer, (error, png) => {
            if (error || !png) {
                reject(error ?? new Error(`Failed to parse PNG: ${pngPath}`));
                return;
            }

            resolve(png);
        });
    });
}

async function writePngAsync(png: PNG, pngPath: string): Promise<void> {
    await pipeline(png.pack(), fse.createWriteStream(pngPath));
}

async function convertTextureToPng(
    options: ConvertModToGlbBufferOptions,
    texturePath: string,
    textureOutDir: string,
    warn: (message: string) => void,
): Promise<string | null> {
    const extension = path.extname(texturePath).toLowerCase();
    if (extension === ".png") {
        options.logger?.debug(
            `Texture is already PNG, skipping conversion: ${texturePath}`,
            "StaticGLB",
        );
        return texturePath;
    }
    if (extension !== ".dds") {
        warn(`Unsupported texture type for GLB embedding: ${texturePath}`);
        return null;
    }

    options.logger?.debug(`Converting DDS to PNG: ${texturePath} -> ${textureOutDir}`, "StaticGLB");
    await fse.ensureDir(textureOutDir);

    const pngPath = path.join(
        textureOutDir,
        `${path.basename(texturePath, path.extname(texturePath))}.png`,
    );

    try {
        await convertDdsToPng(texturePath, pngPath);
        return pngPath;
    } catch {
        try {
            await convertDdsToPngFallback(texturePath, pngPath);
            return pngPath;
        } catch (fallbackError) {
            warn(
                `Failed to convert DDS texture ${texturePath}: ${
                    fallbackError instanceof Error ? fallbackError.message : String(fallbackError)
                }`,
            );
            return null;
        }
    }
}

async function convertDdsToPngFallback(texturePath: string, pngPath: string): Promise<void> {
    const dds = await fse.readFile(texturePath);
    const info = parseDDSHeader(dds);
    if (!info || !info.layers[0]) {
        throw new Error("DDS header could not be parsed");
    }

    const rgba = decodeImage(dds, info.format, info.layers[0]);
    const png = new PNG({ width: info.shape.width, height: info.shape.height });
    png.data.set(rgba);
    await writePngAsync(png, pngPath);
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
    return value.trim().replace(/^Resource/i, "");
}

function bestKeyForIb(stem: string, resourceName: string, keys: string[]): string {
    const normalizedStem = normalizeKey(stem);
    const normalizedName = normalizeKey(stripIbResourceSuffix(resourceName));
    const sorted = [...keys].sort((a, b) => b.length - a.length);
    const suffix = extractNumericSuffix(resourceName) ?? extractNumericSuffix(stem);
    const sameSuffixKeys = suffix
        ? sorted.filter((key) => extractNumericSuffix(key) === suffix)
        : [];

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
    return `${meshRemoved}${numericSuffix ? `.${suffixToString(numericSuffix)}` : ""}`;
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
    return a === b || a.includes(b) || b.includes(a);
}

function normalizeKey(value: string): string {
    return value.toLowerCase().replace(/[^a-z0-9]/g, "");
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
        const ibHash = normalizeKey(ib.overrideHash || ib.key || stem);
        const ibTxt = await findRecursive(assetDir, "**/*.txt", (file) => {
            const lower = path.basename(file).toLowerCase();
            return lower.includes("-ib=") && normalizeKey(lower).includes(ibHash);
        });

        if (ibTxt) {
            const ibBase = path.basename(ibTxt).replace(/-ib=.*$/i, "");
            vb0Txt = await findRecursive(assetDir, "**/*.txt", (file) => {
                const lower = path.basename(file).toLowerCase();
                return lower.includes("vb0") && lower.startsWith(ibBase.toLowerCase());
            });
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
    const upper = format.toUpperCase();
    if (upper.includes("R16_UINT")) {
        const out = new Uint32Array(Math.floor(bytes.length / 2));
        for (let i = 0; i < out.length; i++) out[i] = bytes.readUInt16LE(i * 2);
        return out;
    }
    if (upper.includes("R32_UINT") || upper.includes("UNKNOWN")) {
        const out = new Uint32Array(Math.floor(bytes.length / 4));
        for (let i = 0; i < out.length; i++) out[i] = bytes.readUInt32LE(i * 4);
        return out;
    }
    throw new Error(`Unsupported IB format: ${format}`);
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
        indices: builder.addAccessorFromIndices(removeDegenerateTriangles(indices, warn)),
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
    const out = new Float32Array(vertexCount * width);
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const base = vertex * stride + element.alignedByteOffset;
        const values = readDxgiValues(bytes, base, element.format);
        for (let c = 0; c < width; c++) out[vertex * width + c] = values[c] ?? 0;
    }
    return out;
}

function ensureVec4(
    data: Float32Array,
    vertexCount: number,
    width: number,
    fillW = 1,
): Float32Array {
    if (width === 4) return data;
    const out = new Float32Array(vertexCount * 4);
    for (let i = 0; i < vertexCount; i++) {
        out[i * 4 + 0] = data[i * width + 0] ?? 0;
        out[i * 4 + 1] = data[i * width + 1] ?? 0;
        out[i * 4 + 2] = data[i * width + 2] ?? 0;
        out[i * 4 + 3] = width > 3 ? data[i * width + 3] : fillW;
    }
    return out;
}

function normalizeVec3Array(data: Float32Array): Float32Array {
    const out = new Float32Array(data.length);
    for (let i = 0; i < data.length; i += 3) {
        const x = data[i + 0];
        const y = data[i + 1];
        const z = data[i + 2];
        const length = Math.hypot(x, y, z);
        if (length > 1e-8) {
            out[i + 0] = x / length;
            out[i + 1] = y / length;
            out[i + 2] = z / length;
        }
    }
    return out;
}

function normalizeTangentArray(data: Float32Array): Float32Array {
    const out = new Float32Array(data.length);
    for (let i = 0; i < data.length; i += 4) {
        const x = data[i + 0];
        const y = data[i + 1];
        const z = data[i + 2];
        const length = Math.hypot(x, y, z);
        if (length > 1e-8) {
            out[i + 0] = x / length;
            out[i + 1] = y / length;
            out[i + 2] = z / length;
        }
        out[i + 3] = data[i + 3] >= 0 ? 1 : -1;
    }
    return out;
}

function removeDegenerateTriangles(
    indices: Uint32Array,
    warn: (message: string) => void,
): Uint32Array {
    const out: number[] = [];
    let removed = 0;
    for (let i = 0; i + 2 < indices.length; i += 3) {
        const a = indices[i + 0];
        const b = indices[i + 1];
        const c = indices[i + 2];
        if (a === b || b === c || a === c) {
            removed++;
            continue;
        }
        out.push(a, b, c);
    }
    if (removed > 0) {
        warn(`Removed ${removed} degenerate triangles`);
    }
    return Uint32Array.from(out);
}

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

class GlbBuilder {
    private chunks: Buffer[] = [];
    private gltf: GlTf = {
        asset: { version: "2.0", generator: "Nahida Desktop static mod GLB converter" },
        scene: 0,
        scenes: [{ nodes: [] }],
        nodes: [],
        meshes: [],
        buffers: [{ byteLength: 0 }],
        bufferViews: [],
        accessors: [],
    };

    addMesh(name: string, primitive: Record<string, unknown>) {
        const meshIndex = this.gltf.meshes.length;
        this.gltf.meshes.push({ name, primitives: [primitive] });
        const nodeIndex = this.gltf.nodes.length;
        this.gltf.nodes.push({ mesh: meshIndex, name });
        this.gltf.scenes[0].nodes.push(nodeIndex);
    }

    meshCount(): number {
        return this.gltf.meshes.length;
    }

    addAccessorFromFloat32(
        data: Float32Array,
        type: "VEC2" | "VEC3" | "VEC4",
        withMinMax: boolean,
    ): number {
        const buffer = Buffer.from(data.buffer, data.byteOffset, data.byteLength);
        const bufferView = this.addBufferView(buffer, 34962);
        const accessor: Record<string, unknown> = {
            bufferView,
            byteOffset: 0,
            componentType: GL_COMPONENT.FLOAT,
            count: data.length / typeWidth(type),
            type,
        };
        if (withMinMax) {
            const { min, max } = minMax(data, typeWidth(type));
            accessor.min = min;
            accessor.max = max;
        }
        this.gltf.accessors.push(accessor);
        return this.gltf.accessors.length - 1;
    }

    addAccessorFromIndices(indices: Uint32Array): number {
        const useUint16 = indices.every((value) => value <= 65535);
        let buffer: Buffer;
        let componentType: number;
        if (useUint16) {
            const compact = new Uint16Array(indices.length);
            compact.set(indices);
            buffer = Buffer.from(compact.buffer);
            componentType = GL_COMPONENT.UNSIGNED_SHORT;
        } else {
            buffer = Buffer.from(indices.buffer, indices.byteOffset, indices.byteLength);
            componentType = GL_COMPONENT.UNSIGNED_INT;
        }
        const bufferView = this.addBufferView(buffer, 34963);
        this.gltf.accessors.push({
            bufferView,
            byteOffset: 0,
            componentType,
            count: indices.length,
            type: "SCALAR",
        });
        return this.gltf.accessors.length - 1;
    }

    addImage(data: Buffer, mimeType: string, name?: string): number {
        const bufferView = this.addBufferView(data);
        this.gltf.images ??= [];
        this.gltf.images.push({
            ...(name ? { name } : {}),
            bufferView,
            mimeType,
        });
        return this.gltf.images.length - 1;
    }

    addTexture(source: number): number {
        this.gltf.textures ??= [];
        this.gltf.textures.push({ source });
        return this.gltf.textures.length - 1;
    }

    addMaterial(material: Record<string, unknown>): number {
        this.gltf.materials ??= [];
        this.gltf.materials.push(material);
        return this.gltf.materials.length - 1;
    }

    toGlb(): Buffer {
        const bin = Buffer.concat(this.chunks);
        this.gltf.buffers[0].byteLength = bin.length;
        const json = Buffer.from(JSON.stringify(this.gltf), "utf8");
        const jsonPadded = padBuffer(json, 0x20);
        const binPadded = padBuffer(bin, 0x00);

        const totalLength = 12 + 8 + jsonPadded.length + 8 + binPadded.length;
        const out = Buffer.alloc(totalLength);
        let offset = 0;
        out.writeUInt32LE(0x46546c67, offset);
        offset += 4;
        out.writeUInt32LE(2, offset);
        offset += 4;
        out.writeUInt32LE(totalLength, offset);
        offset += 4;
        out.writeUInt32LE(jsonPadded.length, offset);
        offset += 4;
        out.writeUInt32LE(0x4e4f534a, offset);
        offset += 4;
        jsonPadded.copy(out, offset);
        offset += jsonPadded.length;
        out.writeUInt32LE(binPadded.length, offset);
        offset += 4;
        out.writeUInt32LE(0x004e4942, offset);
        offset += 4;
        binPadded.copy(out, offset);
        return out;
    }

    private addBufferView(data: Buffer, target?: number): number {
        const aligned = padBuffer(data, 0x00);
        const byteOffset = this.chunks.reduce((sum, chunk) => sum + chunk.length, 0);
        this.chunks.push(aligned);
        this.gltf.bufferViews.push({
            buffer: 0,
            byteOffset,
            byteLength: data.length,
            ...(target ? { target } : {}),
        });
        return this.gltf.bufferViews.length - 1;
    }
}

function typeWidth(type: "SCALAR" | "VEC2" | "VEC3" | "VEC4"): number {
    if (type === "SCALAR") return 1;
    if (type === "VEC2") return 2;
    if (type === "VEC3") return 3;
    return 4;
}

function minMax(data: Float32Array, width: number): { min: number[]; max: number[] } {
    const min = Array(width).fill(Number.POSITIVE_INFINITY);
    const max = Array(width).fill(Number.NEGATIVE_INFINITY);
    for (let i = 0; i < data.length; i += width) {
        for (let c = 0; c < width; c++) {
            const value = data[i + c];
            min[c] = Math.min(min[c], value);
            max[c] = Math.max(max[c], value);
        }
    }
    return { min, max };
}

function padBuffer(buffer: Buffer, fill: number): Buffer {
    const paddedLength = (buffer.length + 3) & ~3;
    if (paddedLength === buffer.length) return buffer;
    const out = Buffer.alloc(paddedLength, fill);
    buffer.copy(out);
    return out;
}

import fs from "node:fs";
import path from "node:path";
import { convertDdsToPng } from "@marcuth/dds-to-png";
import { decodeImage, parseDDSHeader } from "dds-ktx-parser";
import { PNG } from "pngjs";
import type { Logger } from "../internal/logger";

type IniSection = {
    header: string;
    name: string;
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

const GL_COMPONENT = {
    BYTE: 5120,
    UNSIGNED_BYTE: 5121,
    SHORT: 5122,
    UNSIGNED_SHORT: 5123,
    UNSIGNED_INT: 5125,
    FLOAT: 5126,
} as const;

export async function convertModToGlb(
    options: ConvertModToGlbOptions,
): Promise<ConvertModToGlbResult> {
    const isDebug = !!options.debug;
    const outputDir = path.dirname(path.resolve(options.outputPath));
    const textureCacheDir = isDebug
        ? path.resolve(outputDir, "texture-cache")
        : path.resolve(outputDir, `.texture-cache-${Math.random().toString(36).slice(2)}`);

    try {
        const glbResult = await buildModGlb({
            ...options,
            textureCacheDir,
        });

        fs.mkdirSync(outputDir, { recursive: true });
        fs.writeFileSync(options.outputPath, glbResult.glb);

        return {
            iniPath: glbResult.iniPath,
            outputPath: path.resolve(options.outputPath),
            meshCount: glbResult.meshCount,
            warningCount: glbResult.warningCount,
        };
    } finally {
        if (!isDebug && fs.existsSync(textureCacheDir)) {
            fs.rmSync(textureCacheDir, { recursive: true, force: true });
        }
    }
}

export async function convertModToGlbBuffer(
    options: ConvertModToGlbBufferOptions,
): Promise<ConvertModToGlbBufferResult> {
    return buildModGlb(options);
}

async function buildModGlb(
    options: ConvertModToGlbBufferOptions,
): Promise<ConvertModToGlbBufferResult> {
    const warning = createWarningCollector(options.onWarning);
    const iniPath = findIni(options.modPath);
    const modDir = path.dirname(iniPath);
    const sections = parseIni(fs.readFileSync(iniPath, "utf8"));
    const resources = collectResources(sections);
    const bufferGroups = collectBufferGroups(modDir, resources, warning.warn);
    const textureBindings = collectTextureBindings(sections);
    const ibResources = collectIbResources(resources, bufferGroups, textureBindings);

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

        const fmt = loadFmtForIb(modDir, options.assetPath, ib, group.stride);
        const ibPath = path.resolve(modDir, ib.filename);
        if (!fs.existsSync(ibPath)) {
            warning.warn(`Missing IB file: ${ibPath}`);
            continue;
        }

        const indices = decodeIndices(fs.readFileSync(ibPath), ib.format || fmt.indexFormat);
        if (indices.length === 0) {
            warning.warn(`Empty IB file: ${ibPath}`);
            continue;
        }

        const material = materialBindings.get(normalizeKey(ib.name));
        const primitive = buildPrimitive(
            builder,
            group.vbBytes,
            group.stride,
            fmt,
            indices,
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

function findIni(input: string): string {
    const resolved = path.resolve(input);
    const stat = fs.statSync(resolved);
    if (stat.isFile()) return resolved;

    const candidates = fs
        .readdirSync(resolved)
        .filter(
            (file) => file.toLowerCase().endsWith(".ini") && file.toLowerCase() !== "merged.ini",
        )
        .map((file) => path.resolve(resolved, file));

    if (candidates.length === 0) {
        throw new Error(`No .ini found in ${input}`);
    }

    return candidates[0];
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
                values: {},
            };
            sections.push(current);
            continue;
        }

        if (!current) continue;
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

function collectBufferGroups(
    modDir: string,
    resources: Resource[],
    warn: (message: string) => void,
): BufferGroup[] {
    const byKey = new Map<
        string,
        { position?: Resource; blend?: Resource; texcoord?: Resource; single?: Resource }
    >();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const lower = resource.name.toLowerCase();
        if (lower.endsWith("position")) {
            const key = resource.name.slice(0, -"Position".length);
            ensureGroup(byKey, key).position = resource;
        } else if (lower.endsWith("blend")) {
            const key = resource.name.slice(0, -"Blend".length);
            ensureGroup(byKey, key).blend = resource;
        } else if (lower.endsWith("texcoord")) {
            const key = resource.name.slice(0, -"Texcoord".length);
            ensureGroup(byKey, key).texcoord = resource;
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
            if (!fs.existsSync(filePath)) {
                warn(`Missing vertex buffer file: ${filePath}`);
                continue;
            }
            groups.push({
                key,
                vbFilename: group.single.filename,
                vbBytes: fs.readFileSync(filePath),
                stride: group.single.stride,
            });
            continue;
        }

        if (group.position?.filename && group.blend?.filename && group.texcoord?.filename) {
            const position = readResourceBytes(modDir, group.position);
            const blend = readResourceBytes(modDir, group.blend);
            const texcoord = readResourceBytes(modDir, group.texcoord);
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

function readResourceBytes(modDir: string, resource: Resource): Buffer {
    const filePath = path.resolve(modDir, resource.filename!);
    if (!fs.existsSync(filePath)) throw new Error(`Missing resource file: ${filePath}`);
    return fs.readFileSync(filePath);
}

function collectIbResources(
    resources: Resource[],
    bufferGroups: BufferGroup[],
    textureBindings: TextureBinding[],
): IbResource[] {
    const bufferKeys = bufferGroups.map((group) => group.key);
    const bindingsByIbName = new Map(
        textureBindings.map((binding) => [normalizeKey(binding.ibResourceName), binding]),
    );
    const referencedIbNames = new Set(bindingsByIbName.keys());

    return resources
        .filter((resource) => {
            if (!resource.filename || !resource.format) return false;

            const lowerFilename = resource.filename.toLowerCase();
            if (lowerFilename.endsWith(".ib")) return true;

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

function collectTextureBindings(sections: IniSection[]): TextureBinding[] {
    const bindings: TextureBinding[] = [];
    const overrideTextureResources = sections
        .filter((section) => section.header === "TextureOverride")
        .map((section) => {
            const thisValue = section.values.this;
            if (!thisValue || !thisValue.toLowerCase().startsWith("resource")) return null;
            return {
                sectionName: section.name,
                resourceName: trimResourcePrefix(thisValue),
            };
        })
        .filter((entry): entry is { sectionName: string; resourceName: string } => !!entry);

    for (const section of sections) {
        if (section.header !== "TextureOverride") continue;

        const ibValue = section.values.ib;
        if (!ibValue) continue;

        const textureResourceNames = Object.entries(section.values)
            .filter(([key, value]) => {
                return (
                    key.toLowerCase().startsWith("ps-t") &&
                    value.toLowerCase().startsWith("resource")
                );
            })
            .map(([, value]) => trimResourcePrefix(value));

        const diffuseResourceName =
            textureResourceNames.find((name) => name.toLowerCase().includes("diffuse")) ||
            textureResourceNames.find((name) => {
                const lower = name.toLowerCase();
                return !lower.includes("normal") && !lower.includes("light");
            }) ||
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
        const resourceLower = entry.resourceName.toLowerCase();
        return resourceLower.includes("diffuse");
    });

    return (
        preferred.find((entry) => entry.sectionName.toLowerCase() === exactSection)?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase() === exactResource)
            ?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(sectionName.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase().startsWith(ibStem.toLowerCase()))
            ?.resourceName ||
        preferred.find((entry) =>
            entry.resourceName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName
    );
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

    for (const binding of textureBindings) {
        if (!binding.diffuseResourceName) {
            options.logger?.debug(
                `Binding for ${binding.ibResourceName} has no diffuse resource name`,
                "StaticGLB",
            );
            continue;
        }

        const textureResource = resourcesByName.get(normalizeKey(binding.diffuseResourceName));
        if (!textureResource?.filename) {
            options.logger?.debug(
                `Texture resource ${binding.diffuseResourceName} not found or has no filename`,
                "StaticGLB",
            );
            continue;
        }

        const texturePath = path.resolve(modDir, textureResource.filename);
        if (!fs.existsSync(texturePath)) {
            warn(`Texture file not found: ${texturePath}`);
            continue;
        }

        let cached = textureCache.get(texturePath);
        if (!cached) {
            const texture = await prepareTexturePng(
                options,
                texturePath,
                textureOutDir,
                binding.diffuseResourceName,
                warn,
            );
            if (!texture) {
                options.logger?.debug(`Failed to prepare texture ${texturePath}`, "StaticGLB");
                continue;
            }

            options.logger?.debug(
                `Prepared texture: ${texture.pngPath} (inverted: ${texture.invertedAlpha})`,
                "StaticGLB",
            );

            const imageIndex = builder.addImage(
                fs.readFileSync(texture.pngPath),
                "image/png",
                path.basename(texture.pngPath),
            );
            const textureIndex = builder.addTexture(imageIndex);
            const materialIndex = builder.addMaterial({
                name: binding.diffuseResourceName,
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
                textureResourceName: binding.diffuseResourceName,
                pngPath: texture.pngPath,
            };
            textureCache.set(texturePath, cached);
        }

        materialByIb.set(normalizeKey(binding.ibResourceName), cached);
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
        const png = PNG.sync.read(fs.readFileSync(pngPath));
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
            fs.mkdirSync(textureOutDir, { recursive: true });
            fs.writeFileSync(invertedPath, PNG.sync.write(png));
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
    fs.mkdirSync(textureOutDir, { recursive: true });

    const pngPath = path.join(
        textureOutDir,
        `${path.basename(texturePath, path.extname(texturePath))}.png`,
    );

    try {
        await convertDdsToPng(texturePath, pngPath);
        return pngPath;
    } catch {
        try {
            convertDdsToPngFallback(texturePath, pngPath);
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

function convertDdsToPngFallback(texturePath: string, pngPath: string): void {
    const dds = fs.readFileSync(texturePath);
    const info = parseDDSHeader(dds);
    if (!info || !info.layers[0]) {
        throw new Error("DDS header could not be parsed");
    }

    const rgba = decodeImage(dds, info.format, info.layers[0]);
    const png = new PNG({ width: info.shape.width, height: info.shape.height });
    png.data.set(rgba);
    fs.writeFileSync(pngPath, PNG.sync.write(png));
}

function trimResourcePrefix(value: string): string {
    return value.trim().replace(/^Resource/i, "");
}

function bestKeyForIb(stem: string, resourceName: string, keys: string[]): string {
    const normalizedStem = normalizeKey(stem);
    const normalizedName = normalizeKey(resourceName.replace(/IB$/i, ""));
    const sorted = [...keys].sort((a, b) => b.length - a.length);
    return (
        sorted.find((key) => normalizedStem.includes(normalizeKey(key))) ||
        sorted.find((key) => normalizedName.includes(normalizeKey(key))) ||
        stem
    );
}

function keyMatchesIb(groupKey: string, ibKey: string): boolean {
    const a = normalizeKey(groupKey);
    const b = normalizeKey(ibKey);
    return a === b || a.includes(b) || b.includes(a);
}

function normalizeKey(value: string): string {
    return value.toLowerCase().replace(/[^a-z0-9]/g, "");
}

function loadFmtForIb(modDir: string, assetDir: string, ib: IbResource, stride: number): FmtLayout {
    const stem = path.basename(ib.filename, path.extname(ib.filename));
    const localFmt = path.resolve(modDir, `${stem}.fmt`);
    if (fs.existsSync(localFmt)) {
        return parseFmt(fs.readFileSync(localFmt, "utf8"), stride, ib.format);
    }

    const assetFmt = findRecursive(assetDir, (file) => {
        const lower = path.basename(file).toLowerCase();
        return lower.endsWith(".fmt") && normalizeKey(lower).includes(normalizeKey(stem));
    });
    if (assetFmt) {
        return parseFmt(fs.readFileSync(assetFmt, "utf8"), stride, ib.format);
    }

    let vb0Txt = findRecursive(assetDir, (file) => {
        const lower = path.basename(file).toLowerCase();
        return (
            lower.endsWith(".txt") &&
            lower.includes("vb0") &&
            normalizeKey(lower).includes(normalizeKey(stem))
        );
    });

    if (!vb0Txt) {
        const ibHash = normalizeKey(ib.overrideHash || ib.key || stem);
        const ibTxt = findRecursive(assetDir, (file) => {
            const lower = path.basename(file).toLowerCase();
            return (
                lower.endsWith(".txt") &&
                lower.includes("-ib=") &&
                normalizeKey(lower).includes(ibHash)
            );
        });

        if (ibTxt) {
            const ibBase = path.basename(ibTxt).replace(/-ib=.*$/i, "");
            vb0Txt = findRecursive(assetDir, (file) => {
                const lower = path.basename(file).toLowerCase();
                return (
                    lower.endsWith(".txt") &&
                    lower.includes("vb0") &&
                    lower.startsWith(ibBase.toLowerCase())
                );
            });
        }
    }

    if (!vb0Txt) {
        throw new Error(`No matching .fmt or *-vb0.txt found for ${ib.filename} under ${assetDir}`);
    }

    return parseFmt(
        extractFmtFromVb0(fs.readFileSync(vb0Txt, "utf8"), stride, ib.format),
        stride,
        ib.format,
    );
}

function findRecursive(root: string, predicate: (file: string) => boolean): string | null {
    const stack = [path.resolve(root)];
    while (stack.length > 0) {
        const dir = stack.pop()!;
        for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
            const full = path.join(dir, entry.name);
            if (entry.isDirectory()) {
                stack.push(full);
            } else if (entry.isFile() && predicate(full)) {
                return full;
            }
        }
    }
    return null;
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

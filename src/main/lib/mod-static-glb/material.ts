import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { prepareTextureForMaterial } from "@native/static-glb";
import { decodeImage, parseDDSHeader } from "dds-ktx-parser";
import fse from "fs-extra";
import pLimit from "p-limit";
import { PNG } from "pngjs";
import type { GlbBuilder } from "./builder";
import { createTextureCacheBaseName, normalizeKey } from "./shared";
import type { PreparedTexture, StaticGlbTextureFormat } from "./texture-utils";
import { textureNamePriority } from "./texture-utils";
import type {
    ConvertModToGlbBufferOptions,
    MaterialBinding,
    Resource,
    TextureBinding,
} from "./types";

const DEFAULT_TEXTURE_FORMAT: StaticGlbTextureFormat = "jpeg-safe";
const DEFAULT_JPEG_QUALITY = 85;

export async function buildMaterials(
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

export async function prepareTextureImage(
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

export function collectMaterialTextureCandidateNames(binding: TextureBinding): string[] {
    const ordered = new Set<string>();
    for (const resourceName of binding.textureResourceNames ?? []) {
        ordered.add(resourceName);
    }
    if (binding.diffuseResourceName) {
        ordered.add(binding.diffuseResourceName);
    }
    return Array.from(ordered);
}

export function resolveTextureFormatOption(
    format?: StaticGlbTextureFormat,
): StaticGlbTextureFormat {
    if (format === "png" || format === "jpeg-safe" || format === "jpeg-force") {
        return format;
    }

    return DEFAULT_TEXTURE_FORMAT;
}

export function normalizeJpegQualityOption(quality?: number): number {
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

export async function convertDdsToPngFallback(texturePath: string, pngPath: string): Promise<void> {
    const png = await decodeDdsToPngObject(texturePath);
    await writePngBuffer(png, pngPath);
}

export async function decodeDdsToPngObject(texturePath: string): Promise<PNG> {
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

export async function writePngBuffer(png: PNG, pngPath: string): Promise<void> {
    await pipeline(png.pack(), fse.createWriteStream(pngPath));
}

export function normalizeSrgbConfidence(value: string): PreparedTexture["srgbConfidence"] {
    return value === "srgb" || value === "linear" || value === "unknown" ? value : "unknown";
}

export { convertDdsToPng } from "@native/utils";

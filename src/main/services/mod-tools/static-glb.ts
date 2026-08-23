import path from "node:path";

import type { NahidaDesktop } from "@main/index";
import {
    convertModToGlb,
    convertModToGlbBuffer,
    type ConvertModToGlbResult,
    convertModToVariantArtifacts,
    type ConvertModVariantArtifactsResult,
    resolveVariantStateArtifact,
    type StaticGlbTextureFormat,
    type StaticGlbVariantManifest,
    type VariableStateMap,
} from "@main/lib/mod-static-glb";
import { createStateKey } from "@main/lib/mod-static-glb/shared";
import { loadModViewerPayload } from "@main/lib/mod-viewer";
import {
    cleanupModelViewerMemorySession,
    createModelViewerMemorySession,
    writeModelViewerMemoryBuffer,
} from "@main/services/protocol/model-viewer-memory";
import type { ModViewerTransport } from "@shared/mod-viewer/types";
import { toErrorMessage } from "@shared/utils";
import { app } from "electron";
import fse from "fs-extra";

const ASSET_PATH_SETTING_KEY = "mod_static_glb_asset_path";
const TEXTURE_FORMAT_SETTING_KEY = "mod_static_glb_texture_format";
const JPEG_QUALITY_SETTING_KEY = "mod_static_glb_jpeg_quality";
const MODEL_VIEWER_TEMP_PREFIX = "nhd-model-viewer-";
const DEFAULT_TEXTURE_FORMAT: StaticGlbTextureFormat = "jpeg-safe";
const DEFAULT_JPEG_QUALITY = 85;

export type StaticGlbConvertInput = {
    modPath: string;
    assetPath?: string;
    outputPath: string;
    textureFormat?: StaticGlbTextureFormat;
    jpegQuality?: number;
    includeTangents?: boolean;
    debug?: boolean;
};

export type StaticGlbTextureSettings = {
    textureFormat: StaticGlbTextureFormat;
    jpegQuality: number;
};

export type StaticGlbSingleResult = {
    mode: "single";
    iniPath: string;
    glbPath: string;
    memorySessionId?: string;
    meshCount: number;
    warningCount: number;
    name: string;
};

export type StaticGlbVariantResult = {
    mode: "variant-set";
    iniPath: string;
    artifactRoot: string;
    manifestPath: string;
    manifest: StaticGlbVariantManifest;
    memorySessionId?: string;
    defaultGlbPath: string;
    activeGlbPath: string;
    meshCount: number;
    warningCount: number;
    name: string;
};

export type StaticGlbViewerResult =
    | (ConvertModToGlbResult & {
          mode: "single";
          glbPath: string;
          name: string;
      })
    | (ConvertModVariantArtifactsResult & {
          mode: "variant-set";
          glbPath: string;
          name: string;
      });

export type StaticGlbPreviewResult = StaticGlbSingleResult | StaticGlbVariantResult;

export type StaticGlbViewerInput =
    | string
    | {
          modPath?: string;
          artifactRoot?: string;
          manifestPath?: string;
          memorySessionId?: string;
          state?: VariableStateMap;
      };

type ViewerMemorySession = {
    manifest?: StaticGlbVariantManifest;
    manifestPath?: string;
    modPath: string;
};

export class StaticGlb {
    private readonly viewerMemorySessions = new Map<string, ViewerMemorySession>();

    constructor(private readonly desktop: NahidaDesktop) {
        this.desktop.service.startupCleanup.register({
            name: "mod-tools:static-glb-viewer",
            run: () => this.cleanupStaleViewerTempDirs(),
        });
    }

    public async getAssetPath(): Promise<string> {
        return (await this.getSettingValue(ASSET_PATH_SETTING_KEY)) || "";
    }

    public async setAssetPath(assetPath: string): Promise<string> {
        const normalized = path.resolve(assetPath.trim());
        const stat = await fse.stat(normalized);
        if (!stat.isDirectory()) {
            throw new Error("Asset path must be a directory.");
        }

        await this.desktop.lib.db.settings.upsert(ASSET_PATH_SETTING_KEY, normalized);

        return normalized;
    }

    public async getTextureFormat(): Promise<StaticGlbTextureFormat> {
        return normalizeTextureFormat(await this.getSettingValue(TEXTURE_FORMAT_SETTING_KEY));
    }

    public async setTextureFormat(
        textureFormat: StaticGlbTextureFormat,
    ): Promise<StaticGlbTextureFormat> {
        const normalized = normalizeTextureFormat(textureFormat);
        await this.saveSettingValue(TEXTURE_FORMAT_SETTING_KEY, normalized);
        return normalized;
    }

    public async getJpegQuality(): Promise<number> {
        return normalizeJpegQuality(await this.getSettingValue(JPEG_QUALITY_SETTING_KEY));
    }

    public async setJpegQuality(jpegQuality: number): Promise<number> {
        const normalized = normalizeJpegQuality(jpegQuality);
        await this.saveSettingValue(JPEG_QUALITY_SETTING_KEY, String(normalized));
        return normalized;
    }

    public async getTextureSettings(): Promise<StaticGlbTextureSettings> {
        const [textureFormat, jpegQuality] = await Promise.all([
            this.getTextureFormat(),
            this.getJpegQuality(),
        ]);
        return {
            textureFormat,
            jpegQuality,
        };
    }

    public async convert(input: StaticGlbConvertInput): Promise<StaticGlbViewerResult> {
        const assetPath = input.assetPath?.trim() || (await this.getAssetPath());
        if (!assetPath) {
            throw new Error("Set the static GLB asset path in Mod Tools first.");
        }

        const savedTextureSettings = await this.getTextureSettings();
        const textureFormat = normalizeTextureFormat(
            input.textureFormat ?? savedTextureSettings.textureFormat,
        );
        const jpegQuality = normalizeJpegQuality(
            input.jpegQuality ?? savedTextureSettings.jpegQuality,
        );

        await this.setAssetPath(assetPath);
        await Promise.all([this.setTextureFormat(textureFormat), this.setJpegQuality(jpegQuality)]);

        const outputPath = ensureGlbExtension(path.resolve(input.outputPath));
        const warnings: string[] = [];
        const artifactRoot = path.resolve(stripGlbExtension(outputPath));
        const variantResult = await convertModToVariantArtifacts({
            modPath: input.modPath,
            assetPath,
            artifactRoot,
            includeTangents: input.includeTangents,
            textureFormat,
            jpegQuality,
            debug: input.debug,
            logger: this.desktop.logger,
            onWarning: (message) => {
                warnings.push(message);
                this.desktop.logger.warn(message, "StaticGlb.convert");
            },
        });

        if (variantResult) {
            if (warnings.length > 0) {
                this.desktop.window.main.window?.webContents.send(
                    "fn:toast",
                    "Static GLB conversion completed with warnings",
                    { description: warnings.slice(0, 3).join("\n") },
                );
            }

            return {
                ...variantResult,
                mode: "variant-set",
                glbPath: variantResult.defaultGlbPath,
                name: path.basename(variantResult.artifactRoot),
            };
        }

        const result = await convertModToGlb({
            modPath: input.modPath,
            assetPath,
            outputPath,
            includeTangents: input.includeTangents,
            textureFormat,
            jpegQuality,
            debug: input.debug,
            logger: this.desktop.logger,
            onWarning: (message) => {
                warnings.push(message);
                this.desktop.logger.warn(message, "StaticGlb.convert");
            },
        });

        if (warnings.length > 0) {
            this.desktop.window.main.window?.webContents.send(
                "fn:toast",
                "Static GLB conversion completed with warnings",
                { description: warnings.slice(0, 3).join("\n") },
            );
        }

        return {
            ...result,
            mode: "single",
            glbPath: result.outputPath,
            name: path.basename(result.outputPath, ".glb"),
        };
    }

    public async loadForViewer(modPath: string): Promise<ModViewerTransport> {
        const startedAt = Date.now();
        this.desktop.logger.info("Starting model viewer load", "StaticGlb.loadForViewer");
        const memorySessionId = createModelViewerMemorySession();
        try {
            const payload = await loadModViewerPayload(modPath, this.desktop.logger);
            const writeBuffer = (bufferId: string, buffer: Buffer, contentType?: string) =>
                writeModelViewerMemoryBuffer(memorySessionId, bufferId, buffer, contentType);

            const textures: ModViewerTransport["textures"] = {};
            for (const [key, texture] of Object.entries(payload.textures)) {
                textures[key] = {
                    url: writeBuffer(`tex:${key}`, texture.bytes, texture.mimeType),
                    role: texture.role,
                };
            }

            const meshes: ModViewerTransport["meshes"] = payload.meshes.map((mesh) => ({
                id: mesh.id,
                component: mesh.component,
                positionsUrl: writeBuffer(
                    `${mesh.id}.pos`,
                    typedArrayBuffer(mesh.positions),
                    "application/octet-stream",
                ),
                normalsUrl: mesh.normals
                    ? writeBuffer(
                          `${mesh.id}.normal`,
                          typedArrayBuffer(mesh.normals),
                          "application/octet-stream",
                      )
                    : undefined,
                tangentsUrl: mesh.tangents
                    ? writeBuffer(
                          `${mesh.id}.tangent`,
                          typedArrayBuffer(mesh.tangents),
                          "application/octet-stream",
                      )
                    : undefined,
                uvsUrl: mesh.uvs
                    ? writeBuffer(
                          `${mesh.id}.uv`,
                          typedArrayBuffer(mesh.uvs),
                          "application/octet-stream",
                      )
                    : undefined,
                indicesUrl: writeBuffer(
                    `${mesh.id}.idx`,
                    typedArrayBuffer(mesh.indices),
                    "application/octet-stream",
                ),
                conditions: mesh.conditions,
                texKey: mesh.texKey,
                textureVariants: mesh.textureVariants,
                normalMapKey: mesh.normalMapKey,
                normalMapVariants: mesh.normalMapVariants,
                lightMapKey: mesh.lightMapKey,
                lightMapVariants: mesh.lightMapVariants,
                materialMapKey: mesh.materialMapKey,
                materialMapVariants: mesh.materialMapVariants,
                shapeTargets: mesh.shapeTargets.map((target, index) => ({
                    var: target.var,
                    positionsUrl: writeBuffer(
                        `${mesh.id}.shape.${index}`,
                        typedArrayBuffer(target.positions),
                        "application/octet-stream",
                    ),
                    mode: target.mode,
                    lowPositionsUrl: target.lowPositions
                        ? writeBuffer(
                              `${mesh.id}.shape.${index}.low`,
                              typedArrayBuffer(target.lowPositions),
                              "application/octet-stream",
                          )
                        : undefined,
                })),
                positionVariants: mesh.positionVariants.map((variant, index) => ({
                    conditions: variant.conditions,
                    positionsUrl: writeBuffer(
                        `${mesh.id}.posvar.${index}`,
                        typedArrayBuffer(variant.positions),
                        "application/octet-stream",
                    ),
                })),
            }));

            this.viewerMemorySessions.set(memorySessionId, {
                modPath,
            });
            this.desktop.logger.info(
                `Completed model viewer load in ${Date.now() - startedAt}ms (meshes=${meshes.length})`,
                "StaticGlb.loadForViewer",
            );
            return {
                memorySessionId,
                iniPath: payload.iniPath,
                modPath,
                name: path.basename(modPath.replace(/[\\/]+$/, "")),
                materialProfile: payload.materialProfile,
                meshes,
                textures,
                variables: payload.variables,
                defaultState: payload.defaultState,
                stateRules: payload.stateRules,
                uiAssets: payload.uiAssets,
                animations: payload.animations,
            };
        } catch (error) {
            cleanupModelViewerMemorySession(memorySessionId);
            this.viewerMemorySessions.delete(memorySessionId);
            this.desktop.logger.error(
                `Model viewer load failed after ${Date.now() - startedAt}ms for ${modPath}: ${toErrorMessage(error)}`,
                "StaticGlb.loadForViewer",
            );
            throw error;
        }
    }

    public async convertForViewer(input: StaticGlbViewerInput): Promise<StaticGlbPreviewResult> {
        const startedAt = Date.now();
        let memorySessionId: string | undefined;
        let lastCheckpointAt = startedAt;
        const logTiming = (stage: string) => {
            const now = Date.now();
            const totalElapsedMs = now - startedAt;
            const stageElapsedMs = now - lastCheckpointAt;
            lastCheckpointAt = now;
            this.desktop.logger.info(
                `${stage} completed in ${stageElapsedMs}ms (total ${totalElapsedMs}ms)`,
                "StaticGlb.convertForViewer",
            );
        };

        this.desktop.logger.info("Starting model viewer conversion", "StaticGlb.convertForViewer");
        const assetPath = await this.getAssetPath();
        if (!assetPath) {
            throw new Error("Set the static GLB asset path in Mod Tools first.");
        }
        const textureSettings = await this.getTextureSettings();
        logTiming("Loaded asset path and texture settings");

        if (typeof input !== "string" && input.artifactRoot && input.state) {
            const session = input.memorySessionId
                ? this.viewerMemorySessions.get(input.memorySessionId)
                : undefined;
            if (input.memorySessionId && !session) {
                throw new Error(`Missing model viewer memory session: ${input.memorySessionId}`);
            }
            const writeMemoryBuffer = async (
                bufferId: string,
                buffer: Buffer,
                options?: { contentType?: string },
            ) => {
                if (!input.memorySessionId) {
                    throw new Error("Missing model viewer memory session.");
                }

                return writeModelViewerMemoryBuffer(
                    input.memorySessionId,
                    bufferId,
                    buffer,
                    options?.contentType,
                );
            };
            const result = await resolveVariantStateArtifact({
                artifactRoot: input.artifactRoot,
                artifactBufferWriter: session ? writeMemoryBuffer : undefined,
                manifest: session?.manifest,
                manifestPath: session?.manifestPath ?? input.manifestPath,
                state: input.state,
                assetPath,
                modPath: session?.modPath ?? input.modPath ?? input.artifactRoot,
                textureFormat: textureSettings.textureFormat,
                jpegQuality: textureSettings.jpegQuality,
                useTextureCache: !session,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });
            logTiming("Resolved variant state artifact");
            if (session) {
                session.manifest = result.manifest;
                session.manifestPath = result.manifestPath;
            }

            return {
                mode: "variant-set",
                iniPath: result.manifest.iniPath,
                artifactRoot: input.artifactRoot,
                manifestPath: result.manifestPath,
                manifest: result.manifest,
                memorySessionId: input.memorySessionId,
                defaultGlbPath:
                    result.manifest.states.find(
                        (entry) =>
                            entry.key ===
                            createStateKey(result.manifest.defaultState as VariableStateMap),
                    )?.glbPath || result.glbPath,
                activeGlbPath: result.glbPath,
                meshCount: result.meshCount,
                warningCount: result.warningCount,
                name: result.manifest.name,
            };
        }

        const modPath = typeof input === "string" ? input : input.modPath;
        if (!modPath) {
            throw new Error("Missing mod path for static GLB viewer conversion.");
        }

        const modName = path.basename(modPath.replace(/[\\/]+$/, ""));
        memorySessionId = createModelViewerMemorySession();
        const warnings: string[] = [];
        const writeMemoryBuffer = async (
            bufferId: string,
            buffer: Buffer,
            options?: { contentType?: string },
        ) => {
            if (!memorySessionId) {
                throw new Error("Missing model viewer memory session.");
            }

            return writeModelViewerMemoryBuffer(
                memorySessionId,
                bufferId,
                buffer,
                options?.contentType,
            );
        };
        logTiming("Created viewer memory session");

        try {
            const variantResult = await convertModToVariantArtifacts({
                modPath,
                assetPath,
                artifactRoot: memorySessionId,
                artifactBufferWriter: writeMemoryBuffer,
                animationBufferWriter: writeMemoryBuffer,
                preGenerateVariableStates: false,
                textureFormat: textureSettings.textureFormat,
                jpegQuality: textureSettings.jpegQuality,
                useTextureCache: false,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    warnings.push(message);
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });
            logTiming("Attempted variant artifact conversion");

            if (variantResult) {
                this.viewerMemorySessions.set(memorySessionId, {
                    manifest: variantResult.manifest,
                    manifestPath: variantResult.manifestPath,
                    modPath,
                });
                this.desktop.logger.info(
                    `Completed model viewer conversion in ${Date.now() - startedAt}ms`,
                    "StaticGlb.convertForViewer",
                );
                return {
                    mode: "variant-set",
                    iniPath: variantResult.iniPath,
                    artifactRoot: memorySessionId,
                    manifestPath: variantResult.manifestPath,
                    manifest: variantResult.manifest,
                    memorySessionId,
                    defaultGlbPath: variantResult.defaultGlbPath,
                    activeGlbPath: variantResult.defaultGlbPath,
                    meshCount: variantResult.meshCount,
                    warningCount: variantResult.warningCount,
                    name: modName,
                };
            }

            const result = await convertModToGlbBuffer({
                modPath,
                assetPath,
                textureFormat: textureSettings.textureFormat,
                jpegQuality: textureSettings.jpegQuality,
                useTextureCache: false,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    warnings.push(message);
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });
            logTiming("Converted mod to GLB buffer");

            const glbPath = await writeMemoryBuffer(
                `${sanitizeModelViewerFileName(modName)}.glb`,
                result.glb,
                { contentType: "model/gltf-binary" },
            );
            logTiming("Stored GLB buffer");

            if (warnings.length > 0) {
                this.desktop.window.main.window?.webContents.send(
                    "fn:toast",
                    "Model viewer opened with conversion warnings",
                    { description: warnings.slice(0, 3).join("\n") },
                );
            }

            this.desktop.logger.info(
                `Completed model viewer conversion in ${Date.now() - startedAt}ms`,
                "StaticGlb.convertForViewer",
            );
            return {
                mode: "single",
                iniPath: result.iniPath,
                glbPath,
                memorySessionId,
                meshCount: result.meshCount,
                warningCount: result.warningCount,
                name: modName,
            };
        } catch (error) {
            cleanupModelViewerMemorySession(memorySessionId);
            if (memorySessionId) {
                this.viewerMemorySessions.delete(memorySessionId);
            }
            this.desktop.logger.error(
                `Model viewer conversion failed after ${Date.now() - startedAt}ms`,
                "StaticGlb.convertForViewer",
            );
            throw error;
        }
    }

    public async cleanupViewerFile(targetPath: string, memorySessionId?: string): Promise<void> {
        cleanupModelViewerMemorySession(memorySessionId);
        if (memorySessionId) {
            this.viewerMemorySessions.delete(memorySessionId);
        }
        if (!targetPath) {
            return;
        }

        if (targetPath.startsWith("model-viewer-memory://") || targetPath === memorySessionId) {
            return;
        }

        const resolvedPath = path.resolve(targetPath);
        const tempRoot = path.resolve(app.getPath("temp"));

        if (!resolvedPath.startsWith(tempRoot + path.sep)) {
            this.desktop.logger.warn(
                `Skipped cleanup for non-temp model viewer artifact: ${resolvedPath}`,
                "StaticGlb.cleanupViewerFile",
            );
            return;
        }

        const viewerTempDir = (await fse.stat(resolvedPath).catch(() => null))?.isDirectory()
            ? resolvedPath
            : path.dirname(resolvedPath);
        const viewerTempDirName = path.basename(viewerTempDir);

        if (!viewerTempDirName.startsWith(MODEL_VIEWER_TEMP_PREFIX)) {
            this.desktop.logger.warn(
                `Skipped cleanup for unexpected model viewer temp directory: ${viewerTempDir}`,
                "StaticGlb.cleanupViewerFile",
            );
            return;
        }

        await fse.remove(viewerTempDir).catch((error) => {
            this.desktop.logger.warn(
                `Failed to remove model viewer temp directory: ${toErrorMessage(error)}`,
                "StaticGlb.cleanupViewerFile",
            );
        });
    }

    private async cleanupStaleViewerTempDirs(): Promise<void> {
        const tempRoot = path.resolve(app.getPath("temp"));
        const tempEntries = await fse.readdir(tempRoot, { withFileTypes: true }).catch((error) => {
            this.desktop.logger.warn(
                `Failed to read temp directory for model viewer cleanup: ${toErrorMessage(error)}`,
                "StaticGlb.cleanupStaleViewerTempDirs",
            );
            return [];
        });

        await Promise.all(
            tempEntries
                .filter(
                    (entry) =>
                        entry.isDirectory() && entry.name.startsWith(MODEL_VIEWER_TEMP_PREFIX),
                )
                .map((entry) =>
                    this.cleanupViewerFile(path.join(tempRoot, entry.name, "stale.glb")),
                ),
        );
    }

    private async getSettingValue(key: string): Promise<string | null> {
        return await this.desktop.lib.db.settings.getValue(key);
    }

    private async saveSettingValue(key: string, value: string): Promise<void> {
        await this.desktop.lib.db.settings.upsert(key, value);
    }
}

function ensureGlbExtension(filePath: string): string {
    if (path.extname(filePath).toLowerCase() === ".glb") {
        return filePath;
    }

    return `${filePath}.glb`;
}

function stripGlbExtension(filePath: string): string {
    return path.extname(filePath).toLowerCase() === ".glb"
        ? filePath.slice(0, -path.extname(filePath).length)
        : filePath;
}

function sanitizeModelViewerFileName(name: string): string {
    const sanitized = Array.from(name, (char) => {
        const codePoint = char.codePointAt(0) ?? 0;
        const isControlCharacter = codePoint <= 0x1f;
        const isReservedCharacter = '<>:"/\\|?*'.includes(char);
        return isControlCharacter || isReservedCharacter ? "_" : char;
    })
        .join("")
        .trim();

    return sanitized || "model-viewer";
}

function normalizeTextureFormat(value?: string | null): StaticGlbTextureFormat {
    if (value === "png" || value === "jpeg-safe" || value === "jpeg-force") {
        return value;
    }

    return DEFAULT_TEXTURE_FORMAT;
}

function normalizeJpegQuality(value?: string | number | null): number {
    const parsed =
        typeof value === "number"
            ? value
            : typeof value === "string"
              ? Number.parseInt(value, 10)
              : Number.NaN;

    if (!Number.isFinite(parsed)) {
        return DEFAULT_JPEG_QUALITY;
    }

    return Math.max(1, Math.min(100, Math.round(parsed)));
}

function typedArrayBuffer(values: Float32Array | Uint32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

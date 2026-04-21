import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { setting } from "@main/internal/db/schema";
import {
    convertModToGlb,
    convertModToGlbBuffer,
    type ConvertModToGlbResult,
    convertModToVariantArtifacts,
    resolveVariantStateArtifact,
    type ConvertModVariantArtifactsResult,
    type StaticGlbVariantManifest,
    type VariableStateMap,
} from "@main/lib/mod-static-glb";
import { app } from "electron";
import fse from "fs-extra";

const ASSET_PATH_SETTING_KEY = "mod_static_glb_asset_path";
const MODEL_VIEWER_TEMP_PREFIX = "nhd-model-viewer-";

export type StaticGlbConvertInput = {
    modPath: string;
    assetPath?: string;
    outputPath: string;
    includeTangents?: boolean;
    debug?: boolean;
};

export type StaticGlbSingleResult = {
    mode: "single";
    iniPath: string;
    glbPath: string;
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
    defaultGlbPath: string;
    activeGlbPath: string;
    meshCount: number;
    warningCount: number;
    name: string;
};

export type StaticGlbViewerResult = (ConvertModToGlbResult & {
    mode: "single";
    glbPath: string;
    name: string;
}) | (ConvertModVariantArtifactsResult & {
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
          state?: VariableStateMap;
      };

export class StaticGlb {
    constructor(private readonly desktop: NahidaDesktop) {
        this.desktop.service.startupCleanup.register({
            name: "mod-tools:static-glb-viewer",
            run: () => this.cleanupStaleViewerTempDirs(),
        });
    }

    public async getAssetPath(): Promise<string> {
        const saved = await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, ASSET_PATH_SETTING_KEY),
        });

        return saved?.value || "";
    }

    public async setAssetPath(assetPath: string): Promise<string> {
        const normalized = path.resolve(assetPath.trim());
        const stat = await fse.stat(normalized);
        if (!stat.isDirectory()) {
            throw new Error("Asset path must be a directory.");
        }

        await this.desktop.lib.db
            .insert(setting)
            .values({ key: ASSET_PATH_SETTING_KEY, value: normalized })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value: normalized },
            });

        return normalized;
    }

    public async convert(input: StaticGlbConvertInput): Promise<StaticGlbViewerResult> {
        const assetPath = input.assetPath?.trim() || (await this.getAssetPath());
        if (!assetPath) {
            throw new Error("Set the static GLB asset path in Mod Tools first.");
        }

        await this.setAssetPath(assetPath);

        const outputPath = ensureGlbExtension(path.resolve(input.outputPath));
        const warnings: string[] = [];
        const artifactRoot = path.resolve(stripGlbExtension(outputPath));
        const variantResult = await convertModToVariantArtifacts({
            modPath: input.modPath,
            assetPath,
            artifactRoot,
            includeTangents: input.includeTangents,
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

    public async convertForViewer(input: StaticGlbViewerInput): Promise<StaticGlbPreviewResult> {
        const assetPath = await this.getAssetPath();
        if (!assetPath) {
            throw new Error("Set the static GLB asset path in Mod Tools first.");
        }

        if (typeof input !== "string" && input.artifactRoot && input.state) {
            const result = await resolveVariantStateArtifact({
                artifactRoot: input.artifactRoot,
                manifestPath: input.manifestPath,
                state: input.state,
                assetPath,
                modPath: input.modPath || input.artifactRoot,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });

            return {
                mode: "variant-set",
                iniPath: result.manifest.iniPath,
                artifactRoot: input.artifactRoot,
                manifestPath: result.manifestPath,
                manifest: result.manifest,
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
        const tempDir = await fse.mkdtemp(path.join(app.getPath("temp"), MODEL_VIEWER_TEMP_PREFIX));
        const textureCacheDir = path.join(tempDir, "textures");
        const glbPath = path.join(tempDir, `${sanitizeModelViewerFileName(modName)}.glb`);
        const warnings: string[] = [];

        try {
            const variantResult = await convertModToVariantArtifacts({
                modPath,
                assetPath,
                artifactRoot: tempDir,
                preGenerateVariableStates: false,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    warnings.push(message);
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });

            if (variantResult) {
                return {
                    mode: "variant-set",
                    iniPath: variantResult.iniPath,
                    artifactRoot: tempDir,
                    manifestPath: variantResult.manifestPath,
                    manifest: variantResult.manifest,
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
                textureCacheDir,
                logger: this.desktop.logger,
                onWarning: (message) => {
                    warnings.push(message);
                    this.desktop.logger.warn(message, "StaticGlb.convertForViewer");
                },
            });

            await fse.writeFile(glbPath, result.glb);

            if (warnings.length > 0) {
                this.desktop.window.main.window?.webContents.send(
                    "fn:toast",
                    "Model viewer opened with conversion warnings",
                    { description: warnings.slice(0, 3).join("\n") },
                );
            }

            return {
                mode: "single",
                iniPath: result.iniPath,
                glbPath,
                meshCount: result.meshCount,
                warningCount: result.warningCount,
                name: modName,
            };
        } catch (error) {
            await this.cleanupViewerFile(glbPath);
            throw error;
        } finally {
            await fse.remove(textureCacheDir).catch((error) => {
                this.desktop.logger.warn(
                    `Failed to remove model viewer texture cache: ${
                        error instanceof Error ? error.message : String(error)
                    }`,
                    "StaticGlb.convertForViewer",
                );
            });
        }
    }

    public async cleanupViewerFile(targetPath: string): Promise<void> {
        if (!targetPath) {
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
                `Failed to remove model viewer temp directory: ${
                    error instanceof Error ? error.message : String(error)
                }`,
                "StaticGlb.cleanupViewerFile",
            );
        });
    }

    private async cleanupStaleViewerTempDirs(): Promise<void> {
        const tempRoot = path.resolve(app.getPath("temp"));
        const tempEntries = await fse.readdir(tempRoot, { withFileTypes: true }).catch((error) => {
            this.desktop.logger.warn(
                `Failed to read temp directory for model viewer cleanup: ${
                    error instanceof Error ? error.message : String(error)
                }`,
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

function createStateKey(state: VariableStateMap): string {
    return Object.entries(state)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${key.toLowerCase()}=${String(value)}`)
        .join("&");
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

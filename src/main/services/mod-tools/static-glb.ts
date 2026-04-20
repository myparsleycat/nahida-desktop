import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { setting } from "@main/internal/db/schema";
import {
    convertModToGlb,
    convertModToGlbBuffer,
    type ConvertModToGlbResult,
} from "@main/lib/mod-static-glb";
import { app } from "electron";
import fse from "fs-extra";

const ASSET_PATH_SETTING_KEY = "mod_static_glb_asset_path";

export type StaticGlbConvertInput = {
    modPath: string;
    assetPath?: string;
    outputPath: string;
    includeTangents?: boolean;
    debug?: boolean;
};

export type StaticGlbViewerResult = ConvertModToGlbResult & {
    glbPath: string;
    name: string;
};

export type StaticGlbPreviewResult = {
    iniPath: string;
    glbBase64: string;
    meshCount: number;
    warningCount: number;
    name: string;
};

export class StaticGlb {
    constructor(private readonly desktop: NahidaDesktop) {}

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
            glbPath: result.outputPath,
            name: path.basename(result.outputPath, ".glb"),
        };
    }

    public async convertForViewer(modPath: string): Promise<StaticGlbPreviewResult> {
        const assetPath = await this.getAssetPath();
        if (!assetPath) {
            throw new Error("Set the static GLB asset path in Mod Tools first.");
        }

        const modName = path.basename(modPath.replace(/[\\/]+$/, ""));
        const textureCacheDir = await fse.mkdtemp(path.join(app.getPath("temp"), "nhd-model-viewer-"));
        const warnings: string[] = [];

        try {
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

            if (warnings.length > 0) {
                this.desktop.window.main.window?.webContents.send(
                    "fn:toast",
                    "Model viewer opened with conversion warnings",
                    { description: warnings.slice(0, 3).join("\n") },
                );
            }

            return {
                iniPath: result.iniPath,
                glbBase64: result.glb.toString("base64"),
                meshCount: result.meshCount,
                warningCount: result.warningCount,
                name: modName,
            };
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
}

function ensureGlbExtension(filePath: string): string {
    if (path.extname(filePath).toLowerCase() === ".glb") {
        return filePath;
    }

    return `${filePath}.glb`;
}

import path from "node:path";
import fse from "fs-extra";
import type { Logger } from "../../internal/logger";
import { convertDdsToPng, convertDdsToPngBuffer, convertDdsToPngFallback } from "./material";
import { normalizeKey } from "./shared";
import type { IniSection, StaticGlbArtifactBufferWriter, StaticGlbViewerUiAssets } from "./types";

export function collectViewerUiAssetPaths(sections: IniSection[]): StaticGlbViewerUiAssets {
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

export async function materializeViewerUiAssets(
    uiAssets: StaticGlbViewerUiAssets,
    modDir: string,
    uiDir: string,
    options: { artifactBufferWriter?: StaticGlbArtifactBufferWriter; logger?: Logger },
    warn: (message: string) => void,
): Promise<StaticGlbViewerUiAssets> {
    if (!options.artifactBufferWriter) {
        await fse.ensureDir(uiDir);
    }

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

export async function materializeUiAsset(
    sourcePath: string,
    outputDir: string,
    outputName: string,
    warn?: (message: string) => void,
    options?: { artifactBufferWriter?: StaticGlbArtifactBufferWriter; logger?: Logger },
): Promise<string | undefined> {
    if (!(await fse.pathExists(sourcePath))) {
        warn?.(`Missing UI asset: ${sourcePath}`);
        return undefined;
    }

    const extension = path.extname(sourcePath).toLowerCase();
    const outputPath = path.join(outputDir, `${outputName}.png`);

    if (extension === ".png") {
        if (options?.artifactBufferWriter) {
            return options.artifactBufferWriter(outputName, await fse.readFile(sourcePath), {
                contentType: "image/png",
                fileName: `${outputName}.png`,
            });
        }

        await fse.copyFile(sourcePath, outputPath);
        return outputPath;
    }

    if (extension === ".dds") {
        if (options?.artifactBufferWriter) {
            try {
                return await options.artifactBufferWriter(
                    outputName,
                    await convertDdsToPngBuffer(sourcePath),
                    {
                        contentType: "image/png",
                        fileName: `${outputName}.png`,
                    },
                );
            } catch (error) {
                warn?.(
                    `Failed to convert UI DDS ${sourcePath}: ${
                        error instanceof Error ? error.message : String(error)
                    }`,
                );
                return undefined;
            }
        }

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

export function findFirstResourcePath(
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

export function deriveVariableUiToken(variableId: string): string {
    const raw = variableId.replace(/^\$+/, "");
    const trimmed = raw.replace(/^swapvar/i, "");
    return trimmed || raw;
}

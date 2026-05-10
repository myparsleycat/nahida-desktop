import path from "node:path";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import writeFileAtomic from "write-file-atomic";
import { materializeAnimationClips } from "./animation";
import { buildModGlb, getDrawBindingsForIb, prepareStaticGlbBuildContext } from "./build";
import {
    createStateArtifactFileName,
    createStateKey,
    createTimedStageLogger,
    createWarningCollector,
} from "./shared";
import type {
    ConvertModToGlbBufferOptions,
    ConvertModToGlbBufferResult,
    ConvertModToGlbOptions,
    ConvertModToGlbResult,
    ConvertModVariantArtifactsResult,
    StaticGlbVariantManifest,
    VariableStateMap,
} from "./types";
import { materializeUiAsset, materializeViewerUiAssets } from "./ui-asset";
import { analyzeModVariants } from "./variant";

export type { StaticGlbTextureFormat } from "./texture-utils";
export type {
    ConvertModToGlbBufferOptions,
    ConvertModToGlbBufferResult,
    ConvertModToGlbOptions,
    ConvertModToGlbResult,
    ConvertModVariantArtifactsResult,
    StaticGlbAnimationClip,
    StaticGlbRealtimeShapeKey,
    StaticGlbVariantManifest,
    StaticGlbVariantSlider,
    StaticGlbVariantValue,
    StaticGlbVariantVariable,
    StaticGlbViewerUiAssets,
    VariableStateMap,
    VariableStateValue,
} from "./types";

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
    const logTiming = createTimedStageLogger(
        options.logger,
        "mod-static-glb.convertModToVariantArtifacts",
    );
    const analysis = await analyzeModVariants(options);
    logTiming(
        `Analyzed mod variants (variables=${analysis.variables.length}, animations=${analysis.animations.length}, shapeKeys=${analysis.shapeKeys.length})`,
    );
    if (analysis.variables.length === 0 && analysis.animations.length === 0) {
        return null;
    }

    const artifactRoot = path.resolve(options.artifactRoot);
    const glbDir = path.join(artifactRoot, "glb");
    const uiDir = path.join(artifactRoot, "ui");
    const textureCacheDir = path.join(artifactRoot, ".texture-cache");
    await fse.ensureDir(glbDir);
    logTiming("Prepared artifact directories");

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
            const stateStartedAt = Date.now();
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
            logTiming(
                `Generated state GLB ${key} (meshCount=${result.meshCount}, warnings=${result.warningCount})`,
                stateStartedAt,
            );
        }

        const uiAssets = await materializeViewerUiAssets(
            analysis.uiAssets,
            path.dirname(analysis.iniPath),
            uiDir,
            options,
            warning.warn,
        );
        logTiming(
            `Materialized viewer UI assets (${Object.values(uiAssets).filter(Boolean).length})`,
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
        logTiming(`Materialized variable metadata (${variables.length})`);
        const animations = await materializeAnimationClips(
            analysis.animations,
            artifactRoot,
            options,
            analysis.defaultState,
            warning.warn,
            prepareStaticGlbBuildContext,
            getDrawBindingsForIb,
        );
        logTiming(`Materialized animation clips (${animations.length})`);
        const manifest: StaticGlbVariantManifest = {
            version: 1,
            name: path.basename(path.dirname(analysis.iniPath)),
            modPath: path.dirname(analysis.iniPath),
            iniPath: analysis.iniPath,
            defaultState: analysis.defaultState,
            variables,
            uiAssets,
            shapeKeys: analysis.shapeKeys,
            animations,
            states,
        };
        const manifestPath = path.join(artifactRoot, "manifest.json");
        await writeVariantManifestAtomic(manifestPath, manifest);
        logTiming("Wrote variant manifest");

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
        const logTiming = createTimedStageLogger(
            options.logger,
            "mod-static-glb.resolveVariantStateArtifact",
        );
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
        logTiming("Loaded variant manifest");

        const key = createStateKey(options.state);
        const existing = manifest.states.find((entry) => entry.key === key);
        if (existing && (await fse.pathExists(existing.glbPath))) {
            logTiming(`Reused existing state artifact ${key}`);
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
            const buildStartedAt = Date.now();
            const result = await buildModGlb({
                ...options,
                modPath: manifest.modPath,
                textureCacheDir,
                variableState: options.state,
            });
            logTiming(
                `Built requested state GLB ${key} (meshCount=${result.meshCount}, warnings=${result.warningCount})`,
                buildStartedAt,
            );
            const glbDir = path.join(artifactRoot, "glb");
            await fse.ensureDir(glbDir);
            const glbPath = path.join(glbDir, createStateArtifactFileName(key));
            await fse.writeFile(glbPath, result.glb);
            logTiming(`Wrote requested state GLB ${key}`);

            const current = manifest.states.find((entry) => entry.key === key);
            if (!current) {
                const entry = {
                    key,
                    values: options.state,
                    glbPath,
                };
                manifest.states.push(entry);
                await writeVariantManifestAtomic(manifestPath, manifest);
                logTiming(`Appended state manifest entry ${key}`);
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
                logTiming(`Updated state manifest entry ${key}`);
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

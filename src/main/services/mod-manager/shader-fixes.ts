import { createHash } from "node:crypto";
import path from "node:path";

import { getMatchingImporter } from "@shared/xxmi-match";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";

import type { NahidaDesktop } from "../..";
import type { ModLibraryService } from "./library";

import { isSameOrChildPath, normalizeModPath } from "./path-utils";

interface ShaderFixesModManifestFile {
    file: string;
    targetPath: string;
    targetKey: string;
    hash: string;
}

interface ShaderFixesModManifest {
    version: number;
    modKey: string;
    files: ShaderFixesModManifestFile[];
}

interface ShaderFixesFileCandidate {
    file: string;
    sourcePath: string;
}

export interface ShaderFixesProcessedFile extends ShaderFixesModManifestFile {
    modKey: string;
    createdTarget: boolean;
}

const SHADER_FIXES_DIR_NAME = "ShaderFixes";
export const SHADER_FIXES_MOD_MARKER_FILE = ".nahida-shader-fixes.json";
const SHADER_FIXES_MOD_MARKER_VERSION = 1;

export class ModShaderFixesService {
    private shaderOperationQueue: Promise<void> = Promise.resolve();

    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly library: ModLibraryService,
    ) {}

    public async handleShaders(
        modPath: string,
        enable: boolean,
    ): Promise<ShaderFixesProcessedFile[]> {
        return await this.withShaderOperationLock(async () => {
            return await this.handleShadersLocked(modPath, enable);
        });
    }

    public async rollbackEnabledShaders(
        modPath: string,
        processedShaders: ShaderFixesProcessedFile[],
    ): Promise<void> {
        await this.withShaderOperationLock(async () => {
            let rollbackError: unknown = null;

            for (const file of [...processedShaders].reverse()) {
                try {
                    if (file.createdTarget && (await fse.pathExists(file.targetPath))) {
                        const currentHash = await this.hashFile(file.targetPath);
                        if (currentHash === file.hash) {
                            await fse.remove(file.targetPath);
                        }
                    }
                } catch (error) {
                    rollbackError = error;
                }
            }

            const modKey =
                processedShaders[0]?.modKey ?? (await this.getShaderFixesModKey(modPath));
            if (modKey) {
                try {
                    await this.deleteModManifest(modPath);
                } catch (error) {
                    rollbackError = error;
                }
            }

            if (rollbackError) throw rollbackError;
        });
    }

    public async deleteModManifest(modPath: string): Promise<void> {
        await fse.remove(this.getShaderFixesModManifestPath(modPath));
    }

    private async getGlobalShaderFixesPath(modPath: string): Promise<string | null> {
        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const modImporter = this.getModImporter(modPath, importers);
        if (modImporter) {
            return path.join(modImporter.importerFolder, SHADER_FIXES_DIR_NAME);
        }

        const games = await this.library.games();
        const matchedGame = games.find((g) => isSameOrChildPath(g.modFolderPath, modPath));
        if (!matchedGame) return null;

        const importerKey =
            matchedGame.importer ??
            getMatchingImporter(
                matchedGame.game,
                importers.map((i) => i.key),
            );
        const importer = importers.find((i) => i.key.toUpperCase() === importerKey?.toUpperCase());

        if (!importer) return null;

        return path.join(importer.importerFolder, SHADER_FIXES_DIR_NAME);
    }

    private getModImporter<T extends { key: string; importerFolder: string }>(
        modPath: string,
        importers: T[],
    ): T | null {
        const importersByKey = new Map(importers.map((i) => [i.key.toUpperCase(), i]));

        let currentPath = path.resolve(modPath);
        let parentPath = path.dirname(currentPath);

        while (parentPath !== currentPath) {
            const importer = importersByKey.get(path.basename(parentPath).toUpperCase());
            if (importer) return importer;

            currentPath = parentPath;
            parentPath = path.dirname(currentPath);
        }

        return null;
    }

    private async withShaderOperationLock<T>(operation: () => Promise<T>): Promise<T> {
        const previousOperation = this.shaderOperationQueue;
        let releaseOperation!: () => void;

        this.shaderOperationQueue = new Promise<void>((resolve) => {
            releaseOperation = resolve;
        });

        await previousOperation.catch(() => undefined);

        try {
            return await operation();
        } finally {
            releaseOperation();
        }
    }

    private hashString(value: string): string {
        return createHash("sha256").update(value).digest("hex");
    }

    private async hashFile(filePath: string): Promise<string> {
        return await this.desktop.lib.utils.getFileHash(filePath);
    }

    private getShaderFixesModManifestPath(modPath: string): string {
        return path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE);
    }

    private validateShaderFixesModManifest(
        manifest: Partial<ShaderFixesModManifest> | null,
    ): ShaderFixesModManifest | null {
        if (
            !manifest ||
            manifest.version !== SHADER_FIXES_MOD_MARKER_VERSION ||
            typeof manifest.modKey !== "string" ||
            manifest.modKey.length === 0
        ) {
            return null;
        }

        const files = Array.isArray(manifest.files)
            ? manifest.files.filter(
                  (file): file is ShaderFixesModManifestFile =>
                      typeof file.file === "string" &&
                      typeof file.targetPath === "string" &&
                      typeof file.targetKey === "string" &&
                      typeof file.hash === "string",
              )
            : [];

        return {
            version: SHADER_FIXES_MOD_MARKER_VERSION,
            modKey: manifest.modKey,
            files,
        };
    }

    private async readShaderFixesModManifestFile(
        manifestPath: string,
    ): Promise<ShaderFixesModManifest | null> {
        try {
            return this.validateShaderFixesModManifest(await fse.readJson(manifestPath));
        } catch (error) {
            if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
                this.desktop.logger.error(
                    error,
                    `Mod:readShaderFixesModManifestFile:${manifestPath}`,
                );
            }
        }

        return null;
    }

    private async readShaderFixesModManifest(
        modPath: string,
    ): Promise<ShaderFixesModManifest | null> {
        return await this.readShaderFixesModManifestFile(
            this.getShaderFixesModManifestPath(modPath),
        );
    }

    private async writeShaderFixesModManifest(
        modPath: string,
        manifest: ShaderFixesModManifest,
    ): Promise<void> {
        await fse.writeJson(this.getShaderFixesModManifestPath(modPath), manifest, { spaces: 2 });
    }

    private async getShaderFixesManifestSearchRoots(modPath: string): Promise<string[]> {
        const roots = new Map<string, string>();
        const addRoot = (root: string) => {
            const resolvedRoot = path.resolve(root);
            roots.set(normalizeModPath(resolvedRoot), resolvedRoot);
        };
        addRoot(path.dirname(modPath));

        try {
            for (const game of await this.library.games()) {
                addRoot(game.modFolderPath);
            }
        } catch (error) {
            this.desktop.logger.error(error, "Mod:getShaderFixesManifestSearchRoots:games");
        }

        for (const importer of this.desktop.service.xxmi.getEnabledImporters()) {
            addRoot(importer.importerFolder);
        }

        const existingRoots = (
            await Promise.all(
                Array.from(roots.values()).map(async (root) =>
                    (await fse.pathExists(root)) ? root : null,
                ),
            )
        )
            .filter((root): root is string => root !== null)
            .sort((a, b) => a.length - b.length);

        return existingRoots.filter(
            (root, index) =>
                !existingRoots
                    .slice(0, index)
                    .some((parentRoot) => isSameOrChildPath(parentRoot, root)),
        );
    }

    private async getOtherOwnedShaderFixesTargetKeys(
        modPath: string,
        modKey: string,
    ): Promise<Set<string>> {
        const manifestPaths = new Map<string, string>();
        await Promise.all(
            (await this.getShaderFixesManifestSearchRoots(modPath)).map(async (root) => {
                for (const manifestFile of await fg(`**/${SHADER_FIXES_MOD_MARKER_FILE}`, {
                    cwd: root,
                    onlyFiles: true,
                    dot: true,
                    ignore: [`**/${SHADER_FIXES_DIR_NAME}/**`],
                })) {
                    const manifestPath = path.join(root, manifestFile);
                    manifestPaths.set(normalizeModPath(manifestPath), manifestPath);
                }
            }),
        );

        return new Set(
            (
                await Promise.all(
                    Array.from(manifestPaths.values()).map(
                        async (manifestPath) =>
                            await this.readShaderFixesModManifestFile(manifestPath),
                    ),
                )
            ).flatMap((manifest) =>
                manifest && manifest.modKey !== modKey
                    ? manifest.files.map((file) => file.targetKey)
                    : [],
            ),
        );
    }

    private async getShaderFixesModKey(modPath: string, create: true): Promise<string>;
    private async getShaderFixesModKey(modPath: string, create?: false): Promise<string | null>;
    private async getShaderFixesModKey(modPath: string, create = false): Promise<string | null> {
        const manifest = await this.readShaderFixesModManifest(modPath);
        if (manifest) return manifest.modKey;

        if (!create) return null;

        const modKey = nanoid();
        await this.writeShaderFixesModManifest(modPath, {
            version: SHADER_FIXES_MOD_MARKER_VERSION,
            modKey,
            files: [],
        });
        return modKey;
    }

    private getShaderFixesTargetKey(targetPath: string): string {
        return this.hashString(normalizeModPath(path.resolve(targetPath)));
    }

    private normalizeShaderFixesRelativePath(targetPath: string): string {
        return targetPath
            .split(/[\\/]+/)
            .filter(Boolean)
            .join("/");
    }

    private async getShaderFixesFileCandidates(
        modPath: string,
    ): Promise<ShaderFixesFileCandidate[]> {
        const shaderDirectories = await fg(`**/${SHADER_FIXES_DIR_NAME}`, {
            cwd: modPath,
            onlyDirectories: true,
            dot: true,
            caseSensitiveMatch: false,
        });

        if (shaderDirectories.length === 0) return [];

        const uniqueShaderDirectories = Array.from(new Set(shaderDirectories)).sort((a, b) => {
            const aIsRootShaderDirectory =
                this.normalizeShaderFixesRelativePath(a).toUpperCase() ===
                SHADER_FIXES_DIR_NAME.toUpperCase();
            const bIsRootShaderDirectory =
                this.normalizeShaderFixesRelativePath(b).toUpperCase() ===
                SHADER_FIXES_DIR_NAME.toUpperCase();

            if (aIsRootShaderDirectory && !bIsRootShaderDirectory) return -1;
            if (bIsRootShaderDirectory && !aIsRootShaderDirectory) return 1;
            return a.localeCompare(b);
        });
        const candidates: ShaderFixesFileCandidate[] = [];

        for (const shaderDirectory of uniqueShaderDirectories) {
            const shaderPath = path.join(modPath, shaderDirectory);
            const files = await fg("**/*", {
                cwd: shaderPath,
                onlyFiles: true,
                dot: true,
            });

            for (const file of files.sort((a, b) => a.localeCompare(b))) {
                candidates.push({
                    file: this.normalizeShaderFixesRelativePath(file),
                    sourcePath: path.join(shaderPath, file),
                });
            }
        }

        return candidates;
    }

    private async handleShadersLocked(
        modPath: string,
        enable: boolean,
    ): Promise<ShaderFixesProcessedFile[]> {
        if (!enable) return await this.disableShaders(modPath);

        const shaderFiles = await this.getShaderFixesFileCandidates(modPath);
        if (shaderFiles.length === 0) return [];

        const globalShaderPath = await this.getGlobalShaderFixesPath(modPath);
        if (!globalShaderPath) return [];

        const processedFiles: ShaderFixesProcessedFile[] = [];

        try {
            const modKey = await this.getShaderFixesModKey(modPath, true);
            const manifest: ShaderFixesModManifest = {
                version: SHADER_FIXES_MOD_MARKER_VERSION,
                modKey,
                files: [],
            };

            await fse.ensureDir(globalShaderPath);
            for (const { file, sourcePath: source } of shaderFiles) {
                const target = path.join(globalShaderPath, file);
                const hash = await this.hashFile(source);
                const targetKey = this.getShaderFixesTargetKey(target);
                const targetExists = await fse.pathExists(target);

                if (targetExists) {
                    const currentHash = await this.hashFile(target);
                    if (currentHash === hash) {
                        const manifestFile = { file, targetPath: target, targetKey, hash };
                        manifest.files.push(manifestFile);
                        processedFiles.push({ ...manifestFile, modKey, createdTarget: false });
                        await this.writeShaderFixesModManifest(modPath, manifest);
                    }
                    continue;
                }

                await fse.copy(source, target);
                const manifestFile = { file, targetPath: target, targetKey, hash };
                manifest.files.push(manifestFile);
                processedFiles.push({ ...manifestFile, modKey, createdTarget: true });
                await this.writeShaderFixesModManifest(modPath, manifest);
            }

            if (manifest.files.length > 0) {
                await this.writeShaderFixesModManifest(modPath, manifest);
            } else {
                await this.deleteModManifest(modPath);
            }
        } catch (err) {
            const shaderError = err instanceof Error ? err : new Error(String(err));
            throw Object.assign(shaderError, { processedFiles });
        }

        return processedFiles;
    }

    private async disableShaders(modPath: string): Promise<ShaderFixesProcessedFile[]> {
        const manifest = await this.readShaderFixesModManifest(modPath);
        if (!manifest) return [];

        const processedFiles: ShaderFixesProcessedFile[] = [];

        try {
            const otherOwnedTargetKeys = await this.getOtherOwnedShaderFixesTargetKeys(
                modPath,
                manifest.modKey,
            );

            for (const file of manifest.files) {
                if (otherOwnedTargetKeys.has(file.targetKey)) continue;

                if (await fse.pathExists(file.targetPath)) {
                    const currentHash = await this.hashFile(file.targetPath);
                    if (currentHash === file.hash) {
                        processedFiles.push({
                            ...file,
                            modKey: manifest.modKey,
                            createdTarget: true,
                        });
                        await fse.remove(file.targetPath);
                    }
                }
            }

            await this.deleteModManifest(modPath);
        } catch (err) {
            const shaderError = err instanceof Error ? err : new Error(String(err));
            throw Object.assign(shaderError, { processedFiles });
        }

        return processedFiles;
    }
}

import path from "node:path";
import type { ApplyPresetResult, Preset } from "@shared/types";
import { trim } from "es-toolkit";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "../..";
import type { ModPresetItemRow, ModPresetRow } from "../../internal/db/schema";
import type { ModLibraryService } from "./library";
import type { ModActionsService } from "./mod-actions";
import {
    DISABLED_PREFIX_REGEX,
    normalizeModPath,
    renameWithUniqueName,
    restoreDisabledPrefix,
    stripDisabledPrefix,
    toGameRelativePath,
} from "./path-utils";

interface PresetSnapshotItemRecord {
    modKey: string;
    relativePath: string;
    groupRelativePath: string;
    folderName: string;
    isEnabled: boolean;
}

interface ScannedPresetItem extends PresetSnapshotItemRecord {
    actualPath: string;
}

interface PresetConflictCandidate {
    actualPath: string;
    relativePath: string;
    folderName: string;
    isEnabled: boolean;
}

export interface PresetConflict {
    modKey: string;
    candidates: PresetConflictCandidate[];
}

const MOD_PRESET_ITEM_INSERT_BATCH_SIZE = 100;
const MOD_PRESET_VERSION = 2;

export class ModPresetsService {
    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly library: ModLibraryService,
        private readonly actions: ModActionsService,
    ) {}

    public async presets(game: string): Promise<Preset[]> {
        const results = await this.desktop.lib.db.modPresets.listByGame(game);

        return results
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((r) => ({
                id: r.id,
                game: r.game,
                name: r.name,
                description: r.description ?? null,
                createdAt: r.createdAt,
                updatedAt: r.updatedAt,
                version: r.version,
                isLegacy: r.version < MOD_PRESET_VERSION,
            }));
    }

    public async presetCreateConflicts(game: string): Promise<PresetConflict[]> {
        return await this.getPresetSnapshotConflicts(game);
    }

    public async createPreset(
        game: string,
        name: string,
        description?: string,
        resolveConflicts = false,
    ): Promise<Preset> {
        const trimmedName = trim(name);
        const trimmedDescription = trim(description ?? "");
        if (!trimmedName) {
            throw new Error("INVALID_PRESET_NAME");
        }

        const existingPreset = await this.desktop.lib.db.modPresets.findByGameAndName(
            game,
            trimmedName,
        );
        if (existingPreset) {
            throw new Error("PRESET_NAME_EXISTS");
        }

        if (resolveConflicts) {
            const remainingConflicts = await this.resolvePresetSnapshotConflicts(game);
            if (remainingConflicts.length > 0) {
                throw new Error("PRESET_CONFLICT_RESOLUTION_FAILED");
            }
        } else {
            const conflicts = await this.getPresetSnapshotConflicts(game);
            if (conflicts.length > 0) {
                throw new Error("PRESET_CONFLICTS_EXIST");
            }
        }

        const snapshot = await this.getPresetSnapshot(game);

        const id = nanoid();
        const now = new Date().toISOString();

        this.desktop.lib.db.transaction(() => {
            this.desktop.lib.db.modPresets.insert({
                id,
                game,
                name: trimmedName,
                description: trimmedDescription || null,
                itemCount: 0,
                createdAt: now,
                updatedAt: now,
                version: MOD_PRESET_VERSION,
            } satisfies ModPresetRow);

            if (snapshot.length > 0) {
                const presetItems = snapshot.map((item, index) => ({
                    presetId: id,
                    ...this.toPresetRecord(item),
                    itemOrder: index,
                }));

                for (
                    let startIndex = 0;
                    startIndex < presetItems.length;
                    startIndex += MOD_PRESET_ITEM_INSERT_BATCH_SIZE
                ) {
                    this.desktop.lib.db.modPresetItems.insertMany(
                        presetItems.slice(
                            startIndex,
                            startIndex + MOD_PRESET_ITEM_INSERT_BATCH_SIZE,
                        ) satisfies ModPresetItemRow[],
                    );
                }
            }
        });

        return {
            id,
            game,
            name: trimmedName,
            description: trimmedDescription || null,
            createdAt: now,
            updatedAt: now,
            version: MOD_PRESET_VERSION,
            isLegacy: false,
        };
    }

    public async applyPreset(presetId: string): Promise<ApplyPresetResult> {
        const preset = await this.desktop.lib.db.modPresets.findById(presetId);

        if (!preset) {
            throw new Error(`Preset ${presetId} not found`);
        }

        if (preset.version < MOD_PRESET_VERSION) {
            throw new Error("LEGACY_PRESET_NOT_SUPPORTED");
        }

        const presetItems = await this.desktop.lib.db.modPresetItems.listByPresetId(presetId);
        const currentItems = await this.getPresetSnapshot(preset.game);
        const currentByKey = new Map(currentItems.map((item) => [item.modKey, item] as const));
        const currentByRelativePath = new Map(
            currentItems.map((item) => [item.relativePath.toLowerCase(), item] as const),
        );
        const result: ApplyPresetResult = {
            presetId,
            applied: [],
            skipped: [],
            missing: [],
        };

        for (const presetItem of presetItems.sort((a, b) => a.itemOrder - b.itemOrder)) {
            const currentItem =
                currentByKey.get(presetItem.modKey) ??
                currentByRelativePath.get(presetItem.relativePath.toLowerCase());

            if (!currentItem) {
                result.missing.push({
                    modKey: presetItem.modKey,
                    expectedFolderName: presetItem.folderName,
                    expectedRelativePath: presetItem.relativePath,
                });
                continue;
            }

            if (currentItem.isEnabled === presetItem.isEnabled) {
                result.skipped.push(currentItem.relativePath);
                continue;
            }

            try {
                if (presetItem.isEnabled) {
                    await this.actions.enable(currentItem.actualPath);
                } else {
                    await this.actions.disable(currentItem.actualPath);
                }
                result.applied.push(currentItem.relativePath);
            } catch (error) {
                this.desktop.logger.error(
                    error,
                    `Mod:applyPreset:${presetItem.isEnabled ? "enable" : "disable"}:${currentItem.actualPath}`,
                );
            }
        }

        return result;
    }

    public async deletePreset(presetId: string): Promise<void> {
        await this.desktop.lib.db.modPresets.delete(presetId);
    }

    public async updatePresetName(presetId: string, newName: string): Promise<void> {
        const preset = await this.desktop.lib.db.modPresets.findById(presetId);

        if (!preset) {
            throw new Error(`Preset ${presetId} not found`);
        }

        const trimmedName = trim(newName);
        if (!trimmedName) {
            throw new Error("INVALID_PRESET_NAME");
        }

        const existingPreset = await this.desktop.lib.db.modPresets.findByGameAndName(
            preset.game,
            trimmedName,
        );
        if (existingPreset && existingPreset.id !== presetId) {
            throw new Error("PRESET_NAME_EXISTS");
        }

        await this.desktop.lib.db.modPresets.updateName(
            presetId,
            trimmedName,
            new Date().toISOString(),
        );
    }

    private buildModKey(gamePath: string, groupPath: string, modPath: string): string {
        const groupRelativePath = toGameRelativePath(gamePath, groupPath);
        const modRelativePath = toGameRelativePath(gamePath, modPath);
        return `${groupRelativePath}::${modRelativePath}`;
    }

    private buildPresetSnapshotItem(
        gamePath: string,
        groupPath: string,
        modPath: string,
    ): ScannedPresetItem {
        const folderName = path.basename(modPath);
        return {
            modKey: this.buildModKey(gamePath, groupPath, modPath),
            relativePath: toGameRelativePath(gamePath, modPath),
            groupRelativePath: toGameRelativePath(gamePath, groupPath),
            folderName: stripDisabledPrefix(folderName),
            isEnabled: !DISABLED_PREFIX_REGEX.test(folderName),
            actualPath: modPath,
        };
    }

    private getPresetGroupPath(gamePath: string, modPath: string): string {
        const relativePath = path.relative(gamePath, modPath);
        const segments = relativePath.split(/[\\/]+/).filter(Boolean);

        if (segments.length <= 1) {
            return gamePath;
        }

        return path.join(gamePath, ...segments.slice(0, -1));
    }

    private async collectPresetSnapshotItems(gamePath: string): Promise<ScannedPresetItem[]> {
        const iniPaths = await this.desktop.lib.fs.findFiles(gamePath, {
            extensions: [".ini"],
        });
        const modPathMap = new Map<string, string>();

        for (const iniPath of iniPaths) {
            const modPath = path.dirname(iniPath);
            const normalizedModPath = normalizeModPath(modPath);
            if (!modPathMap.has(normalizedModPath)) {
                modPathMap.set(normalizedModPath, modPath);
            }
        }

        return Array.from(modPathMap.values())
            .sort((a, b) => a.localeCompare(b))
            .map((modPath) => {
                const groupPath = this.getPresetGroupPath(gamePath, modPath);
                return this.buildPresetSnapshotItem(gamePath, groupPath, modPath);
            });
    }

    private async getPresetSnapshot(game: string): Promise<ScannedPresetItem[]> {
        const gamePath = await this.library.gamePath(game);
        if (!gamePath) {
            throw new Error(`No mod folder path set for ${game}`);
        }

        const items = await this.collectPresetSnapshotItems(gamePath);
        return items
            .sort((a, b) => a.relativePath.localeCompare(b.relativePath))
            .map((item) => ({
                ...item,
                relativePath: item.relativePath,
            }));
    }

    private async getPresetSnapshotConflicts(game: string): Promise<PresetConflict[]> {
        const items = await this.getPresetSnapshot(game);
        const itemsByModKey = new Map<string, ScannedPresetItem[]>();

        for (const item of items) {
            const conflicts = itemsByModKey.get(item.modKey) ?? [];
            conflicts.push(item);
            itemsByModKey.set(item.modKey, conflicts);
        }

        return Array.from(itemsByModKey.entries())
            .filter(([, conflicts]) => conflicts.length > 1)
            .map(([modKey, conflicts]) => ({
                modKey,
                candidates: conflicts
                    .sort((a, b) => a.relativePath.localeCompare(b.relativePath))
                    .map((item) => ({
                        actualPath: item.actualPath,
                        relativePath: item.relativePath,
                        folderName: item.folderName,
                        isEnabled: item.isEnabled,
                    })),
            }))
            .sort((a, b) => a.modKey.localeCompare(b.modKey));
    }

    private async resolvePresetSnapshotConflicts(game: string): Promise<PresetConflict[]> {
        const conflicts = await this.getPresetSnapshotConflicts(game);

        for (const conflict of conflicts) {
            const candidates = conflict.candidates
                .map((candidate) =>
                    candidate.actualPath
                        ? {
                              ...candidate,
                              actualPath: candidate.actualPath,
                          }
                        : null,
                )
                .filter((candidate): candidate is PresetConflictCandidate => candidate !== null);
            const enabledCandidates = candidates.filter((candidate) => candidate.isEnabled);
            const disabledCandidates = candidates.filter((candidate) => !candidate.isEnabled);

            const renameTargets =
                enabledCandidates.length > 0
                    ? disabledCandidates
                    : disabledCandidates.length > 1
                      ? disabledCandidates.slice(1)
                      : [];

            for (const candidate of renameTargets) {
                await renameWithUniqueName(
                    this.desktop.lib.fs,
                    candidate.actualPath,
                    restoreDisabledPrefix(
                        path.basename(candidate.actualPath),
                        candidate.folderName,
                    ),
                );
            }
        }

        return await this.getPresetSnapshotConflicts(game);
    }

    private toPresetRecord(item: ScannedPresetItem): PresetSnapshotItemRecord {
        return {
            modKey: item.modKey,
            relativePath: item.relativePath,
            groupRelativePath: item.groupRelativePath,
            folderName: item.folderName,
            isEnabled: item.isEnabled,
        };
    }
}

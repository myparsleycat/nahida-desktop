export const AUTO_MOD_ACTIONS_SETTING_KEY = "xxmi_auto_mod_actions_config";
export const AUTO_MOD_ACTIONS_TRIGGER_MODE = "startup+watch";
export const AUTO_MOD_ACTIONS_BACKUP_PREFIX = "DISABLED auto-mod-actions_backup_";

export interface AutoFixerConfig {
    enabled: boolean;
    presetId: string | null;
}

export interface GIMIAutoModActionsConfig {
    enabled: boolean;
}

export interface AutoModActionsImporterConfig {
    autoFixer: AutoFixerConfig;
    orFix?: GIMIAutoModActionsConfig;
    faceHeadFix?: GIMIAutoModActionsConfig;
}

export interface AutoModActionsConfig {
    triggerMode: typeof AUTO_MOD_ACTIONS_TRIGGER_MODE;
    importers: Record<string, AutoModActionsImporterConfig>;
}

export interface AutoModActionsRestoreResult {
    importerKey: string;
    scannedBackups: number;
    restoredFiles: number;
    removedBackups: number;
}

export function createDefaultAutoModActionsImporterConfig(
    importerKey: string,
): AutoModActionsImporterConfig {
    const normalizedKey = importerKey.trim().toUpperCase();
    const baseConfig: AutoModActionsImporterConfig = {
        autoFixer: {
            enabled: false,
            presetId: null,
        },
    };

    if (normalizedKey === "GIMI") {
        baseConfig.orFix = { enabled: false };
        baseConfig.faceHeadFix = { enabled: false };
    }

    return baseConfig;
}

export function normalizeAutoModActionsConfig(
    input: unknown,
    importerKeys: string[],
): AutoModActionsConfig {
    const source =
        input && typeof input === "object" ? (input as Partial<AutoModActionsConfig>) : {};
    const sourceImporters =
        source.importers && typeof source.importers === "object" ? source.importers : {};

    const normalizedImporters: Record<string, AutoModActionsImporterConfig> = {};

    for (const importerKey of importerKeys) {
        const fallbackConfig = createDefaultAutoModActionsImporterConfig(importerKey);
        const rawImporter =
            sourceImporters &&
            typeof sourceImporters === "object" &&
            importerKey in sourceImporters
                ? (sourceImporters[importerKey] as Partial<AutoModActionsImporterConfig>)
                : null;

        normalizedImporters[importerKey] = {
            autoFixer: {
                enabled: rawImporter?.autoFixer?.enabled === true,
                presetId:
                    typeof rawImporter?.autoFixer?.presetId === "string" &&
                    rawImporter.autoFixer.presetId.trim()
                        ? rawImporter.autoFixer.presetId
                        : null,
            },
            ...(fallbackConfig.orFix
                ? {
                      orFix: {
                          enabled: rawImporter?.orFix?.enabled === true,
                      },
                  }
                : {}),
            ...(fallbackConfig.faceHeadFix
                ? {
                      faceHeadFix: {
                          enabled: rawImporter?.faceHeadFix?.enabled === true,
                      },
                  }
                : {}),
        };
    }

    return {
        triggerMode: AUTO_MOD_ACTIONS_TRIGGER_MODE,
        importers: normalizedImporters,
    };
}

export function isAutoModActionsImporterEnabled(
    importerKey: string,
    importerConfig: AutoModActionsImporterConfig | undefined,
): boolean {
    if (!importerConfig) {
        return false;
    }

    if (importerConfig.autoFixer.enabled && !!importerConfig.autoFixer.presetId) {
        return true;
    }

    if (importerKey.trim().toUpperCase() === "GIMI") {
        return importerConfig.orFix?.enabled === true || importerConfig.faceHeadFix?.enabled === true;
    }

    return false;
}

export function hasAnyAutoModActionsEnabled(
    config: AutoModActionsConfig,
    importerKeys: string[],
): boolean {
    return importerKeys.some((importerKey) =>
        isAutoModActionsImporterEnabled(importerKey, config.importers[importerKey]),
    );
}

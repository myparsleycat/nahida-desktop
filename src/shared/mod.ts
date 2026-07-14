export const ARCHIVE_EXTRACT_PATH_MODES = [
    "flatten_single_root",
    "keep_archive_root",
    "ask_every_time",
] as const;

export type ArchiveExtractPathMode = (typeof ARCHIVE_EXTRACT_PATH_MODES)[number];

export type ResolvedArchiveExtractPathMode = Exclude<ArchiveExtractPathMode, "ask_every_time">;

export const MOD_GRID_LAYOUT_MODES = [
    "responsive",
    "fixed_card_width",
    "fixed_column_count",
] as const;

export type ModGridLayoutMode = (typeof MOD_GRID_LAYOUT_MODES)[number];

export const SIDEBAR_LAYOUT_MODES = ["row", "grid"] as const;

export type SidebarLayoutMode = (typeof SIDEBAR_LAYOUT_MODES)[number];

export const DISABLED_PREFIX_STYLES = ["space", "underscore"] as const;

export type DisabledPrefixStyle = (typeof DISABLED_PREFIX_STYLES)[number];

export const DISABLED_PREFIX_REGEX = /^(?:disabled[\s_]*)+[\s_]+/i;

export function disabledPrefixString(style: DisabledPrefixStyle): string {
    return style === "underscore" ? "DISABLED_" : "DISABLED ";
}

export function stripDisabledPrefix(name: string): string {
    return name.replace(DISABLED_PREFIX_REGEX, "").trim();
}

export const DOWNLOAD_SOURCES = ["gamebanana", "nahidaLive", "hui", "drive"] as const;

export type DownloadSource = (typeof DOWNLOAD_SOURCES)[number];

export const NTE_IMPORTER_KEY = "NTE";
export const NTE_GAMEBANANA_ID = 23012;

export const GAMEBANANA_ID_TO_IMPORTER = {
    8552: "GIMI",
    18366: "SRMI",
    10349: "HIMI",
    19567: "ZZMI",
    20357: "WWMI",
    21842: "EFMI",
    [NTE_GAMEBANANA_ID]: NTE_IMPORTER_KEY,
} as const;

export const IMPORTER_TO_GAMEBANANA_GAME_KEY = {
    GIMI: "gi",
    SRMI: "sr",
    HIMI: "hi",
    ZZMI: "zz",
    WWMI: "ww",
    EFMI: "ef",
    [NTE_IMPORTER_KEY]: "nte",
} as const;

export const isNteImporter = (importer: string | null | undefined) => importer === NTE_IMPORTER_KEY;

export function getImporterForGameBananaId(gameId: number): string | null {
    return GAMEBANANA_ID_TO_IMPORTER[gameId as keyof typeof GAMEBANANA_ID_TO_IMPORTER] ?? null;
}

export function getGameBananaKeyForImporter(importer: string | null | undefined) {
    if (!importer) return null;
    if (isNteImporter(importer)) return IMPORTER_TO_GAMEBANANA_GAME_KEY[NTE_IMPORTER_KEY];
    return (
        IMPORTER_TO_GAMEBANANA_GAME_KEY[importer as keyof typeof IMPORTER_TO_GAMEBANANA_GAME_KEY] ??
        null
    );
}

export function findGameByImporter<T extends { game: string; importer: string | null }>(
    games: T[],
    importerKey: string,
): T | null {
    return (
        games.find((game) =>
            importerKey === NTE_IMPORTER_KEY
                ? isNteImporter(game.importer)
                : game.importer === importerKey,
        ) ?? null
    );
}

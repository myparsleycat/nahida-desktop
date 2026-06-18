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

export const isNteImporter = (importer: string | null | undefined) => importer === NTE_IMPORTER_KEY;

export function getImporterForGameBananaId(gameId: number): string | null {
    return GAMEBANANA_ID_TO_IMPORTER[gameId as keyof typeof GAMEBANANA_ID_TO_IMPORTER] ?? null;
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

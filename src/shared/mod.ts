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

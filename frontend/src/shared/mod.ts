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

export function hasDisabledPrefix(name: string): boolean {
    return DISABLED_PREFIX_REGEX.test(name.trim());
}

// Mirrors Siblings.Display in internal/modname: the prefix stays when another folder of the same
// directory has the same name without it. siblingNames includes name itself.
export function siblingDisplayName(name: string, siblingNames: readonly string[]): string {
    const key = stripDisabledPrefix(name).toLowerCase();
    const twins = siblingNames.filter(
        (sibling) => stripDisabledPrefix(sibling).toLowerCase() === key,
    ).length;
    return twins > 1 ? name.trim() : stripDisabledPrefix(name);
}

const MERGE_NAME_FORBIDDEN_RE = /[\0[\]=\r\n;"'$<>:/\\|?*]/;

export function isSafeMergeName(name: string) {
    const trimmed = name.trim();
    return (
        Boolean(trimmed) &&
        trimmed !== "." &&
        trimmed !== ".." &&
        !MERGE_NAME_FORBIDDEN_RE.test(name)
    );
}

export const DOWNLOAD_SOURCES = ["gamebanana", "nahidaLive", "hui", "drive"] as const;

export type DownloadSource = (typeof DOWNLOAD_SOURCES)[number];

export const NTE_IMPORTER_KEY = "NTE";

export const isNteImporter = (importer: string | null | undefined) => importer === NTE_IMPORTER_KEY;

export const EFMI_IMPORTER_KEY = "EFMI";

export const isEfmiImporter = (importer: string | null | undefined) =>
    importer === EFMI_IMPORTER_KEY;

export function getParentGroupPath(groupPath: string) {
    const separatorIndex = Math.max(groupPath.lastIndexOf("\\"), groupPath.lastIndexOf("/"));
    return separatorIndex < 0 ? null : groupPath.slice(0, separatorIndex);
}

export function isSameOrChildPath(path: string, parentPath: string) {
    const rest = path.slice(parentPath.length);
    return path.startsWith(parentPath) && (rest === "" || rest[0] === "\\" || rest[0] === "/");
}

export function replacePathPrefix(path: string, oldPath: string, newPath: string) {
    return isSameOrChildPath(path, oldPath) ? newPath + path.slice(oldPath.length) : path;
}

export function getGameBananaKeyForImporter(
    importer: string | null | undefined,
    registry: readonly { key: string; importer: string }[],
) {
    if (!importer) return null;
    return registry.find((game) => game.importer === importer)?.key ?? null;
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

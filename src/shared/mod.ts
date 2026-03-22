export const ARCHIVE_EXTRACT_PATH_MODES = [
  "flatten_single_root",
  "keep_archive_root",
  "ask_every_time",
] as const;

export type ArchiveExtractPathMode = (typeof ARCHIVE_EXTRACT_PATH_MODES)[number];

export type ResolvedArchiveExtractPathMode = Exclude<ArchiveExtractPathMode, "ask_every_time">;

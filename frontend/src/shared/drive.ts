export const DRIVE_NAME_SORT_POLICIES = ["natural_ignore_spacing", "natural"] as const;

export type DriveNameSortPolicy = (typeof DRIVE_NAME_SORT_POLICIES)[number];

export const DEFAULT_DRIVE_NAME_SORT_POLICY: DriveNameSortPolicy = "natural_ignore_spacing";

export function normalizeDriveNameSortPolicy(
    value: string | null | undefined,
): DriveNameSortPolicy {
    return DRIVE_NAME_SORT_POLICIES.includes(value as DriveNameSortPolicy)
        ? (value as DriveNameSortPolicy)
        : DEFAULT_DRIVE_NAME_SORT_POLICY;
}

import type { SortType } from "@renderer/types";
import type { DriveNameSortPolicy } from "@shared/drive";
import type { Content } from "@shared/types";
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

// Drive descendant search is unavailable when the backend OpenSearch dependency is down (503)
// or the backend itself is unreachable; DriveApiError fields may be lost through IPC
// serialization, so both status/code and message markers are checked.
export function isDriveSearchUnavailable(error: unknown): boolean {
    if (typeof error !== "object" || error === null) return false;

    const record = error as Record<string, unknown>;
    if (record.status === 502 || record.status === 503 || record.status === 504) return true;
    if (record.code === "DRIVE_BACKEND_UNAVAILABLE") return true;

    const message = typeof record.message === "string" ? record.message : "";
    return message.includes("DRIVE_BACKEND_UNAVAILABLE") || message.includes("search_unavailable");
}

export const naturalCompare = (a: string, b: string, mp: number) => {
    const collator = new Intl.Collator(undefined, {
        numeric: true,
        sensitivity: "case",
        caseFirst: "lower",
    });
    return mp * collator.compare(a, b);
};

function compareDriveName(a: string, b: string, mp: number, policy: DriveNameSortPolicy) {
    if (policy === "natural_ignore_spacing") {
        const normalizedCompare = naturalCompare(a.replace(/\s+/g, ""), b.replace(/\s+/g, ""), mp);

        if (normalizedCompare !== 0) {
            return normalizedCompare;
        }
    }

    return naturalCompare(a, b, mp);
}

export function commonSort(
    content: Content[],
    sortType: SortType,
    nameSortPolicy: DriveNameSortPolicy = "natural_ignore_spacing",
) {
    return [...content].sort((a, b) => {
        if (a.isDir && !b.isDir) return -1;
        if (!a.isDir && b.isDir) return 1;

        const [field, order] = sortType.split(":");
        const multiplier = order === "DESC" ? -1 : 1;

        switch (field) {
            case "NAME":
                return compareDriveName(a.name, b.name, multiplier, nameSortPolicy);
            case "SIZE": {
                const sizeA = Number(a.size) || 0;
                const sizeB = Number(b.size) || 0;
                return multiplier * (sizeA - sizeB);
            }
            case "DATE": {
                const dateA = new Date(a.updatedAt).getTime();
                const dateB = new Date(b.updatedAt).getTime();
                return multiplier * (dateA - dateB);
            }
            default:
                return 0;
        }
    });
}

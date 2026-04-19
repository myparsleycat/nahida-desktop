import type { SortType } from "@renderer/types";
import type { DriveNameSortPolicy } from "@shared/drive";
import type { Content } from "@shared/types.gen";
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
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

import { SortType } from "@renderer/types";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { Content } from "@shared/types";
import { orderBy } from "es-toolkit";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function commonSort(content: Content[], sortType: SortType) {
    const [field, order] = sortType.split(":");
    const sortOrder = order.toLowerCase() as "asc" | "desc";

    return orderBy(
        content,
        [
            (item) => item.isDir,
            (item) => {
                if (field !== "NAME") return 0;
                const name = item.name || "";
                if (/^[0-9]/.test(name)) return 2;
                if (/^[ㄱ-ㅎ가-힣]/.test(name)) return 3;
                if (/^[a-zA-Z]/.test(name)) return 4;
                return 1;
            },
            (item) => {
                switch (field) {
                    case "NAME":
                        return item.name;
                    case "SIZE":
                        return Number(item.size) || 0;
                    case "DATE":
                        return new Date(item.updatedAt).getTime();
                    default:
                        return 0;
                }
            },
        ],
        ["desc", sortOrder, sortOrder],
    );
}

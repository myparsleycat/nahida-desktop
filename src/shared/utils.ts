import { format, formatDuration, intervalToDuration } from "date-fns";
import { enUS, ko, zhCN } from "date-fns/locale";
import { isNil } from "es-toolkit";
import { type FilesizeOptions, filesize } from "filesize";

export function formatSize(size?: number | null, options?: FilesizeOptions) {
    if (isNil(size)) return "0 B";
    if (!Number.isFinite(size)) return "--";
    return filesize(size, { standard: "jedec", ...options });
}

export const formatDate = (
    date: Date | string,
    lang?: string | undefined | null,
    formatStr?: string,
) => {
    return format(date, formatStr || "PPpp", {
        locale: (() => {
            if (lang?.startsWith("ko")) return ko;
            else if (lang?.startsWith("zh")) return zhCN;
            return enUS;
        })(),
    });
};

export function formatTime(seconds: number): string {
    if (!Number.isFinite(seconds) || seconds < 0) {
        return "--";
    }

    const duration = intervalToDuration({
        start: 0,
        end: Math.ceil(seconds) * 1000,
    });

    return (
        formatDuration(duration, {
            format: ["hours", "minutes", "seconds"],
            locale: ko,
        }) || "0초"
    );
}

export function normalizePath(path: string) {
    return path.replace(/\\/g, "/").replace(/^\/|\/$/g, "");
}

export function getRandInt(min: number, max: number) {
    return Math.floor(Math.random() * (max - min + 1)) + min;
}

export function getRandFloat(min: number, max: number): number {
    return Math.random() * (max - min) + min;
}

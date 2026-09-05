import { Logger } from "@renderer/lib/logger";
import DOMPurify from "dompurify";
import { iconNames } from "lucide-react/dynamic";

const SAFE_TAGS = ["svg", "g", "path", "circle", "ellipse", "line", "polyline", "polygon", "rect"];
const SAFE_ATTRIBUTES = [
    "xmlns",
    "viewBox",
    "width",
    "height",
    "fill",
    "fill-opacity",
    "stroke",
    "stroke-width",
    "stroke-linecap",
    "stroke-linejoin",
    "stroke-opacity",
    "d",
    "cx",
    "cy",
    "r",
    "rx",
    "ry",
    "x",
    "y",
    "x1",
    "x2",
    "y1",
    "y2",
    "points",
    "transform",
];

export interface IconSearchResult {
    source: "lucide" | "iconify";
    name: string;
    svg?: string;
}

export interface IconifyCacheStats {
    count: number;
    bytes: number;
    prefixes: string[];
}

export function searchLucideIcons(query: string, limit = 80): IconSearchResult[] {
    const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    return iconNames
        .filter((name) => words.every((word) => name.includes(word)))
        .slice(0, limit)
        .map((name) => ({ source: "lucide", name }));
}

export async function searchIconifyIcons(
    query: string,
    signal?: AbortSignal,
): Promise<IconSearchResult[]> {
    const response = await fetch(
        `https://api.iconify.design/search?query=${encodeURIComponent(query)}&limit=64`,
        { signal },
    );
    if (!response.ok) throw new Error(`Iconify search failed: ${response.status}`);
    const payload: unknown = await response.json();
    if (!isRecord(payload) || !Array.isArray(payload.icons)) return [];
    const names = payload.icons.filter((name): name is string => typeof name === "string");
    const results = await Promise.all(
        names.map(async (name): Promise<IconSearchResult | null> => {
            const iconResponse = await fetch(
                `https://api.iconify.design/${name.replace(":", "/")}.svg`,
                { signal },
            );
            if (!iconResponse.ok) return null;
            const svg = sanitizeIconifySVG(await iconResponse.text());
            return svg ? { source: "iconify", name, svg } : null;
        }),
    );
    const safeResults = results.filter((result): result is IconSearchResult => result !== null);
    await cacheIconifyResults(safeResults);
    return safeResults;
}

export async function searchCachedIconifyIcons(
    query: string,
    limit = 128,
): Promise<IconSearchResult[]> {
    if (typeof indexedDB === "undefined") return [];
    const database = await openIconDatabase();
    const entries = await iconTransaction<IconSearchResult[]>(database, "readonly", (store) =>
        store.getAll(),
    );
    database.close();
    const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    return entries
        .filter((entry) => words.every((word) => entry.name.toLowerCase().includes(word)))
        .slice(0, limit);
}

export async function getIconifyCacheStats(): Promise<IconifyCacheStats> {
    const entries = await searchCachedIconifyIcons("", Number.MAX_SAFE_INTEGER);
    return {
        count: entries.length,
        bytes: entries.reduce(
            (total, entry) => total + entry.name.length + (entry.svg?.length ?? 0),
            0,
        ),
        prefixes: [...new Set(entries.map((entry) => entry.name.split(":")[0]))].toSorted(),
    };
}

export async function deleteIconifyPrefix(prefix: string): Promise<void> {
    const entries = await searchCachedIconifyIcons("", Number.MAX_SAFE_INTEGER);
    const database = await openIconDatabase();
    await Promise.all(
        entries
            .filter((entry) => entry.name.startsWith(`${prefix}:`))
            .map((entry) =>
                iconTransaction(database, "readwrite", (store) => store.delete(entry.name)),
            ),
    );
    database.close();
}

export async function clearIconifyCache(): Promise<void> {
    const database = await openIconDatabase();
    await iconTransaction(database, "readwrite", (store) => store.clear());
    database.close();
}

export async function downloadIconifyCollection(
    prefix: string,
    signal?: AbortSignal,
): Promise<number> {
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/i.test(prefix)) {
        throw new Error("Invalid Iconify collection prefix");
    }
    const response = await fetch(
        `https://cdn.jsdelivr.net/npm/@iconify-json/${prefix.toLowerCase()}/icons.json`,
        { signal },
    );
    if (!response.ok) throw new Error(`Iconify collection download failed: ${response.status}`);
    const payload: unknown = await response.json();
    if (!isRecord(payload) || !isRecord(payload.icons))
        throw new Error("Invalid Iconify collection");
    const width = typeof payload.width === "number" ? payload.width : 24;
    const height = typeof payload.height === "number" ? payload.height : width;
    const icons = Object.entries(payload.icons).flatMap(([name, value]): IconSearchResult[] => {
        if (!isRecord(value) || typeof value.body !== "string") return [];
        const svg = sanitizeIconifySVG(
            `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}">${value.body}</svg>`,
        );
        return svg ? [{ source: "iconify", name: `${prefix.toLowerCase()}:${name}`, svg }] : [];
    });
    await cacheIconifyResults(icons);
    return icons.length;
}

export function getFavoriteIconifyPrefixes(): string[] {
    try {
        const value: unknown = JSON.parse(
            localStorage.getItem("nahida.menu-maker.iconify.favorites") ?? "[]",
        );
        return Array.isArray(value)
            ? value.filter((prefix): prefix is string => typeof prefix === "string")
            : [];
    } catch (error) {
        Logger.capture("shared/menu-maker/icons.ts", error);
        return [];
    }
}

export function toggleFavoriteIconifyPrefix(prefix: string): string[] {
    const current = getFavoriteIconifyPrefixes();
    const next = current.includes(prefix)
        ? current.filter((item) => item !== prefix)
        : [...current, prefix];
    localStorage.setItem("nahida.menu-maker.iconify.favorites", JSON.stringify(next));
    return next;
}

export function sanitizeIconifySVG(svg: string): string | null {
    if (
        !/^\s*<svg\b/i.test(svg) ||
        /<(?:script|foreignObject|iframe|object|embed|image|use|style|link|a)\b/i.test(svg) ||
        /\son[a-z]+\s*=/i.test(svg) ||
        /\s(?:href|src)\s*=\s*["']\s*(?:javascript:|data:|https?:|\/\/)/i.test(svg) ||
        /url\s*\(/i.test(svg)
    )
        return null;
    if (typeof document === "undefined") return svg;
    const clean = DOMPurify.sanitize(svg, {
        USE_PROFILES: { svg: true, svgFilters: false },
        ALLOWED_TAGS: SAFE_TAGS,
        ALLOWED_ATTR: SAFE_ATTRIBUTES,
        ALLOW_DATA_ATTR: false,
    });
    return /<svg\b/i.test(clean) && !/\son[a-z]+\s*=/i.test(clean) ? clean : null;
}

export function createStateToken(documentSHA256: string, slotId: string, revision: number): string {
    return `${documentSHA256}:${slotId}:${revision}`;
}

export function acceptsIconResult(currentToken: string, resultToken: string): boolean {
    return currentToken === resultToken;
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null;
}

async function cacheIconifyResults(results: IconSearchResult[]): Promise<void> {
    if (!results.length || typeof indexedDB === "undefined") return;
    const database = await openIconDatabase();
    await new Promise<void>((resolve, reject) => {
        const transaction = database.transaction("icons", "readwrite");
        const store = transaction.objectStore("icons");
        results.forEach((result) => store.put(result));
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error);
    });
    database.close();
}

function openIconDatabase(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open("nahida.menu-maker.iconify", 1);
        request.onupgradeneeded = () => {
            if (!request.result.objectStoreNames.contains("icons")) {
                request.result.createObjectStore("icons", { keyPath: "name" });
            }
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

function iconTransaction<T>(
    database: IDBDatabase,
    mode: IDBTransactionMode,
    operation: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
    return new Promise((resolve, reject) => {
        const request = operation(database.transaction("icons", mode).objectStore("icons"));
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

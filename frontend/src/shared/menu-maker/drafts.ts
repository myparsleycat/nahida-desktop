import { Logger } from "@renderer/lib/logger";

import type { MenuMakerSettings, MenuMakerSlot } from "./types";

export const MENU_MAKER_DRAFT_LIMIT = 30;
export const MENU_MAKER_STORAGE_PREFIX = "nahida.menu-maker";

export interface MenuMakerDraftMeta {
    id: string;
    sourcePath: string;
    sourceName: string;
    sourceSHA256: string;
    slotSignature: string;
    updatedAt: number;
    sourceEncoding?: string;
    sourceHasBOM?: boolean;
    sourceNewline?: string;
    settings: MenuMakerSettings;
    slots: MenuMakerSlot[];
}

export interface MenuMakerDraftContent {
    settings: MenuMakerSettings;
    slots: MenuMakerSlot[];
}

export function canRestoreDraft(
    draft: MenuMakerDraftMeta,
    sourceSHA256: string,
    signature: string,
): boolean {
    return draft.sourceSHA256 === sourceSHA256 && draft.slotSignature === signature;
}

export function evictDrafts(
    drafts: MenuMakerDraftMeta[],
    limit = MENU_MAKER_DRAFT_LIMIT,
): {
    kept: MenuMakerDraftMeta[];
    removed: MenuMakerDraftMeta[];
} {
    const sorted = drafts.toSorted((left, right) => right.updatedAt - left.updatedAt);
    return { kept: sorted.slice(0, limit), removed: sorted.slice(limit) };
}

export function loadDraftMetadata(): MenuMakerDraftMeta[] {
    try {
        const value: unknown = JSON.parse(
            localStorage.getItem(`${MENU_MAKER_STORAGE_PREFIX}.drafts`) ?? "[]",
        );
        return Array.isArray(value) ? value.filter(isDraftMeta) : [];
    } catch (error) {
        Logger.capture("shared/menu-maker/drafts.ts", error);
        return [];
    }
}

export function saveDraftMetadata(drafts: MenuMakerDraftMeta[]): MenuMakerDraftMeta[] {
    const { kept, removed } = evictDrafts(drafts);
    localStorage.setItem(`${MENU_MAKER_STORAGE_PREFIX}.drafts`, JSON.stringify(kept));
    void Promise.all(removed.map((draft) => deleteDraftBlobs(draft.id)));
    return kept;
}

export async function saveDraftBlobs(
    id: string,
    values: Record<string, Blob | string | null>,
): Promise<void> {
    const database = await openDatabase();
    await transactionPromise(database, "readwrite", (store) => store.put({ id, ...values }));
}

export async function loadDraftBlobs(
    id: string,
): Promise<Record<string, Blob | string | null> | undefined> {
    const database = await openDatabase();
    return transactionPromise(database, "readonly", (store) => store.get(id));
}

export async function deleteDraftBlobs(id: string): Promise<void> {
    const database = await openDatabase();
    await transactionPromise(database, "readwrite", (store) => store.delete(id));
}

export function detachDraftMedia(
    settings: MenuMakerSettings,
    slots: MenuMakerSlot[],
): MenuMakerDraftContent & { blobs: Record<string, string | null> } {
    const slotImages = Object.fromEntries(
        slots.flatMap((slot) =>
            slot.icon.kind === "upload" ? [[slot.id, slot.icon.dataUrl] as const] : [],
        ),
    );
    return {
        settings: { ...settings, panelImageDataUrl: undefined },
        slots: slots.map((slot) =>
            slot.icon.kind === "upload" ? { ...slot, icon: { ...slot.icon, dataUrl: "" } } : slot,
        ),
        blobs: {
            panelImageDataUrl: settings.panelImageDataUrl ?? null,
            slotImages: JSON.stringify(slotImages),
        },
    };
}

export function restoreDraftMedia(
    draft: MenuMakerDraftMeta,
    blobs?: Record<string, Blob | string | null>,
): MenuMakerDraftContent {
    const slotImages = parseSlotImages(blobs?.slotImages);
    return {
        settings: {
            ...draft.settings,
            panelImageDataUrl:
                typeof blobs?.panelImageDataUrl === "string"
                    ? blobs.panelImageDataUrl
                    : draft.settings.panelImageDataUrl,
        },
        slots: draft.slots.map((slot) =>
            slot.icon.kind === "upload" && slotImages[slot.id]
                ? { ...slot, icon: { ...slot.icon, dataUrl: slotImages[slot.id] } }
                : slot,
        ),
    };
}

function openDatabase(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open(`${MENU_MAKER_STORAGE_PREFIX}.db`, 1);
        request.onupgradeneeded = () => {
            if (!request.result.objectStoreNames.contains("drafts"))
                request.result.createObjectStore("drafts", { keyPath: "id" });
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

function transactionPromise<T>(
    database: IDBDatabase,
    mode: IDBTransactionMode,
    operation: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
    return new Promise((resolve, reject) => {
        const transaction = database.transaction("drafts", mode);
        const request = operation(transaction.objectStore("drafts"));
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
        transaction.oncomplete = () => database.close();
        transaction.onerror = () => reject(transaction.error);
    });
}

function isDraftMeta(value: unknown): value is MenuMakerDraftMeta {
    return (
        typeof value === "object" &&
        value !== null &&
        typeof (value as MenuMakerDraftMeta).id === "string" &&
        typeof (value as MenuMakerDraftMeta).sourceSHA256 === "string" &&
        typeof (value as MenuMakerDraftMeta).slotSignature === "string" &&
        Array.isArray((value as MenuMakerDraftMeta).slots)
    );
}

function parseSlotImages(value: Blob | string | null | undefined): Record<string, string> {
    if (typeof value !== "string") return {};
    try {
        const parsed: unknown = JSON.parse(value);
        if (typeof parsed !== "object" || parsed === null) return {};
        return Object.fromEntries(
            Object.entries(parsed).filter(
                (entry): entry is [string, string] => typeof entry[1] === "string",
            ),
        );
    } catch (error) {
        Logger.capture("shared/menu-maker/drafts.ts", error);
        return {};
    }
}

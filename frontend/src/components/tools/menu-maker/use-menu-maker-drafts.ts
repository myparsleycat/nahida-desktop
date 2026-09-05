import type { MenuMakerSource } from "@bindings/menumaker";
import { MenuMaker } from "@bindings/menumaker";
import { Logger } from "@renderer/lib/logger";
import {
    canRestoreDraft,
    deleteDraftBlobs,
    detachDraftMedia,
    loadDraftBlobs,
    loadDraftMetadata,
    restoreDraftMedia,
    saveDraftBlobs,
    saveDraftMetadata,
} from "@shared/menu-maker/drafts";
import { slotSignature } from "@shared/menu-maker/parser";
import { DEFAULT_MENU_MAKER_SETTINGS, type MenuMakerSettings } from "@shared/menu-maker/types";
import { useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { type EditorState, type EditorAction } from "./menu-maker-editor-state";

export function useMenuMakerDrafts(
    state: EditorState,
    dispatch: React.Dispatch<EditorAction>,
    onRestored: () => void,
) {
    const { t } = useTranslation();
    const persistDraft = useCallback(() => {
        if (!state.source || !state.document) return;
        const source = state.source;
        const signature = slotSignature(state.document.slots ?? []);
        const id = `${source.sha256}:${signature}`;
        const detached = detachDraftMedia(state.settings, state.slots);
        const existing = loadDraftMetadata();
        const superseded = existing.filter(
            (draft) =>
                draft.id !== id &&
                draft.sourcePath === source.path &&
                draft.slotSignature === signature,
        );
        try {
            saveDraftMetadata([
                {
                    id,
                    sourcePath: source.path,
                    sourceName: source.fileName,
                    sourceSHA256: source.sha256,
                    slotSignature: signature,
                    updatedAt: Date.now(),
                    sourceEncoding: source.encoding,
                    sourceHasBOM: source.hasBOM,
                    sourceNewline: source.newline,
                    settings: detached.settings,
                    slots: detached.slots,
                },
                ...existing.filter(
                    (draft) => draft.id !== id && !superseded.some((item) => item.id === draft.id),
                ),
            ]);
        } catch (error) {
            Logger.error({ error, draftId: id }, "MenuMakerPage:saveDraftMetadata");
        }
        for (const draft of superseded) {
            void deleteDraftBlobs(draft.id).catch((error) =>
                Logger.error({ error, draftId: draft.id }, "MenuMakerPage:deleteDraftBlobs"),
            );
        }
        void saveDraftBlobs(id, { originalText: source.text, ...detached.blobs }).catch((error) =>
            Logger.error({ error, draftId: id }, "MenuMakerPage:saveDraftBlobs"),
        );
    }, [state.document, state.settings, state.slots, state.source]);
    useEffect(() => {
        if (!state.source || !state.document) return;
        const timeout = window.setTimeout(persistDraft, 500);
        return () => window.clearTimeout(timeout);
    }, [persistDraft, state.document, state.source]);
    useEffect(() => {
        window.addEventListener("beforeunload", persistDraft);
        return () => {
            persistDraft();
            window.removeEventListener("beforeunload", persistDraft);
        };
    }, [persistDraft]);
    useEffect(() => {
        localStorage.setItem(
            "nahida.menu-maker.settings",
            JSON.stringify({ ...state.settings, panelImageDataUrl: undefined }),
        );
    }, [state.settings]);
    const restoreDraft = async (draft: ReturnType<typeof loadDraftMetadata>[number]) => {
        try {
            const blobs = await loadDraftBlobs(draft.id);
            const matchesCurrent = Boolean(
                state.source &&
                state.document &&
                canRestoreDraft(
                    draft,
                    state.source.sha256,
                    slotSignature(state.document.slots ?? []),
                ),
            );
            const originalText = matchesCurrent ? state.source?.text : blobs?.originalText;
            if (typeof originalText !== "string") throw new Error("MENU_MAKER_DRAFT_TEXT_MISSING");
            const document = matchesCurrent ? state.document! : await MenuMaker.Parse(originalText);
            if (!canRestoreDraft(draft, draft.sourceSHA256, slotSignature(document.slots ?? [])))
                throw new Error("MENU_MAKER_DRAFT_SIGNATURE_CHANGED");
            const restored = restoreDraftMedia(draft, blobs);
            dispatch({
                type: "load",
                source: matchesCurrent
                    ? state.source!
                    : {
                          path: draft.sourcePath,
                          fileName: draft.sourceName,
                          text: originalText,
                          sha256: draft.sourceSHA256,
                          encoding: draft.sourceEncoding ?? "utf8",
                          hasBOM: draft.sourceHasBOM ?? false,
                          newline: draft.sourceNewline ?? "lf",
                          document,
                      },
                document,
                slots: restored.slots,
                settings: restored.settings,
                sourceAvailable: matchesCurrent ? state.sourceAvailable : false,
            });
            onRestored();
            toast.success(t("page.tools.menu_maker.draft_restored"));
        } catch (error) {
            Logger.error({ error, draftId: draft.id }, "MenuMakerPage:restoreDraft");
            toast.error(t("page.tools.menu_maker.draft_restore_failed"));
        }
    };
    return { restoreDraft };
}

export async function restoreSourceDraft(source: MenuMakerSource) {
    const document = source.document;
    const draft = loadDraftMetadata().find((item) =>
        canRestoreDraft(item, source.sha256, slotSignature(document.slots ?? [])),
    );
    const blobs = draft
        ? await loadDraftBlobs(draft.id).catch((error) => {
              Logger.error({ error, draftId: draft.id }, "MenuMakerPage:loadDraftBlobs");
              return undefined;
          })
        : undefined;
    return { draft, restored: draft ? restoreDraftMedia(draft, blobs) : undefined };
}

export function loadStoredSettings(): MenuMakerSettings {
    if (typeof localStorage === "undefined") return DEFAULT_MENU_MAKER_SETTINGS;
    try {
        const value: unknown = JSON.parse(
            localStorage.getItem("nahida.menu-maker.settings") ?? "null",
        );
        if (!value || typeof value !== "object") return DEFAULT_MENU_MAKER_SETTINGS;
        const stored = value as Partial<MenuMakerSettings>;
        return {
            ...DEFAULT_MENU_MAKER_SETTINGS,
            ...stored,
            columns: stored.columns || DEFAULT_MENU_MAKER_SETTINGS.columns,
            palette: { ...DEFAULT_MENU_MAKER_SETTINGS.palette, ...stored.palette },
        };
    } catch (error) {
        Logger.capture("menu-maker:read-settings", error);
        return DEFAULT_MENU_MAKER_SETTINGS;
    }
}

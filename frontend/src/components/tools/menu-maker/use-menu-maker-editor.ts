import type { MenuMakerGenerateResult } from "@bindings/menumaker";
import { MenuMaker } from "@bindings/menumaker";
import { Dialog } from "@bindings/platform";
import { Logger } from "@renderer/lib/logger";
import { moveMenuMakerSlot } from "@shared/menu-maker/generator";
import { withSuggestedIcons } from "@shared/menu-maker/parser";
import { renderMenuMakerAssets } from "@shared/menu-maker/resources";
import { type MenuMakerSlot } from "@shared/menu-maker/types";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { type EditorState, reducer } from "./menu-maker-editor-state";
import {
    useMenuMakerDrafts,
    restoreSourceDraft,
    loadStoredSettings,
} from "./use-menu-maker-drafts";

const initialState: EditorState = {
    slots: [],
    settings: loadStoredSettings(),
    sourceAvailable: false,
    busy: false,
    revision: 0,
};
export function useMenuMakerEditor({
    path,
    ini,
    onSourceLoaded,
    onDraftRestored,
}: {
    path: string;
    ini: string;
    onSourceLoaded: () => void;
    onDraftRestored: () => void;
}) {
    const { t } = useTranslation();
    const [state, dispatch] = useReducer(reducer, initialState);
    const [preview, setPreview] = useState<MenuMakerGenerateResult>();
    const initialized = useRef(false);
    const loadSource = useCallback(
        async (filePath: string) => {
            dispatch({ type: "busy", value: true });
            try {
                const source = await MenuMaker.LoadSource(filePath);
                const document = source.document;
                const { draft, restored } = await restoreSourceDraft(source);
                dispatch({
                    type: "load",
                    source,
                    document,
                    slots: restored?.slots ?? withSuggestedIcons(document.slots),
                    settings: restored?.settings,
                    sourceAvailable: true,
                });
                onSourceLoaded();
                toast.success(
                    draft
                        ? t("page.tools.menu_maker.draft_restored")
                        : t("page.tools.menu_maker.loaded"),
                );
            } catch (error) {
                Logger.error({ error, filePath }, "MenuMakerPage:loadSource");
                toast.error(
                    String(error).includes("MENU_MAKER_NO_KEY_SECTIONS")
                        ? t("page.tools.menu_maker.no_keys")
                        : t("page.tools.menu_maker.load_failed"),
                );
            } finally {
                dispatch({ type: "busy", value: false });
            }
        },
        [onSourceLoaded, t],
    );
    const scanFolder = useCallback(
        async (root: string, includeTXT = false) => {
            dispatch({ type: "busy", value: true });
            try {
                const result = await MenuMaker.ScanFolder(root, includeTXT);
                dispatch({ type: "scan", value: result });
                if (result.files?.length === 1) await loadSource(result.files[0].path);
            } catch (error) {
                Logger.error({ error, root, includeTXT }, "MenuMakerPage:scanFolder");
                toast.error(t("page.tools.menu_maker.scan_failed"));
            } finally {
                dispatch({ type: "busy", value: false });
            }
        },
        [loadSource, t],
    );
    useEffect(() => {
        if (initialized.current) return;
        initialized.current = true;
        // Route search parameters are an external navigation input and must hydrate the editor once.
        // oxlint-disable-next-line react/set-state-in-effect
        if (ini) void loadSource(ini);
        else if (path) void scanFolder(path);
    }, [ini, loadSource, path, scanFolder]);
    const { restoreDraft } = useMenuMakerDrafts(state, dispatch, onDraftRestored);
    useEffect(() => {
        if (!state.source?.text) return;
        let cancelled = false;
        void MenuMaker.Generate({
            sourceText: state.source.text,
            slots: state.slots,
            settings: state.settings,
        })
            .then((result) => {
                if (!cancelled) setPreview(result);
            })
            .catch((error) => {
                if (cancelled) return;
                Logger.error({ error, sourcePath: state.source?.path }, "MenuMakerPage:generate");
                setPreview(undefined);
            });
        return () => {
            cancelled = true;
        };
    }, [state.settings, state.slots, state.source]);
    const chooseFile = async () => {
        const result = await Dialog.ShowOpenDialog({
            title: t("page.tools.menu_maker.choose_file"),
            defaultPath: state.source?.path ?? path,
            filters: [{ name: "3DMigoto INI", extensions: ["ini", "txt"] }],
            properties: ["openFile"],
        });
        const selectedPath = result.filePaths?.[0];
        if (selectedPath) await loadSource(selectedPath);
    };
    const chooseFolder = async () => {
        const result = await Dialog.SelectDirectory();
        if (!result.canceled && result.filePath) await scanFolder(result.filePath);
    };
    const outputName = state.source
        ? buildOutputName(state.source.fileName, state.settings.useOriginalININame)
        : "menu_gui.ini";
    const generateCurrent = async () => {
        if (!state.source) return null;
        try {
            const result = await MenuMaker.Generate({
                sourceText: state.source.text,
                slots: state.slots,
                settings: state.settings,
            });
            setPreview(result);
            return result;
        } catch (error) {
            Logger.error({ error, sourcePath: state.source.path }, "MenuMakerPage:generate");
            toast.error(t("page.tools.menu_maker.load_failed"));
            return null;
        }
    };
    const buildAssets = async (generated: MenuMakerGenerateResult) => {
        try {
            return await renderMenuMakerAssets(
                state.slots,
                state.settings,
                generated.geometry,
                generated.slotStates,
            );
        } catch (error) {
            Logger.error({ error, sourcePath: state.source?.path }, "MenuMakerPage:renderAssets");
            toast.error(t("page.tools.menu_maker.font_failed"));
            return null;
        }
    };
    const applyBundle = async () => {
        if (!state.source || !state.sourceAvailable) return;
        dispatch({ type: "busy", value: true });
        try {
            const generated = await generateCurrent();
            if (!generated) return;
            const assets = await buildAssets(generated);
            if (!assets) return;
            const result = await MenuMaker.ApplyBundle({
                sourcePath: state.source.path,
                sourceSHA256: state.source.sha256,
                outputININame: outputName,
                slots: state.slots,
                settings: state.settings,
                encoding: state.source.encoding,
                hasBOM: state.source.hasBOM,
                newline: state.source.newline,
                assets,
                useOriginalININame: state.settings.useOriginalININame,
            });
            toast.success(t("page.tools.menu_maker.applied", { path: result.outputINIPath }));
            if (result.sourceSHA256) {
                dispatch({
                    type: "sourceContent",
                    text: generated.iniText,
                    sha256: result.sourceSHA256,
                });
            } else if (
                /\.ini$/i.test(state.source.fileName) &&
                !state.settings.useOriginalININame
            ) {
                dispatch({ type: "sourceAvailable", value: false });
            }
        } catch (error) {
            Logger.error(
                { error, sourcePath: state.source.path, outputName },
                "MenuMakerPage:applyBundle",
            );
            toast.error(
                String(error).includes("MENU_MAKER_SOURCE_CHANGED")
                    ? t("page.tools.menu_maker.source_changed")
                    : t("page.tools.menu_maker.apply_failed"),
            );
        } finally {
            dispatch({ type: "busy", value: false });
        }
    };
    const saveINI = async () => {
        if (!state.source) return;
        const selection = await Dialog.SaveFile({
            suggestedName: outputName,
            filters: [{ name: "INI", extensions: ["ini"] }],
        });
        if (selection.canceled || !selection.filePath) return;
        dispatch({ type: "busy", value: true });
        try {
            await MenuMaker.SaveINI({
                destinationPath: selection.filePath,
                sourceText: state.source.text,
                slots: state.slots,
                settings: state.settings,
                encoding: state.source.encoding,
                hasBOM: state.source.hasBOM,
                newline: state.source.newline,
            });
            toast.success(t("page.tools.menu_maker.saved"));
        } catch (error) {
            Logger.error(
                { error, destinationPath: selection.filePath, outputName },
                "MenuMakerPage:saveINI",
            );
            toast.error(t("page.tools.menu_maker.save_failed"));
        } finally {
            dispatch({ type: "busy", value: false });
        }
    };
    const saveZIP = async () => {
        if (!state.source) return;
        const selection = await Dialog.SaveFile({
            suggestedName: outputName.replace(/\.ini$/i, ".zip"),
            filters: [{ name: "ZIP", extensions: ["zip"] }],
        });
        if (selection.canceled || !selection.filePath) return;
        dispatch({ type: "busy", value: true });
        try {
            const generated = await generateCurrent();
            if (!generated) return;
            const assets = await buildAssets(generated);
            if (!assets) return;
            await MenuMaker.SaveZIP({
                destinationPath: selection.filePath,
                outputININame: outputName,
                sourceText: state.source.text,
                slots: state.slots,
                settings: state.settings,
                encoding: state.source.encoding,
                hasBOM: state.source.hasBOM,
                newline: state.source.newline,
                assets,
            });
            toast.success(t("page.tools.menu_maker.saved"));
        } catch (error) {
            Logger.error(
                { error, destinationPath: selection.filePath, outputName },
                "MenuMakerPage:saveZIP",
            );
            toast.error(t("page.tools.menu_maker.save_failed"));
        } finally {
            dispatch({ type: "busy", value: false });
        }
    };
    const setSlot = (id: string, transform: (slot: MenuMakerSlot) => MenuMakerSlot) =>
        dispatch({
            type: "slots",
            value: state.slots.map((slot) => (slot.id === id ? transform(slot) : slot)),
        });
    const moveSlot = (from: string, to: string) =>
        dispatch({ type: "slots", value: moveMenuMakerSlot(state.slots, from, to) });
    return {
        t,
        state,
        dispatch,
        preview,
        chooseFile,
        chooseFolder,
        applyBundle,
        saveINI,
        saveZIP,
        setSlot,
        moveSlot,
        loadSource,
        scanFolder,
        restoreDraft,
    };
}

function buildOutputName(fileName: string, original: boolean): string {
    const base = fileName.replace(/\.(?:ini|txt)$/i, "");
    return `${base}${original ? "" : "_gui"}.ini`;
}

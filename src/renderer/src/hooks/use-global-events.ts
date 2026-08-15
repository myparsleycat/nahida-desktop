import type { DownloadSource } from "@shared/mod";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { globalStore, useGlobalStore } from "../store/global";

export function useGlobalEvents(
    onPathSelectorModeSelect?: (data: {
        selectionId: string;
        suggestedName?: string;
        downloadTargetName?: string;
        downloadImporterKey?: string;
        downloadSource: DownloadSource;
    }) => void,
) {
    const navi = useNavigate();
    const setSession = useGlobalStore((state) => state.setSession);
    const setHasToken = useGlobalStore((state) => state.setHasToken);
    const setBackendStatus = useGlobalStore((state) => state.setBackendStatus);
    const setPendingSessionRestore = useGlobalStore((state) => state.setPendingSessionRestore);
    const [listeners, setListeners] = useState<Map<string, () => void>>(new Map());
    const { i18n } = useTranslation();

    const removeAllListeners = () => {
        listeners.forEach((listener) => {
            listener();
        });
        setListeners(new Map());
    };

    // biome-ignore lint/correctness/useExhaustiveDependencies: <>
    useEffect(() => {
        const removeToastListener = window.api.on("fn:toast", (event, args) => {
            toast(event, {
                description: args?.description,
            });
        });
        setListeners(new Map(listeners.set("fn:toast", removeToastListener)));

        const removeNaviListener = window.api.on("fn:navi", (path) => {
            void navi({ to: path });
        });
        setListeners(new Map(listeners.set("fn:navi", removeNaviListener)));

        const removePathSelectorListener = window.api.on("pathSelector:modeSelect", (data) => {
            if (onPathSelectorModeSelect) {
                onPathSelectorModeSelect(data);
            }
        });
        setListeners(new Map(listeners.set("pathSelector:modeSelect", removePathSelectorListener)));

        const removeAuthListener = window.api.on("auth:update", (session) => {
            setSession(session);
            setHasToken(!!session);
        });
        setListeners(new Map(listeners.set("auth:update", removeAuthListener)));

        const removeBackendStatusListener = window.api.on("backend:status", (status) => {
            const previousStatus = globalStore.getState().backendStatus;
            setBackendStatus(status);
            if (status !== "online") return;

            const isColdStartRestore = previousStatus === "unknown";
            if (previousStatus !== "offline" && !isColdStartRestore) return;

            if (isColdStartRestore) setPendingSessionRestore(true);

            void (async () => {
                try {
                    if (isColdStartRestore) {
                        await whenSessionInitialized();
                        const state = globalStore.getState();
                        if (state.session || !state.hasToken) return;
                    }

                    const session = await window.api.invoke("auth:getSession");
                    setSession(session);
                    setHasToken(!!session || (await window.api.invoke("auth:hasToken")));
                } catch (error) {
                    console.error("Failed to refresh session after backend recovery", error);
                } finally {
                    if (isColdStartRestore) setPendingSessionRestore(false);
                }
            })();
        });
        setListeners(new Map(listeners.set("backend:status", removeBackendStatusListener)));

        const removeLanguageListener = window.api.on("language:update", (language) => {
            void i18n.changeLanguage(language);
        });
        setListeners(new Map(listeners.set("language:update", removeLanguageListener)));

        return () => {
            removeAllListeners();
        };
    }, [onPathSelectorModeSelect, i18n]);
}

function whenSessionInitialized() {
    if (globalStore.getState().sessionInitialized) return Promise.resolve();

    return new Promise<void>((resolve) => {
        const unsubscribe = globalStore.subscribe((state) => {
            if (!state.sessionInitialized) return;
            unsubscribe();
            resolve();
        });

        if (globalStore.getState().sessionInitialized) {
            unsubscribe();
            resolve();
        }
    });
}

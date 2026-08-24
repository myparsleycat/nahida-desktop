import type { DownloadSource } from "@shared/mod";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
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
    const { i18n } = useTranslation();

    // biome-ignore lint/correctness/useExhaustiveDependencies: <>
    useEffect(() => {
        const removeToastListener = window.api.on("fn:toast", (event, args) => {
            toast(event, {
                description: args?.description,
            });
        });

        const removeNaviListener = window.api.on("fn:navi", (path) => {
            void navi({ to: path });
        });

        const removePathSelectorListener = window.api.on("pathSelector:modeSelect", (data) => {
            if (onPathSelectorModeSelect) {
                onPathSelectorModeSelect(data);
            }
        });

        const removeAuthListener = window.api.on("auth:update", (session) => {
            setSession(session);
            setHasToken(!!session);
        });

        const removeBackendStatusListener = window.api.on("backend:status", (status) => {
            const previousStatus = globalStore.getState().backendStatus;
            const isColdStartRestore = status === "online" && previousStatus === "unknown";
            if (isColdStartRestore) {
                globalStore.setState({
                    backendStatus: status,
                    pendingSessionRestore: true,
                });
            } else {
                setBackendStatus(status);
            }
            if (status !== "online") return;
            if (
                previousStatus !== "offline" &&
                previousStatus !== "maintenance" &&
                !isColdStartRestore
            )
                return;

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

        const removeLanguageListener = window.api.on("language:update", (language) => {
            void i18n.changeLanguage(language);
        });

        return () => {
            removeToastListener();
            removeNaviListener();
            removePathSelectorListener();
            removeAuthListener();
            removeBackendStatusListener();
            removeLanguageListener();
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

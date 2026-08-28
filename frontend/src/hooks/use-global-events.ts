import { Auth } from "@bindings/auth";
import type { BackendStatus } from "@shared/backend";
import type { DownloadSource } from "@shared/mod";
import { useNavigate } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { globalStore, useGlobalStore } from "../store/global";

export function useGlobalEvents(
    onPathSelectorModeSelect?: (data: {
        selectionId: string;
        suggestedName?: string;
        suggestedNames?: string[];
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
        const removeToastListener = Events.On("fn:toast", (event) => {
            const payload = event.data as
                | string
                | [string, { description?: string } | undefined]
                | undefined;
            if (Array.isArray(payload)) {
                const [message, args] = payload;
                toast(message, { description: args?.description });
                return;
            }
            toast(typeof payload === "string" ? payload : "");
        });

        const removeNaviListener = Events.On("fn:navi", (event) => {
            void navi({ to: event.data as string });
        });

        const removePathSelectorListener = Events.On("pathSelector:modeSelect", (event) => {
            if (!onPathSelectorModeSelect) return;
            const payload = Array.isArray(event.data) ? event.data[0] : event.data;
            if (payload) {
                onPathSelectorModeSelect(payload);
            }
        });

        const removeAuthListener = Events.On("auth:update", (event) => {
            const session = event.data;
            setSession(session);
            setHasToken(!!session);
        });

        const removeBackendStatusListener = Events.On("backend:status", (event) => {
            const status = event.data as BackendStatus;
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

                    const session = await Auth.GetSession();
                    setSession(session);
                    setHasToken(!!session || (await Auth.HasToken()));
                } catch (error) {
                    console.error("Failed to refresh session after backend recovery", error);
                } finally {
                    if (isColdStartRestore) setPendingSessionRestore(false);
                }
            })();
        });

        const removeLanguageListener = Events.On("language:update", (event) => {
            void i18n.changeLanguage(event.data as string);
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

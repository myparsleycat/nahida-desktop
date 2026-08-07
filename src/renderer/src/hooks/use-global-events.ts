import { CollectionCopyToast } from "@renderer/components/drive/collection-copy-toast";
import type { DownloadSource } from "@shared/mod";
import { useNavigate } from "@tanstack/react-router";
import { createElement, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { useGlobalStore } from "../store/global";

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

        const removeCollectionCopyProgressListener = window.api.on(
            "drive:copy-progress",
            (progress) => {
                const isFinished =
                    progress.phase === "completed" ||
                    progress.phase === "canceled" ||
                    progress.phase === "error";
                toast.custom(
                    () =>
                        createElement(CollectionCopyToast, {
                            progress,
                            onCancel: () =>
                                window.api.invoke(
                                    "drive:fn:cancelCopyFromUrl",
                                    progress.operationId,
                                ),
                        }),
                    {
                        id: progress.operationId,
                        duration: isFinished ? 4000 : Infinity,
                    },
                );
            },
        );
        setListeners(
            new Map(listeners.set("drive:copy-progress", removeCollectionCopyProgressListener)),
        );

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
            setBackendStatus(status);
            if (status !== "online") return;

            void (async () => {
                try {
                    const session = await window.api.invoke("auth:getSession");
                    setSession(session);
                    setHasToken(!!session || (await window.api.invoke("auth:hasToken")));
                } catch (error) {
                    console.error("Failed to refresh session after backend recovery", error);
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

import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { useGlobalStore } from "../store/global";

export function useGlobalEvents(
    onPathSelectorModeSelect?: (data: {
        selectionId: string;
        suggestedName?: string;
        downloadTargetName?: string;
        downloadImporterKey?: string;
    }) => void,
) {
    const navi = useNavigate();
    const setSession = useGlobalStore((state) => state.setSession);
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
            navi({ to: path });
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
        });
        setListeners(new Map(listeners.set("auth:update", removeAuthListener)));

        const removeLanguageListener = window.api.on("language:update", (language) => {
            i18n.changeLanguage(language);
        });
        setListeners(new Map(listeners.set("language:update", removeLanguageListener)));

        return () => {
            removeAllListeners();
        };
    }, [onPathSelectorModeSelect, i18n]);
}

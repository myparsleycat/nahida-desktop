import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";

export function useGlobalEvents(
    onDownloadModeSelect?: (data: { downloadId: string; suggestedName?: string }) => void,
) {
    const navi = useNavigate();
    const [listeners, setListeners] = useState<Map<string, () => void>>(new Map());

    const removeAllListeners = () => {
        listeners.forEach((listener) => listener());
        setListeners(new Map());
    };

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

        const removeDownloadModeListener = window.api.on("download:modeSelect", (data) => {
            if (onDownloadModeSelect) {
                onDownloadModeSelect(data);
            }
        });
        setListeners(new Map(listeners.set("download:modeSelect", removeDownloadModeListener)));

        return () => {
            removeAllListeners();
        };
    }, [onDownloadModeSelect]);
}

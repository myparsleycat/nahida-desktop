import { supportsWindowsDesktopFeatures } from "@shared/platform";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, type ReactNode } from "react";
import { useGlobalStore } from "../store/global";

export function WindowsOnlyRoute({
    children,
    fallbackTo,
}: {
    children: ReactNode;
    fallbackTo: "/transfer" | "/setting/gen";
}) {
    const navi = useNavigate();
    const appStatus = useGlobalStore((state) => state.appStatus);
    const isWindows = supportsWindowsDesktopFeatures(appStatus?.platform);

    useEffect(() => {
        if (appStatus && !isWindows) {
            navi({ to: fallbackTo });
        }
    }, [appStatus, fallbackTo, isWindows, navi]);

    if (!appStatus || !isWindows) {
        return null;
    }

    return <>{children}</>;
}

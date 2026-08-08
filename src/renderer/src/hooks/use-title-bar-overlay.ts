import { useEffect } from "react";

function readTitleBarSymbolColor() {
    const symbolColor = getComputedStyle(document.documentElement)
        .getPropertyValue("--titlebar-overlay-symbol-color")
        .trim();
    if (!symbolColor) return null;
    return { symbolColor };
}

export function useTitleBarOverlay() {
    useEffect(() => {
        const sync = () => {
            const options = readTitleBarSymbolColor();
            if (!options) return;
            void window.api.invoke("window:syncTitleBarOverlay", options);
        };
        sync();
        const observer = new MutationObserver(sync);
        observer.observe(document.documentElement, {
            attributes: true,
            attributeFilter: ["class"],
        });
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        mediaQuery.addEventListener("change", sync);
        return () => {
            observer.disconnect();
            mediaQuery.removeEventListener("change", sync);
        };
    }, []);
}

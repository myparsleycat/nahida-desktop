import { useLayoutEffect, useRef } from "react";

export function resetDriveFolderScroll(pane: HTMLElement | null) {
    if (!pane) return;

    pane.scrollTo({ top: 0, left: 0 });
    pane.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')?.scrollTo({
        top: 0,
        left: 0,
    });
}

export function useDriveFolderScroll(folderId: string) {
    const paneRef = useRef<HTMLDivElement>(null);

    useLayoutEffect(() => {
        // Reset before paint. Reveal still scrolls in a later frame.
        resetDriveFolderScroll(paneRef.current);
    }, [folderId]);

    return paneRef;
}

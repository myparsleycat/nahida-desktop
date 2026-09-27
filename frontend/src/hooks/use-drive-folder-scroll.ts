import { useEffect, useRef } from "react";

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

    useEffect(() => {
        // Reveal scrolls in a later frame, so a pending id must not keep the previous offset.
        resetDriveFolderScroll(paneRef.current);
    }, [folderId]);

    return paneRef;
}

import { get, writable } from "svelte/store";

export const isTransferSheetOpen = writable(false);
export const openChangeTransferSheet = (open: boolean) => isTransferSheetOpen.set(open);

export class PreviewModalClass {
    static store = writable<{
        isOpen: boolean;
        src: string | null;
        alt: string | null;
    }>({
        isOpen: false,
        src: null,
        alt: null,
    });

    static open(src: string, alt: string) {
        PreviewModalClass.store.set({
            isOpen: true,
            src,
            alt
        });
    }

    static close = () => {
        PreviewModalClass.store.set({
            isOpen: false,
            src: null,
            alt: null
        });
    }
}

export class CharPathSelectorClass {
    static store = writable<{
        isOpen: boolean;
        selectedPath: string | null;
    }>({
        isOpen: false,
        selectedPath: null
    });

    static open() {
        CharPathSelectorClass.store.set({
            isOpen: true,
            selectedPath: null
        });
    }

    static close() {
        CharPathSelectorClass.store.set({
            isOpen: false,
            selectedPath: null
        });
    }

    static setPath(path: string) {
        CharPathSelectorClass.store.set({
            isOpen: get(this.store).isOpen,
            selectedPath: path
        })
    }
}
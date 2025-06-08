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

class CharPathSelectorClass {
    store = writable<{
        isOpen: boolean;
        selectedPath: string | null;
        currentFolderPath: string;
        currentCharPath: string;
    }>({
        isOpen: false,
        selectedPath: null,
        currentFolderPath: "",
        currentCharPath: ""
    });

    private resolvePromise: ((path: string) => void) | null = null;

    open(): Promise<string> {
        this.store.set({
            isOpen: true,
            selectedPath: null,
            currentCharPath: "",
            currentFolderPath: ""
        });

        return new Promise<string>((resolve) => {
            this.resolvePromise = resolve;
        });
    }

    close() {
        this.store.set({
            isOpen: false,
            selectedPath: null,
            currentCharPath: "",
            currentFolderPath: ""
        });
    }

    setPath(path: string) {
        this.store.update(state => ({
            ...state,
            selectedPath: path
        }));

        if (this.resolvePromise) {
            this.resolvePromise(path);
            this.resolvePromise = null;
            this.close();
        }
    }
}

export const CharPathSelector = new CharPathSelectorClass();
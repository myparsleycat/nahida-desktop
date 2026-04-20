function toLocalUrl(filePath: string): string {
    const normalized = filePath.replaceAll("\\", "/");
    return `local://${encodeURI(normalized).replaceAll("#", "%23").replaceAll("?", "%3F")}`;
}

export function modelViewerSourceToUrl(source: File | ArrayBuffer | string): string {
    if (typeof source === "string") {
        return source.startsWith("file:///") || source.startsWith("local://")
            ? source
            : toLocalUrl(source);
    }

    const blob =
        source instanceof File
            ? source
            : new Blob([source as BlobPart], { type: "model/gltf-binary" });

    return URL.createObjectURL(blob);
}

export function cleanupModelViewerUrl(url: string): void {
    if (url.startsWith("blob:")) {
        URL.revokeObjectURL(url);
    }
}

export function suppressModelViewerFocusOutline(element: HTMLElement | null): void {
    if (!element) {
        return;
    }

    requestAnimationFrame(() => {
        const shadowRoot = element.shadowRoot;
        if (!shadowRoot || shadowRoot.querySelector("style[data-nhd-focus-outline]")) {
            return;
        }

        const style = document.createElement("style");
        style.dataset.nhdFocusOutline = "true";
        style.textContent = ".userInput { outline: none !important; }";
        shadowRoot.appendChild(style);
    });
}

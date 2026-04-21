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

type ModelViewerOrbit = {
    toString(): string;
};

type ModelViewerTarget = {
    toString(): string;
};

export type ModelViewerElement = HTMLElement & {
    cameraOrbit?: string;
    cameraTarget?: string;
    fieldOfView?: string;
    jumpCameraToGoal?: () => void;
    getCameraOrbit?: () => ModelViewerOrbit;
    getCameraTarget?: () => ModelViewerTarget;
    getFieldOfView?: () => number;
};

export type ModelViewerCameraState = {
    orbit: string;
    target: string;
    fieldOfView: string;
};

export function captureModelViewerCameraState(
    element: ModelViewerElement | null,
): ModelViewerCameraState | null {
    const orbit = element?.getCameraOrbit?.()?.toString();
    const target = element?.getCameraTarget?.()?.toString();
    const fieldOfViewValue = element?.getFieldOfView?.();
    if (!orbit || !target || fieldOfViewValue == null || Number.isNaN(fieldOfViewValue)) {
        return null;
    }

    return {
        orbit,
        target,
        fieldOfView: `${fieldOfViewValue}deg`,
    };
}

export function restoreModelViewerCameraState(
    element: ModelViewerElement | null,
    state: ModelViewerCameraState | null,
): void {
    if (!element || !state) {
        return;
    }

    requestAnimationFrame(() => {
        if (!element.isConnected) {
            return;
        }

        element.cameraTarget = state.target;
        element.fieldOfView = state.fieldOfView;
        element.cameraOrbit = state.orbit;
        element.jumpCameraToGoal?.();
    });
}

export function suppressModelViewerFocusOutline(element: HTMLElement | null): void {
    if (!element) {
        return;
    }

    element.tabIndex = -1;
    element.style.outline = "none";

    requestAnimationFrame(() => {
        const shadowRoot = element.shadowRoot;
        if (!shadowRoot || shadowRoot.querySelector("style[data-nhd-focus-outline]")) {
            return;
        }

        const style = document.createElement("style");
        style.dataset.nhdFocusOutline = "true";
        style.textContent = `
            :host {
                outline: none !important;
            }

            :host(:focus),
            :host(:focus-visible) {
                outline: none !important;
            }

            .userInput:focus,
            .userInput:focus:not(:focus-visible) {
                outline: none !important;
            }

            .userInput:focus-visible {
                outline: none !important;
            }
        `;
        shadowRoot.appendChild(style);
    });
}

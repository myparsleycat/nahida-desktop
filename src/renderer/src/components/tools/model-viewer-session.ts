import type { ModelViewerCameraState } from "./model-viewer-contract";

const ANGLED_VIEW_THETA_DEGREES = Math.atan2(0.45, 1) * (180 / Math.PI);
const ANGLED_VIEW_PHI_DEGREES =
    Math.acos(0.15 / Math.sqrt(0.45 ** 2 + 0.15 ** 2 + 1 ** 2)) * (180 / Math.PI);

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

type ModelViewerVector3D = {
    x: number;
    y: number;
    z: number;
};

export type ModelViewerElement = HTMLElement & {
    cameraOrbit?: string;
    cameraTarget?: string;
    fieldOfView?: string;
    orientation?: string;
    updateComplete?: Promise<unknown>;
    model?: {
        materials?: Array<{
            ensureLoaded?: () => Promise<void>;
            setDoubleSided?: (doubleSided: boolean) => void;
        }>;
    };
    jumpCameraToGoal?: () => void;
    updateFraming?: () => Promise<void>;
    getBoundingBoxCenter?: () => ModelViewerVector3D;
    getCameraOrbit?: () => ModelViewerOrbit;
    getCameraTarget?: () => ModelViewerTarget;
    getFieldOfView?: () => number;
};

export function captureViewerCameraState(
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

export function restoreViewerCameraState(
    element: ModelViewerElement | null,
    state: ModelViewerCameraState | null,
    options?: {
        includeFieldOfView?: boolean;
    },
): void {
    if (!element || !state) {
        return;
    }

    requestAnimationFrame(() => {
        if (!element.isConnected) {
            return;
        }

        element.cameraTarget = state.target;
        if (options?.includeFieldOfView !== false) {
            element.fieldOfView = state.fieldOfView;
        }
        element.cameraOrbit = state.orbit;
        element.jumpCameraToGoal?.();
    });
}

export const captureModelViewerCameraState = captureViewerCameraState;
export const restoreModelViewerCameraState = restoreViewerCameraState;

export function applyAngledViewerFraming(element: ModelViewerElement | null): void {
    if (!element) {
        return;
    }

    const orbit = element.getCameraOrbit?.()?.toString();
    const target = element.getCameraTarget?.()?.toString();
    const radius = orbit?.split(/\s+/)[2];
    if (!target || !radius) {
        return;
    }

    element.cameraTarget = target;
    element.cameraOrbit = `${ANGLED_VIEW_THETA_DEGREES}deg ${ANGLED_VIEW_PHI_DEGREES}deg ${radius}`;
    element.jumpCameraToGoal?.();
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

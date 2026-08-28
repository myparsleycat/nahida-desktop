import { localFileSrc } from "@renderer/lib/local-file";

export function modelViewerSourceToUrl(source: File | ArrayBuffer | string): string {
    if (typeof source === "string") {
        return localFileSrc(source);
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

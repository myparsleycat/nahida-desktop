import { fetchBinaryBytes } from "@renderer/wails/binary-memory";
import type { ViewerMeshTransport } from "@shared/mod-viewer/types";

import {
    decodeModelViewerGeometry,
    type ModelViewerPositionGeometry,
} from "./model-viewer-position-codec";

export interface PositionVariantLoader {
    load(
        descriptor: ViewerMeshTransport["positionVariants"][number],
        vertexCount: number,
        signal?: AbortSignal,
    ): Promise<ModelViewerPositionGeometry>;
}

export class ModelViewerPositionLoader implements PositionVariantLoader {
    private readonly lifetime = new AbortController();

    async load(
        descriptor: ViewerMeshTransport["positionVariants"][number],
        vertexCount: number,
        signal?: AbortSignal,
    ): Promise<ModelViewerPositionGeometry> {
        const combined = signal
            ? AbortSignal.any([signal, this.lifetime.signal])
            : this.lifetime.signal;
        combined.throwIfAborted();
        const bytes = await fetchBinaryBytes(
            descriptor.geometryUrl,
            80 + vertexCount * 24,
            combined,
        );
        combined.throwIfAborted();
        return decodeModelViewerGeometry(bytes.buffer as ArrayBuffer, vertexCount);
    }

    dispose(): void {
        this.lifetime.abort();
    }
}

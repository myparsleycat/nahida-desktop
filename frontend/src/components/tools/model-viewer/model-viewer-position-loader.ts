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

const DEFAULT_CACHE_BYTES = 32 * 1024 * 1024;

type CacheEntry = {
    geometry: ModelViewerPositionGeometry;
    bytes: number;
};

type PendingLoad = {
    controller: AbortController;
    promise: Promise<ModelViewerPositionGeometry>;
    users: number;
    settled: boolean;
};

export class ModelViewerPositionLoader implements PositionVariantLoader {
    private readonly lifetime = new AbortController();
    private readonly cache = new Map<string, CacheEntry>();
    private readonly inflight = new Map<string, PendingLoad>();
    private cacheBytes = 0;

    constructor(private readonly maxCacheBytes = DEFAULT_CACHE_BYTES) {}

    async load(
        descriptor: ViewerMeshTransport["positionVariants"][number],
        vertexCount: number,
        signal?: AbortSignal,
    ): Promise<ModelViewerPositionGeometry> {
        this.lifetime.signal.throwIfAborted();
        signal?.throwIfAborted();
        const key = cacheKey(descriptor.geometryUrl, vertexCount);
        const cached = this.takeCache(key);
        if (cached) {
            this.putCache(key, cached);
            return cached.geometry;
        }

        const pending = this.inflight.get(key) ?? this.startLoad(key, descriptor, vertexCount);
        pending.users += 1;
        const combined = signal
            ? AbortSignal.any([signal, this.lifetime.signal])
            : this.lifetime.signal;
        try {
            return await awaitWithSignal(pending.promise, combined);
        } finally {
            pending.users -= 1;

            // Give a frame transition the current turn to acquire a shared
            // prefetch before cancelling work with no remaining consumers.
            queueMicrotask(() => {
                if (pending.users === 0 && !pending.settled) {
                    if (this.inflight.get(key) === pending) this.inflight.delete(key);
                    pending.controller.abort();
                }
            });
        }
    }

    dispose(): void {
        this.lifetime.abort();
        this.cache.clear();
        this.inflight.clear();
        this.cacheBytes = 0;
    }

    private startLoad(
        key: string,
        descriptor: ViewerMeshTransport["positionVariants"][number],
        vertexCount: number,
    ): PendingLoad {
        const controller = new AbortController();
        const combined = AbortSignal.any([controller.signal, this.lifetime.signal]);
        const promise = fetchBinaryBytes(descriptor.geometryUrl, 80 + vertexCount * 24, combined)
            .then((bytes) => {
                combined.throwIfAborted();
                const geometry = decodeModelViewerGeometry(
                    bytes.buffer as ArrayBuffer,
                    vertexCount,
                );
                this.putCache(key, { geometry, bytes: bytes.byteLength });
                return geometry;
            })
            .finally(() => {
                pending.settled = true;
                if (this.inflight.get(key) === pending) {
                    this.inflight.delete(key);
                }
            });
        const pending: PendingLoad = { controller, promise, users: 0, settled: false };
        this.inflight.set(key, pending);
        return pending;
    }

    private takeCache(key: string): CacheEntry | undefined {
        const entry = this.cache.get(key);
        if (!entry) {
            return undefined;
        }
        this.cache.delete(key);
        this.cacheBytes -= entry.bytes;
        return entry;
    }

    private putCache(key: string, entry: CacheEntry): void {
        // Oversized frames must not evict useful entries or exceed the budget.
        if (entry.bytes > this.maxCacheBytes) return;
        this.takeCache(key);
        while (this.cache.size > 0 && this.cacheBytes + entry.bytes > this.maxCacheBytes) {
            const oldest = this.cache.keys().next().value;
            if (oldest === undefined) {
                break;
            }
            this.takeCache(oldest);
        }
        this.cache.set(key, entry);
        this.cacheBytes += entry.bytes;
    }
}

function cacheKey(geometryUrl: string, vertexCount: number): string {
    return `${vertexCount}:${geometryUrl}`;
}

function awaitWithSignal<T>(pending: Promise<T>, signal: AbortSignal): Promise<T> {
    if (signal.aborted) {
        return Promise.reject(abortReason(signal));
    }
    return new Promise((resolve, reject) => {
        const onAbort = () => {
            signal.removeEventListener("abort", onAbort);
            reject(abortReason(signal));
        };
        signal.addEventListener("abort", onAbort, { once: true });
        pending.then(
            (value) => {
                signal.removeEventListener("abort", onAbort);
                if (signal.aborted) {
                    reject(abortReason(signal));
                    return;
                }
                resolve(value);
            },
            (error: unknown) => {
                signal.removeEventListener("abort", onAbort);
                if (signal.aborted) {
                    reject(abortReason(signal));
                    return;
                }
                reject(error);
            },
        );
    });
}

function abortReason(signal: AbortSignal): unknown {
    return signal.reason ?? new DOMException("Aborted", "AbortError");
}

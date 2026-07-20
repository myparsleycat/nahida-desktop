import { Transform } from "node:stream";

import type { BandwidthLimiter } from "./bandwidth-limiter";
import type { SlowChunkTransferPhase } from "./slow-chunk-monitor";

export function createBandwidthLimitTransform(
    limiter: BandwidthLimiter,
    options?: {
        signal?: AbortSignal;
        onPhaseChange?: (
            phase: Extract<SlowChunkTransferPhase, "network" | "bandwidth-wait">,
        ) => void;
    },
) {
    return new Transform({
        transform(chunk: Buffer, _encoding, callback) {
            const run = async () => {
                let waited = false;
                await limiter.take(chunk.byteLength, {
                    signal: options?.signal,
                    onWait: () => {
                        waited = true;
                        options?.onPhaseChange?.("bandwidth-wait");
                    },
                });
                if (waited) {
                    options?.onPhaseChange?.("network");
                }
                return chunk;
            };

            run().then(
                (data) => callback(null, data),
                (error) => callback(error instanceof Error ? error : new Error(String(error))),
            );
        },
    });
}

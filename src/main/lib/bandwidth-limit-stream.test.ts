import { Transform } from "node:stream";

import { afterEach, describe, expect, it, vi } from "vitest";

import { createBandwidthLimitTransform } from "./bandwidth-limit-stream";
import { BandwidthLimiter } from "./bandwidth-limiter";
import { SlowChunkMonitor } from "./slow-chunk-monitor";

describe("createBandwidthLimitTransform", () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it("preserves network samples when the limiter does not wait", async () => {
        vi.useFakeTimers();
        const monitor = new SlowChunkMonitor();
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024,
            attemptController: new AbortController(),
            slowReconnects: 0,
        });
        const stream = createBandwidthLimitTransform(new BandwidthLimiter(), {
            onPhaseChange: (phase) => monitor.setPhase(transfer.key, phase),
        });
        stream.resume();

        await new Promise<void>((resolve, reject) => {
            stream.write(Buffer.alloc(100), (error) => {
                if (error) {
                    reject(error);
                    return;
                }
                monitor.recordSample(transfer.key, 100);
                resolve();
            });
        });
        await vi.advanceTimersByTimeAsync(600);
        await new Promise<void>((resolve, reject) => {
            stream.write(Buffer.alloc(100), (error) => {
                if (error) {
                    reject(error);
                    return;
                }
                monitor.recordSample(transfer.key, 200);
                resolve();
            });
        });

        expect(transfer.samples.at(-1)!.t - transfer.samples[0]!.t).toBe(600);
        expect(transfer.transferredBytes).toBe(200);

        stream.destroy();
        monitor.unregister(transfer.key);
    });

    it("changes phase only while a chunk actually waits for bandwidth", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);
        const phases: string[] = [];
        const stream = createBandwidthLimitTransform(limiter, {
            onPhaseChange: (phase) => phases.push(phase),
        });
        stream.pipe(
            new Transform({
                transform(chunk, _encoding, callback) {
                    callback(null, chunk);
                },
            }),
        );

        await new Promise<void>((resolve, reject) => {
            stream.write(Buffer.alloc(1000), (error) => {
                if (error) {
                    reject(error);
                    return;
                }
                resolve();
            });
        });
        expect(phases).toEqual([]);

        const pending = new Promise<void>((resolve, reject) => {
            stream.write(Buffer.alloc(1000), (error) => {
                if (error) {
                    reject(error);
                    return;
                }
                resolve();
            });
        });
        await vi.advanceTimersByTimeAsync(0);
        expect(phases).toEqual(["bandwidth-wait"]);

        await vi.advanceTimersByTimeAsync(1000);
        await pending;
        expect(phases).toEqual(["bandwidth-wait", "network"]);

        stream.destroy();
    });
});

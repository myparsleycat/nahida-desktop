import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SLOW_CHUNK_MAX_RECONNECTS, SlowChunkMonitor } from "./slow-chunk-monitor";

describe("SlowChunkMonitor", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("aborts a stalled chunk after observe and stall timeout", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024 * 1024,
            attemptController,
            slowReconnects: 0,
        });

        vi.advanceTimersByTime(3_000 + 15_000);
        monitor.evaluateNow();

        expect(transfer.abortReason).toBe("slow-chunk");
        expect(transfer.detect).toBe("stall");
        expect(attemptController.signal.aborted).toBe(true);

        monitor.unregister(transfer.key);
    });

    it("aborts the slowest relative chunk after two slow ticks", () => {
        const monitor = new SlowChunkMonitor();
        const peerControllerA = new AbortController();
        const peerControllerB = new AbortController();
        const slowController = new AbortController();
        const peerA = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024 * 1024,
            attemptController: peerControllerA,
            slowReconnects: 0,
        });
        const peerB = monitor.register({
            fileId: "file-a",
            chunkIndex: 2,
            chunkSize: 1024 * 1024,
            attemptController: peerControllerB,
            slowReconnects: 0,
        });
        const slow = monitor.register({
            fileId: "file-a",
            chunkIndex: 1,
            chunkSize: 1024 * 1024,
            attemptController: slowController,
            slowReconnects: 0,
        });

        vi.advanceTimersByTime(3_000);
        monitor.recordSample(peerA.key, 256 * 1024);
        monitor.recordSample(peerB.key, 256 * 1024);
        monitor.recordSample(slow.key, 4 * 1024);
        vi.advanceTimersByTime(1_000);
        monitor.recordSample(peerA.key, 512 * 1024);
        monitor.recordSample(peerB.key, 512 * 1024);
        monitor.recordSample(slow.key, 8 * 1024);

        monitor.evaluateNow();
        expect(slow.abortReason).toBeNull();

        monitor.evaluateNow();
        expect(slow.abortReason).toBe("slow-chunk");
        expect(slow.detect).toBe("relative");
        expect(slowController.signal.aborted).toBe(true);
        expect(peerA.abortReason).toBeNull();
        expect(peerB.abortReason).toBeNull();
        expect(peerControllerA.signal.aborted).toBe(false);
        expect(peerControllerB.signal.aborted).toBe(false);

        monitor.unregister(peerA.key);
        monitor.unregister(peerB.key);
        monitor.unregister(slow.key);
    });

    it("requires at least two comparable peers for a relative abort", () => {
        const monitor = new SlowChunkMonitor();
        const peer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024 * 1024,
            cohortKey: "cdn-a",
            attemptController: new AbortController(),
            slowReconnects: 0,
        });
        const slowController = new AbortController();
        const slow = monitor.register({
            fileId: "file-a",
            chunkIndex: 1,
            chunkSize: 1024 * 1024,
            cohortKey: "cdn-a",
            attemptController: slowController,
            slowReconnects: 0,
        });

        vi.advanceTimersByTime(3_000);
        monitor.recordSample(peer.key, 256 * 1024);
        monitor.recordSample(slow.key, 4 * 1024);
        vi.advanceTimersByTime(1_000);
        monitor.recordSample(peer.key, 512 * 1024);
        monitor.recordSample(slow.key, 8 * 1024);

        monitor.evaluateNow();
        monitor.evaluateNow();

        expect(slow.abortReason).toBeNull();
        expect(slowController.signal.aborted).toBe(false);

        monitor.unregister(peer.key);
        monitor.unregister(slow.key);
    });

    it("aborts a sustained absolute slow transfer without comparable peers", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 10 * 1024 * 1024,
            attemptController,
            slowReconnects: 0,
        });

        for (let second = 1; second <= 6; second++) {
            vi.advanceTimersByTime(1_000);
            monitor.recordSample(transfer.key, second * 40 * 1024);
        }

        expect(transfer.abortReason).toBeNull();

        vi.advanceTimersByTime(1_000);
        monitor.recordSample(transfer.key, 7 * 40 * 1024);

        expect(transfer.abortReason).toBe("slow-chunk");
        expect(transfer.detect).toBe("absolute");
        expect(attemptController.signal.aborted).toBe(true);

        monitor.unregister(transfer.key);
    });

    it("lets an absolute slow transfer finish when less than thirty seconds remain", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const initialTransferredBytes = 1024 * 1024;
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 2 * 1024 * 1024,
            initialTransferredBytes,
            attemptController,
            slowReconnects: 0,
        });

        for (let second = 1; second <= 10; second++) {
            vi.advanceTimersByTime(1_000);
            monitor.recordSample(transfer.key, initialTransferredBytes + second * 40 * 1024);
        }

        expect(transfer.abortReason).toBeNull();
        expect(attemptController.signal.aborted).toBe(false);

        monitor.unregister(transfer.key);
    });

    it("skips relative abort when the chunk is near complete", () => {
        const monitor = new SlowChunkMonitor();
        const peerController = new AbortController();
        const nearController = new AbortController();
        const chunkSize = 1024 * 1024;
        const peer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize,
            attemptController: peerController,
            slowReconnects: 0,
        });
        const near = monitor.register({
            fileId: "file-a",
            chunkIndex: 1,
            chunkSize,
            attemptController: nearController,
            slowReconnects: 0,
        });

        vi.advanceTimersByTime(3_000);
        monitor.recordSample(peer.key, 256 * 1024);
        monitor.recordSample(near.key, Math.floor(chunkSize * 0.9));
        vi.advanceTimersByTime(1_000);
        monitor.recordSample(peer.key, 512 * 1024);
        monitor.recordSample(near.key, Math.floor(chunkSize * 0.9) + 1_000);

        monitor.evaluateNow();
        monitor.evaluateNow();

        expect(near.abortReason).toBeNull();
        expect(nearController.signal.aborted).toBe(false);

        monitor.unregister(peer.key);
        monitor.unregister(near.key);
    });

    it("extends the stall timeout for a near-complete chunk but eventually aborts it", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const chunkSize = 1024 * 1024;
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize,
            attemptController,
            slowReconnects: 0,
        });
        monitor.recordSample(transfer.key, Math.floor(chunkSize * 0.99));

        vi.advanceTimersByTime(15_000);
        monitor.evaluateNow();

        expect(transfer.abortReason).toBeNull();
        expect(attemptController.signal.aborted).toBe(false);

        vi.advanceTimersByTime(30_000);
        monitor.evaluateNow();

        expect(transfer.abortReason).toBe("slow-chunk");
        expect(transfer.detect).toBe("stall");
        expect(attemptController.signal.aborted).toBe(true);

        monitor.unregister(transfer.key);
    });

    it("applies near-complete protection before a resumed transfer receives another byte", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const chunkSize = 1024 * 1024;
        const initialTransferredBytes = Math.floor(chunkSize * 0.99);
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize,
            initialTransferredBytes,
            attemptController,
            slowReconnects: 0,
        });

        vi.advanceTimersByTime(15_000);
        monitor.evaluateNow();

        expect(transfer.transferredBytes).toBe(initialTransferredBytes);
        expect(transfer.samples).toEqual([{ t: transfer.startedAt, b: initialTransferredBytes }]);
        expect(transfer.abortReason).toBeNull();
        expect(attemptController.signal.aborted).toBe(false);

        vi.advanceTimersByTime(30_000);
        monitor.evaluateNow();
        expect(transfer.detect).toBe("stall");
        expect(attemptController.signal.aborted).toBe(true);

        monitor.unregister(transfer.key);
    });

    it.each(["bandwidth-wait", "disk-write", "processing"] as const)(
        "does not count %s time toward the network stall timeout",
        (phase) => {
            const monitor = new SlowChunkMonitor();
            const attemptController = new AbortController();
            const transfer = monitor.register({
                fileId: "file-a",
                chunkIndex: 0,
                chunkSize: 1024 * 1024,
                attemptController,
                slowReconnects: 0,
            });

            monitor.setPhase(transfer.key, phase);
            vi.advanceTimersByTime(60_000);
            monitor.evaluateNow();

            expect(transfer.abortReason).toBeNull();
            expect(attemptController.signal.aborted).toBe(false);

            monitor.setPhase(transfer.key, "network");
            vi.advanceTimersByTime(14_999);
            monitor.evaluateNow();
            expect(transfer.abortReason).toBeNull();

            vi.advanceTimersByTime(1);
            monitor.evaluateNow();
            expect(transfer.detect).toBe("stall");
            expect(attemptController.signal.aborted).toBe(true);

            monitor.unregister(transfer.key);
        },
    );

    it("keeps transferred bytes monotonic when a stale sample reports fewer bytes", () => {
        const monitor = new SlowChunkMonitor();
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024 * 1024,
            attemptController: new AbortController(),
            slowReconnects: 0,
        });

        monitor.recordSample(transfer.key, 512 * 1024);
        vi.advanceTimersByTime(1_000);
        monitor.recordSample(transfer.key, 256 * 1024);

        expect(transfer.transferredBytes).toBe(512 * 1024);
        expect(transfer.samples.at(-1)?.b).toBe(512 * 1024);

        monitor.unregister(transfer.key);
    });

    it("keeps simultaneous registrations for the same file and chunk independent", () => {
        const monitor = new SlowChunkMonitor();
        const first = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024,
            attemptController: new AbortController(),
            slowReconnects: 0,
        });
        const second = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024,
            attemptController: new AbortController(),
            slowReconnects: 0,
        });

        expect(first.key).not.toBe(second.key);

        monitor.recordSample(first.key, 256);
        expect(first.transferredBytes).toBe(256);
        expect(second.transferredBytes).toBe(0);

        monitor.unregister(first.key);
        monitor.recordSample(second.key, 512);
        expect(second.transferredBytes).toBe(512);

        monitor.unregister(second.key);
    });

    it("does not abort when slow reconnects are exhausted", () => {
        const monitor = new SlowChunkMonitor();
        const attemptController = new AbortController();
        const transfer = monitor.register({
            fileId: "file-a",
            chunkIndex: 0,
            chunkSize: 1024 * 1024,
            attemptController,
            slowReconnects: SLOW_CHUNK_MAX_RECONNECTS,
        });

        vi.advanceTimersByTime(3_000 + 15_000);
        monitor.evaluateNow();

        expect(transfer.abortReason).toBeNull();
        expect(attemptController.signal.aborted).toBe(false);

        monitor.unregister(transfer.key);
    });
});

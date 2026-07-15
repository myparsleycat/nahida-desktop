import { afterEach, describe, expect, it, vi } from "vitest";

import { BandwidthLimiter } from "./bandwidth-limiter";

describe("BandwidthLimiter", () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it("allows take immediately when unlimited", async () => {
        const limiter = new BandwidthLimiter();
        await expect(limiter.take(1024 * 1024)).resolves.toBeUndefined();
    });

    it("rate-limits a single take", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);

        const first = limiter.take(1000);
        await expect(first).resolves.toBeUndefined();

        const second = limiter.take(1000);
        let settled = false;
        void second.then(() => {
            settled = true;
        });

        await vi.advanceTimersByTimeAsync(500);
        expect(settled).toBe(false);

        await vi.advanceTimersByTimeAsync(500);
        await expect(second).resolves.toBeUndefined();
        expect(settled).toBe(true);
    });

    it("allows a take larger than the one-second bucket capacity after enough refill", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);

        const pending = limiter.take(2500);
        let settled = false;
        void pending.then(() => {
            settled = true;
        });

        await vi.advanceTimersByTimeAsync(1499);
        expect(settled).toBe(false);

        await vi.advanceTimersByTimeAsync(1);
        await expect(pending).resolves.toBeUndefined();
        expect(settled).toBe(true);
    });

    it("shares capacity across concurrent takes in FIFO order", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);

        const order: number[] = [];
        const first = limiter.take(1000).then(() => {
            order.push(1);
        });
        const second = limiter.take(1000).then(() => {
            order.push(2);
        });

        await vi.advanceTimersByTimeAsync(0);
        await first;
        expect(order).toEqual([1]);

        await vi.advanceTimersByTimeAsync(1000);
        await second;
        expect(order).toEqual([1, 2]);
    });

    it("releases waiters when rate becomes unlimited", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1);

        const pending = limiter.take(1_000_000);
        let settled = false;
        void pending.then(() => {
            settled = true;
        });

        await vi.advanceTimersByTimeAsync(10);
        expect(settled).toBe(false);

        limiter.setRateBps(0);
        await expect(pending).resolves.toBeUndefined();
        expect(settled).toBe(true);
    });

    it("rejects take when aborted while waiting", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1);
        const controller = new AbortController();

        const pending = limiter.take(1_000_000, { signal: controller.signal });
        await vi.advanceTimersByTimeAsync(10);
        controller.abort();

        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    });

    it("lets the next waiter use accrued tokens when the head waiter aborts", async () => {
        vi.useFakeTimers();
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);
        const controller = new AbortController();

        await limiter.take(1000);
        const head = limiter.take(1000, { signal: controller.signal });
        const next = limiter.take(500);

        await vi.advanceTimersByTimeAsync(500);
        controller.abort();

        await expect(head).rejects.toMatchObject({ name: "AbortError" });
        await expect(next).resolves.toBeUndefined();
    });

    it("rejects take immediately when already aborted", async () => {
        const limiter = new BandwidthLimiter();
        limiter.setRateBps(1000);
        const controller = new AbortController();
        controller.abort();

        await expect(limiter.take(100, { signal: controller.signal })).rejects.toMatchObject({
            name: "AbortError",
        });
    });
});

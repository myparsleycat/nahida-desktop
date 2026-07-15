type Waiter = {
    bytes: number;
    resolve: () => void;
    reject: (error: Error) => void;
    signal?: AbortSignal;
    onAbort?: () => void;
};

export class BandwidthLimiter {
    private rateBps = 0;
    private tokens = 0;
    private lastRefillAt = Date.now();
    private readonly waiters: Waiter[] = [];
    private timer: ReturnType<typeof setTimeout> | null = null;

    public setRateBps(rateBps: number) {
        this.refill();
        this.rateBps = rateBps > 0 && Number.isFinite(rateBps) ? rateBps : 0;
        if (this.rateBps <= 0) {
            this.tokens = 0;
            this.clearTimer();
            this.resolveAllWaiters();
            return;
        }
        this.tokens = this.rateBps;
        this.lastRefillAt = Date.now();
        this.drain();
    }

    public async take(
        bytes: number,
        options?: { signal?: AbortSignal; onWait?: () => void },
    ): Promise<void> {
        if (bytes <= 0 || this.rateBps <= 0) {
            return;
        }

        if (options?.signal?.aborted) {
            throw new DOMException("The operation was aborted.", "AbortError");
        }

        this.refill();
        if (this.waiters.length === 0 && this.tokens >= bytes) {
            this.tokens -= bytes;
            return;
        }

        options?.onWait?.();

        return new Promise<void>((resolve, reject) => {
            const waiter: Waiter = { bytes, resolve, reject, signal: options?.signal };
            if (options?.signal) {
                waiter.onAbort = () => {
                    const index = this.waiters.indexOf(waiter);
                    if (index >= 0) {
                        this.waiters.splice(index, 1);
                    }
                    reject(new DOMException("The operation was aborted.", "AbortError"));
                    this.drain();
                };
                options.signal.addEventListener("abort", waiter.onAbort, { once: true });
            }
            this.waiters.push(waiter);
            this.drain();
        });
    }

    private refill() {
        if (this.rateBps <= 0) {
            this.lastRefillAt = Date.now();
            return;
        }

        const now = Date.now();
        const elapsedMs = now - this.lastRefillAt;
        if (elapsedMs <= 0) {
            return;
        }

        this.tokens = Math.min(
            Math.max(this.rateBps, this.waiters[0]?.bytes ?? 0),
            this.tokens + (this.rateBps * elapsedMs) / 1000,
        );
        this.lastRefillAt = now;
    }

    private drain() {
        if (this.rateBps <= 0) {
            this.resolveAllWaiters();
            return;
        }

        this.refill();
        while (this.waiters.length > 0) {
            const waiter = this.waiters[0];
            if (!waiter) {
                break;
            }
            if (this.tokens < waiter.bytes) {
                break;
            }
            this.waiters.shift();
            this.tokens -= waiter.bytes;
            if (waiter.signal && waiter.onAbort) {
                waiter.signal.removeEventListener("abort", waiter.onAbort);
            }
            waiter.resolve();
        }
        this.schedule();
    }

    private schedule() {
        this.clearTimer();
        if (this.rateBps <= 0 || this.waiters.length === 0) {
            return;
        }

        const waiter = this.waiters[0];
        if (!waiter) {
            return;
        }

        const deficit = waiter.bytes - this.tokens;
        if (deficit <= 0) {
            this.drain();
            return;
        }

        const delayMs = Math.max(1, Math.ceil((deficit / this.rateBps) * 1000));
        this.timer = setTimeout(() => {
            this.timer = null;
            this.drain();
        }, delayMs);
        this.timer.unref?.();
    }

    private resolveAllWaiters() {
        while (this.waiters.length > 0) {
            const waiter = this.waiters.shift();
            if (!waiter) {
                continue;
            }
            if (waiter.signal && waiter.onAbort) {
                waiter.signal.removeEventListener("abort", waiter.onAbort);
            }
            waiter.resolve();
        }
    }

    private clearTimer() {
        if (!this.timer) {
            return;
        }
        clearTimeout(this.timer);
        this.timer = null;
    }
}

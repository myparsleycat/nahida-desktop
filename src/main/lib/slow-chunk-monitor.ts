export type SlowChunkDetect = "stall" | "relative";
export type SlowChunkTransferPhase = "network" | "bandwidth-wait" | "disk-write" | "processing";

export type InFlightChunkTransfer = {
    key: string;
    fileId: string;
    chunkIndex: number;
    chunkSize: number;
    cohortKey: string;
    startedAt: number;
    lastProgressAt: number;
    samples: ByteSample[];
    transferredBytes: number;
    attemptController: AbortController;
    slowReconnects: number;
    slowTickCount: number;
    abortReason: "slow-chunk" | null;
    detect: SlowChunkDetect | null;
    chunkSpeedBps: number;
    peerMedianBps: number;
    phase: SlowChunkTransferPhase;
    phaseStartedAt: number;
};

type ByteSample = { t: number; b: number };

type SlowAbortCandidate = {
    entry: InFlightChunkTransfer;
    speed: number | null;
    peerMedianBps: number;
    detect: SlowChunkDetect;
};

export const SLOW_CHUNK_MAX_RECONNECTS = 2;
export const SLOW_CHUNK_THRESHOLD_RATIO = 0.25;
const SLOW_CHUNK_MIN_OBSERVE_MS = 3000;
const SLOW_CHUNK_MIN_PEERS = 2;
const SLOW_CHUNK_CHECK_INTERVAL_MS = 1000;
const SLOW_CHUNK_SPEED_WINDOW_MS = 2000;
const SLOW_CHUNK_MIN_SPEED_SAMPLE_SPAN_MS = 500;
const SLOW_CHUNK_NEAR_COMPLETE_RATIO = 0.85;
const SLOW_CHUNK_REQUIRED_SLOW_TICKS = 2;
const SLOW_CHUNK_MIN_ABSOLUTE_BPS = 64 * 1024;
const SLOW_CHUNK_RECONNECT_DELAY_MS = 500;
const SLOW_CHUNK_RECONNECT_JITTER_MS = 250;
const SLOW_CHUNK_STALL_TIMEOUT_MS = 15_000;
const SLOW_CHUNK_NEAR_COMPLETE_STALL_TIMEOUT_MS = 45_000;
const SLOW_CHUNK_NEAR_COMPLETE_REMAINING_BYTES = 256 * 1024;
const SLOW_CHUNK_SHORT_REMAINING_SECONDS = 10;

export function isAbortError(error: unknown) {
    return (
        (error instanceof DOMException && error.name === "AbortError") ||
        (error instanceof Error && error.name === "AbortError")
    );
}

export function sleepWithAbort(ms: number, signal: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
        if (signal.aborted) {
            reject(new DOMException("The operation was aborted.", "AbortError"));
            return;
        }

        const timer = setTimeout(() => {
            signal.removeEventListener("abort", onAbort);
            resolve();
        }, ms);
        const onAbort = () => {
            clearTimeout(timer);
            signal.removeEventListener("abort", onAbort);
            reject(new DOMException("The operation was aborted.", "AbortError"));
        };
        signal.addEventListener("abort", onAbort, { once: true });
    });
}

export function slowReconnectDelayMs() {
    return (
        SLOW_CHUNK_RECONNECT_DELAY_MS +
        Math.floor(Math.random() * (SLOW_CHUNK_RECONNECT_JITTER_MS + 1))
    );
}

/** null = not measurable; number = measured bps (including 0 stall). */
function speedFromSamples(samples: ByteSample[], now: number): number | null {
    const window = samples.filter((sample) => now - sample.t <= SLOW_CHUNK_SPEED_WINDOW_MS);
    if (window.length < 2) {
        return null;
    }

    const first = window[0];
    const last = window[window.length - 1];
    if (!first || !last) {
        return null;
    }

    const elapsedMs = last.t - first.t;
    if (elapsedMs < SLOW_CHUNK_MIN_SPEED_SAMPLE_SPAN_MS) {
        return null;
    }

    return Math.max(0, (last.b - first.b) / (elapsedMs / 1000));
}

function median(values: number[]) {
    if (values.length === 0) {
        return 0;
    }

    const sorted = [...values].sort((left, right) => left - right);
    const mid = Math.floor(sorted.length / 2);
    if (sorted.length % 2 === 0) {
        return ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2;
    }
    return sorted[mid] ?? 0;
}

function pickOldestCandidate(candidates: SlowAbortCandidate[]) {
    let best: SlowAbortCandidate | null = null;
    for (const candidate of candidates) {
        if (!best || candidate.entry.startedAt < best.entry.startedAt) {
            best = candidate;
        }
    }
    return best;
}

function pickSlowestCandidate(candidates: SlowAbortCandidate[]) {
    let best: SlowAbortCandidate | null = null;
    for (const candidate of candidates) {
        if (!best) {
            best = candidate;
            continue;
        }
        const candidateSpeed = candidate.speed ?? Number.POSITIVE_INFINITY;
        const bestSpeed = best.speed ?? Number.POSITIVE_INFINITY;
        if (candidateSpeed < bestSpeed) {
            best = candidate;
        }
    }
    return best;
}

export class SlowChunkMonitor {
    private readonly inFlightTransfers = new Map<string, InFlightChunkTransfer>();
    private slowChunkMonitorTimer: ReturnType<typeof setInterval> | null = null;
    private registrationSequence = 0;

    public register(input: {
        fileId: string;
        chunkIndex: number;
        chunkSize: number;
        cohortKey?: string;
        initialTransferredBytes?: number;
        attemptController: AbortController;
        slowReconnects: number;
    }) {
        const now = Date.now();
        const key = `${input.fileId}:${input.chunkIndex}:${this.registrationSequence++}`;
        const initialTransferredBytes = Math.max(
            0,
            Math.min(input.chunkSize, input.initialTransferredBytes ?? 0),
        );
        const transfer: InFlightChunkTransfer = {
            key,
            fileId: input.fileId,
            chunkIndex: input.chunkIndex,
            chunkSize: input.chunkSize,
            cohortKey: input.cohortKey ?? input.fileId,
            startedAt: now,
            lastProgressAt: now,
            samples: [{ t: now, b: initialTransferredBytes }],
            transferredBytes: initialTransferredBytes,
            attemptController: input.attemptController,
            slowReconnects: input.slowReconnects,
            slowTickCount: 0,
            abortReason: null,
            detect: null,
            chunkSpeedBps: 0,
            peerMedianBps: 0,
            phase: "network",
            phaseStartedAt: now,
        };
        this.inFlightTransfers.set(key, transfer);
        this.ensureMonitor();
        return transfer;
    }

    public recordSample(key: string, transferredBytes: number) {
        const transfer = this.inFlightTransfers.get(key);
        if (!transfer) {
            return;
        }

        const now = Date.now();
        const normalized = Math.max(transfer.transferredBytes, transferredBytes);
        if (normalized > transfer.transferredBytes) {
            transfer.lastProgressAt = now;
        }
        transfer.transferredBytes = normalized;
        transfer.samples.push({ t: now, b: normalized });
        transfer.samples = transfer.samples.filter(
            (sample) => now - sample.t <= SLOW_CHUNK_SPEED_WINDOW_MS,
        );
    }

    public setPhase(key: string, phase: SlowChunkTransferPhase) {
        const transfer = this.inFlightTransfers.get(key);
        if (!transfer || transfer.phase === phase) {
            return;
        }

        const now = Date.now();
        transfer.phase = phase;
        transfer.phaseStartedAt = now;
        transfer.slowTickCount = 0;
        if (phase === "network") {
            transfer.lastProgressAt = now;
            transfer.samples = [{ t: now, b: transfer.transferredBytes }];
        }
    }

    public unregister(key: string) {
        this.inFlightTransfers.delete(key);
        this.stopIfIdle();
    }

    /** Exposed for unit tests with fake timers. */
    public evaluateNow() {
        this.evaluateSlowChunks();
    }

    private ensureMonitor() {
        if (this.slowChunkMonitorTimer) {
            return;
        }

        this.slowChunkMonitorTimer = setInterval(() => {
            this.evaluateSlowChunks();
        }, SLOW_CHUNK_CHECK_INTERVAL_MS);
        this.slowChunkMonitorTimer.unref?.();
    }

    private stopIfIdle() {
        if (this.inFlightTransfers.size > 0 || !this.slowChunkMonitorTimer) {
            return;
        }

        clearInterval(this.slowChunkMonitorTimer);
        this.slowChunkMonitorTimer = null;
    }

    private evaluateSlowChunks() {
        const entries = [...this.inFlightTransfers.values()];
        if (entries.length === 0) {
            return;
        }

        const now = Date.now();
        const scored = entries.map((entry) => ({
            entry,
            speed: speedFromSamples(entry.samples, now),
        }));

        const stallCandidates: SlowAbortCandidate[] = [];
        const relativeCandidates: SlowAbortCandidate[] = [];

        for (const { entry, speed } of scored) {
            if (entry.slowReconnects >= SLOW_CHUNK_MAX_RECONNECTS) {
                entry.slowTickCount = 0;
                continue;
            }
            if (entry.abortReason !== null || entry.attemptController.signal.aborted) {
                continue;
            }
            if (entry.phase !== "network") {
                entry.slowTickCount = 0;
                continue;
            }

            const observedMs = now - entry.startedAt;
            if (observedMs < SLOW_CHUNK_MIN_OBSERVE_MS) {
                entry.slowTickCount = 0;
                continue;
            }

            const remainingBytes = Math.max(0, entry.chunkSize - entry.transferredBytes);
            const completionRatio =
                entry.chunkSize > 0 ? entry.transferredBytes / entry.chunkSize : 0;
            const nearComplete =
                entry.chunkSize > 0 &&
                (completionRatio >= SLOW_CHUNK_NEAR_COMPLETE_RATIO ||
                    (completionRatio >= 0.5 &&
                        remainingBytes <= SLOW_CHUNK_NEAR_COMPLETE_REMAINING_BYTES));
            const estimatedRemainingSeconds =
                speed !== null && speed > 0 ? remainingBytes / speed : Number.POSITIVE_INFINITY;
            const shouldProtectRelative =
                nearComplete || estimatedRemainingSeconds <= SLOW_CHUNK_SHORT_REMAINING_SECONDS;
            const stallTimeoutMs = nearComplete
                ? SLOW_CHUNK_NEAR_COMPLETE_STALL_TIMEOUT_MS
                : SLOW_CHUNK_STALL_TIMEOUT_MS;
            const isStalled = now - entry.lastProgressAt >= stallTimeoutMs;
            if (isStalled) {
                entry.slowTickCount = 0;
                stallCandidates.push({
                    entry,
                    speed,
                    peerMedianBps: 0,
                    detect: "stall",
                });
                continue;
            }

            if (shouldProtectRelative) {
                entry.slowTickCount = 0;
                continue;
            }

            if (speed === null || speed >= SLOW_CHUNK_MIN_ABSOLUTE_BPS) {
                entry.slowTickCount = 0;
                continue;
            }

            const peerMedianBps = this.peerMedianBps(scored, entry);
            if (peerMedianBps <= 0) {
                entry.slowTickCount = 0;
                continue;
            }

            if (speed >= peerMedianBps * SLOW_CHUNK_THRESHOLD_RATIO) {
                entry.slowTickCount = 0;
                continue;
            }

            entry.slowTickCount += 1;
            if (entry.slowTickCount >= SLOW_CHUNK_REQUIRED_SLOW_TICKS) {
                relativeCandidates.push({
                    entry,
                    speed,
                    peerMedianBps,
                    detect: "relative",
                });
            }
        }

        const pick =
            pickOldestCandidate(stallCandidates) ?? pickSlowestCandidate(relativeCandidates);
        if (!pick) {
            return;
        }

        pick.entry.abortReason = "slow-chunk";
        pick.entry.detect = pick.detect;
        pick.entry.chunkSpeedBps = pick.speed ?? 0;
        pick.entry.peerMedianBps = pick.peerMedianBps;
        pick.entry.attemptController.abort();
    }

    private peerMedianBps(
        scored: Array<{ entry: InFlightChunkTransfer; speed: number | null }>,
        candidate: InFlightChunkTransfer,
    ) {
        const positiveSpeeds = (items: typeof scored) =>
            items
                .filter(
                    (item) =>
                        item.entry.key !== candidate.key &&
                        item.entry.cohortKey === candidate.cohortKey &&
                        item.entry.phase === "network" &&
                        item.entry.abortReason === null &&
                        item.speed !== null &&
                        item.speed > 0,
                )
                .map((item) => item.speed as number);

        const sameFilePeers = positiveSpeeds(
            scored.filter((item) => item.entry.fileId === candidate.fileId),
        );
        if (sameFilePeers.length >= SLOW_CHUNK_MIN_PEERS) {
            return median(sameFilePeers);
        }

        const globalPeers = positiveSpeeds(scored);
        if (globalPeers.length < SLOW_CHUNK_MIN_PEERS) {
            return 0;
        }
        return median(globalPeers);
    }
}

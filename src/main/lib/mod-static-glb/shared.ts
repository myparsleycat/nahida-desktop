import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";
import LRUCache from "mnemonist/lru-cache";
import type { Logger } from "../../internal/logger";
import type { VariableStateMap, VariableStateValue } from "./types";

const MAX_NORMALIZE_KEY_CACHE = 4096;
const normalizeKeyCache = new LRUCache<string, string>(MAX_NORMALIZE_KEY_CACHE);

export function createWarningCollector(onWarning?: (message: string) => void) {
    let count = 0;
    return {
        get count() {
            return count;
        },
        warn(message: string) {
            count += 1;
            onWarning?.(message);
        },
    };
}

export function createTimedStageLogger(logger: Logger | undefined, scope: string) {
    const startedAt = Date.now();
    let lastCheckpointAt = startedAt;
    return (stage: string, stageStartedAt?: number) => {
        if (!logger) {
            return;
        }

        const now = Date.now();
        const totalElapsedMs = now - startedAt;
        const stageElapsedMs = stageStartedAt ? now - stageStartedAt : now - lastCheckpointAt;
        lastCheckpointAt = now;
        logger.info(`${stage} completed in ${stageElapsedMs}ms (total ${totalElapsedMs}ms)`, scope);
    };
}

export function getStaticGlbWorkConcurrency(maxConcurrency = 8): number {
    return Math.max(1, Math.min(os.availableParallelism(), maxConcurrency));
}

export function mapToRecord(
    input: Map<string, number | string>,
    keys: string[],
): Record<string, VariableStateValue> {
    const record: Record<string, VariableStateValue> = {};
    for (const key of keys) {
        record[key] = input.get(normalizeKey(key)) ?? 0;
    }
    return record;
}

export function humanizeVariableLabel(id: string): string {
    return id
        .replace(/^\$+/, "")
        .split(/[._-]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ");
}

export function createStateKey(state: VariableStateMap): string {
    return Object.entries(state)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${normalizeKey(key)}=${String(value)}`)
        .join("&");
}

export function createStateArtifactFileName(stateKey: string): string {
    const sanitized = sanitizeStateKey(stateKey).replace(/^_+|_+$/g, "");
    const digest = crypto.createHash("sha256").update(stateKey).digest("hex").slice(0, 12);
    const prefix = sanitized.slice(0, 80) || "state";
    return `${prefix}-${digest}.glb`;
}

export function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function createTextureCacheBaseName(texturePath: string): string {
    const extensionless = path.basename(texturePath, path.extname(texturePath));
    const digest = crypto
        .createHash("sha256")
        .update(path.resolve(texturePath))
        .digest("hex")
        .slice(0, 12);
    return `${extensionless}-${digest}`;
}

export function normalizeKey(value: string): string {
    const cached = normalizeKeyCache.get(value);
    if (cached !== undefined) {
        return cached;
    }
    const normalized = value.toLowerCase().replace(/[^a-z0-9]/g, "");
    normalizeKeyCache.set(value, normalized);
    return normalized;
}

function sanitizeStateKey(stateKey: string): string {
    return stateKey.replace(/[^a-z0-9=&_-]+/gi, "_").replace(/[=&]/g, "_");
}

export function sanitizeArtifactName(value: string): string {
    const sanitized = value.replace(/[^a-z0-9._-]+/gi, "_").replace(/^_+|_+$/g, "");
    return sanitized || "artifact";
}

import type { AgentContextUsage } from "@bindings/agent/models";

/** Bounded context usage rendered by the agent composer meter. */
export interface ContextOccupancy {
    percent: number;
    usedTokens: number;
    contextWindow: number;
}

/**
 * Resolve display occupancy from a session's context usage. The projection wins over the
 * provider-reported sample so a compaction shows immediately, matching the harness meter.
 * Returns null until both a numerator and a capacity are known.
 */
export function contextOccupancy(
    usage: AgentContextUsage | null | undefined,
): ContextOccupancy | null {
    const usedTokens = usage?.projectedTokens ?? usage?.pressureTokens;
    if (usedTokens === undefined || !usage?.contextWindow) return null;
    return {
        percent: Math.min(100, Math.round((usedTokens / usage.contextWindow) * 100)),
        usedTokens,
        contextWindow: usage.contextWindow,
    };
}

/**
 * Format a token count compactly for the locale: 128000 becomes "128K" in English and "12.8만" in
 * Korean, so the figure reads naturally without a hand-written K/M suffix table.
 */
export function formatTokens(value: number, locale: string): string {
    if (!Number.isFinite(value) || value <= 0) return "0";
    return new Intl.NumberFormat(locale, {
        notation: "compact",
        maximumFractionDigits: 1,
    }).format(value);
}

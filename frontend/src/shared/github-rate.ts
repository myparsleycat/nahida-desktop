import type { GitHubRateState } from "@bindings/tools";

export function formatRateResetText(rateState: GitHubRateState | null) {
    if (!rateState) return null;

    const reset = Math.max(rateState.remaining <= 0 ? rateState.reset : 0, rateState.retryAt ?? 0);
    return reset > 0 ? new Date(reset * 1000).toLocaleString() : null;
}

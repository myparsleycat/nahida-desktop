import type { GitHubRateState } from "@shared/types";
import type { NahidaDesktop } from "..";

const GITHUB_CORE_RATE_KEY = "github:core-rate";
const GITHUB_RATE_LIMIT_URL = "https://api.github.com/rate_limit";

type RateLimitApiResponse = {
    rate?: Partial<GitHubRateState>;
};

export class GitHubRateCoordinator {
    constructor(private readonly desktop: NahidaDesktop) {}

    public async getRateState() {
        const row = await this.desktop.lib.db.appState.get(GITHUB_CORE_RATE_KEY);
        if (!row?.value) return null;
        try {
            return JSON.parse(row.value) as GitHubRateState;
        } catch {
            return null;
        }
    }

    public isRateLimited(rateState: GitHubRateState | null) {
        if (!rateState) return false;
        return rateState.remaining <= 0 && rateState.reset * 1000 > Date.now();
    }

    public async canUseGitHubApi(options?: { refreshIfMissing?: boolean }) {
        const rateState = await this.getRateState();
        if (rateState || !options?.refreshIfMissing) {
            return { allowed: !this.isRateLimited(rateState), rateState };
        }
        const refreshed = await this.refreshRateState();
        return { allowed: !this.isRateLimited(refreshed), rateState: refreshed };
    }

    public async refreshRateState() {
        try {
            const response = await this.desktop.httpService.fetcher(GITHUB_RATE_LIMIT_URL, {
                method: "GET",
                headers: {
                    Accept: "application/vnd.github+json",
                },
            });

            const data = (await response.json()) as RateLimitApiResponse;
            const state =
                this.extractRateStateFromHeaders(response.headers) ??
                normalizeRateState(data.rate ?? null);
            if (state) await this.saveRateState(state);
            return state;
        } catch (error) {
            this.desktop.logger.warn(
                `Failed to refresh GitHub rate state: ${String(error)}`,
                "GitHubRateCoordinator",
            );
            return this.getRateState();
        }
    }

    public async captureFromResponse(response: Response) {
        const state = this.extractRateStateFromHeaders(response.headers);
        if (!state) return null;
        await this.saveRateState(state);
        return state;
    }

    private extractRateStateFromHeaders(headers: Headers) {
        const limit = Number(headers.get("x-ratelimit-limit"));
        const remaining = Number(headers.get("x-ratelimit-remaining"));
        const reset = Number(headers.get("x-ratelimit-reset"));
        const used = Number(headers.get("x-ratelimit-used"));
        const resource = headers.get("x-ratelimit-resource") ?? "core";

        if (
            !Number.isFinite(limit) ||
            !Number.isFinite(remaining) ||
            !Number.isFinite(reset) ||
            !Number.isFinite(used)
        ) {
            return null;
        }

        return {
            limit,
            remaining,
            reset,
            used,
            resource,
            updatedAt: new Date().toISOString(),
        };
    }

    private async saveRateState(rateState: GitHubRateState) {
        await this.desktop.lib.db.appState.upsert(
            GITHUB_CORE_RATE_KEY,
            JSON.stringify(rateState),
            new Date().toISOString(),
        );
    }
}

function normalizeRateState(rate: Partial<GitHubRateState> | null) {
    if (
        !rate ||
        typeof rate.limit !== "number" ||
        typeof rate.remaining !== "number" ||
        typeof rate.reset !== "number" ||
        typeof rate.used !== "number"
    ) {
        return null;
    }

    return {
        limit: rate.limit,
        remaining: rate.remaining,
        reset: rate.reset,
        used: rate.used,
        resource: typeof rate.resource === "string" ? rate.resource : "core",
        updatedAt: new Date().toISOString(),
    };
}

import type { GitHubRateState } from "@shared/types";
import { describe, expect, it } from "vitest";

import { formatRateResetText } from "./github-rate";

const state: GitHubRateState = {
    limit: 60,
    remaining: 20,
    reset: 1_800_001_000,
    used: 40,
    resource: "core",
    updatedAt: "2026-10-04T00:00:00Z",
};

describe("formatRateResetText", () => {
    it("has no reset text without rate state", () => {
        expect(formatRateResetText(null)).toBeNull();
    });

    it.each([
        ["available primary quota", {}, null],
        ["zero retry timestamp", { retryAt: 0 }, null],
        ["exhausted primary quota", { remaining: 0 }, state.reset],
        ["negative remaining quota", { remaining: -1 }, state.reset],
        [
            "secondary cooldown with available quota",
            { retryAt: state.reset - 100 },
            state.reset - 100,
        ],
        [
            "later secondary cooldown",
            { remaining: 0, retryAt: state.reset + 100 },
            state.reset + 100,
        ],
        [
            "later exhausted primary reset",
            { remaining: 0, retryAt: state.reset - 100 },
            state.reset,
        ],
        ["missing primary reset", { remaining: 0, reset: 0 }, null],
        [
            "secondary cooldown without primary reset",
            { remaining: 0, reset: 0, retryAt: state.reset },
            state.reset,
        ],
    ])("formats %s using Unix seconds", (_name, overrides, reset) => {
        expect(formatRateResetText({ ...state, ...overrides })).toBe(
            reset === null ? null : new Date(reset * 1000).toLocaleString(),
        );
    });
});

import type { AgentContextUsage } from "@bindings/agent/models";
import { describe, expect, it } from "vitest";

import { contextOccupancy, formatTokens } from "./agent-context";

function usage(overrides: Partial<AgentContextUsage>): AgentContextUsage {
    return {
        projectedTokens: 0,
        contextWindow: 0,
        systemTokens: 0,
        toolsTokens: 0,
        messageTokens: 0,
        ...overrides,
    };
}

describe("contextOccupancy", () => {
    it("reads the ring from the projected figure while the provider sample stays available", () => {
        expect(
            contextOccupancy(
                usage({ projectedTokens: 6_000, pressureTokens: 32_000, contextWindow: 128_000 }),
            ),
        ).toEqual({ percent: 5, usedTokens: 6_000, contextWindow: 128_000 });
    });

    it("falls back to the provider sample when no projection is served", () => {
        // The projection field is required on the model, so this guards the partial payload the
        // backend can emit before an anchor exists.
        expect(
            contextOccupancy({
                pressureTokens: 32_000,
                contextWindow: 128_000,
            } as AgentContextUsage),
        ).toEqual({ percent: 25, usedTokens: 32_000, contextWindow: 128_000 });
    });

    it("returns null until a usage and a capacity are both known", () => {
        expect(contextOccupancy(undefined)).toBeNull();
        expect(contextOccupancy(null)).toBeNull();
        expect(contextOccupancy(usage({ projectedTokens: 32_000 }))).toBeNull();
    });

    it("clamps occupancy at 100 percent", () => {
        expect(
            contextOccupancy(usage({ projectedTokens: 300_000, contextWindow: 128_000 }))?.percent,
        ).toBe(100);
    });

    it("reports an empty context as zero percent", () => {
        expect(contextOccupancy(usage({ projectedTokens: 0, contextWindow: 128_000 }))).toEqual({
            percent: 0,
            usedTokens: 0,
            contextWindow: 128_000,
        });
    });
});

describe("formatTokens", () => {
    it("compacts counts for the active locale", () => {
        expect(formatTokens(0, "en")).toBe("0");
        expect(formatTokens(999, "en")).toBe("999");
        expect(formatTokens(32_000, "en")).toBe("32K");
        expect(formatTokens(128_000, "en")).toBe("128K");
        expect(formatTokens(1_500_000, "en")).toBe("1.5M");
    });

    it("falls back to zero for unusable input", () => {
        expect(formatTokens(Number.NaN, "en")).toBe("0");
        expect(formatTokens(-5, "en")).toBe("0");
    });
});

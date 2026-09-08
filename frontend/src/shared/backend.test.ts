import { describe, expect, it } from "vitest";

import { driveContentsRefetchInterval, driveContentsRetry, isBackendDown } from "./backend";

describe("isBackendDown", () => {
    it("treats offline and maintenance as down", () => {
        expect(isBackendDown("offline")).toBe(true);
        expect(isBackendDown("maintenance")).toBe(true);
        expect(isBackendDown("online")).toBe(false);
        expect(isBackendDown("unknown")).toBe(false);
    });
});

describe("driveContentsRefetchInterval", () => {
    it("disables polling while the backend is down", () => {
        expect(driveContentsRefetchInterval("offline", false)).toBe(false);
        expect(driveContentsRefetchInterval("maintenance", true)).toBe(false);
    });

    it("keeps the existing foreground and hidden intervals while online", () => {
        expect(driveContentsRefetchInterval("online", false)).toBe(30_000);
        expect(driveContentsRefetchInterval("online", true)).toBe(180_000);
        expect(driveContentsRefetchInterval("unknown", false)).toBe(30_000);
    });
});

describe("driveContentsRetry", () => {
    it("disables retries while the backend is down", () => {
        expect(driveContentsRetry("offline")).toBe(false);
        expect(driveContentsRetry("maintenance")).toBe(false);
    });

    it("keeps the default retry count while the backend is up", () => {
        expect(driveContentsRetry("online")).toBe(3);
        expect(driveContentsRetry("unknown")).toBe(3);
    });
});

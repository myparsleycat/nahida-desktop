import { describe, expect, it } from "vitest";

import type { Content } from "./types";

import {
    driveContentsRefetchInterval,
    driveContentsRetry,
    hasStoringContent,
    isBackendDown,
} from "./backend";

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

    it("reads a folder with a storing file every 5 seconds", () => {
        expect(driveContentsRefetchInterval("online", false, true)).toBe(5_000);
        expect(driveContentsRefetchInterval("unknown", false, true)).toBe(5_000);
    });

    it("keeps the hidden and down rules ahead of a storing file", () => {
        expect(driveContentsRefetchInterval("online", true, true)).toBe(180_000);
        expect(driveContentsRefetchInterval("offline", false, true)).toBe(false);
    });
});

describe("hasStoringContent", () => {
    const NOW = 1_000_000_000_000;
    const child = (storing?: boolean, ageMs = 1_000) =>
        ({ id: "id", storing, createdAt: new Date(NOW - ageMs) }) as Content;

    it("finds a storing file among the children", () => {
        expect(hasStoringContent([child(), child(true)], NOW)).toBe(true);
    });

    it("answers false without one", () => {
        expect(hasStoringContent([child(), child(false)], NOW)).toBe(false);
        expect(hasStoringContent([], NOW)).toBe(false);
        expect(hasStoringContent(undefined, NOW)).toBe(false);
        expect(hasStoringContent(null, NOW)).toBe(false);
    });

    it("stops watching a file that has been storing for long", () => {
        expect(hasStoringContent([child(true, 11 * 60_000)], NOW)).toBe(false);
        expect(hasStoringContent([child(true, 11 * 60_000), child(true)], NOW)).toBe(true);
    });

    it("reads dates the server sent as strings, and treats an unreadable one as new", () => {
        const sent = new Date(NOW - 1_000).toISOString() as unknown as Date;
        expect(hasStoringContent([{ storing: true, createdAt: sent }], NOW)).toBe(true);
        expect(
            hasStoringContent([{ storing: true, createdAt: undefined as unknown as Date }], NOW),
        ).toBe(true);
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

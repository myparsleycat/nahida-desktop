import { describe, expect, it } from "vitest";

import { isStartPageSessionReady, resolveStartPage } from "./start-page";

describe("isStartPageSessionReady", () => {
    it("waits until the initial session load has finished", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: false,
                pendingSessionRestore: false,
                hasSession: false,
                hasToken: true,
                backendStatus: "unknown",
            }),
        ).toBe(false);
    });

    it("waits while cold-start restore is still fetching the session", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: true,
                pendingSessionRestore: true,
                hasSession: false,
                hasToken: true,
                backendStatus: "online",
            }),
        ).toBe(false);
    });

    it("waits when a token exists but the backend status is still unknown", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: true,
                pendingSessionRestore: false,
                hasSession: false,
                hasToken: true,
                backendStatus: "unknown",
            }),
        ).toBe(false);
    });

    it("resolves as logged out when restore finished without a session", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: true,
                pendingSessionRestore: false,
                hasSession: false,
                hasToken: true,
                backendStatus: "online",
            }),
        ).toBe(true);
    });

    it("resolves immediately when a session is already present", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: true,
                pendingSessionRestore: false,
                hasSession: true,
                hasToken: true,
                backendStatus: "online",
            }),
        ).toBe(true);
    });

    it("does not block local-first startup while the backend is offline", () => {
        expect(
            isStartPageSessionReady({
                sessionInitialized: true,
                pendingSessionRestore: false,
                hasSession: false,
                hasToken: true,
                backendStatus: "offline",
            }),
        ).toBe(true);
    });
});

describe("resolveStartPage", () => {
    it("materializes the drive root from the restored session", () => {
        expect(
            resolveStartPage("/drive/drive/root", {
                isLoggedIn: true,
                sessionRootId: "root-123",
            }),
        ).toBe("/drive/drive/root-123");
    });

    it("falls back when the start page is chosen before the session exists", () => {
        expect(
            resolveStartPage("/drive/drive/root", {
                isLoggedIn: false,
            }),
        ).toBe("/mod");
    });
});

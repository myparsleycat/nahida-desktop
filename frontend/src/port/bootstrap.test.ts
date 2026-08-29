import { describe, expect, it, vi } from "vitest";

import { loadFrontendBootstrap, normalizeBackendStatus } from "./bootstrap";

describe("normalizeBackendStatus", () => {
    it("keeps known backend states", () => {
        expect(normalizeBackendStatus("maintenance")).toBe("maintenance");
    });

    it("treats unknown backend payloads as unknown", () => {
        expect(normalizeBackendStatus("broken")).toBe("unknown");
    });
});

describe("loadFrontendBootstrap", () => {
    it("loads the configured local-first route through Wails services", async () => {
        const result = await loadFrontendBootstrap({
            getSession: vi.fn().mockResolvedValue(null),
            hasToken: vi.fn().mockResolvedValue(false),
            getBackendStatus: vi.fn().mockResolvedValue("offline"),
            getDefaultStartPage: vi.fn().mockResolvedValue("/tools"),
        });

        expect(result).toEqual({
            session: null,
            hasToken: false,
            backendStatus: "offline",
            configuredStartPage: "/tools",
            startPage: "/tools",
        });
    });

    it("retries session restoration once a stored token and online backend are known", async () => {
        const session = { drive: { rootId: "drive-root" } };
        const getSession = vi.fn().mockResolvedValueOnce(null).mockResolvedValueOnce(session);

        const result = await loadFrontendBootstrap({
            getSession,
            hasToken: vi.fn().mockResolvedValue(true),
            getBackendStatus: vi.fn().mockResolvedValue("online"),
            getDefaultStartPage: vi.fn().mockResolvedValue("/drive/drive/root"),
        });

        expect(getSession).toHaveBeenCalledTimes(2);
        expect(result.startPage).toBe("/drive/drive/drive-root");
        expect(result.hasToken).toBe(true);
    });

    it("falls back to the mod manager when an authenticated route cannot be restored", async () => {
        const result = await loadFrontendBootstrap({
            getSession: vi.fn().mockRejectedValue(new Error("backend unavailable")),
            hasToken: vi.fn().mockResolvedValue(true),
            getBackendStatus: vi.fn().mockResolvedValue("offline"),
            getDefaultStartPage: vi.fn().mockResolvedValue("/drive/share/root"),
        });

        expect(result.startPage).toBe("/mod");
    });

    it("keeps local-first startup alive when Wails service calls fail", async () => {
        const failure = new Error("service unavailable");
        const result = await loadFrontendBootstrap({
            getSession: vi.fn().mockRejectedValue(failure),
            hasToken: vi.fn().mockRejectedValue(failure),
            getBackendStatus: vi.fn().mockRejectedValue(failure),
            getDefaultStartPage: vi.fn().mockRejectedValue(failure),
        });

        expect(result).toMatchObject({
            session: null,
            hasToken: false,
            backendStatus: "unknown",
            configuredStartPage: "/mod",
            startPage: "/mod",
        });
    });
});

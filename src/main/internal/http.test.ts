import type { NahidaDesktop } from "@main/index";
import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    ky: vi.fn(),
}));

vi.mock("ky", () => ({
    default: mocks.ky,
    isNetworkError: () => false,
    isTimeoutError: () => false,
}));

vi.mock("electron", () => ({ app: { getVersion: () => "test-version" } }));

import { DesktopHttpService } from "./http";

function createService(getToken = vi.fn(async () => "stored-token")) {
    const desktop = {
        service: {
            auth: { getToken, getSession: vi.fn() },
            backendConnectivity: { setOffline: vi.fn(), setOnline: vi.fn() },
        },
        logger: { warn: vi.fn(), error: vi.fn() },
    } as unknown as NahidaDesktop;
    return { service: new DesktopHttpService(desktop), getToken };
}

describe("DesktopHttpService", () => {
    it("does not replace an Authorization header already provided by the caller", async () => {
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service, getToken } = createService();

        await service.fetcher("https://api.nahida.live/api/auth/get-session", {
            headers: { Authorization: "Bearer pinned-token" },
        });

        expect(getToken).not.toHaveBeenCalled();
        expect(mocks.ky).toHaveBeenCalledWith(
            "https://api.nahida.live/api/auth/get-session",
            expect.objectContaining({
                headers: {
                    Authorization: "Bearer pinned-token",
                    "User-Agent": "Nahida Desktop/test-version",
                },
            }),
        );
    });

    it("resolves Authorization from the current token when the caller does not provide one", async () => {
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service, getToken } = createService();

        await service.fetcher("https://api.nahida.live/api/drive");

        expect(getToken).toHaveBeenCalledOnce();
        expect(mocks.ky).toHaveBeenCalledWith(
            "https://api.nahida.live/api/drive",
            expect.objectContaining({
                headers: {
                    Authorization: "Bearer stored-token",
                    "User-Agent": "Nahida Desktop/test-version",
                },
            }),
        );
    });
});

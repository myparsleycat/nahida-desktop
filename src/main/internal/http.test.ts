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
            backendConnectivity: {
                setOffline: vi.fn(),
                setOnline: vi.fn(),
                getStatus: vi.fn(() => "online" as const),
                probe: vi.fn(async () => "online" as const),
            },
        },
        logger: { warn: vi.fn(), error: vi.fn() },
    } as unknown as NahidaDesktop;
    return { service: new DesktopHttpService(desktop), getToken };
}

describe("DesktopHttpService", () => {
    it("does not replace an Authorization header already provided by the caller", async () => {
        mocks.ky.mockReset();
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

    it("does not replace an Authorization header provided as a tuple array", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service, getToken } = createService();

        await service.fetcher("https://api.nahida.live/api/auth/get-session", {
            headers: [["Authorization", "Bearer pinned-token"]],
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
        mocks.ky.mockReset();
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

    it("short-circuits NHD requests when backend is offline", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service, getToken } = createService();

        const desktop = service["desktop"] as unknown as {
            service: { backendConnectivity: { getStatus: ReturnType<typeof vi.fn> } };
        };
        desktop.service.backendConnectivity.getStatus.mockReturnValue("offline");

        await expect(
            service.fetcher("https://api.nahida.live/api/drive"),
        ).rejects.toThrow("DRIVE_BACKEND_UNAVAILABLE");
        expect(getToken).not.toHaveBeenCalled();
        expect(mocks.ky).not.toHaveBeenCalled();
    });

    it("short-circuits NHD requests when backend is in maintenance", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service } = createService();

        const desktop = service["desktop"] as unknown as {
            service: { backendConnectivity: { getStatus: ReturnType<typeof vi.fn> } };
        };
        desktop.service.backendConnectivity.getStatus.mockReturnValue("maintenance");

        await expect(
            service.fetcher("https://api.nahida.live/api/drive"),
        ).rejects.toThrow("DRIVE_BACKEND_UNAVAILABLE");
        expect(mocks.ky).not.toHaveBeenCalled();
    });

    it("still sends session requests when backend is offline", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("ok"));
        const { service } = createService();

        const desktop = service["desktop"] as unknown as {
            service: { backendConnectivity: { getStatus: ReturnType<typeof vi.fn> } };
        };
        desktop.service.backendConnectivity.getStatus.mockReturnValue("offline");

        await service.fetcher("https://api.nahida.live/api/auth/get-session");
        expect(mocks.ky).toHaveBeenCalledOnce();
    });

    it("probes /status when a 503 response is received from an NHD endpoint", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("Service Unavailable", { status: 503 }));
        const { service } = createService();

        await service.fetcher("https://api.nahida.live/api/drive");

        const backendConnectivity = (service["desktop"] as unknown as {
            service: { backendConnectivity: { probe: ReturnType<typeof vi.fn>; setOffline: ReturnType<typeof vi.fn> } };
        }).service.backendConnectivity;
        expect(backendConnectivity.probe).toHaveBeenCalledOnce();
        expect(backendConnectivity.setOffline).not.toHaveBeenCalled();
    });

    it("calls setOffline when a 502 response is received from an NHD endpoint", async () => {
        mocks.ky.mockReset();
        mocks.ky.mockResolvedValue(new Response("Bad Gateway", { status: 502 }));
        const { service } = createService();

        await service.fetcher("https://api.nahida.live/api/drive");

        const backendConnectivity = (service["desktop"] as unknown as {
            service: { backendConnectivity: { probe: ReturnType<typeof vi.fn>; setOffline: ReturnType<typeof vi.fn> } };
        }).service.backendConnectivity;
        expect(backendConnectivity.setOffline).toHaveBeenCalledOnce();
        expect(backendConnectivity.probe).not.toHaveBeenCalled();
    });
});

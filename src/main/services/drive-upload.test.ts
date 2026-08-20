import { describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";

import { DriveService } from "./drive";

vi.mock("@main/client", () => ({
    eden: {
        akasha: {
            content: vi.fn(() => ({ get: vi.fn() })),
        },
    },
}));

vi.mock("electron", () => ({
    dialog: { showOpenDialog: vi.fn() },
}));

vi.mock("./util", () => ({
    processChunked: vi.fn(),
}));

vi.mock("@native/fs", () => ({
    collectFiles: vi.fn(),
}));

vi.mock("@main/worker/drive/sha256-piscina.worker?modulePath", () => ({
    default: "mock-worker",
}));

describe("DriveService upload source validation", () => {
    it("requires source paths to be readable without requiring write access", async () => {
        const isPathReadable = vi.fn().mockResolvedValue(false);
        const service = new DriveService({
            window: { main: { window: {} } },
            lib: { fs: { isPathReadable } },
        } as unknown as NahidaDesktop);

        await expect(
            service.fn.startUpload({ destId: "destination", paths: ["C:/Mods"] }),
        ).rejects.toThrow("Path is not readable");
        expect(isPathReadable).toHaveBeenCalledWith("C:/Mods");
    });
});

// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn() }));
vi.mock("@bindings/setting", () => ({
    Setting: { GetRendererState: mocks.get, SetRendererState: mocks.set },
}));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));

beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    mocks.set.mockResolvedValue(undefined);
});

describe("renderer state", () => {
    it("keeps writes in memory when the stored rows could not be loaded", async () => {
        mocks.get.mockRejectedValue(new Error("database is locked"));
        const { hydrateRendererState, rendererState } = await import("./renderer-state");
        await expect(hydrateRendererState()).rejects.toThrow();

        await expect(rendererState.setItem("drafts", "[]")).resolves.toBe(false);
        expect(rendererState.getItem("drafts")).toBe("[]");
        expect(mocks.set).not.toHaveBeenCalled();
    });

    it("stores writes once the stored rows are loaded", async () => {
        mocks.get.mockResolvedValue({ drafts: "[1]" });
        const { hydrateRendererState, rendererState } = await import("./renderer-state");
        await hydrateRendererState();

        expect(rendererState.getItem("drafts")).toBe("[1]");
        await expect(rendererState.setItem("drafts", "[]")).resolves.toBe(true);
        expect(mocks.set).toHaveBeenCalledWith("drafts", "[]");
    });

    it("reports the queued write's result for a repeated value", async () => {
        mocks.get.mockResolvedValue({ drafts: "[1]" });
        mocks.set.mockRejectedValue(new Error("database is locked"));
        const { hydrateRendererState, rendererState } = await import("./renderer-state");
        await hydrateRendererState();

        const first = rendererState.setItem("drafts", "[]");
        await expect(rendererState.setItem("drafts", "[]")).resolves.toBe(false);
        await expect(first).resolves.toBe(false);
        expect(mocks.set).toHaveBeenCalledTimes(1);

        void rendererState.removeItem("drafts");
        await expect(rendererState.removeItem("drafts")).resolves.toBe(false);
        await expect(rendererState.removeItem("view")).resolves.toBe(true);
    });
});

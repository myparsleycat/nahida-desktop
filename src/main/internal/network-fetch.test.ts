import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    fromPartition: vi.fn(),
    networkFetch: vi.fn(),
}));

vi.mock("electron", () => ({
    net: { fetch: mocks.networkFetch },
    session: { fromPartition: mocks.fromPartition },
}));

import {
    createDownloadNetworkContext,
    networkFetch,
    resetDownloadSessionsForTests,
} from "./network-fetch";

describe("download network contexts", () => {
    afterEach(() => {
        resetDownloadSessionsForTests();
        mocks.fromPartition.mockReset();
    });

    it("resets only the leased context and reuses released sessions", async () => {
        const sessions = Array.from({ length: 2 }, () => ({
            closeAllConnections: vi.fn().mockResolvedValue(undefined),
            fetch: vi.fn().mockResolvedValue(new Response("download")),
        }));
        mocks.fromPartition.mockReturnValueOnce(sessions[0]).mockReturnValueOnce(sessions[1]);
        mocks.networkFetch.mockResolvedValue(new Response("api"));

        const first = createDownloadNetworkContext();
        const second = createDownloadNetworkContext();

        await first.fetch("https://n3.nahida.live/132/first");
        await second.fetch("https://n3.nahida.live/132/second");
        await networkFetch("https://api.nahida.live/ping");
        await first.resetConnections();

        expect(mocks.fromPartition).toHaveBeenCalledTimes(2);
        expect(mocks.fromPartition.mock.calls[0]?.[0]).not.toBe(
            mocks.fromPartition.mock.calls[1]?.[0],
        );
        expect(sessions[0]?.fetch).toHaveBeenCalledWith(
            "https://n3.nahida.live/132/first",
            undefined,
        );
        expect(sessions[1]?.fetch).toHaveBeenCalledWith(
            "https://n3.nahida.live/132/second",
            undefined,
        );
        expect(sessions[0]?.closeAllConnections).toHaveBeenCalledOnce();
        expect(sessions[1]?.closeAllConnections).not.toHaveBeenCalled();
        expect(mocks.networkFetch).toHaveBeenCalledWith("https://api.nahida.live/ping", undefined);

        first.release();
        const reused = createDownloadNetworkContext();
        expect(mocks.fromPartition).toHaveBeenCalledTimes(2);

        second.release();
        reused.release();
    });

    it("returns a session to the pool only after a pending reset settles", async () => {
        let resolveReset: (() => void) | undefined;
        const pendingReset = new Promise<void>((resolve) => {
            resolveReset = resolve;
        });
        mocks.fromPartition.mockReturnValueOnce({
            closeAllConnections: vi.fn().mockReturnValue(pendingReset),
            fetch: vi.fn().mockResolvedValue(new Response("download")),
        });

        const context = createDownloadNetworkContext();
        const reset = context.resetConnections();
        context.release();

        createDownloadNetworkContext().release();
        expect(mocks.fromPartition).toHaveBeenCalledTimes(2);

        resolveReset?.();
        await reset;
        createDownloadNetworkContext().release();
        expect(mocks.fromPartition).toHaveBeenCalledTimes(2);
    });
});

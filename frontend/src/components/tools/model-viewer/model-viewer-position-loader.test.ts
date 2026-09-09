import { afterEach, describe, expect, it, vi } from "vitest";

import { ModelViewerPositionLoader } from "./model-viewer-position-loader";
const descriptor = { conditions: [], geometryUrl: "/geometry" };
afterEach(() => vi.unstubAllGlobals());

describe("ModelViewerPositionLoader", () => {
    it("fetches prepared geometry without fetching raw files or source indices", async () => {
        const fetch = vi.fn(async () => new Response(new ArrayBuffer(104)));
        vi.stubGlobal("fetch", fetch);
        const loader = new ModelViewerPositionLoader();
        const result = await loader.load(descriptor, 1);
        expect(result.positions).toHaveLength(3);
        expect(result.normals).toHaveLength(3);
        expect(fetch).toHaveBeenCalledOnce();
        expect(fetch).toHaveBeenCalledWith(
            "/geometry",
            expect.objectContaining({ cache: "no-store" }),
        );
        loader.dispose();
    });
    it.each(["caller", "dispose"])("cancels pending fetch on %s", async (kind) => {
        vi.stubGlobal(
            "fetch",
            vi.fn(
                (_url: string, options: RequestInit) =>
                    new Promise((_resolve, reject) => {
                        options.signal?.addEventListener(
                            "abort",
                            () => reject(options.signal?.reason),
                            { once: true },
                        );
                    }),
            ),
        );
        const loader = new ModelViewerPositionLoader();
        const controller = new AbortController();
        const pending = loader.load(descriptor, 1, controller.signal);
        if (kind === "caller") controller.abort();
        else loader.dispose();
        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
        loader.dispose();
        await expect(loader.load(descriptor, 1)).rejects.toMatchObject({ name: "AbortError" });
    });
    it("rejects truncated backend geometry", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => new Response(new ArrayBuffer(80))),
        );
        await expect(new ModelViewerPositionLoader().load(descriptor, 1)).rejects.toThrow(
            "Invalid binary buffer length",
        );
    });
});

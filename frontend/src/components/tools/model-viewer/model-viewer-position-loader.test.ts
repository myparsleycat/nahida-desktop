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
    it("coalesces in-flight loads and reuses a bounded cache", async () => {
        const fetch = vi.fn(async () => new Response(new ArrayBuffer(104)));
        vi.stubGlobal("fetch", fetch);
        const loader = new ModelViewerPositionLoader(104);
        const first = loader.load(descriptor, 1);
        const second = loader.load(descriptor, 1);
        await expect(first).resolves.toMatchObject({ positions: expect.any(Float32Array) });
        await expect(second).resolves.toMatchObject({ positions: expect.any(Float32Array) });
        expect(fetch).toHaveBeenCalledOnce();
        await loader.load(descriptor, 1);
        expect(fetch).toHaveBeenCalledOnce();
        await loader.load({ conditions: [], geometryUrl: "/other" }, 1);
        expect(fetch).toHaveBeenCalledTimes(2);
        await loader.load(descriptor, 1);
        expect(fetch).toHaveBeenCalledTimes(3);
        loader.dispose();
    });

    it.each([0, 104])("does not retain oversized frames with a %i-byte budget", async (budget) => {
        const fetch = vi.fn(
            async (url: string) => new Response(new ArrayBuffer(url === "/large" ? 128 : 104)),
        );
        vi.stubGlobal("fetch", fetch);
        const loader = new ModelViewerPositionLoader(budget);
        await loader.load(descriptor, 1);
        await loader.load({ ...descriptor, geometryUrl: "/large" }, 2);
        await loader.load({ ...descriptor, geometryUrl: "/large" }, 2);
        expect(fetch).toHaveBeenCalledTimes(3);
        await loader.load(descriptor, 1);
        expect(fetch).toHaveBeenCalledTimes(budget === 0 ? 4 : 3);
        loader.dispose();
    });

    it("cancels abandoned work and permits a fresh request for the same geometry", async () => {
        const signals: AbortSignal[] = [];
        const fetch = vi.fn((_url: string, options: RequestInit) => {
            signals.push(options.signal!);
            return new Promise<Response>((_resolve, reject) => {
                options.signal!.addEventListener("abort", () => reject(options.signal!.reason), {
                    once: true,
                });
            });
        });
        vi.stubGlobal("fetch", fetch);
        const loader = new ModelViewerPositionLoader();
        const first = new AbortController();
        const second = new AbortController();
        const a = loader.load(descriptor, 1, first.signal);
        const b = loader.load(descriptor, 1, second.signal);
        first.abort();
        await expect(a).rejects.toMatchObject({ name: "AbortError" });
        expect(signals[0]!.aborted).toBe(false);
        second.abort();
        await expect(b).rejects.toMatchObject({ name: "AbortError" });
        expect(signals[0]!.aborted).toBe(true);
        const retry = loader.load(descriptor, 1);
        expect(fetch).toHaveBeenCalledTimes(2);
        loader.dispose();
        await expect(retry).rejects.toMatchObject({ name: "AbortError" });
    });

    it("hands an in-flight prefetch to the next frame in the same turn", async () => {
        let resolveFetch: ((response: Response) => void) | undefined;
        let fetchSignal: AbortSignal | null | undefined;
        vi.stubGlobal(
            "fetch",
            vi.fn((_url: string, options: RequestInit) => {
                fetchSignal = options.signal;
                return new Promise<Response>((resolve) => {
                    resolveFetch = resolve;
                });
            }),
        );
        const loader = new ModelViewerPositionLoader();
        const controller = new AbortController();
        const prefetch = loader.load(descriptor, 1, controller.signal);
        controller.abort();
        const next = loader.load(descriptor, 1);
        await expect(prefetch).rejects.toMatchObject({ name: "AbortError" });
        expect(fetchSignal?.aborted).toBe(false);
        resolveFetch!(new Response(new ArrayBuffer(104)));
        await expect(next).resolves.toHaveProperty("positions");
        loader.dispose();
    });

    it("rejects one caller abort without cancelling a shared fetch", async () => {
        let resolveFetch: ((value: Response) => void) | undefined;
        const fetch = vi.fn(
            (_url: string, options: RequestInit) =>
                new Promise<Response>((resolve, reject) => {
                    options.signal?.addEventListener(
                        "abort",
                        () => reject(options.signal?.reason),
                        { once: true },
                    );
                    resolveFetch = resolve;
                }),
        );
        vi.stubGlobal("fetch", fetch);
        const loader = new ModelViewerPositionLoader();
        const controller = new AbortController();
        const aborted = loader.load(descriptor, 1, controller.signal);
        const shared = loader.load(descriptor, 1);
        controller.abort();
        await expect(aborted).rejects.toMatchObject({ name: "AbortError" });
        resolveFetch?.(new Response(new ArrayBuffer(104)));
        await expect(shared).resolves.toMatchObject({ positions: expect.any(Float32Array) });
        expect(fetch).toHaveBeenCalledOnce();
        loader.dispose();
    });

    it("cancels pending fetch on dispose", async () => {
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
        const pending = loader.load(descriptor, 1);
        loader.dispose();
        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
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

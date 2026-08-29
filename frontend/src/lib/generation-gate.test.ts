import { describe, expect, it } from "vitest";

import { isCurrentRequest, resolveIfCurrent } from "./generation-gate";

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((res) => {
        resolve = res;
    });
    return { promise, resolve };
}

describe("generation-gate", () => {
    it("resolves only the latest request", async () => {
        const latestId = { current: 0 };
        const first = deferred<string>();
        const second = deferred<string>();

        const firstId = ++latestId.current;
        const firstResult = resolveIfCurrent(firstId, latestId, () => first.promise);
        const secondId = ++latestId.current;
        const secondResult = resolveIfCurrent(secondId, latestId, () => second.promise);

        first.resolve("stale");
        second.resolve("latest");

        expect(await firstResult).toBeUndefined();
        expect(await secondResult).toBe("latest");
        expect(isCurrentRequest(firstId, latestId)).toBe(false);
        expect(isCurrentRequest(secondId, latestId)).toBe(true);
    });
});

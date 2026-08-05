import assert from "node:assert/strict";

import { describe, it, vi } from "vitest";

import {
    TOUCH_PROFILE_VISION_CACHE_SIZE,
    TouchProfileVisionCache,
} from "./touch-profile-vision-cache";

describe("TouchProfileVisionCache", () => {
    it("uses the database as persistent storage", async () => {
        let stored: { cacheKey: string; result: string; updatedAt: string } | null = null;
        const db = {
            touchProfileVisionCache: {
                get: vi.fn(async () => stored),
                upsert: vi.fn(async (row) => {
                    stored = row;
                }),
                deleteAll: vi.fn(async () => {}),
            },
        };
        const first = new TouchProfileVisionCache(db);
        await first.set("key", '{"interactive":false}');

        const second = new TouchProfileVisionCache(db);
        assert.equal(await second.get("key"), '{"interactive":false}');
        assert.equal(db.touchProfileVisionCache.upsert.mock.calls.length, 1);
        assert.equal(db.touchProfileVisionCache.get.mock.calls.length, 1);
    });

    it("keeps the in-memory cache bounded to 100 entries", () => {
        assert.equal(TOUCH_PROFILE_VISION_CACHE_SIZE, 100);
    });

    it("falls through to the database after the oldest entry is evicted", async () => {
        const get = vi.fn(async () => null);
        const cache = new TouchProfileVisionCache({
            touchProfileVisionCache: {
                get,
                upsert: vi.fn(async () => {}),
                deleteAll: vi.fn(async () => {}),
            },
        });

        for (let index = 0; index <= TOUCH_PROFILE_VISION_CACHE_SIZE; index++) {
            await cache.set(`key-${index}`, `result-${index}`);
        }

        assert.equal(
            await cache.get(`key-${TOUCH_PROFILE_VISION_CACHE_SIZE}`),
            `result-${TOUCH_PROFILE_VISION_CACHE_SIZE}`,
        );
        assert.equal(await cache.get("key-0"), null);
        assert.equal(get.mock.calls.length, 1);
    });

    it("clears both memory and database storage", async () => {
        let stored: { cacheKey: string; result: string; updatedAt: string } | null = {
            cacheKey: "key",
            result: "database-result",
            updatedAt: "",
        };
        const get = vi.fn(async () => stored);
        const deleteAll = vi.fn(async () => {
            stored = null;
        });
        const cache = new TouchProfileVisionCache({
            touchProfileVisionCache: {
                get,
                upsert: vi.fn(async () => {}),
                deleteAll,
            },
        });

        await cache.set("key", "memory-result");
        await cache.clear();

        assert.equal(deleteAll.mock.calls.length, 1);
        assert.equal(await cache.get("key"), null);
        assert.equal(get.mock.calls.length, 1);
    });

    it("fails open when database reads or writes fail", async () => {
        const cache = new TouchProfileVisionCache({
            touchProfileVisionCache: {
                get: vi.fn(async () => {
                    throw new Error("database read failed");
                }),
                upsert: vi.fn(async () => {
                    throw new Error("database write failed");
                }),
                deleteAll: vi.fn(async () => {}),
            },
        });

        assert.equal(await cache.get("missing"), null);
        await cache.set("key", "value");
        assert.equal(await cache.get("key"), "value");
    });
});

import type { DatabaseClient } from "@main/internal/db/client";
import type { Logger } from "@main/internal/logger";
import LRUCache from "mnemonist/lru-cache";

export const TOUCH_PROFILE_VISION_CACHE_SIZE = 100;

type TouchProfileVisionCacheDatabase = Pick<DatabaseClient, "touchProfileVisionCache">;

export class TouchProfileVisionCache {
    private readonly memory = new LRUCache<string, string>(TOUCH_PROFILE_VISION_CACHE_SIZE);

    public constructor(
        private readonly db: TouchProfileVisionCacheDatabase,
        private readonly logger?: Pick<Logger, "error">,
    ) {}

    public async get(cacheKey: string) {
        const cached = this.memory.get(cacheKey);
        if (cached !== undefined) {
            return cached;
        }

        try {
            const row = await this.db.touchProfileVisionCache.get(cacheKey);
            if (!row) {
                return null;
            }

            this.memory.set(cacheKey, row.result);
            return row.result;
        } catch (error) {
            this.logger?.error(error, `TouchProfileVisionCache:get:${cacheKey}`);
            return null;
        }
    }

    public async set(cacheKey: string, result: string) {
        this.memory.set(cacheKey, result);

        try {
            await this.db.touchProfileVisionCache.upsert({
                cacheKey,
                result,
                updatedAt: new Date().toISOString(),
            });
        } catch (error) {
            this.logger?.error(error, `TouchProfileVisionCache:set:${cacheKey}`);
        }
    }

    public async clear() {
        this.memory.clear();

        try {
            await this.db.touchProfileVisionCache.deleteAll();
        } catch (error) {
            this.logger?.error(error, "TouchProfileVisionCache:clear");
            throw error;
        }
    }
}

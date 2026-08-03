import { resolve } from "node:path";

import { defineConfig } from "vitest/config";

export default defineConfig({
    resolve: {
        alias: {
            "@native": resolve("native"),
            "@shared": resolve("src/shared"),
            "@main": resolve("src/main"),
            "@preload": resolve("src/preload"),
            "@": resolve("src"),
            "@backend": resolve("../backend/src"),
            // mnemonist subpath exports are require-only; Vite ESM resolve needs explicit files
            "mnemonist/lru-cache": resolve("node_modules/mnemonist/lru-cache.js"),
            "mnemonist/heap": resolve("node_modules/mnemonist/heap.js"),
        },
    },
    test: {
        include: ["src/**/*.test.ts"],
        environment: "node",
    },
});

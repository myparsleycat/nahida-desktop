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
        },
    },
    test: {
        include: ["src/**/*.test.ts"],
        environment: "node",
    },
});

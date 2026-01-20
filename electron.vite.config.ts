import { resolve } from "path";
import { defineConfig } from "electron-vite";
import react from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import tailwindcss from "@tailwindcss/vite";

import { visualizer } from "rollup-plugin-visualizer";

export default defineConfig({
    main: {
        resolve: {
            alias: {
                "@shared": resolve("src/shared"),
                "@main": resolve("src/main"),
                "@preload": resolve("src/preload"),
                "@": resolve("src"),
                "@backend": resolve("../backend/src"),
            },
        },
        plugins: [
            // visualizer({
            //     filename: "dist/stats-main.html",
            //     open: true,
            //     gzipSize: true,
            //     brotliSize: true,
            // }),
        ],
    },
    preload: {
        resolve: {
            alias: {
                "@shared": resolve("src/shared"),
                "@main": resolve("src/main"),
                "@preload": resolve("src/preload"),
                "@": resolve("src"),
                "@backend": resolve("../backend/src"),
            },
        },
    },
    renderer: {
        resolve: {
            alias: {
                "@renderer": resolve("src/renderer/src"),
                "@shared": resolve("src/shared"),
                "@": resolve("src"),
            },
        },
        plugins: [
            tanstackRouter({
                target: "react",
                autoCodeSplitting: true,
            }),
            react(),
            tailwindcss(),
            // visualizer({
            //     filename: "dist/stats-renderer.html",
            //     open: true,
            //     gzipSize: true,
            //     brotliSize: true,
            // }),
        ],
    },
});

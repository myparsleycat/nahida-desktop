import { resolve } from "node:path";
import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import { defineConfig } from "electron-vite";
import { ipcGeneratorPlugin } from "./plugins/ipc-generator";
import { nativeBindingPlugin } from "./plugins/native-binding";

export default defineConfig({
    main: {
        build: {
            externalizeDeps: {
                exclude: ["html-to-text", "file-type"],
            },
        },
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
        plugins: [ipcGeneratorPlugin(), nativeBindingPlugin()],
    },
    preload: {
        build: {
            rollupOptions: {
                external: ["electron"],
            },
        },
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
            babel({ presets: [reactCompilerPreset()] } as Parameters<typeof babel>[0]),
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

import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "electron-vite";
import { ipcGeneratorPlugin } from "./plugins/ipc-generator";
import { nativeBindingPlugin } from "./plugins/native-binding";

const ReactCompilerConfig = {
    target: "19",
    runtimeModule: "react-compiler-runtime",
};

export default defineConfig({
    main: {
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
            react({
                babel: {
                    plugins: [["babel-plugin-react-compiler", ReactCompilerConfig]],
                },
            }),
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

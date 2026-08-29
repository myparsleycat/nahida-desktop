import { resolve } from "node:path";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
    resolve: {
        alias: {
            "@renderer": resolve("src"),
            "@shared": resolve("src/shared"),
            "@": resolve("src"),
            "@bindings": resolve(__dirname, "./bindings/nahida.live/desktop/internal"),
        },
    },
    server: {
        host: "127.0.0.1",
        port: Number(process.env.WAILS_VITE_PORT) || 9245,
        strictPort: true,
    },
    plugins: [
        tanstackRouter({
            target: "react",
            autoCodeSplitting: true,
        }),
        react(),
        tailwindcss(),
        wails("./bindings"),
    ],
});

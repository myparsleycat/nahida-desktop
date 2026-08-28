/// <reference types="vite/client" />

declare global {
    interface Window {
        electron: {
            process: {
                platform: string;
            };
        };
    }
}

export {};

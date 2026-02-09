import { app } from "electron";
export const appVersion = app.getVersion();
export const CHUNK_SIZE = 1024 * 1024;
export const MAX_UPLOAD_THREADS = 16;
export const env = {
    isNode:
        typeof process !== "undefined" &&
        process.versions !== null &&
        process.versions.node !== null,
    isElectron:
        typeof process.versions["electron"] === "string" && process.versions["electron"].length > 0,
} as const;

export const IS_ELECTRON = env.isElectron;
export const IS_NODE = env.isNode;
export const DISALLOWED_SYNC_DIRS = [
    "C:\\Windows",
    "C:\\Program Files",
    "C:\\Program Files (x86)",
    "C:\\Temp",
    "C:\\Windows\\Temp",
    "/System",
    "/Library",
    "/Applications",
    "/usr",
    "/bin",
    "/sbin",
    // "/var",
    "/tmp",
    "/private",
    `/home/${process.env.USER ?? "User"}/.cache`,
    `/var/home/${process.env.USER ?? "User"}/.cache`,
];

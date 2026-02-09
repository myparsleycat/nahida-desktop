import { app } from "electron";
export const BACKEND_URL = !app.isPackaged ? "https://api.nahida.live" : "https://api.nahida.live";

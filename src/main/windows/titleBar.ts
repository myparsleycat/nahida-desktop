import { TITLE_BAR_HEIGHT } from "@shared/const";
import type { BrowserWindow, TitleBarOverlayOptions } from "electron";

const TRAFFIC_LIGHT_SIZE = 20;

export const TRAFFIC_LIGHT_Y = Math.round((TITLE_BAR_HEIGHT - TRAFFIC_LIGHT_SIZE) / 2) + 3;

const TRANSPARENT_OVERLAY_COLOR = "#00000000";

const DEFAULT_SYMBOL_COLOR = "#252525";

export interface TitleBarOverlaySyncOptions {
    symbolColor: string;
}

export function createTitleBarOverlay(
    options: TitleBarOverlaySyncOptions = { symbolColor: DEFAULT_SYMBOL_COLOR },
): TitleBarOverlayOptions {
    return {
        height: TITLE_BAR_HEIGHT,
        color: TRANSPARENT_OVERLAY_COLOR,
        symbolColor: options.symbolColor,
    };
}

export function applyTitleBarOverlay(
    window: BrowserWindow,
    options: TitleBarOverlaySyncOptions = { symbolColor: DEFAULT_SYMBOL_COLOR },
) {
    if (process.platform === "darwin") return;
    if (window.isDestroyed()) return;
    window.setTitleBarOverlay(createTitleBarOverlay(options));
}

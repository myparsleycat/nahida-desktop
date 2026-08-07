import type { NahidaDesktop } from "@main/index";

import { applyTitleBarOverlay, type TitleBarOverlaySyncOptions } from "../../windows/titleBar";
import { rh } from "../helper";

export function registerWindowHandlers(d: NahidaDesktop) {
    rh("window:openSetting", async () => {
        await d.window.main.focusAndNavigate("/setting/gen");
    });

    rh("window:syncTitleBarOverlay", (options: TitleBarOverlaySyncOptions) => {
        const window = d.window.main.window;
        if (!window || window.isDestroyed()) return;
        applyTitleBarOverlay(window, options);
    });
}

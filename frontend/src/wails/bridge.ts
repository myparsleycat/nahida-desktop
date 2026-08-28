import { Shell } from "@bindings/platform";

import "./file-drop";

// The app is Windows-only; Electron hardcodes "win32" (see PA-030).
function platformName() {
    return "win32";
}

function installPlatform() {
    window.electron = {
        process: {
            platform: platformName(),
        },
    };

    void Shell.GetAppStatus().then((status) => {
        window.electron.process.platform = status.platform;
    });
}

if (typeof window !== "undefined") {
    installPlatform();
}

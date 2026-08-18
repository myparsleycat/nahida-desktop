import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerXXMIHandlers(d: NahidaDesktop) {
    rh("xxmi:getXXMIConfig", () => d.service.xxmi.getXXMIConfig());
    rh("xxmi:getXXMIData", () => d.service.xxmi.getXXMIData());
    rh("xxmi:getXXMIPath", () => d.service.xxmi.getXXMIPath());
    rh("xxmi:saveXXMIPath", (path: string) => d.service.xxmi.saveXXMIPath(path));
    rh("xxmi:findXXMIPath", () => d.service.xxmi.findXXMIPath());
    rh("xxmi:startGame", (importer: string) => d.service.xxmi.startGame(importer));
    rh("xxmi:getEnabledImporters", () => d.service.xxmi.getEnabledImporters());
    rh("xxmi:getLibsReleases", () => d.service.xxmi.getLibsReleases());
    rh("xxmi:installDllVersion", (input: { version: string }) => {
        if (!input || typeof input.version !== "string" || !input.version.trim()) {
            throw new Error("Invalid version: must be a non-empty string");
        }
        return d.service.xxmi.installDllVersion(input);
    });
}

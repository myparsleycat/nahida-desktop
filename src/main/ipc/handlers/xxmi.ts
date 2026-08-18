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
    rh("xxmi:installDllVersion", (input: { version: string }) =>
        d.service.xxmi.installDllVersion(input),
    );
}

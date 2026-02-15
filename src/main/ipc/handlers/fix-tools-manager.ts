import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerFixToolsManagerHandlers(d: NahidaDesktop) {
    rh("ftm:getScripts", () => d.service.fixTools.getScripts());
    rh("ftm:saveScript", (path) => d.service.fixTools.saveScript(path));
    rh("ftm:deleteScript", (id) => d.service.fixTools.deleteScript(id));
    rh("ftm:getPresets", () => d.service.fixTools.getPresets());
    rh("ftm:createPreset", ({ name, scriptIds }) =>
        d.service.fixTools.createPreset({ name, scriptIds }),
    );
    rh("ftm:deletePreset", (id) => d.service.fixTools.deletePreset(id));
    rh("ftm:runPreset", (id, destPath) => d.service.fixTools.runPreset(id, destPath));
    rh("ftm:runScript", (id, destPath) => d.service.fixTools.runScript(id, destPath));
    rh("ftm:cancelRun", () => d.service.fixTools.cancelRun());
    rh("ftm:sendInput", (input) => d.service.fixTools.sendInput(input));
}

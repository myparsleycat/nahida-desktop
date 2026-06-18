import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerFixToolsManagerHandlers(d: NahidaDesktop) {
    rh("ftm:getScripts", () => d.service.modTools.fixTool.getScripts());
    rh("ftm:saveScript", (path) => d.service.modTools.fixTool.saveScript(path));
    rh("ftm:deleteScript", (id) => d.service.modTools.fixTool.deleteScript(id));
    rh("ftm:getPresets", () => d.service.modTools.fixTool.getPresets());
    rh("ftm:isPythonAvailable", () => d.service.modTools.fixTool.isPythonAvailable());
    rh("ftm:createPreset", ({ name, scriptIds }) =>
        d.service.modTools.fixTool.createPreset({ name, scriptIds }),
    );
    rh("ftm:deletePreset", (id) => d.service.modTools.fixTool.deletePreset(id));
    rh("ftm:runPreset", (id, destPath) => d.service.modTools.fixTool.runPreset(id, destPath));
    rh("ftm:runScript", (id, destPath) => d.service.modTools.fixTool.runScript(id, destPath));
    rh("ftm:cancelRun", () => d.service.modTools.fixTool.cancelRun());
    rh("ftm:sendInput", (input) => d.service.modTools.fixTool.sendInput(input));
}

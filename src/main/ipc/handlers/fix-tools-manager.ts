import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerFixToolsManagerHandlers(d: NahidaDesktop) {
    rh("ftm:getTools", () => d.service.fixTools.getTools());
    rh("ftm:saveTool", (path) => d.service.fixTools.saveTool(path));
    rh("ftm:deleteTool", (id) => d.service.fixTools.deleteTool(id));
    rh("ftm:getPresets", () => d.service.fixTools.getPresets());
    rh("ftm:createPreset", ({ name, toolIds }) =>
        d.service.fixTools.createPreset({ name, toolIds }),
    );
    rh("ftm:deletePreset", (id) => d.service.fixTools.deletePreset(id));
    rh("ftm:runPreset", (id, destPath) => d.service.fixTools.runPreset(id, destPath));
    rh("ftm:runFixTool", (id, destPath) => d.service.fixTools.runFixTool(id, destPath));
    rh("ftm:cancelRun", () => d.service.fixTools.cancelRun());
    rh("ftm:sendInput", (input) => d.service.fixTools.sendInput(input));
}

import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerToolsHandlers(d: NahidaDesktop) {
    rh("tools:getGIMIPath", () => d.service.modTools.dllBuilder.getGIMIPath());
    rh("tools:saveGIMIPath", (gimiPath: string) =>
        d.service.modTools.dllBuilder.saveGIMIPath(gimiPath),
    );
    rh("tools:buildNewD3DDLL", ({ gimiPath }: { gimiPath?: string }) =>
        d.service.modTools.dllBuilder.buildNewD3DDLL({ gimiPath }),
    );
}

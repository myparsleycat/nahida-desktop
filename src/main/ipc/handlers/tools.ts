import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerToolsHandlers(d: NahidaDesktop) {
    rh("tools:getGIMIPath", () => d.service.tools.getGIMIPath());
    rh("tools:saveGIMIPath", (gimiPath: string) => d.service.tools.saveGIMIPath(gimiPath));
    rh("tools:buildNewD3DDLL", ({ gimiPath }: { gimiPath?: string }) =>
        d.service.tools.buildNewD3DDLL({ gimiPath }),
    );
}

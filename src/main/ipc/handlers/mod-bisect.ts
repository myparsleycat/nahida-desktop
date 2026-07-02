import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerModBisectHandlers(d: NahidaDesktop) {
    rh("tools:bisectStart", (game: string) => d.service.modTools.modBisect.start(game));
    rh("tools:bisectRespond", (fixed: boolean) => d.service.modTools.modBisect.respond(fixed));
    rh("tools:bisectUndoLastRound", () => d.service.modTools.modBisect.undoLastRound());
    rh("tools:bisectFinalize", (keepDisabled: string[]) =>
        d.service.modTools.modBisect.finalize(keepDisabled),
    );
    rh("tools:bisectCancel", () => d.service.modTools.modBisect.cancel());
    rh("tools:bisectGetState", () => d.service.modTools.modBisect.getState());
    rh("tools:bisectRecover", (game: string) => d.service.modTools.modBisect.recoverOrphans(game));
}

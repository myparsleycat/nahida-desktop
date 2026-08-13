import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { toErrorMessage } from "@shared/utils";

export function registerModBisectHandlers(d: NahidaDesktop) {
    rh("tools:bisectStart", async (game: string, excludePaths: string[] = []) => {
        try {
            return await d.service.modTools.modBisect.start(game, excludePaths);
        } catch (error) {
            d.logger.error(error, "tools:bisectStart");
            d.logger.error(
                {
                    channel: "tools:bisectStart",
                    stage: "start",
                    game,
                    excludePaths,
                    error: toErrorMessage(error),
                },
                "tools:bisectStart:context",
            );
            throw error;
        }
    });
    rh("tools:bisectValidateExcludePath", async (game: string, inputPath: string) => {
        try {
            return await d.service.modTools.modBisect.validateExcludePath(game, inputPath);
        } catch (error) {
            d.logger.error(error, "tools:bisectValidateExcludePath");
            d.logger.error(
                {
                    channel: "tools:bisectValidateExcludePath",
                    stage: "validate-exclude-path",
                    game,
                    inputPath,
                    error: toErrorMessage(error),
                },
                "tools:bisectValidateExcludePath:context",
            );
            throw error;
        }
    });
    rh("tools:bisectRespond", (fixed: boolean) => d.service.modTools.modBisect.respond(fixed));
    rh("tools:bisectUndoLastRound", () => d.service.modTools.modBisect.undoLastRound());
    rh("tools:bisectFinalize", (keepDisabled: string[]) =>
        d.service.modTools.modBisect.finalize(keepDisabled),
    );
    rh("tools:bisectCancel", () => d.service.modTools.modBisect.cancel());
    rh("tools:bisectGetState", () => d.service.modTools.modBisect.getState());
    rh("tools:bisectRecover", (game: string) => d.service.modTools.modBisect.recoverOrphans(game));
}

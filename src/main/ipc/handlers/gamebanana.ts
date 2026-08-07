import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { getGameBananaLikeFailureContext } from "@main/services/gamebanana";
import { toErrorMessage } from "@shared/utils";

export function registerGameBananaHandlers(d: NahidaDesktop) {
    rh("gamebanana:ensureAuthenticated", async () => d.service.gamebanana.ensureSession());
    rh("gamebanana:getGames", async () => d.service.gamebanana.games);
    rh("gamebanana:setManualRmcToken", async (token: string) =>
        d.service.gamebanana.setManualRmcToken(token),
    );
    rh("gamebanana:getGameOverview", async (gameId: number) =>
        d.service.gamebanana.getGameOverview(gameId),
    );
    rh("gamebanana:getGameSubfeed", async (input) => d.service.gamebanana.getGameSubfeed(input));
    rh("gamebanana:getModCategoryOverview", async (input) =>
        d.service.gamebanana.getModCategoryOverview(input),
    );
    rh("gamebanana:getModIndex", async (input) => d.service.gamebanana.getModIndex(input));
    rh("gamebanana:getModOverview", async (input) => d.service.gamebanana.getModOverview(input));
    rh("gamebanana:toggleModLike", async (input) => {
        try {
            return await d.service.gamebanana.toggleModLike(input);
        } catch (error) {
            const failureContext = getGameBananaLikeFailureContext(error);
            d.logger.error(error, "GameBanana:toggleModLike");
            d.logger.error(
                {
                    channel: "gamebanana:toggleModLike",
                    operation: "toggleModLike",
                    itemId: input.itemId,
                    modelName: input.modelName ?? "Mod",
                    stage: failureContext?.stage ?? "unknown",
                    cacheState: failureContext?.cacheState ?? "unknown",
                    cleanupState: failureContext?.cleanupState ?? "unknown",
                    error: toErrorMessage(error),
                },
                "GameBanana:toggleModLike:context",
            );
            throw error;
        }
    });
    rh("gamebanana:getModPosts", async (input) => d.service.gamebanana.getModPosts(input));
    rh("gamebanana:logout", async () => d.service.gamebanana.logout());
}

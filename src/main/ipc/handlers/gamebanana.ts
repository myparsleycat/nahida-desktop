import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

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
    rh("gamebanana:getModOverview", async (input) =>
        d.service.gamebanana.getModOverview(input),
    );
    rh("gamebanana:getModPosts", async (input) => d.service.gamebanana.getModPosts(input));
    rh("gamebanana:logout", async () => d.service.gamebanana.logout());
}

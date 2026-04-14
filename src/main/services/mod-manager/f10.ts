import path from "node:path";
import { sendF10 } from "@native/native-mod";
import type { NahidaDesktop } from "../..";
import type { ModLibraryService } from "./library";

export class ModF10Service {
    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly library: ModLibraryService,
    ) {}

    public async triggerF10(modPath: string) {
        try {
            const groupPath = path.dirname(modPath);
            const group = await this.library.mods(groupPath);
            const activeCount = group.mods.filter((m) => m.isEnabled).length;

            if (activeCount <= 1) {
                const games = await this.library.games();
                const matchedGame = games.find((g) => modPath.startsWith(g.modFolderPath));
                if (!matchedGame) return;

                const pid = await this.library.gamePid(matchedGame.game);
                if (pid) {
                    try {
                        const sent = await sendF10(pid);
                        if (sent) {
                            this.desktop.logger.info(
                                `Sent F10 to ${matchedGame.game} (PID: ${pid})`,
                                "Mod:triggerF10",
                            );
                        } else {
                            this.desktop.logger.warn(
                                `Failed to send F10 to ${matchedGame.game} (PID: ${pid})`,
                                "Mod:triggerF10",
                            );
                        }
                    } catch (e) {
                        this.desktop.logger.error(e, "Mod:triggerF10:native");
                    }
                }
            }
        } catch (error) {
            this.desktop.logger.error(error, `Mod:triggerF10:${modPath}`);
        }
    }
}

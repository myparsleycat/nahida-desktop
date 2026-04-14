import type { NahidaDesktop } from "../..";
import type { ModLibraryService } from "./library";

export class ModWatchersService {
    private gameWatcherId: string | null = null;
    private characterWatcherId: string | null = null;

    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly library: ModLibraryService,
    ) {}

    public async watchGame(game: string) {
        const modFolderPath = await this.library.gamePath(game);
        if (!modFolderPath) return;

        if (this.gameWatcherId) {
            await this.desktop.lib.watcher.remove(this.gameWatcherId);
            this.gameWatcherId = null;
        }

        try {
            this.gameWatcherId = await this.desktop.lib.watcher.create(
                modFolderPath,
                { depth: 1 },
                (event) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        if (this.desktop.window.main.window) {
                            this.desktop.ipc.postMessageToWindow(
                                this.desktop.window.main.window,
                                "mod:update-game",
                            );
                        }
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchGame:${game}`);
        }
    }

    public async watchCharacter(characterPath: string) {
        if (this.characterWatcherId) {
            await this.desktop.lib.watcher.remove(this.characterWatcherId);
            this.characterWatcherId = null;
        }

        try {
            this.characterWatcherId = await this.desktop.lib.watcher.create(
                characterPath,
                { depth: 1 },
                (event) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        if (this.desktop.window.main.window) {
                            this.desktop.ipc.postMessageToWindow(
                                this.desktop.window.main.window,
                                "mod:update-mods",
                            );
                        }
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchCharacter:${characterPath}`);
        }
    }
}

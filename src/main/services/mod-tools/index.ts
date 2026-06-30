import type { NahidaDesktop } from "@/main";
import { FourThousandOneFixer } from "./4001-fixer";
import { FixTool } from "./fix-tool";
import { ModBisect } from "./mod-bisect";
import { StaticGlb } from "./static-glb";
import { TextureResizer } from "./texture-resizer";
import { TogglePersist } from "./toggle-persist";
import { ToggleViewer } from "./toggle-viewer";
import { WuwaModFixer } from "./wuwa-mod-fixer";

export class ModTools {
    public readonly fixTool: FixTool;
    public readonly fourThousandOneFixer: FourThousandOneFixer;
    public readonly togglePersist: TogglePersist;
    public readonly toggleViewer: ToggleViewer;
    public readonly staticGlb: StaticGlb;
    public readonly textureResizer: TextureResizer;
    public readonly wuwaModFixer: WuwaModFixer;
    public readonly modBisect: ModBisect;

    constructor(private readonly desktop: NahidaDesktop) {
        this.fixTool = new FixTool(this.desktop);
        this.fourThousandOneFixer = new FourThousandOneFixer(this.desktop);
        this.togglePersist = new TogglePersist(this.desktop);
        this.toggleViewer = new ToggleViewer(this.desktop);
        this.staticGlb = new StaticGlb(this.desktop);
        this.textureResizer = new TextureResizer(this.desktop);
        this.wuwaModFixer = new WuwaModFixer(this.desktop);
        const modBisect = (this.modBisect = new ModBisect(this.desktop));
        modBisect.recovering = (async () => {
            try {
                const games = await this.desktop.service.mod.get.games();
                await modBisect.recover(games);
            } catch (error) {
                this.desktop.logger.error(error, "ModBisect");
            }
        })();
    }

    public async startPersistWatcher() {
        await this.togglePersist.startPersistWatcher();
    }

    public async stopPersistWatcher() {
        await this.togglePersist.stopPersistWatcher();
    }

    public async startToggleViewerWatcher() {
        await this.toggleViewer.startWatcher();
    }

    public async stopToggleViewerWatcher() {
        await this.toggleViewer.stopWatcher();
    }
}

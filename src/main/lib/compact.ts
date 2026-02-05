import { app } from "electron";
import path from "node:path";
import { GoProcess } from "./go-process";
import { db } from "@main/internal/db";
import type { NahidaDesktop } from "@main/index";

export class CompactService {
    private desktop: NahidaDesktop;
    private process: GoProcess | null = null;
    private isEnabled = false;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async initialize() {
        let binPath = "";

        if (app.isPackaged) {
            binPath = path.join(process.resourcesPath, "compact.exe");
        } else {
            binPath = path.join(app.getAppPath(), "build", "compact", "compact.exe");
        }

        this.isEnabled = await this.desktop.setting.general.getGameFolderCompressionEnabled();

        this.process = new GoProcess({
            path: binPath,
            args: [],
            cwd: path.dirname(binPath),
        });

        this.process.on("log", (msg) => {
            this.desktop.logger.info(msg, "compact service");
            this.desktop.ipc.broadcast("compact:log", msg);
        });

        this.process.on("progress", (payload) => {
            this.desktop.ipc.broadcast("compact:progress", payload);
        });

        this.process.on("stderr", (err) => {
            this.desktop.logger.error(err, "compact service");
        });

        this.process.on("exit", (code) => {
            this.desktop.logger.info(`Process exited with code: ${code}`, "compact service");
        });

        try {
            this.process.start();
            this.desktop.logger.info("Started go process", "compact service");

            await this.updateCompression();
        } catch (e) {
            this.desktop.logger.error(e, "compact service");
        }
    }

    public async updateCompression() {
        this.isEnabled = await this.desktop.setting.general.getGameFolderCompressionEnabled();
        const games = await db.query.gamePaths.findMany();
        const paths = games.map((g) => g.modFolderPath);

        if (this.isEnabled) {
            if (paths.length > 0 && this.process) {
                this.process.write(
                    JSON.stringify({
                        type: "compress",
                        paths: paths,
                    }) + "\n",
                );
            }
        } else {
            if (paths.length > 0 && this.process) {
                this.process.write(
                    JSON.stringify({
                        type: "decompress",
                        paths: paths,
                    }) + "\n",
                );
            } else if (this.process) {
                this.process.write(
                    JSON.stringify({
                        type: "stop",
                        paths: [],
                    }) + "\n",
                );
            }
        }
    }
}

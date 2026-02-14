import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { app } from "electron";
import { GoProcess } from "./go-process";

export class CompactService {
    private desktop: NahidaDesktop;
    private process: GoProcess | null = null;
    private isEnabled = false;
    private isFeatureEnabled = false;
    private binPath = "";

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async initialize() {
        if (app.isPackaged) {
            this.binPath = path.join(process.resourcesPath, "compact", "compact.exe");
        } else {
            this.binPath = path.join(app.getAppPath(), "native", "compact", "compact.exe");
        }

        this.isFeatureEnabled =
            await this.desktop.setting.general.getGameFolderCompressionFeatureEnabled();

        if (this.isFeatureEnabled) {
            await this.startProcess();
            await this.updateCompression();
        }
    }

    private async startProcess() {
        if (this.process) return;

        this.desktop.logger.info(`Starting compact process at: ${this.binPath}`, "compact service");

        this.process = new GoProcess({
            path: this.binPath,
            args: [],
            cwd: path.dirname(this.binPath),
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
            this.process = null;
        });

        try {
            await this.process.start();
            this.desktop.logger.info("Started go process", "compact service");
        } catch (e) {
            this.desktop.logger.error(e, "compact service");
            this.process = null;
        }
    }

    private stopProcess() {
        if (this.process) {
            this.process.kill();
            this.process = null;
        }
    }

    public async updateFeature() {
        this.isFeatureEnabled =
            await this.desktop.setting.general.getGameFolderCompressionFeatureEnabled();

        if (this.isFeatureEnabled) {
            await this.startProcess();
            await this.updateCompression();
        } else {
            this.stopProcess();
        }
    }

    public async updateCompression() {
        if (!this.isFeatureEnabled) return;

        this.isEnabled = await this.desktop.setting.general.getGameFolderCompressionEnabled();
        const games = await this.desktop.lib.db.query.gamePaths.findMany();
        const paths = games.map((g) => g.modFolderPath);

        if (this.isEnabled) {
            if (paths.length > 0 && this.process) {
                this.process.write(
                    `${JSON.stringify({
                        type: "compress",
                        paths: paths,
                    })}\n`,
                );
            }
        } else {
            if (paths.length > 0 && this.process) {
                this.process.write(
                    `${JSON.stringify({
                        type: "decompress",
                        paths: paths,
                    })}\n`,
                );
            } else if (this.process) {
                this.process.write(
                    `${JSON.stringify({
                        type: "stop",
                        paths: [],
                    })}\n`,
                );
            }
        }
    }
}

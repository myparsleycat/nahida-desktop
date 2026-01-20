import pathModule from "path";
import pino, { type Logger as PinoLogger } from "pino";
import fse from "fs-extra";
import { createStream } from "rotating-file-stream";
import { app } from "electron";

export async function nahidaLogsPath(): Promise<string> {
    const configPath = pathModule.join(app.getPath("userData"), "logs");

    if (!(await fse.pathExists(configPath))) {
        await fse.mkdir(configPath, {
            recursive: true,
        });
    }

    return configPath;
}

export class Logger {
    private logger: PinoLogger | null = null;
    private dest: string | null = null;
    private isCleaning: boolean = false;
    private readonly disableLogging: boolean;
    private readonly isWorker: boolean;

    public constructor(disableLogging: boolean = false, isWorker: boolean = false) {
        this.disableLogging = disableLogging;
        this.isWorker = isWorker;

        this.init();
    }

    public async init(): Promise<void> {
        try {
            this.dest = pathModule.join(
                await nahidaLogsPath(),
                this.isWorker ? "desktop-worker.log" : "desktop.log",
            );

            this.logger = pino(
                createStream(pathModule.basename(this.dest), {
                    size: "10M",
                    interval: "7d",
                    compress: "gzip",
                    encoding: "utf-8",
                    maxFiles: 3,
                    path: pathModule.dirname(this.dest),
                }),
            );
        } catch (e) {
            console.error(e);
        }
    }

    public async waitForPino(): Promise<void> {
        if (this.logger) {
            return;
        }

        await new Promise<void>((resolve) => {
            const wait = setInterval(() => {
                if (this.logger) {
                    clearInterval(wait);

                    resolve();
                }
            }, 100);
        });
    }

    public log(
        level: "info" | "debug" | "warn" | "error" | "trace" | "fatal",
        object?: unknown,
        where?: string,
    ): void {
        if (this.isCleaning || this.disableLogging) {
            return;
        }

        (async () => {
            try {
                if (!this.logger) {
                    await this.waitForPino();
                }

                const log = `${where ? `[${where}] ` : ""}${
                    typeof object !== "undefined"
                        ? typeof object === "string" || typeof object === "number"
                            ? object
                            : JSON.stringify(object)
                        : ""
                }`;

                if (level === "info") {
                    this.logger?.info(log);
                } else if (level === "debug") {
                    this.logger?.debug(log);
                } else if (level === "error") {
                    this.logger?.error(log);

                    if (object instanceof Error) {
                        this.logger?.error(object);
                    }
                } else if (level === "warn") {
                    this.logger?.warn(log);
                } else if (level === "trace") {
                    this.logger?.trace(log);
                } else if (level === "fatal") {
                    this.logger?.fatal(log);
                } else {
                    this.logger?.info(log);
                }
            } catch (e) {
                console.error(e);
            }
        })();
    }

    public info(object?: unknown, where?: string): void {
        this.log("info", object, where);
    }

    public debug(object?: unknown, where?: string): void {
        this.log("debug", object, where);
    }

    public warn(object?: unknown, where?: string): void {
        this.log("warn", object, where);
    }

    public error(object?: unknown, where?: string): void {
        this.log("error", object, where);
    }

    public trace(object?: unknown, where?: string): void {
        this.log("trace", object, where);
    }

    public fatal(object?: unknown, where?: string): void {
        this.log("fatal", object, where);
    }
}

export default Logger;

import pathModule from "node:path";
import { is } from "@electron-toolkit/utils";
import { app } from "electron";
import fse from "fs-extra";
import pino, { type Logger as PinoLogger } from "pino";
import { createStream } from "rotating-file-stream";

export async function nahidaLogsPath(): Promise<string> {
    const configPath = pathModule.join(app.getPath("userData"), "logs");

    if (!(await fse.pathExists(configPath))) {
        await fse.mkdir(configPath, {
            recursive: true,
        });
    }

    return configPath;
}

export type LogLevel = "info" | "debug" | "warn" | "error" | "trace" | "fatal";

export class Logger {
    private logger: PinoLogger | null = null;
    private dest: string | null = null;
    private isCleaning: boolean = false;
    private readonly disableLogging: boolean;
    private readonly isWorker: boolean;
    private currentLevel: LogLevel = "error";

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

            if (is.dev) {
                return;
            }

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
            this.logger.level = this.currentLevel;
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

    public setLevel(level: LogLevel): void {
        this.currentLevel = level;
        if (this.logger) {
            this.logger.level = level;
        }
    }

    public log(level: LogLevel, object?: unknown, where?: string): void {
        if (this.isCleaning || this.disableLogging) {
            return;
        }

        if (is.dev) {
            const consoleArgs = where ? [`[${where}]`, object] : [object];
            if (level === "error" || level === "fatal") {
                console.error(...consoleArgs);
            } else if (level === "warn") {
                console.warn(...consoleArgs);
            } else if (level === "debug" || level === "trace") {
                console.debug(...consoleArgs);
            } else {
                console.log(...consoleArgs);
            }
            return;
        }

        (async () => {
            try {
                if (!this.logger) {
                    await this.waitForPino();
                }

                const logContent = `${where ? `[${where}] ` : ""}${
                    typeof object !== "undefined"
                        ? typeof object === "string" || typeof object === "number"
                            ? object
                            : JSON.stringify(object)
                        : ""
                }`;

                if (level === "info") {
                    this.logger?.info(logContent);
                } else if (level === "debug") {
                    this.logger?.debug(logContent);
                } else if (level === "error") {
                    this.logger?.error(logContent);

                    if (object instanceof Error) {
                        this.logger?.error(object);
                    }
                } else if (level === "warn") {
                    this.logger?.warn(logContent);
                } else if (level === "trace") {
                    this.logger?.trace(logContent);
                } else if (level === "fatal") {
                    this.logger?.fatal(logContent);
                } else {
                    this.logger?.info(logContent);
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

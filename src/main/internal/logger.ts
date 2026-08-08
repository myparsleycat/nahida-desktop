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

function isBrokenPipeError(error: unknown) {
    return typeof error === "object" && error !== null && "code" in error && error.code === "EPIPE";
}

export function installOutputStreamErrorGuards() {
    const ignoreOutputStreamError = (error: unknown) => {
        if (isBrokenPipeError(error)) return;

        // Console output is diagnostic only; never let its stream failure terminate the app.
    };

    process.stdout.on("error", ignoreOutputStreamError);
    process.stderr.on("error", ignoreOutputStreamError);
}

export class Logger {
    private logger: PinoLogger | null = null;
    private dest: string | null = null;
    private initPromise: Promise<void> | null = null;
    private isCleaning: boolean = false;
    private readonly disableLogging: boolean;
    private readonly isWorker: boolean;
    private currentLevel: LogLevel = "error";

    public constructor(disableLogging: boolean = false, isWorker: boolean = false) {
        this.disableLogging = disableLogging;
        this.isWorker = isWorker;

        void this.ensureInitialized();
    }

    private async ensureInitialized(): Promise<void> {
        if (this.initPromise) {
            await this.initPromise;
            return;
        }

        this.initPromise = (async () => {
            try {
                await app.whenReady();

                this.dest = pathModule.join(
                    await nahidaLogsPath(),
                    this.isWorker ? "desktop-worker.log" : "desktop.log",
                );

                if (is.dev || this.disableLogging) {
                    return;
                }

                this.logger = pino(
                    {
                        level: this.currentLevel,
                        base: undefined,
                        formatters: {
                            level: (label) => ({ level: label }),
                        },
                    },
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
                this.writeToConsole("error", e);
            }
        })();

        await this.initPromise;
    }

    private shouldWrite(level: LogLevel): boolean {
        const priorities: Record<LogLevel, number> = {
            trace: 10,
            debug: 20,
            info: 30,
            warn: 40,
            error: 50,
            fatal: 60,
        };

        return priorities[level] >= priorities[this.currentLevel];
    }

    private writeToConsole(level: LogLevel, ...consoleArgs: Parameters<typeof console.log>) {
        try {
            if (level === "error" || level === "fatal") {
                console.error(...consoleArgs);
            } else if (level === "warn") {
                console.warn(...consoleArgs);
            } else if (level === "debug" || level === "trace") {
                console.debug(...consoleArgs);
            } else {
                console.log(...consoleArgs);
            }
        } catch (error) {
            if (!isBrokenPipeError(error)) throw error;
        }
    }

    private async writeFallback(
        level: LogLevel,
        logContent: string,
        object?: unknown,
    ): Promise<void> {
        if (!this.dest || !this.shouldWrite(level)) {
            return;
        }

        const line = JSON.stringify({
            level,
            time: new Date().toISOString(),
            msg: logContent,
            err:
                object instanceof Error
                    ? { name: object.name, message: object.message, stack: object.stack }
                    : undefined,
        });

        await fse.appendFile(this.dest, `${line}\n`, "utf8");
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
            this.writeToConsole(level, ...consoleArgs);
            return;
        }

        void (async () => {
            try {
                const objectContent =
                    typeof object === "undefined"
                        ? ""
                        : typeof object === "string" || typeof object === "number"
                          ? String(object)
                          : JSON.stringify(object);
                const logContent = `${where ? `[${where}] ` : ""}${objectContent}`;

                await this.ensureInitialized();

                if (!this.logger) {
                    await this.writeFallback(level, logContent, object);
                    return;
                }

                if (level === "info") {
                    this.logger.info(logContent);
                } else if (level === "debug") {
                    this.logger.debug(logContent);
                } else if (level === "error") {
                    this.logger.error(logContent);

                    if (object instanceof Error) {
                        this.logger.error(object);
                    }
                } else if (level === "warn") {
                    this.logger.warn(logContent);
                } else if (level === "trace") {
                    this.logger.trace(logContent);
                } else if (level === "fatal") {
                    this.logger.fatal(logContent);
                } else {
                    this.logger.info(logContent);
                }
            } catch (e) {
                this.writeToConsole("error", e);
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

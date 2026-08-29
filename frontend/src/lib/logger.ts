import { Log } from "@bindings/infra";

type LogLevel = "info" | "debug" | "warn" | "error" | "trace" | "fatal";

export class Logger {
    public static log(level: LogLevel, object: unknown, where?: string) {
        void Log.Log(level, object, where ?? "renderer");

        if (import.meta.env.DEV) {
            const consoleLog = `${where ? `[${where}] ` : ""}${
                typeof object !== "undefined"
                    ? typeof object === "string" || typeof object === "number"
                        ? object
                        : JSON.stringify(object)
                    : ""
            }`;

            if (level === "error") console.error(consoleLog);
            else if (level === "warn") console.warn(consoleLog);
            else console.log(consoleLog);
        }
    }

    public static info(object: unknown, where?: string) {
        Logger.log("info", object, where);
    }

    public static debug(object: unknown, where?: string) {
        Logger.log("debug", object, where);
    }

    public static warn(object: unknown, where?: string) {
        Logger.log("warn", object, where);
    }

    public static error(object: unknown, where?: string) {
        Logger.log("error", object, where);
    }
}

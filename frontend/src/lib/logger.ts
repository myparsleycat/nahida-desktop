import { Log } from "@bindings/infra";
import { isSilentDiagnostic, serializeDiagnostic } from "@shared/diagnostic";

type LogLevel = "info" | "debug" | "warn" | "error" | "trace" | "fatal";

export class Logger {
    public static log(level: LogLevel, object: unknown, where?: string) {
        if (level === "error" || level === "warn") {
            if (isSilentDiagnostic(object)) return;
            if (object && typeof object === "object") {
                const descriptor = Object.getOwnPropertyDescriptor(object, "error");
                if (descriptor && "value" in descriptor && isSilentDiagnostic(descriptor.value))
                    return;
            }
        }
        const normalized = serializeDiagnostic(object);
        try {
            void Log.Log(level, normalized, where ?? "renderer").catch((error: unknown) => {
                Logger.fallback(error, normalized, where);
            });
        } catch (error) {
            Logger.fallback(error, normalized, where);
        }

        if (import.meta.env.DEV) {
            const consoleLog = `${where ? `[${where}] ` : ""}${typeof normalized === "string" ? normalized : (JSON.stringify(normalized) ?? "")}`;

            if (level === "error") console.error(consoleLog);
            else if (level === "warn") console.warn(consoleLog);
            else console.log(consoleLog);
        }
    }

    private static fallbackReported = false;

    private static fallback(error: unknown, record: unknown, where?: string) {
        if (Logger.fallbackReported) return;
        Logger.fallbackReported = true;
        console.error(
            "Failed to persist diagnostic",
            serializeDiagnostic({ error, record, where }),
        );
    }

    public static capture(where: string, ...details: unknown[]) {
        const remaining = details.filter((detail) => !isSilentDiagnostic(detail));
        if (
            remaining.length !== details.length &&
            !remaining.some((detail) => detail instanceof Error)
        )
            return;
        Logger.error({ details: remaining }, where);
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

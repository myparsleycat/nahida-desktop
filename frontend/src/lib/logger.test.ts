import { beforeEach, describe, expect, it, vi } from "vitest";

const log = vi.hoisted(() => vi.fn<(...args: unknown[]) => Promise<void>>());
vi.mock("@bindings/infra", () => ({ Log: { Log: log } }));
import { Logger } from "./logger";

describe("Logger", () => {
    beforeEach(() => {
        log.mockReset().mockResolvedValue(undefined);
    });

    it("passes serializable error details to the backend", () => {
        Logger.error(new Error("file denied"), "save");
        expect(log).toHaveBeenCalledWith(
            "error",
            expect.objectContaining({ message: "file denied" }),
            "save",
        );
    });

    it("does not duplicate already reported Wails failures", () => {
        const error = new Error("service failed", { cause: "FAILED" });
        error.name = "RuntimeError";
        Logger.capture("caller", "failed action", error);
        expect(log).not.toHaveBeenCalled();
    });

    it("handles a rejected logging call without recursive calls", async () => {
        const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
        log.mockRejectedValueOnce(new Error("bridge unavailable"));
        Logger.error(new Error("original failure"));
        await Promise.resolve();
        expect(log).toHaveBeenCalledTimes(1);
        expect(consoleError).toHaveBeenCalled();
        consoleError.mockRestore();
    });
    it("keeps a new cleanup failure beside an already reported service error", () => {
        const service = new Error("service failed", { cause: "FAILED" });
        service.name = "RuntimeError";
        Logger.capture("cleanup", service, new Error("cleanup denied"));
        expect(log).toHaveBeenCalledWith(
            "error",
            { details: [expect.objectContaining({ message: "cleanup denied" })] },
            "cleanup",
        );
    });
});

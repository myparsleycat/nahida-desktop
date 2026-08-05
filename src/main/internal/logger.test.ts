import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    app: {
        getPath: vi.fn(() => "C:\\temp\\nahida"),
        whenReady: vi.fn(async () => undefined),
    },
}));

vi.mock("@electron-toolkit/utils", () => ({
    is: { dev: true },
}));

vi.mock("electron", () => ({ app: mocks.app }));

vi.mock("fs-extra", () => ({
    default: {
        appendFile: vi.fn(),
        mkdir: vi.fn(),
        pathExists: vi.fn(async () => true),
    },
}));

vi.mock("pino", () => ({ default: vi.fn() }));
vi.mock("rotating-file-stream", () => ({ createStream: vi.fn() }));

import Logger from "./logger";

describe("Logger", () => {
    afterEach(() => {
        vi.restoreAllMocks();
    });

    it("ignores EPIPE when a development console pipe is closed", () => {
        const logger = new Logger();
        vi.spyOn(console, "warn").mockImplementation(() => {
            throw Object.assign(new Error("broken pipe"), { code: "EPIPE" });
        });

        expect(() => logger.warn("download retry")).not.toThrow();
    });
});

import { subscribeDriveUploadCompleted } from "@renderer/hooks/use-drive-upload-refresh";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
    const eventHandlers = new Map<string, (event: { data: unknown }) => void>();
    return { eventHandlers };
});

vi.mock("@wailsio/runtime", () => ({
    Events: {
        On: (name: string, handler: (event: { data: unknown }) => void) => {
            mocks.eventHandlers.set(name, handler);
            return () => mocks.eventHandlers.delete(name);
        },
    },
}));

function emitUploadCompleted(data: unknown) {
    const handler = mocks.eventHandlers.get("drive:upload-completed");
    if (!handler) {
        throw new Error("drive:upload-completed listener is not registered");
    }
    handler({ data });
}

afterEach(() => {
    mocks.eventHandlers.clear();
});

describe("drive upload completion events", () => {
    it("delivers a valid upload destination", () => {
        const listener = vi.fn();
        const unsubscribe = subscribeDriveUploadCompleted(listener);

        emitUploadCompleted({ pid: "upload-1", currentId: "destination" });

        expect(listener).toHaveBeenCalledOnce();
        expect(listener).toHaveBeenCalledWith({ pid: "upload-1", currentId: "destination" });

        unsubscribe();
        expect(mocks.eventHandlers.has("drive:upload-completed")).toBe(false);
    });

    it.each([null, [], {}, { pid: "upload-1" }, { currentId: "destination" }])(
        "ignores an invalid payload: %j",
        (payload) => {
            const listener = vi.fn();
            subscribeDriveUploadCompleted(listener);

            emitUploadCompleted(payload);

            expect(listener).not.toHaveBeenCalled();
        },
    );
});

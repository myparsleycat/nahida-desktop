import { subscribeWindowFileDrops } from "@renderer/wails/file-drop";
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

function emitFilesDropped(data: unknown) {
    const handler = mocks.eventHandlers.get("window:files-dropped");
    if (!handler) {
        throw new Error("window:files-dropped listener is not registered");
    }
    handler({ data });
}

function dropPayload() {
    return {
        paths: ["C:\\mods\\pack.zip", "C:\\mods\\folder"],
        target: {
            x: 10,
            y: 20,
            id: "mod-content-file-drop",
            classList: ["drop-zone"],
            attributes: {
                "data-file-drop-target": "",
                "data-file-drop-group-path": "C:\\mods\\Character",
            },
        },
    };
}

const cleanups: (() => void)[] = [];

afterEach(() => {
    for (const cleanup of cleanups.splice(0, cleanups.length)) {
        cleanup();
    }
});

describe("Wails file drop events", () => {
    it("delivers paths and drop target details from the official Wails event", () => {
        const listener = vi.fn();
        cleanups.push(subscribeWindowFileDrops(listener));

        const payload = dropPayload();
        emitFilesDropped(payload);

        expect(listener).toHaveBeenCalledOnce();
        expect(listener).toHaveBeenCalledWith(payload);
    });

    it("supports independent subscribers and unsubscription", () => {
        const first = vi.fn();
        const second = vi.fn();
        const unsubscribeFirst = subscribeWindowFileDrops(first);
        cleanups.push(subscribeWindowFileDrops(second));

        unsubscribeFirst();
        emitFilesDropped(dropPayload());

        expect(first).not.toHaveBeenCalled();
        expect(second).toHaveBeenCalledOnce();
    });

    it.each([
        null,
        [],
        { paths: "C:\\file.txt", target: {} },
        { paths: [1], target: {} },
        { paths: ["C:\\file.txt"], target: null },
        {
            paths: ["C:\\file.txt"],
            target: { x: 1, y: 2, id: "target", classList: [], attributes: { invalid: 3 } },
        },
    ])("ignores malformed event data %#", (data) => {
        const listener = vi.fn();
        cleanups.push(subscribeWindowFileDrops(listener));

        emitFilesDropped(data);

        expect(listener).not.toHaveBeenCalled();
    });

    it("accepts an omitted target attributes map", () => {
        const listener = vi.fn();
        cleanups.push(subscribeWindowFileDrops(listener));

        const payload = dropPayload();
        delete (payload.target as { attributes?: Record<string, string> }).attributes;
        emitFilesDropped(payload);

        expect(listener).toHaveBeenCalledWith({
            ...payload,
            target: { ...payload.target, attributes: {} },
        });
    });
});

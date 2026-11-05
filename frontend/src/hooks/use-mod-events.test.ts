// @vitest-environment jsdom
import { QueryClient } from "@tanstack/react-query";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useModWatcherEvents } from "./use-mod-events";

const listeners = vi.hoisted(() => new Map<string, () => void>());

vi.mock("@wailsio/runtime", () => ({
    Events: {
        On: vi.fn((name: string, listener: () => void) => {
            listeners.set(name, listener);
            return () => listeners.delete(name);
        }),
    },
}));

afterEach(() => {
    cleanup();
    vi.useRealTimers();
});

describe("useModWatcherEvents", () => {
    it("merges game and folder events and refreshes only the selected group's listings", async () => {
        vi.useFakeTimers();
        const client = new QueryClient();
        client.setQueryData(["games"], [{ game: "Game", modFolderPath: "D:/Mods" }]);
        client.setQueryData(["subGroups", "D:\\Mods\\Char\\Sub\\Inner"], []);
        client.setQueryData(["subGroups", "D:\\Mods\\Other"], []);
        const invalidate = vi.spyOn(client, "invalidateQueries");
        renderHook(() => useModWatcherEvents("Game", "D:\\Mods\\Char\\Sub", client));

        listeners.get("mod:update-mods")?.();
        listeners.get("mod:update-game")?.();
        expect(invalidate).not.toHaveBeenCalled();
        await vi.advanceTimersByTimeAsync(100);

        expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
            ["characters", "Game"],
            ["modGroup", "D:\\Mods\\Char\\Sub"],
            ["subGroups", "D:\\Mods\\Char\\Sub"],
            ["subGroups", "D:\\Mods\\Char"],
            ["subGroups", "D:\\Mods\\Char\\Sub\\Inner"],
            ["subGroups"],
        ]);

        invalidate.mockClear();
        listeners.get("mod:update-mods")?.();
        await vi.advanceTimersByTimeAsync(100);
        const queryKeys = invalidate.mock.calls.map(([filters]) => filters?.queryKey);
        expect(queryKeys).toContainEqual(["subGroups", "D:\\Mods\\Char\\Sub\\Inner"]);
        expect(queryKeys).not.toContainEqual(["subGroups", "D:\\Mods\\Other"]);
        expect(queryKeys).not.toContainEqual(["subGroups"]);
    });
});

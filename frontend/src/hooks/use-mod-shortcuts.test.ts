// @vitest-environment jsdom
import { modStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useModShortcuts } from "./use-mod-shortcuts";

vi.mock("@bindings/mod", () => ({ Mod: {} }));
vi.mock("@renderer/hooks/use-mod-mutations", () => ({
    useModMutations: () => ({ exclusiveToggleModMutation: { mutate: vi.fn() } }),
}));
vi.mock("@renderer/store/global", () => ({
    globalStore: { getState: () => ({ transfers: [] }) },
}));

const mod = {
    id: "mod",
    name: "Alpha",
    path: "C:/Mods/Char/Alpha",
    isEnabled: false,
    mtime: 1,
    size: 1,
    inis: [],
} satisfies ModInfo;

afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
    modStore.getState().setSearchQuery("");
});

describe("useModShortcuts", () => {
    it("applies an Enter pressed before the deferred search caught up", () => {
        const input = document.createElement("input");
        input.id = "mod-search-input";
        document.body.append(input);
        input.focus();
        const confirmEnable = vi.fn();
        const view = renderHook(({ query, mods }) => useModShortcuts(query, mods, confirmEnable), {
            initialProps: { query: "", mods: [mod] as ModInfo[] },
        });

        modStore.getState().setSearchQuery("alpha");
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));
        expect(confirmEnable).not.toHaveBeenCalled();

        view.rerender({ query: "alpha", mods: [mod] });
        expect(confirmEnable).toHaveBeenCalledWith(mod, expect.any(Function));
        expect(modStore.getState().searchQuery).toBe("");
    });

    it("drops a pending Enter once the search input loses focus", () => {
        const input = document.createElement("input");
        input.id = "mod-search-input";
        document.body.append(input);
        input.focus();
        const confirmEnable = vi.fn();
        const view = renderHook(({ query, mods }) => useModShortcuts(query, mods, confirmEnable), {
            initialProps: { query: "", mods: [mod] as ModInfo[] },
        });

        modStore.getState().setSearchQuery("alpha");
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));
        input.blur();
        view.rerender({ query: "alpha", mods: [mod] });

        expect(confirmEnable).not.toHaveBeenCalled();
    });
});

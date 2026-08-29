import { installExternalWindowHandler } from "@renderer/wails/external-window";
import { describe, expect, it, vi } from "vitest";

function testTargets() {
    let clickHandler: ((event: MouseEvent) => void) | undefined;
    const originalOpen = vi.fn(() => ({}) as WindowProxy);
    const runtimeWindow = {
        location: { href: "https://app.local/#/gamebanana" },
        open: originalOpen as typeof window.open,
    };
    const runtimeDocument = {
        addEventListener: vi.fn((_type: "click", listener: (event: MouseEvent) => void) => {
            clickHandler = listener;
        }),
        removeEventListener: vi.fn(),
    };
    return { runtimeWindow, runtimeDocument, originalOpen, getClickHandler: () => clickHandler };
}

function clickEvent(href: string, target: string) {
    const preventDefault = vi.fn();
    const anchor = {
        tagName: "A",
        getAttribute: (name: string) => ({ href, target })[name as "href" | "target"] ?? null,
        hasAttribute: () => false,
    };
    return {
        event: {
            button: 0,
            defaultPrevented: false,
            composedPath: () => [anchor],
            preventDefault,
        } as unknown as MouseEvent,
        preventDefault,
    };
}

describe("external window interception", () => {
    it("denies window.open and forwards its resolved URL", () => {
        const targets = testTargets();
        const openExternal = vi.fn(async () => undefined);
        const cleanup = installExternalWindowHandler(
            openExternal,
            targets.runtimeWindow,
            targets.runtimeDocument,
        );

        expect(targets.runtimeWindow.open("/docs", "_blank")).toBeNull();
        expect(openExternal).toHaveBeenCalledWith("https://app.local/docs");
        expect(targets.originalOpen).not.toHaveBeenCalled();

        cleanup();
        expect(targets.runtimeWindow.open).toBe(targets.originalOpen);
        expect(targets.runtimeDocument.removeEventListener).toHaveBeenCalledOnce();
    });

    it("prevents target=_blank navigation and opens it externally", () => {
        const targets = testTargets();
        const openExternal = vi.fn(async () => undefined);
        installExternalWindowHandler(openExternal, targets.runtimeWindow, targets.runtimeDocument);

        const { event, preventDefault } = clickEvent("https://gamebanana.com/mods/1", "_blank");
        targets.getClickHandler()?.(event);

        expect(preventDefault).toHaveBeenCalledOnce();
        expect(openExternal).toHaveBeenCalledWith("https://gamebanana.com/mods/1");
    });

    it("leaves same-context links to the router", () => {
        const targets = testTargets();
        const openExternal = vi.fn(async () => undefined);
        installExternalWindowHandler(openExternal, targets.runtimeWindow, targets.runtimeDocument);

        const { event, preventDefault } = clickEvent("#/setting", "_self");
        targets.getClickHandler()?.(event);

        expect(preventDefault).not.toHaveBeenCalled();
        expect(openExternal).not.toHaveBeenCalled();
    });
});

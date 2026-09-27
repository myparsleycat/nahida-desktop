// @vitest-environment jsdom

import {
    resetDriveFolderScroll,
    useDriveFolderScroll,
} from "@renderer/hooks/use-drive-folder-scroll";
import { viewStore } from "@renderer/store/drive";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
    cleanup();
    viewStore.getState().setPendingDriveRevealId(null);
    viewStore.getState().setPendingShareRevealId(null);
});

describe("resetDriveFolderScroll", () => {
    it("resets the pane and the nested scroll viewport", () => {
        const pane = document.createElement("div");
        const viewport = document.createElement("div");
        viewport.dataset.slot = "scroll-area-viewport";
        pane.append(viewport);

        const paneScrollTo = vi.fn();
        const viewportScrollTo = vi.fn();
        pane.scrollTo = paneScrollTo;
        viewport.scrollTo = viewportScrollTo;

        resetDriveFolderScroll(pane);

        expect(paneScrollTo).toHaveBeenCalledWith({ top: 0, left: 0 });
        expect(viewportScrollTo).toHaveBeenCalledWith({ top: 0, left: 0 });
    });
});

describe("useDriveFolderScroll", () => {
    it("resets scroll when the folder changes", () => {
        const pane = document.createElement("div");
        pane.scrollTo = vi.fn();
        const { result, rerender } = renderHook(({ folderId }) => useDriveFolderScroll(folderId), {
            initialProps: { folderId: "folder-a" },
        });
        result.current.current = pane;

        rerender({ folderId: "folder-b" });

        expect(pane.scrollTo).toHaveBeenCalledWith({ top: 0, left: 0 });
    });

    it("still resets when a stale reveal id is pending", () => {
        viewStore.getState().setPendingShareRevealId("child-folder");
        const pane = document.createElement("div");
        pane.scrollTo = vi.fn();
        const { result, rerender } = renderHook(({ folderId }) => useDriveFolderScroll(folderId), {
            initialProps: { folderId: "child-folder" },
        });
        result.current.current = pane;

        rerender({ folderId: "parent-folder" });

        expect(pane.scrollTo).toHaveBeenCalledWith({ top: 0, left: 0 });
    });
});

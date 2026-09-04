// @vitest-environment jsdom

import type { CompressionState } from "@bindings/mod";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

let currentState: CompressionState | null;

vi.mock("@renderer/hooks/use-mod-compression-state", () => ({
    useModCompressionState: () => [currentState],
}));

vi.mock("react-i18next", () => ({
    useTranslation: () => ({ t: (key: string) => key }),
}));

import { useModCompressionTitlebarActivity } from "./use-mod-compression-titlebar-activity";

afterEach(() => {
    cleanup();
    titlebarActivityStore.getState().removeActivity("mod:compression");
});

describe("useModCompressionTitlebarActivity", () => {
    it("removes its activity when unmounted", () => {
        currentState = {
            enabled: true,
            method: "zstd",
            thresholdMiB: 4,
            status: "compressing",
            processedFiles: 1,
            totalFiles: 2,
            processedBytes: 10,
            totalBytes: 20,
            failedFiles: 0,
            externalFiles: 0,
            canToggle: false,
            canConfigure: false,
            canDecompressExternal: false,
        };

        const { unmount } = renderHook(() => useModCompressionTitlebarActivity());
        expect(titlebarActivityStore.getState().activities["mod:compression"]).toBeDefined();

        unmount();

        expect(titlebarActivityStore.getState().activities["mod:compression"]).toBeUndefined();
    });
});

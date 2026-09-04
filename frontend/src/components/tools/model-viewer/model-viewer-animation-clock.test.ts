// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ModelViewerAnimationClip } from "./model-viewer-contract";

import {
    advanceAnimationFrame,
    normalizeAnimationFPS,
    useModelViewerAnimationClock,
} from "./model-viewer-animation-clock";

const clip: ModelViewerAnimationClip = {
    id: "animation",
    label: "Animation",
    variableIds: [],
    fps: 60,
    frameStart: 0,
    frameEnd: 3,
    loop: true,
    frames: Array.from({ length: 4 }, (_, index) => ({
        index,
        time: index / 60,
        values: {},
    })),
};

describe("model viewer animation clock", () => {
    it("preserves valid source FPS and falls back for invalid values", () => {
        expect(normalizeAnimationFPS(0.5)).toBe(0.5);
        expect(normalizeAnimationFPS(30)).toBe(30);
        expect(normalizeAnimationFPS(120)).toBe(120);
        expect(normalizeAnimationFPS(Number.NaN)).toBe(1);
    });

    it("wraps loop clips and stops non-loop clips at their final frame", () => {
        expect(advanceAnimationFrame(3, 1000 / 60, 60, 4, true).frameIndex).toBe(0);
        expect(advanceAnimationFrame(2, 1000, 60, 4, false)).toMatchObject({
            frameIndex: 3,
            complete: true,
        });
    });

    it("jumps to the elapsed target frame without emitting intermediate frames", () => {
        expect(advanceAnimationFrame(0, 10 * (1000 / 60), 60, 100, true).frameIndex).toBe(10);
    });

    it("keeps high-FPS clips on their original timeline while skipping render frames", () => {
        const frameDuration = 1000 / 120;
        expect(advanceAnimationFrame(0, 2 * frameDuration, 120, 120, true).frameIndex).toBe(2);
        expect(
            advanceAnimationFrame(0, 120 * frameDuration + 1e-6, 120, 120, true).frameIndex,
        ).toBe(0);
    });

    it("cancels its pending frame when playback pauses or unmounts", () => {
        let nextId = 0;
        const callbacks = new Map<number, FrameRequestCallback>();
        const request = vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
            const id = ++nextId;
            callbacks.set(id, callback);
            return id;
        });
        const cancel = vi
            .spyOn(window, "cancelAnimationFrame")
            .mockImplementation((id) => void callbacks.delete(id));
        const onFrame = vi.fn();
        const { rerender, unmount } = renderHook(
            ({ playing }) =>
                useModelViewerAnimationClock({ clip, frameIndex: 0, playing, onFrame }),
            { initialProps: { playing: true } },
        );

        expect(request).toHaveBeenCalledOnce();
        rerender({ playing: false });
        expect(cancel).toHaveBeenCalledOnce();
        rerender({ playing: true });
        unmount();
        expect(cancel).toHaveBeenCalledTimes(2);
    });
});

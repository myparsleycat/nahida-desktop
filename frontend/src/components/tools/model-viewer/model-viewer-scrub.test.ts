// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import type { PointerEvent } from "react";
import { describe, expect, it, vi } from "vitest";

import { useModelViewerScrubPlayback } from "./model-viewer-scrub";

const createPointerEvent = () =>
    ({
        button: 0,
        pointerId: 1,
        currentTarget: { setPointerCapture: vi.fn() },
    }) as unknown as PointerEvent<HTMLInputElement>;

describe("model viewer scrub playback", () => {
    it("pauses on pointer down and resumes the prior state on release", () => {
        const setPlaying = vi.fn();
        const event = createPointerEvent();
        const { result, rerender } = renderHook(
            ({ playing }) => useModelViewerScrubPlayback({ playing, setPlaying }),
            { initialProps: { playing: true } },
        );

        result.current.onPointerDown(event);
        expect(setPlaying).toHaveBeenLastCalledWith(false);
        expect(event.currentTarget.setPointerCapture).toHaveBeenCalledWith(1);

        rerender({ playing: false });
        result.current.onPointerUp(event);
        expect(setPlaying).toHaveBeenLastCalledWith(true);
    });

    it("stays paused when scrubbing from a stopped state", () => {
        const setPlaying = vi.fn();
        const { result } = renderHook(() =>
            useModelViewerScrubPlayback({ playing: false, setPlaying }),
        );

        result.current.onPointerDown(createPointerEvent());
        result.current.onPointerUp(createPointerEvent());
        expect(setPlaying).not.toHaveBeenCalledWith(true);
    });

    it("resumes once through pointer up or lost pointer capture", () => {
        const setPlaying = vi.fn();
        const { result } = renderHook(() =>
            useModelViewerScrubPlayback({ playing: true, setPlaying }),
        );

        result.current.onPointerDown(createPointerEvent());
        result.current.onLostPointerCapture(createPointerEvent());
        result.current.onPointerUp(createPointerEvent());
        expect(setPlaying).toHaveBeenLastCalledWith(true);
        expect(setPlaying).toHaveBeenCalledTimes(2);
    });

    it("ignores non-primary buttons", () => {
        const setPlaying = vi.fn();
        const event = createPointerEvent();
        const { result } = renderHook(() =>
            useModelViewerScrubPlayback({ playing: true, setPlaying }),
        );

        result.current.onPointerDown({ ...event, button: 2 } as PointerEvent<HTMLInputElement>);
        result.current.onPointerUp(event);
        expect(setPlaying).not.toHaveBeenCalled();
    });
});

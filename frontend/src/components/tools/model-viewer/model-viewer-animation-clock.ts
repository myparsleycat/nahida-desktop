import { useEffect, useRef } from "react";

import type { ModelViewerAnimationClip } from "./model-viewer-contract";

export function normalizeAnimationFPS(fps: number): number {
    if (!Number.isFinite(fps) || fps <= 0) return 1;
    return Math.min(60, Math.max(1, fps));
}

export function advanceAnimationFrame(
    current: number,
    elapsedMs: number,
    fps: number,
    frameCount: number,
    loop: boolean,
): { frameIndex: number; consumedMs: number; complete: boolean } {
    if (frameCount <= 1) return { frameIndex: 0, consumedMs: 0, complete: true };
    const frameDuration = 1000 / normalizeAnimationFPS(fps);
    const steps = Math.floor(Math.max(0, elapsedMs) / frameDuration);
    if (steps === 0) return { frameIndex: current, consumedMs: 0, complete: false };
    if (loop) {
        return {
            frameIndex: (current + steps) % frameCount,
            consumedMs: steps * frameDuration,
            complete: false,
        };
    }
    const frameIndex = Math.min(current + steps, frameCount - 1);
    return {
        frameIndex,
        consumedMs: steps * frameDuration,
        complete: frameIndex === frameCount - 1,
    };
}

export function useModelViewerAnimationClock({
    clip,
    frameIndex,
    playing,
    onFrame,
    onComplete,
}: {
    clip: ModelViewerAnimationClip | null;
    frameIndex: number;
    playing: boolean;
    onFrame: (frameIndex: number) => void;
    onComplete?: () => void;
}): void {
    const onFrameRef = useRef(onFrame);
    const onCompleteRef = useRef(onComplete);
    const lastAppliedTimeRef = useRef(0);
    const currentFrameRef = useRef(frameIndex);
    const frameDurationRef = useRef(1000 / normalizeAnimationFPS(clip?.fps ?? 1));
    const animationFrameRef = useRef<number | null>(null);

    useEffect(() => {
        onFrameRef.current = onFrame;
        onCompleteRef.current = onComplete;
    }, [onComplete, onFrame]);

    useEffect(() => {
        const lastFrame = Math.max((clip?.frames.length ?? 1) - 1, 0);
        const nextFrame = Math.min(Math.max(frameIndex, 0), lastFrame);
        if (currentFrameRef.current !== nextFrame) {
            currentFrameRef.current = nextFrame;
            lastAppliedTimeRef.current = performance.now();
        }
    }, [clip?.frames.length, frameIndex]);

    useEffect(() => {
        const frameCount = clip?.frames.length ?? 0;
        if (!clip || !playing || frameCount <= 1) return;

        const fps = normalizeAnimationFPS(clip.fps);
        frameDurationRef.current = 1000 / fps;
        currentFrameRef.current = Math.min(Math.max(currentFrameRef.current, 0), frameCount - 1);
        lastAppliedTimeRef.current = performance.now();

        const tick = (now: number) => {
            const advanced = advanceAnimationFrame(
                currentFrameRef.current,
                now - lastAppliedTimeRef.current,
                fps,
                frameCount,
                clip.loop,
            );
            if (advanced.consumedMs > 0) {
                lastAppliedTimeRef.current += advanced.consumedMs;
            }
            if (advanced.frameIndex !== currentFrameRef.current) {
                currentFrameRef.current = advanced.frameIndex;
                onFrameRef.current(advanced.frameIndex);
            }
            if (!advanced.complete) {
                animationFrameRef.current = requestAnimationFrame(tick);
            } else {
                animationFrameRef.current = null;
                onCompleteRef.current?.();
            }
        };

        animationFrameRef.current = requestAnimationFrame(tick);
        return () => {
            if (animationFrameRef.current !== null) {
                cancelAnimationFrame(animationFrameRef.current);
                animationFrameRef.current = null;
            }
        };
    }, [clip?.id, clip?.fps, clip?.frames.length, clip?.loop, playing]);
}

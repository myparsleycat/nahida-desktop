import { useRef, type PointerEvent } from "react";

export function useModelViewerScrubPlayback({
    playing,
    setPlaying,
}: {
    playing: boolean;
    setPlaying: (playing: boolean) => void;
}) {
    const resumeRef = useRef<boolean | null>(null);

    const endScrub = () => {
        const resume = resumeRef.current;
        if (resume === null) {
            return;
        }
        resumeRef.current = null;
        setPlaying(resume);
    };

    return {
        onPointerDown: (event: PointerEvent<HTMLInputElement>) => {
            if (event.button !== 0 || resumeRef.current !== null) {
                return;
            }
            resumeRef.current = playing;
            setPlaying(false);
            // Capture the pointer so pointerup still fires when released outside the input.
            event.currentTarget.setPointerCapture(event.pointerId);
        },
        onPointerUp: endScrub,
        onLostPointerCapture: endScrub,
    };
}

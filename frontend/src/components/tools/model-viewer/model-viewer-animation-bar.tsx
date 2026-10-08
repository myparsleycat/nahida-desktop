import { Button } from "@renderer/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { PauseIcon, PlayIcon } from "lucide-react";
import { useEffect, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";

import type { ModelViewerAnimationClip, ModelViewerHandle } from "./model-viewer-contract";

import { useModelViewerAnimationClock } from "./model-viewer-animation-clock";
import { ModelViewerFpsControl } from "./model-viewer-fps-control";
import { useModelViewerScrubPlayback } from "./model-viewer-scrub";

// Owns the per-frame state so playback re-renders only this row, not the
// whole workspace with its toggle panel.
export function ModelViewerAnimationBar({
  clip,
  clips,
  effectiveClip,
  fpsOverride,
  onClipChange,
  onFpsChange,
  onFrameIndexChange,
  playing,
  setPlaying,
  viewerRef,
}: {
  clip: ModelViewerAnimationClip;
  clips: ModelViewerAnimationClip[];
  effectiveClip: ModelViewerAnimationClip;
  fpsOverride: number | null;
  onClipChange: (clipId: string | null) => void;
  onFpsChange: (fps: number | null) => void;
  onFrameIndexChange: (frameIndex: number) => void;
  playing: boolean;
  setPlaying: (update: boolean | ((current: boolean) => boolean)) => void;
  viewerRef: RefObject<ModelViewerHandle | null>;
}) {
  const { t } = useTranslation();
  const [frameIndex, setFrameIndex] = useState(0);
  const [prevClip, setPrevClip] = useState(clip);

  if (prevClip !== clip) {
    setPrevClip(clip);
    setFrameIndex(0);
  }

  useEffect(() => {
    onFrameIndexChange(frameIndex);
    viewerRef.current?.setAnimationFrame(frameIndex);
  }, [clip.id, frameIndex, onFrameIndexChange, viewerRef]);

  useModelViewerAnimationClock({
    clip: effectiveClip,
    frameIndex,
    playing,
    onFrame: setFrameIndex,
    onComplete: () => setPlaying(false),
  });

  const scrubPlayback = useModelViewerScrubPlayback({ playing, setPlaying });
  const activeFrame = clip.frames[frameIndex] ?? null;

  return (
    <div className="flex items-center gap-2 px-2">
      <div className="w-36 min-w-0">
        {clips.length > 1 ? (
          <Select value={clip.id} onValueChange={onClipChange}>
            <SelectTrigger
              className="h-8 w-full"
              aria-label={t("page.tools.model_viewer.animation_clip")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {clips.map((entry) => (
                  <SelectItem key={entry.id} value={entry.id}>
                    {entry.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : (
          <div className="text-sm font-medium">{clip.label}</div>
        )}
        <div className="text-xs whitespace-nowrap text-muted-foreground">
          <ModelViewerFpsControl
            fps={fpsOverride ?? clip.fps}
            defaultFps={clip.fps}
            onFpsChange={onFpsChange}
          />{" "}
          ·{" "}
          {t("page.tools.model_viewer.animation_frame", {
            current: activeFrame?.index ?? clip.frameStart,
            end: clip.frameEnd,
          })}
        </div>
      </div>

      <div className="flex min-w-0 flex-1 items-center gap-3">
        <span className="text-xs text-muted-foreground tabular-nums">{clip.frameStart}</span>
        <input
          type="range"
          min={0}
          max={Math.max(clip.frames.length - 1, 0)}
          step={1}
          value={frameIndex}
          className="w-full accent-primary"
          aria-label={t("page.tools.model_viewer.animation_frame_slider")}
          {...scrubPlayback}
          onChange={(event) => {
            setFrameIndex(Number(event.currentTarget.value));
          }}
        />
        <span className="text-right text-xs text-muted-foreground tabular-nums">
          {clip.frameEnd}
        </span>
      </div>

      <Button
        type="button"
        size="icon"
        variant="ghost"
        onClick={() => {
          if (clip.frames.length > 1) {
            setPlaying((current) => !current);
          }
        }}
        disabled={clip.frames.length <= 1}
        aria-label={t(
          playing
            ? "page.tools.model_viewer.animation_pause"
            : "page.tools.model_viewer.animation_play",
        )}
      >
        {playing ? <PauseIcon /> : <PlayIcon />}
      </Button>
    </div>
  );
}

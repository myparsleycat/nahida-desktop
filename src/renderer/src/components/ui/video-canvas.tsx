import { cn } from "@renderer/lib/utils";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef } from "react";

interface VideoCanvasProps {
  src: string;
  className?: string;
  objectFit?: "contain" | "cover";
  playing?: boolean;
  muted?: boolean;
  loop?: boolean;
}

export const VideoCanvas = forwardRef<HTMLCanvasElement, VideoCanvasProps>(
  ({ src, className, objectFit = "cover", playing = false, muted = true, loop = true }, ref) => {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const videoRef = useRef<HTMLVideoElement>(document.createElement("video"));
    const requestRef = useRef<number | null>(null);

    useImperativeHandle(ref, () => canvasRef.current!);

    useEffect(() => {
      const video = videoRef.current;

      if (video.src !== src) {
        video.src = src;
        video.load();
      }
      video.muted = muted;
      video.loop = loop;
      video.playsInline = true;

      //   return () => {};
    }, [src, muted, loop]);

    useEffect(() => {
      const video = videoRef.current;
      if (playing) {
        video.play().catch(() => {});
      } else {
        video.pause();
      }
    }, [playing]);

    const renderFrame = useCallback(() => {
      const canvas = canvasRef.current;
      const video = videoRef.current;
      if (!canvas || !video) return;

      if (video.readyState < 2) return;

      const ctx = canvas.getContext("2d", { alpha: true });
      if (!ctx) return;

      const width = canvas.clientWidth;
      const height = canvas.clientHeight;

      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width;
        canvas.height = height;
      }

      const videoW = video.videoWidth;
      const videoH = video.videoHeight;
      const videoRatio = videoW / videoH;
      const canvasRatio = width / height;

      let drawW = width;
      let drawH = height;
      let startX = 0;
      let startY = 0;

      if (objectFit === "cover") {
        if (canvasRatio > videoRatio) {
          drawH = width / videoRatio;
          startY = (height - drawH) / 2;
        } else {
          drawW = height * videoRatio;
          startX = (width - drawW) / 2;
        }
      } else {
        // contain
        if (canvasRatio > videoRatio) {
          drawW = height * videoRatio;
          startX = (width - drawW) / 2;
        } else {
          drawH = width / videoRatio;
          startY = (height - drawH) / 2;
        }
      }

      ctx.clearRect(0, 0, width, height);
      ctx.drawImage(video, startX, startY, drawW, drawH);
    }, [objectFit]);

    useEffect(() => {
      const video = videoRef.current;
      let handle: number;

      const loopDrawing = () => {
        renderFrame();
        handle = video.requestVideoFrameCallback(loopDrawing);
      };

      handle = video.requestVideoFrameCallback(loopDrawing);

      return () => {
        video.cancelVideoFrameCallback(handle);
      };
    }, [renderFrame, src]);

    useEffect(() => {
      const video = videoRef.current;
      return () => {
        video.pause();
        video.removeAttribute("src");
        video.load();
      };
    }, []);

    return <canvas ref={canvasRef} className={cn("block w-full h-full", className)} />;
  },
);

VideoCanvas.displayName = "VideoCanvas";

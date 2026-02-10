import { cn } from "@renderer/lib/utils";
import { useEffect, useRef, useState } from "react";

interface PreviewProps {
  path?: string | null;
  alt?: string;
  className?: string;
  objectFit?: "contain" | "cover";
  fallback?: React.ReactNode;
  allowPlay?: boolean;
}

export function Preview({
  path,
  alt,
  className,
  objectFit = "cover",
  fallback,
  allowPlay = true,
}: PreviewProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [isPlaying, setIsPlaying] = useState(false);

  useEffect(() => {
    const element = videoRef.current;
    if (!element || !allowPlay) {
      setIsPlaying(false);
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.intersectionRatio >= 0.8) {
            setIsPlaying(true);
          } else {
            setIsPlaying(false);
          }
        });
      },
      {
        threshold: [0, 0.8],
      },
    );

    observer.observe(element);

    return () => {
      observer.unobserve(element);
      observer.disconnect();
    };
  }, [path, allowPlay]);

  useEffect(() => {
    if (videoRef.current) {
      if (isPlaying) {
        videoRef.current.play().catch(() => {});
      } else {
        videoRef.current.pause();
      }
    }
  }, [isPlaying]);

  if (!path) return <>{fallback}</>;

  const isVideo = path.toLowerCase().match(/\.(mp4|webm|avi|mkv|mov)$/);

  const commonStyles: React.CSSProperties = {
    imageRendering: "-webkit-optimize-contrast",
    transform: "translateZ(0)",
    backfaceVisibility: "hidden",
  };

  if (isVideo) {
    return (
      <video
        ref={videoRef}
        src={`local://${path}`}
        className={cn(
          "w-full h-full",
          objectFit === "cover" ? "object-cover" : "object-contain",
          className,
        )}
        style={commonStyles}
        muted
        loop
        playsInline
      />
    );
  }

  return (
    <img
      src={`local://${path}`}
      alt={alt}
      className={cn(
        "w-full h-full",
        objectFit === "cover" ? "object-cover" : "object-contain",
        className,
      )}
      style={commonStyles}
      loading="eager"
      decoding="async"
    />
  );
}

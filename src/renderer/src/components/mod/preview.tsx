import { cn } from "@renderer/lib/utils";
import { useEffect, useRef } from "react";

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

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !allowPlay) {
      if (video) video.pause();
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.intersectionRatio >= 0.8) {
            video.play().catch(() => {});
          } else {
            video.pause();
          }
        });
      },
      {
        threshold: [0, 0.8],
      },
    );

    observer.observe(video);

    return () => {
      observer.unobserve(video);
      observer.disconnect();
    };
  }, [path, allowPlay]);

  if (!path) return <>{fallback}</>;

  const isVideo = path.toLowerCase().match(/\.(mp4|webm|avi|mkv|mov)$/);

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
        loop
        muted
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
      loading="lazy"
      decoding="async"
    />
  );
}

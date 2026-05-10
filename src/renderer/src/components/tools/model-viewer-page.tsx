import { Button } from "@renderer/components/ui/button";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, FolderOpenIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ModelViewerAnimationClip } from "./model-viewer-contract";
import { modelViewerSourceToUrl } from "./model-viewer-session";
import { ThreeModelViewer } from "./three-model-viewer";

type ModelViewerPageManifest = {
  animations?: ModelViewerAnimationClip[];
};

export function ModelViewerPage({
  path,
  name,
  manifestPath,
  artifactRoot,
}: {
  path: string;
  name: string;
  manifestPath?: string;
  artifactRoot?: string;
}) {
  const { t } = useTranslation();
  const { Titlebar } = useTitlebar();
  const [manifest, setManifest] = useState<ModelViewerPageManifest | null>(null);
  const [animationFrameIndex, setAnimationFrameIndex] = useState(0);
  const [animationPlaying, setAnimationPlaying] = useState(false);
  const modelName = name || t("page.tools.model_viewer.title");
  const modelSrc = path ? modelViewerSourceToUrl(path) : "";
  const sourceContext = {
    artifactRoot: artifactRoot || "",
    manifestPath: manifestPath || "",
  };
  const displayPath = sourceContext.artifactRoot || path;
  const activeAnimation = manifest?.animations?.[0] ?? null;
  const activeAnimationFrame = activeAnimation?.frames[animationFrameIndex] ?? null;

  useEffect(() => {
    let cancelled = false;

    if (!manifestPath) {
      setManifest(null);
      setAnimationFrameIndex(0);
      setAnimationPlaying(false);
      return;
    }

    fetch(modelViewerSourceToUrl(manifestPath))
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`Failed to load manifest: ${manifestPath}`);
        }
        return (await response.json()) as ModelViewerPageManifest;
      })
      .then((nextManifest) => {
        if (cancelled) {
          return;
        }
        setManifest(nextManifest);
        setAnimationFrameIndex(0);
        setAnimationPlaying(false);
      })
      .catch(() => {
        if (!cancelled) {
          setManifest(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [manifestPath]);

  useEffect(() => {
    if (!activeAnimation || !animationPlaying || activeAnimation.frames.length <= 1) {
      return;
    }

    const timer = window.setInterval(
      () => {
        setAnimationFrameIndex((current) => {
          const next = current + 1;
          if (next < activeAnimation.frames.length) {
            return next;
          }
          return activeAnimation.loop ? 0 : current;
        });
      },
      1000 / Math.max(activeAnimation.fps, 1),
    );

    return () => {
      window.clearInterval(timer);
    };
  }, [activeAnimation, animationPlaying]);

  return (
    <>
      <Titlebar title={{ text: modelName, position: "center" }} />
      <div className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <header className="flex h-11 shrink-0 items-center justify-between gap-3 border-b px-3">
          <div className="flex min-w-0 items-center gap-2">
            <Button asChild variant="ghost" size="sm" className="h-8 gap-1">
              <Link to="/tools">
                <ArrowLeftIcon className="size-4" />
                {t("page.tools.dashboard.tools_label")}
              </Link>
            </Button>
            <div className="min-w-0 text-sm">
              <div className="truncate font-medium">{modelName}</div>
              {displayPath && (
                <div className="truncate text-xs text-muted-foreground">{displayPath}</div>
              )}
            </div>
          </div>
          {displayPath && (
            <Button
              variant="outline"
              size="sm"
              className="h-8 shrink-0 gap-1"
              onClick={() => window.api.invoke("util:openPath", displayPath)}
            >
              <FolderOpenIcon className="size-4" />
              {t("page.tools.model_viewer.open_file")}
            </Button>
          )}
        </header>

        {activeAnimation ? (
          <div className="flex flex-wrap items-center gap-3 border-b px-3 py-3">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">{activeAnimation.label}</div>
              <div className="text-xs text-muted-foreground">
                {activeAnimation.fps} FPS · Frame{" "}
                {activeAnimationFrame?.index ?? activeAnimation.frameStart} /{" "}
                {activeAnimation.frameEnd}
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              onClick={() => setAnimationPlaying((current) => !current)}
            >
              {animationPlaying ? "Pause" : "Play"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              onClick={() => {
                setAnimationPlaying(false);
                setAnimationFrameIndex(0);
              }}
            >
              Reset
            </Button>
            <input
              type="range"
              min={0}
              max={Math.max(activeAnimation.frames.length - 1, 0)}
              step={1}
              value={animationFrameIndex}
              className="min-w-full accent-primary"
              onChange={(event) => {
                setAnimationPlaying(false);
                setAnimationFrameIndex(Number(event.currentTarget.value));
              }}
            />
          </div>
        ) : null}

        <main className="relative min-h-0 flex-1 bg-muted/30">
          {modelSrc ? (
            <ThreeModelViewer
              className="absolute inset-0 h-full w-full"
              src={modelSrc}
              orientation="0deg 0deg 0deg"
              animationClip={activeAnimation ?? undefined}
              animationFrame={animationFrameIndex}
            />
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {t("page.tools.model_viewer.convert_first")}
            </div>
          )}
        </main>
      </div>
    </>
  );
}

import { Button } from "@renderer/components/ui/button";
import "@google/model-viewer";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, FolderOpenIcon } from "lucide-react";
import React from "react";
import { suppressModelViewerFocusOutline } from "./model-viewer-session";

function toLocalUrl(filePath: string) {
  const normalized = filePath.replaceAll("\\", "/");
  return `local://${encodeURI(normalized).replaceAll("#", "%23").replaceAll("?", "%3F")}`;
}

export function ModelViewerPage({ path, name }: { path: string; name: string }) {
  const { Titlebar } = useTitlebar();
  const modelName = name || "Model Viewer";
  const modelSrc = path ? toLocalUrl(path) : "";

  return (
    <>
      <Titlebar title={{ text: modelName, position: "center" }} />
      <div className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <header className="flex h-11 shrink-0 items-center justify-between gap-3 border-b px-3">
          <div className="flex min-w-0 items-center gap-2">
            <Button asChild variant="ghost" size="sm" className="h-8 gap-1">
              <Link to="/tools">
                <ArrowLeftIcon className="size-4" />
                Tools
              </Link>
            </Button>
            <div className="min-w-0 text-sm">
              <div className="truncate font-medium">{modelName}</div>
              {path && <div className="truncate text-xs text-muted-foreground">{path}</div>}
            </div>
          </div>
          {path && (
            <Button
              variant="outline"
              size="sm"
              className="h-8 shrink-0 gap-1"
              onClick={() => window.api.invoke("util:openPath", path)}
            >
              <FolderOpenIcon className="size-4" />
              Open File
            </Button>
          )}
        </header>

        <main className="relative min-h-0 flex-1 bg-muted/30">
          {modelSrc ? (
            <>
              <model-viewer
                ref={suppressModelViewerFocusOutline}
                className="absolute inset-0 h-full w-full"
                src={modelSrc}
                ar
                ar-modes="webxr scene-viewer quick-look"
                camera-controls
                tone-mapping="neutral"
                shadow-intensity="1"
                exposure="1"
              >
                <div className="progress-bar hide" slot="progress-bar">
                  <div className="update-bar"></div>
                </div>
              </model-viewer>
            </>
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              Convert a mod to GLB first, then open it here.
            </div>
          )}
        </main>
      </div>
    </>
  );
}

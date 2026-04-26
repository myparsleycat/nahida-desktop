import { Button } from "@renderer/components/ui/button";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, FolderOpenIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ThreeModelViewer } from "./three-model-viewer";
import { modelViewerSourceToUrl } from "./model-viewer-session";

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
  const modelName = name || t("page.tools.model_viewer.title");
  const modelSrc = path ? modelViewerSourceToUrl(path) : "";
  const sourceContext = {
    artifactRoot: artifactRoot || "",
    manifestPath: manifestPath || "",
  };
  const displayPath = sourceContext.artifactRoot || path;

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

        <main className="relative min-h-0 flex-1 bg-muted/30">
          {modelSrc ? (
            <ThreeModelViewer
              className="absolute inset-0 h-full w-full"
              src={modelSrc}
              orientation="0deg 0deg 0deg"
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

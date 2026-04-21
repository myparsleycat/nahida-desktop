import "@google/model-viewer";
import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import { Loader2Icon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  captureModelViewerCameraState,
  cleanupModelViewerUrl,
  ModelViewerCameraState,
  ModelViewerElement,
  modelViewerSourceToUrl,
  restoreModelViewerCameraState,
  suppressModelViewerFocusOutline,
} from "./model-viewer-session";

type VariableStateValue = number | string;

type ModelViewerVariantManifest = {
  defaultState: Record<string, VariableStateValue>;
  variables: Array<{
    id: string;
    label: string;
    defaultValue: VariableStateValue;
    values: Array<{ value: VariableStateValue; label: string }>;
    order: number;
    slot?: number;
    iconPath?: string;
  }>;
  uiAssets: {
    backgroundPath?: string;
    slotPath?: string;
    slotHoverPath?: string;
    slotActivePath?: string;
  };
  states: Array<{
    key: string;
    values: Record<string, VariableStateValue>;
    glbPath: string;
  }>;
};

export type ModelViewerDialogSource =
  | {
      mode: "single";
      glbPath: string;
      name: string;
    }
  | {
      mode: "variant-set";
      artifactRoot: string;
      manifestPath: string;
      manifest: ModelViewerVariantManifest;
      defaultGlbPath: string;
      activeGlbPath: string;
      name: string;
    };

export function ModelViewerDialog({
  open,
  onOpenChange,
  source,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ModelViewerDialogSource | null;
}) {
  const [activeState, setActiveState] = useState<Record<string, VariableStateValue>>({});
  const [manifest, setManifest] = useState<ModelViewerVariantManifest | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const [viewerUrls, setViewerUrls] = useState<[string, string]>(["", ""]);
  const [activeViewerIndex, setActiveViewerIndex] = useState<0 | 1>(0);
  const [loadingViewerIndex, setLoadingViewerIndex] = useState<0 | 1 | null>(null);
  const viewerRefs = useRef<[ModelViewerElement | null, ModelViewerElement | null]>([null, null]);
  const viewerUrlsRef = useRef<[string, string]>(["", ""]);
  const activeViewerIndexRef = useRef<0 | 1>(0);
  const loadingViewerIndexRef = useRef<0 | 1 | null>(null);
  const pendingCameraStateRef = useRef<ModelViewerCameraState | null>(null);

  function setViewerUrl(index: 0 | 1, sourcePath: string) {
    const nextUrl = sourcePath ? withCacheBuster(modelViewerSourceToUrl(sourcePath)) : "";
    const prevUrl = viewerUrlsRef.current[index];
    if (prevUrl && prevUrl !== nextUrl) {
      cleanupModelViewerUrl(prevUrl);
    }

    viewerUrlsRef.current = viewerUrlsRef.current.map((url, currentIndex) =>
      currentIndex === index ? nextUrl : url,
    ) as [string, string];
    setViewerUrls(viewerUrlsRef.current);
  }

  useEffect(() => {
    return () => {
      for (const url of viewerUrlsRef.current) {
        cleanupModelViewerUrl(url);
      }
    };
  }, []);

  useEffect(() => {
    if (!source) {
      setActiveState({});
      setManifest(null);
      activeViewerIndexRef.current = 0;
      loadingViewerIndexRef.current = null;
      setActiveViewerIndex(0);
      setLoadingViewerIndex(null);
      pendingCameraStateRef.current = null;
      setViewerUrl(0, "");
      setViewerUrl(1, "");
      return;
    }

    if (source.mode === "single") {
      setActiveState({});
      setManifest(null);
      activeViewerIndexRef.current = 0;
      loadingViewerIndexRef.current = null;
      setActiveViewerIndex(0);
      setLoadingViewerIndex(null);
      pendingCameraStateRef.current = null;
      setViewerUrl(0, source.glbPath);
      setViewerUrl(1, "");
      return;
    }

    setActiveState(source.manifest.defaultState);
    setManifest(source.manifest);
    activeViewerIndexRef.current = 0;
    loadingViewerIndexRef.current = null;
    setActiveViewerIndex(0);
    setLoadingViewerIndex(null);
    pendingCameraStateRef.current = null;
    setViewerUrl(0, source.activeGlbPath || source.defaultGlbPath);
    setViewerUrl(1, "");
  }, [source]);

  const handleSelectValue = async (variableId: string, value: VariableStateValue) => {
    if (!source || source.mode !== "variant-set" || isResolving || loadingViewerIndex !== null) {
      return;
    }

    const nextState = {
      ...activeState,
      [variableId]: value,
    };

    const nextViewerIndex: 0 | 1 = activeViewerIndex === 0 ? 1 : 0;
    pendingCameraStateRef.current = captureModelViewerCameraState(
      viewerRefs.current[activeViewerIndex],
    );

    const nextStateKey = createStateKey(nextState);
    const existingStateArtifact = manifest?.states.find((entry) => entry.key === nextStateKey);
    if (existingStateArtifact?.glbPath) {
      setActiveState(nextState);
      loadingViewerIndexRef.current = nextViewerIndex;
      setLoadingViewerIndex(nextViewerIndex);
      setViewerUrl(nextViewerIndex, existingStateArtifact.glbPath);
      return;
    }

    setIsResolving(true);
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", {
        artifactRoot: source.artifactRoot,
        manifestPath: source.manifestPath,
        state: nextState,
      });
      if (result.mode !== "variant-set") {
        return;
      }

      setManifest(result.manifest);
      setActiveState(nextState);
      loadingViewerIndexRef.current = nextViewerIndex;
      setLoadingViewerIndex(nextViewerIndex);
      setViewerUrl(nextViewerIndex, result.activeGlbPath);
    } catch (error) {
      pendingCameraStateRef.current = null;
      loadingViewerIndexRef.current = null;
      setLoadingViewerIndex(null);
      toast.error("Failed to update model variant", {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setIsResolving(false);
    }
  };

  const variables = manifest?.variables ?? [];
  const uiAssets = manifest?.uiAssets;
  const visibleVariables = variables.filter((variable) => variable.values.length > 0);
  const tileBackgroundPath = uiAssets?.backgroundPath;
  const slotPath = uiAssets?.slotPath;
  const slotHoverPath = uiAssets?.slotHoverPath;
  const slotActivePath = uiAssets?.slotActivePath;
  const hasVariantTileUi = Boolean(tileBackgroundPath) && visibleVariables.length > 0;
  const hasVariantToggleUi = visibleVariables.length > 0;
  const showToggleViewer = Boolean(
    source?.mode === "variant-set" && manifest && (hasVariantTileUi || hasVariantToggleUi),
  );
  const isViewerBusy = isResolving || loadingViewerIndex !== null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex min-w-[95vw] max-h-[92vh] h-full flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={source?.name}>
            {source?.name || "Model Viewer"}
          </DialogTitle>
        </DialogHeader>

        <div
          className={cn(
            "grid min-h-0 flex-1 gap-3",
            showToggleViewer && "lg:grid-cols-[minmax(0,1fr)_360px]",
          )}
        >
          <div className="relative min-h-80 overflow-hidden rounded-md border bg-muted/30">
            {viewerUrls.some((url) => Boolean(url)) ? (
              <>
                {([0, 1] as const).map((index) => (
                  <model-viewer
                    key={index}
                    ref={(element) => {
                      viewerRefs.current[index] = element as ModelViewerElement | null;
                      if (!element) {
                        return;
                      }

                      if (element.dataset.nhdViewerLoadBound === "true") {
                        suppressModelViewerFocusOutline(element);
                        return;
                      }

                      element.dataset.nhdViewerLoadBound = "true";
                      suppressModelViewerFocusOutline(element);
                      element.addEventListener("load", () => {
                        if (loadingViewerIndexRef.current !== index) {
                          return;
                        }

                        restoreModelViewerCameraState(
                          viewerRefs.current[index],
                          pendingCameraStateRef.current,
                        );
                        pendingCameraStateRef.current = null;
                        requestAnimationFrame(() => {
                          requestAnimationFrame(() => {
                            activeViewerIndexRef.current = index;
                            loadingViewerIndexRef.current = null;
                            setActiveViewerIndex(index);
                            setLoadingViewerIndex(null);
                          });
                        });
                      });
                    }}
                    className={cn(
                      "absolute inset-0 h-full w-full transition-opacity duration-200",
                      activeViewerIndex === index
                        ? "z-10 opacity-100"
                        : "pointer-events-none z-0 opacity-0",
                    )}
                    src={viewerUrls[index]}
                    camera-controls
                    tone-mapping="neutral"
                    shadow-intensity="1"
                    exposure="1"
                  >
                    <div className="progress-bar hide" slot="progress-bar">
                      <div className="update-bar"></div>
                    </div>
                  </model-viewer>
                ))}
                <div
                  className={cn(
                    "absolute inset-0 z-20 flex items-center justify-center bg-black/20 transition-opacity duration-200",
                    isViewerBusy ? "cursor-progress opacity-100" : "pointer-events-none opacity-0",
                  )}
                >
                  <div
                    className={cn(
                      "inline-flex items-center gap-2 rounded-md border bg-background/90 px-3 py-2 text-sm text-foreground shadow-sm transition-all duration-200",
                      isViewerBusy ? "translate-y-0 scale-100 opacity-100" : "translate-y-1 scale-95 opacity-0",
                    )}
                  >
                      <Loader2Icon className="size-4 animate-spin" />
                      Generating selected state
                  </div>
                </div>
              </>
            ) : (
              <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
                Model data is not available.
              </div>
            )}
          </div>

          {showToggleViewer ? (
            <div
              className={cn(
                "flex min-h-0 flex-col overflow-hidden rounded-md border bg-card/20 transition-opacity",
                isViewerBusy && "pointer-events-none opacity-60",
              )}
              aria-busy={isViewerBusy}
            >
              <div className="border-b px-4 py-3">
                <div className="text-sm font-medium">Toggle Viewer</div>
              </div>
              <ScrollArea className="min-h-0 flex-1">
                <div className="p-4">
                  {hasVariantTileUi ? (
                    <div
                      className="mb-4 overflow-hidden rounded-md border bg-cover bg-center"
                      style={{
                        backgroundImage: tileBackgroundPath
                          ? `url(${modelViewerSourceToUrl(tileBackgroundPath)})`
                          : undefined,
                      }}
                    >
                      <div className="grid grid-cols-3 gap-3 p-4">
                        {visibleVariables.map((variable) => (
                          <VariantTile
                            key={variable.id}
                            variable={variable}
                            activeValue={activeState[variable.id]}
                            slotPath={slotPath}
                            slotHoverPath={slotHoverPath}
                            slotActivePath={slotActivePath}
                            onSelect={handleSelectValue}
                          />
                        ))}
                      </div>
                    </div>
                  ) : null}

                  <div className="space-y-3">
                    {visibleVariables.map((variable) => (
                      <div key={variable.id} className="rounded-md border bg-background/50 p-3">
                        <div className="mb-2 text-sm font-medium">{variable.label}</div>
                        <div className="flex flex-wrap gap-2">
                          {variable.values.map((entry) => {
                            const active = String(activeState[variable.id]) === String(entry.value);
                            return (
                              <Button
                                key={`${variable.id}-${String(entry.value)}`}
                                type="button"
                                size="sm"
                                variant={active ? "default" : "outline"}
                                disabled={isViewerBusy}
                                onClick={() => handleSelectValue(variable.id, entry.value)}
                              >
                                {entry.label}
                              </Button>
                            );
                          })}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </ScrollArea>
              {/* {isViewerBusy && (
                <div className="border-t px-4 py-2 text-xs text-muted-foreground">
                  <span className="inline-flex items-center gap-2">
                    <Loader2Icon className="size-3 animate-spin" />
                    Generating selected state
                  </span>
                </div>
              )} */}
            </div>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function VariantTile({
  variable,
  activeValue,
  slotPath,
  slotHoverPath,
  slotActivePath,
  onSelect,
}: {
  variable: ModelViewerVariantManifest["variables"][number];
  activeValue: VariableStateValue | undefined;
  slotPath?: string;
  slotHoverPath?: string;
  slotActivePath?: string;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const isActive = String(activeValue) !== String(variable.defaultValue);
  const framePath = isActive ? slotActivePath || slotHoverPath || slotPath : slotPath;

  return (
    <button
      type="button"
      className={cn(
        "relative flex aspect-square min-h-20 items-end justify-center overflow-hidden rounded-md border bg-black/20 p-2 text-white transition",
        "hover:bg-black/30",
      )}
      onClick={() => {
        if (variable.values.length === 0) {
          return;
        }

        const nextIndex =
          variable.values.findIndex((entry) => String(entry.value) === String(activeValue)) + 1;
        const next = variable.values[nextIndex % variable.values.length];
        if (!next) {
          return;
        }

        onSelect(variable.id, next.value);
      }}
    >
      {framePath ? (
        <img
          src={modelViewerSourceToUrl(framePath)}
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : null}
      {variable.iconPath ? (
        <img
          src={modelViewerSourceToUrl(variable.iconPath)}
          className="absolute inset-3 h-[calc(100%-24px)] w-[calc(100%-24px)] object-contain"
        />
      ) : null}
      <div className="relative z-10 rounded bg-black/60 px-2 py-1 text-[11px] leading-none">
        {variable.label}
      </div>
    </button>
  );
}

function withCacheBuster(url: string): string {
  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}v=${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function createStateKey(state: Record<string, VariableStateValue>): string {
  return Object.entries(state)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key.toLowerCase()}=${String(value)}`)
    .join("&");
}

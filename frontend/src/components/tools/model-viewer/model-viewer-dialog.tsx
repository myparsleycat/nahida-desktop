import { Mod } from "@bindings/mod";
import { Tools } from "@bindings/tools";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@renderer/components/ui/tooltip";
import { getSetting, setSetting } from "@renderer/lib/settings";
import { cn } from "@renderer/lib/utils";
import {
  applyVariableSelection,
  computeIneffectiveValues,
  evaluateViewerState,
  type IneffectiveMap,
  type IneffectiveSuggestion,
} from "@shared/mod-viewer/eval";
import { toErrorMessage } from "@shared/utils";
import { CheckIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import type { ModelViewerDialogSource, VariableStateValue } from "./model-viewer-dialog-types";

import {
  formatOrientation,
  type ModelViewerCameraState,
  type ModelViewerHandle,
  type ModelViewerThreeEnvironment,
  type ModelViewerThreeToneMapping,
  parseOrientation,
} from "./model-viewer-contract";
import { DEFAULT_MODEL_ORIENTATION, DEFAULT_THREE_EXPOSURE } from "./model-viewer-dialog-types";
import {
  clampThreeExposure,
  getSourceSessionKey,
  normalizeThreeEnvironment,
  normalizeThreeToneMapping,
} from "./model-viewer-dialog-utils";
import { VariantSlider, VariantTile } from "./model-viewer-dialog-variants";
import { ModelViewerMenuBar } from "./model-viewer-menu-bar";
import { modelViewerSourceToUrl } from "./model-viewer-session";
import { ThreeModelViewer } from "./three-model-viewer";

export type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

function getInitialActiveState(
  source: ModelViewerDialogSource | null,
): Record<string, VariableStateValue> {
  return source?.transport.defaultState ?? {};
}

function getInitialActiveAnimationId(source: ModelViewerDialogSource | null): string | null {
  return source?.transport.animations[0]?.id ?? null;
}

export function ModelViewerDialog({
  open,
  onOpenChange,
  source,
  existingPreviewPath,
  onPreviewSaved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ModelViewerDialogSource | null;
  existingPreviewPath?: string;
  onPreviewSaved?: () => void | Promise<void>;
}) {
  const { t } = useTranslation();
  const [prevOpen, setPrevOpen] = useState(open);
  const [prevSource, setPrevSource] = useState(source);
  const [activeState, setActiveState] = useState<Record<string, VariableStateValue>>(() =>
    getInitialActiveState(source),
  );
  const [previewState, setPreviewState] = useState<Record<string, VariableStateValue> | null>(null);
  const [activeAnimationId, setActiveAnimationId] = useState<string | null>(() =>
    getInitialActiveAnimationId(source),
  );
  const [animationFrameIndex, setAnimationFrameIndex] = useState(0);
  const [animationPlaying, setAnimationPlaying] = useState(false);
  const animationFrameIndexRef = useRef(0);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const [doubleSidedEnabled, setDoubleSidedEnabled] = useState(true);
  const [threeToneMapping, setThreeToneMapping] = useState<ModelViewerThreeToneMapping>("neutral");
  const [threeEnvironment, setThreeEnvironment] = useState<ModelViewerThreeEnvironment>("studio");
  const [threeExposure, setThreeExposure] = useState(DEFAULT_THREE_EXPOSURE);
  const [toonShadows, setToonShadows] = useState(false);
  const [isViewerReady, setIsViewerReady] = useState(false);
  const [isSavingPreview, setIsSavingPreview] = useState(false);
  const [showOverwritePreviewDialog, setShowOverwritePreviewDialog] = useState(false);
  const viewerRef = useRef<ModelViewerHandle | null>(null);
  const doubleSidedEnabledRef = useRef(true);
  const initialCameraStateRef = useRef<ModelViewerCameraState | null>(null);

  if (prevOpen !== open) {
    setPrevOpen(open);
    if (!open) {
      setShowOverwritePreviewDialog(false);
      setIsSavingPreview(false);
      setIsViewerReady(false);
      setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    }
  }

  if (prevSource !== source) {
    setPrevSource(source);
    setPreviewState(null);
    setIsViewerReady(false);

    if (getSourceSessionKey(prevSource) !== getSourceSessionKey(source)) {
      setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    }

    setActiveState(source?.transport.defaultState ?? {});
    setActiveAnimationId(source?.transport.animations[0]?.id ?? null);
    setAnimationFrameIndex(0);
    setAnimationPlaying(false);
  }

  useEffect(() => {
    animationFrameIndexRef.current = animationFrameIndex;
    viewerRef.current?.setAnimationFrame(animationFrameIndex);
  }, [animationFrameIndex]);

  useEffect(() => {
    doubleSidedEnabledRef.current = doubleSidedEnabled;
  }, [doubleSidedEnabled]);

  useEffect(() => {
    void viewerRef.current?.setDoubleSided(doubleSidedEnabled);
  }, [doubleSidedEnabled]);

  useEffect(() => {
    let cancelled = false;

    void Promise.all([
      getSetting("modelViewer.toneMapping"),
      getSetting("modelViewer.environment"),
      getSetting("modelViewer.exposure"),
      getSetting("modelViewer.toonShadows"),
    ])
      .then(([toneMapping, environment, exposure, storedToonShadows]) => {
        if (cancelled) {
          return;
        }

        setThreeToneMapping(normalizeThreeToneMapping(toneMapping));
        setThreeEnvironment(normalizeThreeEnvironment(environment));
        setThreeExposure(clampThreeExposure(exposure));
        setToonShadows(storedToonShadows === true);
      })
      .catch((error) => {
        console.error("Failed to load model viewer rendering settings", error);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const payloadTransport = source?.transport ?? null;
  const payloadAnimations = useMemo(
    () =>
      (payloadTransport?.animations ?? []).map((clip) => ({
        ...clip,
        frames: clip.frames.map((frame) => ({ ...frame, meshes: [] })),
      })),
    [payloadTransport],
  );
  const animationClips = payloadAnimations;
  const activeAnimation =
    animationClips.find((animation) => animation.id === activeAnimationId) ??
    animationClips[0] ??
    null;
  const activeAnimationFrame = activeAnimation?.frames[animationFrameIndex] ?? null;
  const animationVariableIds = new Set(activeAnimation?.variableIds ?? []);

  const [prevActiveAnimation, setPrevActiveAnimation] = useState(activeAnimation);
  if (prevActiveAnimation !== activeAnimation) {
    setPrevActiveAnimation(activeAnimation);
    setAnimationFrameIndex(0);
    setAnimationPlaying(Boolean(activeAnimation && activeAnimation.frames.length > 1));
  }

  useEffect(() => {
    if (!activeAnimation || !animationPlaying || activeAnimation.frames.length <= 1) {
      return;
    }

    const intervalMs = 1000 / Math.max(activeAnimation.fps, 1);
    const timer = window.setInterval(() => {
      const current = animationFrameIndexRef.current;
      const next = current + 1;
      const frameIndex =
        next < activeAnimation.frames.length ? next : activeAnimation.loop ? 0 : current;
      animationFrameIndexRef.current = frameIndex;
      viewerRef.current?.setAnimationFrame(frameIndex);
      setAnimationFrameIndex(frameIndex);
    }, intervalMs);

    return () => {
      window.clearInterval(timer);
    };
  }, [activeAnimation, animationPlaying]);

  const updateThreeToneMapping = (value: ModelViewerThreeToneMapping) => {
    setThreeToneMapping(value);
    void setSetting("modelViewer.toneMapping", value).catch((error) => {
      console.error("Failed to persist model viewer tone mapping", error);
      toast.error("Failed to save tone mapping setting.");
    });
  };

  const updateThreeEnvironment = (value: ModelViewerThreeEnvironment) => {
    setThreeEnvironment(value);
    void setSetting("modelViewer.environment", value).catch((error) => {
      console.error("Failed to persist model viewer environment", error);
      toast.error("Failed to save environment setting.");
    });
  };

  const updateThreeExposure = (value: number) => {
    const nextValue = clampThreeExposure(value);
    setThreeExposure(nextValue);
    void setSetting("modelViewer.exposure", nextValue).catch((error) => {
      console.error("Failed to persist model viewer exposure", error);
      toast.error("Failed to save exposure setting.");
    });
  };

  const updateToonShadows = (value: boolean) => {
    setToonShadows(value);
    void setSetting("modelViewer.toonShadows", value).catch((error) => {
      console.error("Failed to persist model viewer toon shadows", error);
      toast.error("Failed to save toon shadow setting.");
    });
  };

  const rotateModel = (delta: [number, number, number]) => {
    setModelOrientation((currentOrientation) => {
      const [roll, pitch, yaw] = parseOrientation(currentOrientation);
      return formatOrientation([roll + delta[0], pitch + delta[1], yaw + delta[2]]);
    });
  };

  const handleResetView = () => {
    setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        viewerRef.current?.restoreCameraState(initialCameraStateRef.current);
      });
    });
  };

  const handleResetToggles = async () => {
    if (!source) {
      return;
    }
    setActiveState(source.transport.defaultState);
  };

  const handleSaveTogglesToIni = async () => {
    if (!source) {
      return;
    }

    const iniPath = source.transport.iniPath;
    if (!iniPath) {
      toast.error(t("page.tools.model_viewer.toast.save_to_ini_error"));
      return;
    }

    try {
      const result = await Tools.PersistModelViewerToggleState(iniPath, activeState);

      if ((result.updatedVariables ?? []).length > 0) {
        toast.success(t("page.tools.model_viewer.toast.save_to_ini_success"));
        return;
      }

      toast.warning(t("page.tools.model_viewer.toast.save_to_ini_no_changes"));
    } catch (error) {
      toast.error(t("page.tools.model_viewer.toast.save_to_ini_error"), {
        description: toErrorMessage(error),
      });
    }
  };

  const handleSelectValue = async (variableId: string, value: VariableStateValue) => {
    if (!source) {
      return;
    }
    const variable = source.transport.variables.find((entry) => entry.id === variableId) ?? {
      id: variableId,
    };
    setActiveState((current) => applyVariableSelection(current, variable, value));
  };

  const handleApplyResolution = (suggestion: IneffectiveSuggestion) => {
    if (!source) return;
    setActiveState((current) => {
      let next = current;
      for (const change of suggestion.changes) {
        const variable = source.transport.variables.find((v) => v.id === change.varId) ?? {
          id: change.varId,
        };
        next = applyVariableSelection(next, variable, change.toValue);
      }
      return next;
    });
    setPreviewState(null);
  };

  const handleAnimationTogglePlayback = () => {
    if (!activeAnimation || activeAnimation.frames.length <= 1) {
      return;
    }

    setAnimationPlaying((current) => !current);
  };

  const handleAnimationReset = () => {
    animationFrameIndexRef.current = 0;
    setAnimationFrameIndex(0);
    setAnimationPlaying(false);
    viewerRef.current?.setAnimationFrame(0);
  };

  const effectiveState = previewState ?? activeState;
  const payloadEval = useMemo(
    () => (payloadTransport ? evaluateViewerState(payloadTransport, effectiveState) : null),
    [effectiveState, payloadTransport],
  );
  const ineffectiveMap = useMemo<IneffectiveMap>(
    () => (payloadTransport ? computeIneffectiveValues(payloadTransport, activeState) : new Map()),
    [activeState, payloadTransport],
  );
  const variables = payloadTransport?.variables ?? [];
  const uiAssets = payloadTransport?.uiAssets;
  const visibleVariables = variables.filter(
    (variable) =>
      (variable.values.length > 0 || variable.controlType === "slider") &&
      !animationVariableIds.has(variable.id),
  );
  const tileVariables = visibleVariables.filter((variable) => variable.controlType !== "slider");
  const sliderVariables = visibleVariables.filter((variable) => variable.controlType === "slider");
  const tileBackgroundPath = uiAssets?.backgroundPath;
  const slotPath = uiAssets?.slotPath;
  const slotHoverPath = uiAssets?.slotHoverPath;
  const slotActivePath = uiAssets?.slotActivePath;
  const hasVariantTileUi = Boolean(tileBackgroundPath) && tileVariables.length > 0;
  const hasVariantToggleUi = visibleVariables.length > 0;
  const showToggleViewer = Boolean(payloadTransport && hasVariantToggleUi);
  const canSaveCapturedPreview = Boolean(source?.modPath) && isViewerReady && !isSavingPreview;

  const handleViewerLoad = useCallback(() => {
    void (async () => {
      const viewer = viewerRef.current;
      if (!viewer) {
        return;
      }

      await viewer.setDoubleSided(doubleSidedEnabledRef.current);
      await viewer.updateFraming();

      requestAnimationFrame(() => {
        if (!initialCameraStateRef.current) {
          initialCameraStateRef.current = viewerRef.current?.captureCameraState() ?? null;
        }
        setIsViewerReady(true);
        viewerRef.current?.setAnimationFrame(animationFrameIndexRef.current);
      });
    })();
  }, []);

  const handleViewerError = useCallback((error: unknown) => {
    setIsViewerReady(false);
    console.error("Failed to load model viewer source", error);
  }, []);

  const captureAndSavePreview = async () => {
    if (!source?.modPath) {
      return;
    }

    const dataUrl = (await viewerRef.current?.captureSquarePngDataUrl()) ?? null;
    if (!dataUrl) {
      throw new Error(t("page.tools.model_viewer.toast.capture_preview_error"));
    }

    await Mod.PastePreview(source.modPath, dataUrl, "base64", existingPreviewPath ?? null);
    await onPreviewSaved?.();
  };

  const handleCapturePreviewClick = () => {
    if (!source?.modPath || !canSaveCapturedPreview) {
      return;
    }

    if (existingPreviewPath) {
      setShowOverwritePreviewDialog(true);
      return;
    }

    void handleConfirmCapturePreview();
  };

  const handleConfirmCapturePreview = async () => {
    setShowOverwritePreviewDialog(false);
    setIsSavingPreview(true);
    try {
      await captureAndSavePreview();
      toast.success(t("page.tools.model_viewer.toast.capture_preview_success"));
    } catch (error) {
      toast.error(t("page.tools.model_viewer.toast.capture_preview_error"), {
        description: toErrorMessage(error),
      });
    } finally {
      setIsSavingPreview(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          className="flex h-full max-h-[92vh] min-w-[95vw] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
          onClick={(e) => e.stopPropagation()}
        >
          <DialogHeader className="pr-10">
            <DialogTitle className="truncate" title={source?.name}>
              {source?.name || t("page.tools.model_viewer.title")}
            </DialogTitle>
          </DialogHeader>

          <ModelViewerMenuBar
            rotateModel={rotateModel}
            onResetView={handleResetView}
            doubleSidedEnabled={doubleSidedEnabled}
            onDoubleSidedChange={(v) => setDoubleSidedEnabled(v)}
            toneMapping={threeToneMapping}
            onToneMappingChange={updateThreeToneMapping}
            environment={threeEnvironment}
            onEnvironmentChange={updateThreeEnvironment}
            exposure={threeExposure}
            onExposureDraftChange={(v) => setThreeExposure(v)}
            onExposureCommit={updateThreeExposure}
            showToonShadows={payloadTransport?.materialProfile === "wuwa:rabbitfx"}
            toonShadows={toonShadows}
            onToonShadowsChange={updateToonShadows}
            showToggleViewer={showToggleViewer}
            isViewerBusy={false}
            onSaveTogglesToIni={handleSaveTogglesToIni}
            onResetToggles={handleResetToggles}
            canSaveCapturedPreview={canSaveCapturedPreview}
            onCapturePreviewClick={handleCapturePreviewClick}
          />

          <div
            className={cn(
              "grid min-h-0 flex-1 gap-3",
              showToggleViewer && "lg:grid-cols-[minmax(0,1fr)_360px]",
            )}
          >
            <div className="relative min-h-80 overflow-hidden rounded-md border bg-muted/30">
              {payloadTransport ? (
                <ThreeModelViewer
                  ref={viewerRef}
                  className="absolute inset-0 h-full w-full"
                  payloadTransport={payloadTransport}
                  payloadEval={payloadEval ?? undefined}
                  orientation={modelOrientation}
                  animationClip={activeAnimation ?? undefined}
                  threeToneMapping={threeToneMapping}
                  threeEnvironment={threeEnvironment}
                  threeExposure={threeExposure}
                  toonShadows={toonShadows}
                  onLoad={handleViewerLoad}
                  onError={handleViewerError}
                />
              ) : (
                <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
                  {t("page.tools.model_viewer.model_data_unavailable")}
                </div>
              )}
            </div>

            {showToggleViewer ? (
              <div className="flex min-h-0 flex-col overflow-hidden rounded-md border bg-card/20">
                <div className="border-b px-4 py-3">
                  <div className="text-sm font-medium">
                    {t("page.tools.model_viewer.toggle_viewer")}
                  </div>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto">
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
                          {tileVariables.map((variable) => (
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

                    <TooltipProvider>
                      <div className="space-y-3">
                        <div className="grid grid-cols-2 gap-3">
                          {tileVariables.map((variable) => (
                            <div key={variable.id} className="border-l-4 border-l-primary/40 pl-3">
                              <div className="mb-2 text-sm font-medium">{variable.label}</div>
                              <Select
                                value={String(activeState[variable.id] ?? variable.defaultValue)}
                                onValueChange={(value) => {
                                  if (value === null) return;
                                  const entry = variable.values.find(
                                    (e) => String(e.value) === value,
                                  );
                                  if (entry) void handleSelectValue(variable.id, entry.value);
                                }}
                                onOpenChange={(open) => {
                                  if (!open) setPreviewState(null);
                                }}
                              >
                                <SelectTrigger className="w-full">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectGroup>
                                    {variable.values.map((entry) => {
                                      const ineffectiveEntry = ineffectiveMap
                                        .get(variable.id)
                                        ?.get(String(entry.value));
                                      const isIneffective = Boolean(ineffectiveEntry);
                                      if (!isIneffective || !ineffectiveEntry)
                                        return (
                                          <SelectItem
                                            key={String(entry.value)}
                                            value={String(entry.value)}
                                            onMouseEnter={() => {
                                              setPreviewState({
                                                ...activeState,
                                                [variable.id]: entry.value,
                                              });
                                            }}
                                            onMouseLeave={() => setPreviewState(null)}
                                          >
                                            {entry.label}
                                          </SelectItem>
                                        );
                                      return (
                                        <div
                                          key={String(entry.value)}
                                          className="group flex w-full items-center justify-between"
                                        >
                                          <Tooltip>
                                            <TooltipTrigger
                                              closeOnClick={false}
                                              render={
                                                <SelectItem
                                                  value={String(entry.value)}
                                                  disabled
                                                  style={{ pointerEvents: "auto" }}
                                                  onMouseEnter={() => {
                                                    setPreviewState({
                                                      ...activeState,
                                                      [variable.id]: entry.value,
                                                    });
                                                  }}
                                                  onMouseLeave={() => setPreviewState(null)}
                                                />
                                              }
                                            >
                                              {entry.label}
                                            </TooltipTrigger>
                                            <TooltipContent>
                                              <div className="space-y-1 text-left">
                                                <div>
                                                  {t(
                                                    "page.tools.model_viewer.ineffective_blocked_by",
                                                    {
                                                      variables: ineffectiveEntry.blockingVars
                                                        .map((v) => `${v.label}=${v.value}`)
                                                        .join(", "),
                                                    },
                                                  )}
                                                </div>
                                                {ineffectiveEntry.suggestions.length > 0 && (
                                                  <div>
                                                    {t(
                                                      "page.tools.model_viewer.ineffective_suggestion",
                                                    )}
                                                    <ul className="ml-3 list-disc">
                                                      {ineffectiveEntry.suggestions.map(
                                                        (suggestion) => (
                                                          <li key={suggestion.display}>
                                                            {suggestion.display}
                                                          </li>
                                                        ),
                                                      )}
                                                    </ul>
                                                  </div>
                                                )}
                                              </div>
                                            </TooltipContent>
                                          </Tooltip>
                                          {ineffectiveEntry.suggestions.length > 0 && (
                                            <div className="w-0 overflow-hidden transition-all group-hover:w-8 focus-within:w-8">
                                              <Tooltip>
                                                <TooltipTrigger
                                                  render={
                                                    <Button
                                                      variant="ghost"
                                                      size="icon-sm"
                                                      onMouseEnter={() => {
                                                        if (!source) {
                                                          return;
                                                        }
                                                        let next = { ...activeState };
                                                        for (const change of ineffectiveEntry
                                                          .suggestions[0].changes) {
                                                          const v = source.transport.variables.find(
                                                            (entry) => entry.id === change.varId,
                                                          ) ?? { id: change.varId };
                                                          next = applyVariableSelection(
                                                            next,
                                                            v,
                                                            change.toValue,
                                                          );
                                                        }
                                                        setPreviewState(next);
                                                      }}
                                                      onMouseLeave={() => setPreviewState(null)}
                                                      onClick={() =>
                                                        handleApplyResolution(
                                                          ineffectiveEntry.suggestions[0],
                                                        )
                                                      }
                                                    />
                                                  }
                                                >
                                                  <CheckIcon className="size-4" />
                                                </TooltipTrigger>
                                                <TooltipContent>
                                                  <div className="space-y-1 text-left">
                                                    <div>
                                                      {t(
                                                        "page.tools.model_viewer.ineffective_apply",
                                                      )}
                                                    </div>
                                                    <ul className="ml-3 list-disc">
                                                      {ineffectiveEntry.suggestions[0].changes.map(
                                                        (change) => (
                                                          <li
                                                            key={`${change.varId}-${change.toValue}`}
                                                          >
                                                            {change.varLabel}: {change.fromValue} →{" "}
                                                            {change.toValue}
                                                          </li>
                                                        ),
                                                      )}
                                                    </ul>
                                                  </div>
                                                </TooltipContent>
                                              </Tooltip>
                                            </div>
                                          )}
                                        </div>
                                      );
                                    })}
                                  </SelectGroup>
                                </SelectContent>
                              </Select>
                            </div>
                          ))}
                        </div>
                        {sliderVariables.map((variable) => (
                          <VariantSlider
                            key={variable.id}
                            variable={variable}
                            activeValue={activeState[variable.id]}
                            realtime
                            onSelect={handleSelectValue}
                          />
                        ))}
                      </div>
                    </TooltipProvider>
                  </div>
                </div>
              </div>
            ) : null}
          </div>

          {activeAnimation ? (
            <div className="flex items-center gap-2 px-2">
              <div className="w-36 min-w-0">
                <div className="text-sm font-medium">{activeAnimation.label}</div>
                <div className="text-xs whitespace-nowrap text-muted-foreground">
                  {activeAnimation.fps} FPS · Frame{" "}
                  {activeAnimationFrame?.index ?? activeAnimation.frameStart} /{" "}
                  {activeAnimation.frameEnd}
                </div>
              </div>

              <div className="flex min-w-0 flex-1 items-center gap-3">
                <span className="text-xs text-muted-foreground tabular-nums">
                  {activeAnimation.frameStart}
                </span>
                <input
                  type="range"
                  min={0}
                  max={Math.max(activeAnimation.frames.length - 1, 0)}
                  step={1}
                  value={animationFrameIndex}
                  className="w-full accent-primary"
                  onChange={(event) => {
                    const index = Number(event.currentTarget.value);
                    setAnimationPlaying(false);
                    animationFrameIndexRef.current = index;
                    setAnimationFrameIndex(index);
                    viewerRef.current?.setAnimationFrame(index);
                  }}
                />
                <span className="text-right text-xs text-muted-foreground tabular-nums">
                  {activeAnimation.frameEnd}
                </span>
              </div>

              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={handleAnimationTogglePlayback}
                  disabled={activeAnimation.frames.length <= 1}
                >
                  {animationPlaying ? "Pause" : "Play"}
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={handleAnimationReset}>
                  Reset
                </Button>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <AlertDialog open={showOverwritePreviewDialog} onOpenChange={setShowOverwritePreviewDialog}>
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.tools.model_viewer.dialog.overwrite_preview.title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.model_viewer.dialog.overwrite_preview.description", {
                name: source?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void handleConfirmCapturePreview()}>
              {t("page.tools.model_viewer.dialog.overwrite_preview.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

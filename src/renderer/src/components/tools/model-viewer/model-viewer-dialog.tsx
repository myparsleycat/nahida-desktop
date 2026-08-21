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
import { CheckIcon, Loader2Icon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import type {
  ModelViewerDialogSource,
  ModelViewerVariantManifest,
  VariableStateValue,
} from "./model-viewer-dialog-types";

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
  createStateKey,
  getSourceSessionKey,
  normalizeRealtimeShapeKeyState,
  normalizeThreeEnvironment,
  normalizeThreeToneMapping,
  stripRealtimeShapeKeyState,
  withCacheBuster,
} from "./model-viewer-dialog-utils";
import { VariantSlider, VariantTile } from "./model-viewer-dialog-variants";
import { ModelViewerMenuBar } from "./model-viewer-menu-bar";
import { cleanupModelViewerUrl, modelViewerSourceToUrl } from "./model-viewer-session";
import { ThreeModelViewer } from "./three-model-viewer";

export type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

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
  const [activeState, setActiveState] = useState<Record<string, VariableStateValue>>({});
  const [previewState, setPreviewState] = useState<Record<string, VariableStateValue> | null>(null);
  const [manifest, setManifest] = useState<ModelViewerVariantManifest | null>(null);
  const [activeAnimationId, setActiveAnimationId] = useState<string | null>(null);
  const [animationFrameIndex, setAnimationFrameIndex] = useState(0);
  const [animationPlaying, setAnimationPlaying] = useState(false);
  const animationFrameIndexRef = useRef(0);
  animationFrameIndexRef.current = animationFrameIndex;
  const [isResolving, setIsResolving] = useState(false);
  const [viewerUrls, setViewerUrls] = useState<[string, string]>(["", ""]);
  const [activeViewerIndex, setActiveViewerIndex] = useState<0 | 1>(0);
  const [loadingViewerIndex, setLoadingViewerIndex] = useState<0 | 1 | null>(null);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const [doubleSidedEnabled, setDoubleSidedEnabled] = useState(true);
  const [threeToneMapping, setThreeToneMapping] = useState<ModelViewerThreeToneMapping>("neutral");
  const [threeEnvironment, setThreeEnvironment] = useState<ModelViewerThreeEnvironment>("studio");
  const [threeExposure, setThreeExposure] = useState(DEFAULT_THREE_EXPOSURE);
  const [isViewerReady, setIsViewerReady] = useState(false);
  const [isSavingPreview, setIsSavingPreview] = useState(false);
  const [showOverwritePreviewDialog, setShowOverwritePreviewDialog] = useState(false);
  const viewerRefs = useRef<[ModelViewerHandle | null, ModelViewerHandle | null]>([null, null]);
  const doubleSidedEnabledRef = useRef(true);
  const viewerUrlsRef = useRef<[string, string]>(["", ""]);
  const activeViewerIndexRef = useRef<0 | 1>(0);
  const loadingViewerIndexRef = useRef<0 | 1 | null>(null);
  const pendingCameraStateRef = useRef<ModelViewerCameraState | null>(null);
  const initialCameraStateRef = useRef<ModelViewerCameraState | null>(null);
  const openRef = useRef(open);
  const sourceRef = useRef(source);
  const sourceSessionKeyRef = useRef<string | null>(getSourceSessionKey(source));
  const pendingVariantRequestRef = useRef<{
    source: ModelViewerDialogSource;
    viewerIndex: 0 | 1;
    stateKey: string;
  } | null>(null);

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
    openRef.current = open;
  }, [open]);

  useEffect(() => {
    if (open) {
      return;
    }

    resetViewerSession({ resetOrientation: true });
    setShowOverwritePreviewDialog(false);
    setIsSavingPreview(false);
  }, [open]);

  useEffect(() => {
    sourceRef.current = source;
    if (!source || source.mode !== "variant-set") {
      pendingVariantRequestRef.current = null;
    }
  }, [source]);

  useEffect(() => {
    doubleSidedEnabledRef.current = doubleSidedEnabled;
  }, [doubleSidedEnabled]);

  useEffect(() => {
    void Promise.allSettled(
      viewerRefs.current.map(async (viewer) => viewer?.setDoubleSided(doubleSidedEnabled)),
    );
  }, [doubleSidedEnabled]);

  useEffect(() => {
    let cancelled = false;

    void Promise.all([
      getSetting("modelViewer.toneMapping"),
      getSetting("modelViewer.environment"),
      getSetting("modelViewer.exposure"),
    ])
      .then(([toneMapping, environment, exposure]) => {
        if (cancelled) {
          return;
        }

        setThreeToneMapping(normalizeThreeToneMapping(toneMapping));
        setThreeEnvironment(normalizeThreeEnvironment(environment));
        setThreeExposure(clampThreeExposure(exposure));
      })
      .catch((error) => {
        console.error("Failed to load model viewer rendering settings", error);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const nextSourceSessionKey = getSourceSessionKey(source);
    const shouldResetOrientation = sourceSessionKeyRef.current !== nextSourceSessionKey;
    sourceSessionKeyRef.current = nextSourceSessionKey;

    setPreviewState(null);

    if (!source) {
      resetViewerSession({ resetOrientation: shouldResetOrientation });
      setActiveState({});
      setManifest(null);
      setActiveAnimationId(null);
      setAnimationFrameIndex(0);
      setAnimationPlaying(false);
      setViewerUrl(0, "");
      setViewerUrl(1, "");
      return;
    }

    if (source.mode === "payload") {
      resetViewerSession({ resetOrientation: shouldResetOrientation });
      setActiveState(source.transport.defaultState);
      setManifest(null);
      setActiveAnimationId(source.transport.animations[0]?.id ?? null);
      setAnimationFrameIndex(0);
      setAnimationPlaying(false);
      setViewerUrl(0, "");
      setViewerUrl(1, "");
      return;
    }

    if (source.mode === "single") {
      resetViewerSession({ resetOrientation: shouldResetOrientation });
      setActiveState({});
      setManifest(null);
      setActiveAnimationId(null);
      setAnimationFrameIndex(0);
      setAnimationPlaying(false);
      setViewerUrl(0, source.glbPath);
      setViewerUrl(1, "");
      return;
    }

    resetViewerSession({ resetOrientation: shouldResetOrientation });
    setActiveState(source.manifest.defaultState);
    setManifest(source.manifest);
    setActiveAnimationId(source.manifest.animations?.[0]?.id ?? null);
    setAnimationFrameIndex(0);
    setAnimationPlaying(false);
    setViewerUrl(0, source.activeGlbPath || source.defaultGlbPath);
    setViewerUrl(1, "");
  }, [source]);

  const payloadTransport = source?.mode === "payload" ? source.transport : null;
  const payloadAnimations = useMemo(
    () =>
      (payloadTransport?.animations ?? []).map((clip) => ({
        ...clip,
        frames: clip.frames.map((frame) => ({ ...frame, meshes: [] })),
      })),
    [payloadTransport],
  );
  const animationClips = manifest?.animations ?? payloadAnimations;
  const activeAnimation =
    animationClips.find((animation) => animation.id === activeAnimationId) ??
    animationClips[0] ??
    null;
  const activeAnimationFrame = activeAnimation?.frames[animationFrameIndex] ?? null;
  const animationVariableIds = new Set(activeAnimation?.variableIds ?? []);

  useEffect(() => {
    if (!activeAnimation) {
      setAnimationFrameIndex(0);
      setAnimationPlaying(false);
      return;
    }

    setAnimationFrameIndex(0);
    setAnimationPlaying(activeAnimation.frames.length > 1);
    viewerRefs.current[0]?.setAnimationFrame(0);
  }, [activeAnimation]);

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
      viewerRefs.current[0]?.setAnimationFrame(frameIndex);
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
        viewerRefs.current[activeViewerIndexRef.current]?.restoreCameraState(
          initialCameraStateRef.current,
        );
      });
    });
  };

  const handleResetToggles = async () => {
    if (!source || isResolving || loadingViewerIndex !== null) {
      return;
    }
    if (source.mode === "payload") {
      setActiveState(source.transport.defaultState);
      return;
    }
    if (source.mode !== "variant-set") {
      return;
    }

    const nextState = manifest?.defaultState ?? source.manifest.defaultState;
    const artifactState = stripRealtimeShapeKeyState(nextState, manifest?.shapeKeys);

    if (
      createStateKey(stripRealtimeShapeKeyState(activeState, manifest?.shapeKeys)) ===
      createStateKey(artifactState)
    ) {
      setActiveState(nextState);
      return;
    }

    const nextViewerIndex: 0 | 1 = activeViewerIndex === 0 ? 1 : 0;
    pendingCameraStateRef.current =
      viewerRefs.current[activeViewerIndex]?.captureCameraState() ?? null;

    const nextStateKey = createStateKey(artifactState);
    const existingStateArtifact = manifest?.states.find((entry) => entry.key === nextStateKey);
    if (existingStateArtifact?.glbPath) {
      setActiveState(nextState);
      loadingViewerIndexRef.current = nextViewerIndex;
      setLoadingViewerIndex(nextViewerIndex);
      setViewerUrl(nextViewerIndex, existingStateArtifact.glbPath);
      return;
    }

    setIsResolving(true);
    const expectedSource = source;
    const expectedViewerIndex = nextViewerIndex;
    const expectedStateKey = nextStateKey;
    pendingVariantRequestRef.current = {
      source: expectedSource,
      viewerIndex: expectedViewerIndex,
      stateKey: expectedStateKey,
    };
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", {
        artifactRoot: source.artifactRoot,
        manifestPath: source.manifestPath,
        memorySessionId: source.memorySessionId,
        modPath: source.modPath,
        state: artifactState,
      });
      if (result.mode !== "variant-set") {
        return;
      }
      if (
        !openRef.current ||
        sourceRef.current !== expectedSource ||
        pendingVariantRequestRef.current?.source !== expectedSource ||
        pendingVariantRequestRef.current.viewerIndex !== expectedViewerIndex ||
        pendingVariantRequestRef.current.stateKey !== expectedStateKey
      ) {
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
      toast.error("Failed to reset model variant", {
        description: toErrorMessage(error),
      });
    } finally {
      if (
        pendingVariantRequestRef.current?.source === expectedSource &&
        pendingVariantRequestRef.current.viewerIndex === expectedViewerIndex &&
        pendingVariantRequestRef.current.stateKey === expectedStateKey
      ) {
        pendingVariantRequestRef.current = null;
      }
      setIsResolving(false);
    }
  };

  const handleSaveTogglesToIni = async () => {
    if (
      !source ||
      (source.mode !== "variant-set" && source.mode !== "payload") ||
      isResolving ||
      loadingViewerIndex !== null
    ) {
      return;
    }

    const iniPath =
      source.mode === "payload"
        ? source.transport.iniPath
        : (manifest?.iniPath ?? source.manifest.iniPath);
    if (!iniPath) {
      toast.error(t("page.tools.model_viewer.toast.save_to_ini_error"));
      return;
    }

    try {
      const result = await window.api.invoke(
        "tools:persistModelViewerToggleState",
        iniPath,
        activeState,
      );

      if (result.updatedVariables.length > 0) {
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
    if (!source || isResolving || loadingViewerIndex !== null) {
      return;
    }
    if (source.mode === "payload") {
      const variable = source.transport.variables.find((entry) => entry.id === variableId) ?? {
        id: variableId,
      };
      setActiveState((current) => applyVariableSelection(current, variable, value));
      return;
    }
    if (source.mode !== "variant-set") {
      return;
    }

    const nextState = {
      ...activeState,
      [variableId]: value,
    };
    const hasRealtimeShapeKey = Boolean(
      manifest?.shapeKeys?.some((shapeKey) =>
        shapeKey.dimensions.some((dimension) => dimension.variableId === variableId),
      ),
    );
    if (hasRealtimeShapeKey) {
      setActiveState(nextState);
      return;
    }
    const artifactState = stripRealtimeShapeKeyState(nextState, manifest?.shapeKeys);

    const nextViewerIndex: 0 | 1 = activeViewerIndex === 0 ? 1 : 0;
    pendingCameraStateRef.current =
      viewerRefs.current[activeViewerIndex]?.captureCameraState() ?? null;

    const nextStateKey = createStateKey(artifactState);
    const existingStateArtifact = manifest?.states.find((entry) => entry.key === nextStateKey);
    if (existingStateArtifact?.glbPath) {
      setActiveState(nextState);
      loadingViewerIndexRef.current = nextViewerIndex;
      setLoadingViewerIndex(nextViewerIndex);
      setViewerUrl(nextViewerIndex, existingStateArtifact.glbPath);
      return;
    }

    setIsResolving(true);
    const expectedSource = source;
    const expectedViewerIndex = nextViewerIndex;
    const expectedStateKey = nextStateKey;
    pendingVariantRequestRef.current = {
      source: expectedSource,
      viewerIndex: expectedViewerIndex,
      stateKey: expectedStateKey,
    };
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", {
        artifactRoot: source.artifactRoot,
        manifestPath: source.manifestPath,
        memorySessionId: source.memorySessionId,
        modPath: source.modPath,
        state: artifactState,
      });
      if (result.mode !== "variant-set") {
        return;
      }
      if (
        !openRef.current ||
        sourceRef.current !== expectedSource ||
        pendingVariantRequestRef.current?.source !== expectedSource ||
        pendingVariantRequestRef.current.viewerIndex !== expectedViewerIndex ||
        pendingVariantRequestRef.current.stateKey !== expectedStateKey
      ) {
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
        description: toErrorMessage(error),
      });
    } finally {
      if (
        pendingVariantRequestRef.current?.source === expectedSource &&
        pendingVariantRequestRef.current.viewerIndex === expectedViewerIndex &&
        pendingVariantRequestRef.current.stateKey === expectedStateKey
      ) {
        pendingVariantRequestRef.current = null;
      }
      setIsResolving(false);
    }
  };

  const handleApplyResolution = (suggestion: IneffectiveSuggestion) => {
    if (!source || source.mode !== "payload") return;
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
    viewerRefs.current[0]?.setAnimationFrame(0);
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
  const variables = manifest?.variables ?? payloadTransport?.variables ?? [];
  const uiAssets = manifest?.uiAssets ?? payloadTransport?.uiAssets;
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
  const shapeKeys = manifest?.shapeKeys;
  const viewerState = {
    ...activeState,
    ...(activeAnimationFrame?.values ?? {}),
  };
  const viewerVariantState = normalizeRealtimeShapeKeyState(viewerState, variables, shapeKeys);
  const hasVariantTileUi = Boolean(tileBackgroundPath) && tileVariables.length > 0;
  const hasVariantToggleUi = visibleVariables.length > 0;
  const showToggleViewer = Boolean(
    (source?.mode === "variant-set" && manifest && (hasVariantTileUi || hasVariantToggleUi)) ||
    (source?.mode === "payload" && hasVariantToggleUi),
  );
  const isViewerBusy = isResolving || loadingViewerIndex !== null;
  const canSaveCapturedPreview =
    Boolean(source?.modPath) && isViewerReady && !isViewerBusy && !isSavingPreview;

  function resetViewerSession(options?: { resetOrientation?: boolean }) {
    const currentIndex = activeViewerIndexRef.current;
    const currentUrl = viewerUrlsRef.current[currentIndex];
    const inactiveIndex: 0 | 1 = currentIndex === 0 ? 1 : 0;
    const inactiveUrl = viewerUrlsRef.current[inactiveIndex];

    if (inactiveUrl && inactiveUrl !== currentUrl) {
      cleanupModelViewerUrl(inactiveUrl);
    }

    viewerUrlsRef.current = [currentUrl, ""];
    setViewerUrls(viewerUrlsRef.current);
    if (options?.resetOrientation !== false) {
      setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    }
    activeViewerIndexRef.current = 0;
    loadingViewerIndexRef.current = null;
    setActiveViewerIndex(0);
    setLoadingViewerIndex(null);
    pendingCameraStateRef.current = null;
    initialCameraStateRef.current = null;
    setIsViewerReady(false);
  }

  const handleViewerLoad = useCallback((index: 0 | 1) => {
    void (async () => {
      const viewer = viewerRefs.current[index];
      if (!viewer) {
        return;
      }

      await viewer.setDoubleSided(doubleSidedEnabledRef.current);
      const isPendingViewerSwap = loadingViewerIndexRef.current === index;
      const isInitialActiveViewerLoad =
        loadingViewerIndexRef.current === null && activeViewerIndexRef.current === index;
      const shouldRestorePendingCamera =
        isPendingViewerSwap && pendingCameraStateRef.current !== null;
      if (!shouldRestorePendingCamera) {
        await viewer.updateFraming();
      }

      requestAnimationFrame(() => {
        if (initialCameraStateRef.current) {
          return;
        }

        initialCameraStateRef.current = viewerRefs.current[index]?.captureCameraState() ?? null;
      });

      if (!isPendingViewerSwap && !isInitialActiveViewerLoad) {
        return;
      }

      if (shouldRestorePendingCamera) {
        viewer.restoreCameraState(pendingCameraStateRef.current, {
          includeFieldOfView: false,
        });
      }
      pendingCameraStateRef.current = null;
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          activeViewerIndexRef.current = index;
          if (isPendingViewerSwap) {
            loadingViewerIndexRef.current = null;
          }
          setActiveViewerIndex(index);
          setLoadingViewerIndex(null);
          setIsViewerReady(true);
          viewerRefs.current[index]?.setAnimationFrame(animationFrameIndexRef.current);
        });
      });
    })();
  }, []);

  const handleViewerError = useCallback((index: 0 | 1, error: unknown) => {
    if (loadingViewerIndexRef.current !== index) {
      return;
    }

    const activeIndex = activeViewerIndexRef.current;
    pendingCameraStateRef.current = null;
    loadingViewerIndexRef.current = null;
    setActiveViewerIndex(activeIndex);
    setLoadingViewerIndex(null);
    setIsViewerReady(false);
    console.error("Failed to load model viewer source", error);
  }, []);

  const viewerRefCallbacks = useMemo(
    () =>
      [
        (viewer: ModelViewerHandle | null) => {
          viewerRefs.current[0] = viewer;
        },
        (viewer: ModelViewerHandle | null) => {
          viewerRefs.current[1] = viewer;
        },
      ] as const,
    [],
  );
  const onViewerLoad = useMemo(
    () => [() => handleViewerLoad(0), () => handleViewerLoad(1)] as const,
    [handleViewerLoad],
  );
  const onViewerError = useMemo(
    () =>
      [
        (error: unknown) => handleViewerError(0, error),
        (error: unknown) => handleViewerError(1, error),
      ] as const,
    [handleViewerError],
  );

  const captureAndSavePreview = async () => {
    if (!source?.modPath) {
      return;
    }

    const dataUrl =
      (await viewerRefs.current[activeViewerIndexRef.current]?.captureSquarePngDataUrl()) ?? null;
    if (!dataUrl) {
      throw new Error(t("page.tools.model_viewer.toast.capture_preview_error"));
    }

    await window.api.invoke(
      "mod:pastePreview",
      source.modPath,
      dataUrl,
      "base64",
      existingPreviewPath,
    );
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
            showToggleViewer={showToggleViewer}
            isViewerBusy={isViewerBusy}
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
              {payloadTransport || viewerUrls.some((url) => Boolean(url)) ? (
                <>
                  {([0, 1] as const).map((index) => (
                    <ThreeModelViewer
                      key={index}
                      ref={viewerRefCallbacks[index]}
                      className={cn(
                        "absolute inset-0 h-full w-full transition-opacity duration-200",
                        activeViewerIndex === index
                          ? "z-10 opacity-100"
                          : "pointer-events-none z-0 opacity-0",
                      )}
                      src={viewerUrls[index]}
                      payloadTransport={index === 0 ? (payloadTransport ?? undefined) : undefined}
                      payloadEval={index === 0 ? (payloadEval ?? undefined) : undefined}
                      orientation={modelOrientation}
                      variantState={payloadTransport ? undefined : viewerVariantState}
                      shapeKeys={shapeKeys}
                      animationClip={activeAnimation ?? undefined}
                      animationFrame={payloadTransport ? undefined : animationFrameIndex}
                      threeToneMapping={threeToneMapping}
                      threeEnvironment={threeEnvironment}
                      threeExposure={threeExposure}
                      onLoad={onViewerLoad[index]}
                      onError={onViewerError[index]}
                    />
                  ))}
                  <div
                    className={cn(
                      "absolute inset-0 z-20 flex items-center justify-center bg-black/20 transition-opacity duration-200",
                      isViewerBusy
                        ? "cursor-progress opacity-100"
                        : "pointer-events-none opacity-0",
                    )}
                  >
                    <div
                      className={cn(
                        "inline-flex items-center gap-2 rounded-md border bg-background/90 px-3 py-2 text-sm text-foreground shadow-sm transition-all duration-200",
                        isViewerBusy
                          ? "translate-y-0 scale-100 opacity-100"
                          : "translate-y-1 scale-95 opacity-0",
                      )}
                    >
                      <Loader2Icon className="size-4 animate-spin" />
                      {t("page.tools.model_viewer.generating_selected_state")}
                    </div>
                  </div>
                </>
              ) : (
                <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
                  {t("page.tools.model_viewer.model_data_unavailable")}
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
                              disabled={isViewerBusy}
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
                                disabled={isViewerBusy}
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
                                              if (source?.mode === "payload")
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
                                                    if (source?.mode === "payload")
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
                                                        if (source?.mode === "payload") {
                                                          let next = { ...activeState };
                                                          for (const change of ineffectiveEntry
                                                            .suggestions[0].changes) {
                                                            const v =
                                                              source.transport.variables.find(
                                                                (entry) =>
                                                                  entry.id === change.varId,
                                                              ) ?? { id: change.varId };
                                                            next = applyVariableSelection(
                                                              next,
                                                              v,
                                                              change.toValue,
                                                            );
                                                          }
                                                          setPreviewState(next);
                                                        }
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
                            disabled={isViewerBusy}
                            realtime={
                              source?.mode === "payload" ||
                              Boolean(
                                shapeKeys?.some((shapeKey) =>
                                  shapeKey.dimensions.some(
                                    (dimension) => dimension.variableId === variable.id,
                                  ),
                                ),
                              )
                            }
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
                    viewerRefs.current[0]?.setAnimationFrame(index);
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

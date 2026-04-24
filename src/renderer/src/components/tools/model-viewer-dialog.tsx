import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import {
  Menubar,
  MenubarCheckboxItem,
  MenubarContent,
  MenubarGroup,
  MenubarItem,
  MenubarLabel,
  MenubarMenu,
  MenubarRadioGroup,
  MenubarRadioItem,
  MenubarSeparator,
  MenubarTrigger,
} from "@renderer/components/ui/menubar";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import { Loader2Icon, RotateCcwIcon, SaveIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { GoogleModelViewer } from "./google-model-viewer";
import {
  formatOrientation,
  type ModelViewerCameraState,
  type ModelViewerHandle,
  type ModelViewerRealtimeShapeKey,
  type ModelViewerRenderer,
  type ModelViewerThreeEnvironment,
  type ModelViewerThreeToneMapping,
  parseOrientation,
} from "./model-viewer-contract";
import { cleanupModelViewerUrl, modelViewerSourceToUrl } from "./model-viewer-session";
import { ThreeModelViewer } from "./three-model-viewer";

type VariableStateValue = number | string;
type ModelRotationAction = { label: string; delta: [number, number, number] };

const DEFAULT_MODEL_ORIENTATION = "0deg 0deg 0deg";
const DEFAULT_THREE_EXPOSURE = 0.7;
const MIN_THREE_EXPOSURE = 0;
const MAX_THREE_EXPOSURE = 4;
const MODEL_ROTATION_ACTIONS: ModelRotationAction[] = [
  { label: "Left 90°", delta: [0, 0, -90] },
  { label: "Right 90°", delta: [0, 0, 90] },
  { label: "Up 90°", delta: [0, -90, 0] },
  { label: "Down 90°", delta: [0, 90, 0] },
  { label: "Flip 180°", delta: [0, 0, 180] },
];

type ModelViewerVariantManifest = {
  iniPath: string;
  defaultState: Record<string, VariableStateValue>;
  variables: Array<{
    id: string;
    label: string;
    defaultValue: VariableStateValue;
    values: Array<{ value: VariableStateValue; label: string }>;
    order: number;
    slot?: number;
    iconPath?: string;
    controlType?: "buttons" | "slider";
    slider?: {
      min: number;
      max: number;
      step: number;
    };
  }>;
  uiAssets: {
    backgroundPath?: string;
    slotPath?: string;
    slotHoverPath?: string;
    slotActivePath?: string;
  };
  shapeKeys?: ModelViewerRealtimeShapeKey[];
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
  const { t } = useTranslation();
  const [activeState, setActiveState] = useState<Record<string, VariableStateValue>>({});
  const [manifest, setManifest] = useState<ModelViewerVariantManifest | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const [viewerUrls, setViewerUrls] = useState<[string, string]>(["", ""]);
  const [activeViewerIndex, setActiveViewerIndex] = useState<0 | 1>(0);
  const [loadingViewerIndex, setLoadingViewerIndex] = useState<0 | 1 | null>(null);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const [doubleSidedEnabled, setDoubleSidedEnabled] = useState(true);
  const [renderer, setRenderer] = useState<ModelViewerRenderer>("three");
  const [threeToneMapping, setThreeToneMapping] = useState<ModelViewerThreeToneMapping>("neutral");
  const [threeEnvironment, setThreeEnvironment] = useState<ModelViewerThreeEnvironment>("studio");
  const [threeExposure, setThreeExposure] = useState(DEFAULT_THREE_EXPOSURE);
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

    setRenderer("three");
    resetViewerSession({ resetOrientation: true });
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
      viewerRefs.current.map((viewer) => viewer?.setDoubleSided(doubleSidedEnabled)),
    );
  }, [doubleSidedEnabled]);

  useEffect(() => {
    let cancelled = false;

    void Promise.all([
      window.api.invoke("setting:modelViewer:getToneMapping"),
      window.api.invoke("setting:modelViewer:getEnvironment"),
      window.api.invoke("setting:modelViewer:getExposure"),
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

    if (!source) {
      resetViewerSession({ resetOrientation: shouldResetOrientation });
      setRenderer("three");
      setActiveState({});
      setManifest(null);
      setViewerUrl(0, "");
      setViewerUrl(1, "");
      return;
    }

    if (source.mode === "single") {
      resetViewerSession({ resetOrientation: shouldResetOrientation });
      setRenderer("three");
      setActiveState({});
      setManifest(null);
      setViewerUrl(0, source.glbPath);
      setViewerUrl(1, "");
      return;
    }

    resetViewerSession({ resetOrientation: shouldResetOrientation });
    setRenderer("three");
    setActiveState(source.manifest.defaultState);
    setManifest(source.manifest);
    setViewerUrl(0, source.activeGlbPath || source.defaultGlbPath);
    setViewerUrl(1, "");
  }, [source]);

  useEffect(() => {
    resetViewerSession({ resetOrientation: false });
  }, [renderer]);

  const updateThreeToneMapping = (value: ModelViewerThreeToneMapping) => {
    setThreeToneMapping(value);
    void window.api.invoke("setting:modelViewer:setToneMapping", value).catch((error) => {
      console.error("Failed to persist model viewer tone mapping", error);
      toast.error("Failed to save tone mapping setting.");
    });
  };

  const updateThreeEnvironment = (value: ModelViewerThreeEnvironment) => {
    setThreeEnvironment(value);
    void window.api.invoke("setting:modelViewer:setEnvironment", value).catch((error) => {
      console.error("Failed to persist model viewer environment", error);
      toast.error("Failed to save environment setting.");
    });
  };

  const updateThreeExposure = (value: number) => {
    const nextValue = clampThreeExposure(value);
    setThreeExposure(nextValue);
    void window.api.invoke("setting:modelViewer:setExposure", nextValue).catch((error) => {
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
    if (!source || source.mode !== "variant-set" || isResolving || loadingViewerIndex !== null) {
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
        description: error instanceof Error ? error.message : String(error),
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
    if (!source || source.mode !== "variant-set" || isResolving || loadingViewerIndex !== null) {
      return;
    }

    const iniPath = manifest?.iniPath ?? source.manifest.iniPath;
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
        description: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const handleSelectValue = async (variableId: string, value: VariableStateValue) => {
    if (!source || source.mode !== "variant-set" || isResolving || loadingViewerIndex !== null) {
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
    if (hasRealtimeShapeKey && renderer === "three") {
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
        description: error instanceof Error ? error.message : String(error),
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

  const variables = manifest?.variables ?? [];
  const uiAssets = manifest?.uiAssets;
  const visibleVariables = variables.filter((variable) => variable.values.length > 0);
  const tileVariables = visibleVariables.filter((variable) => variable.controlType !== "slider");
  const sliderVariables = visibleVariables.filter((variable) => variable.controlType === "slider");
  const tileBackgroundPath = uiAssets?.backgroundPath;
  const slotPath = uiAssets?.slotPath;
  const slotHoverPath = uiAssets?.slotHoverPath;
  const slotActivePath = uiAssets?.slotActivePath;
  const shapeKeys = manifest?.shapeKeys;
  const viewerVariantState =
    renderer === "three"
      ? normalizeRealtimeShapeKeyState(activeState, variables, shapeKeys)
      : activeState;
  const hasVariantTileUi = Boolean(tileBackgroundPath) && tileVariables.length > 0;
  const hasVariantToggleUi = visibleVariables.length > 0;
  const showToggleViewer = Boolean(
    source?.mode === "variant-set" && manifest && (hasVariantTileUi || hasVariantToggleUi),
  );
  const isViewerBusy = isResolving || loadingViewerIndex !== null;

  const ActiveViewerComponent = renderer === "google" ? GoogleModelViewer : ThreeModelViewer;

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
  }

  const handleViewerLoad = (index: 0 | 1) => {
    void (async () => {
      const viewer = viewerRefs.current[index];
      if (!viewer) {
        return;
      }

      await viewer.setDoubleSided(doubleSidedEnabledRef.current);
      const shouldRestorePendingCamera =
        loadingViewerIndexRef.current === index && pendingCameraStateRef.current !== null;
      if (!shouldRestorePendingCamera) {
        await viewer.updateFraming();
      }

      requestAnimationFrame(() => {
        if (initialCameraStateRef.current) {
          return;
        }

        initialCameraStateRef.current = viewerRefs.current[index]?.captureCameraState() ?? null;
      });

      if (loadingViewerIndexRef.current !== index) {
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
          loadingViewerIndexRef.current = null;
          setActiveViewerIndex(index);
          setLoadingViewerIndex(null);
        });
      });
    })();
  };

  const handleViewerError = (index: 0 | 1, error: unknown) => {
    if (loadingViewerIndexRef.current !== index) {
      return;
    }

    const activeIndex = activeViewerIndexRef.current;
    pendingCameraStateRef.current = null;
    loadingViewerIndexRef.current = null;
    setActiveViewerIndex(activeIndex);
    setLoadingViewerIndex(null);
    console.error("Failed to load model viewer source", error);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex min-w-[95vw] max-h-[92vh] h-full flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={source?.name}>
            {source?.name || t("page.tools.model_viewer.title")}
          </DialogTitle>
        </DialogHeader>

        <Menubar>
          <MenubarMenu>
            <MenubarTrigger>{t("page.tools.model_viewer.menu.model")}</MenubarTrigger>
            <MenubarContent>
              <MenubarGroup>
                <MenubarLabel className="text-xs text-muted-foreground">
                  {t("page.tools.model_viewer.menu.rotate")}
                </MenubarLabel>
                {MODEL_ROTATION_ACTIONS.map((action) => (
                  <MenubarItem key={action.label} onClick={() => rotateModel(action.delta)}>
                    {t(`page.tools.model_viewer.rotate_actions.${action.label}`)}
                  </MenubarItem>
                ))}
              </MenubarGroup>
              <MenubarSeparator />
              <MenubarGroup>
                <MenubarItem onClick={handleResetView}>
                  <RotateCcwIcon />
                  {t("page.tools.model_viewer.menu.reset")}
                </MenubarItem>
              </MenubarGroup>
            </MenubarContent>
          </MenubarMenu>
          <MenubarMenu>
            <MenubarTrigger>{t("page.tools.model_viewer.menu.texture")}</MenubarTrigger>
            <MenubarContent>
              <MenubarGroup>
                <MenubarCheckboxItem
                  checked={doubleSidedEnabled}
                  onCheckedChange={(checked) => setDoubleSidedEnabled(checked === true)}
                >
                  Double Sided
                </MenubarCheckboxItem>
              </MenubarGroup>
            </MenubarContent>
          </MenubarMenu>
          <MenubarMenu>
            <MenubarTrigger>{t("page.tools.model_viewer.menu.renderer")}</MenubarTrigger>
            <MenubarContent>
              <MenubarRadioGroup
                value={renderer}
                onValueChange={(value) => setRenderer(value as ModelViewerRenderer)}
              >
                {!shapeKeys?.length ? (
                  <MenubarRadioItem value="google">@google/model-viewer</MenubarRadioItem>
                ) : null}
                <MenubarRadioItem value="three">Three.js</MenubarRadioItem>
              </MenubarRadioGroup>
            </MenubarContent>
          </MenubarMenu>
          <MenubarMenu>
            <MenubarTrigger disabled={renderer !== "three"}>Rendering</MenubarTrigger>
            <MenubarContent>
              <MenubarGroup>
                <MenubarLabel className="text-xs text-muted-foreground">Tone Mapping</MenubarLabel>
                <MenubarRadioGroup
                  value={threeToneMapping}
                  onValueChange={(value) =>
                    updateThreeToneMapping(value as ModelViewerThreeToneMapping)
                  }
                >
                  <MenubarRadioItem value="neutral">Neutral</MenubarRadioItem>
                  <MenubarRadioItem value="aces">ACES Filmic</MenubarRadioItem>
                  <MenubarRadioItem value="none">None</MenubarRadioItem>
                </MenubarRadioGroup>
              </MenubarGroup>
              <MenubarSeparator />
              <MenubarGroup>
                <MenubarLabel className="text-xs text-muted-foreground">Environment</MenubarLabel>
                <MenubarRadioGroup
                  value={threeEnvironment}
                  onValueChange={(value) =>
                    updateThreeEnvironment(value as ModelViewerThreeEnvironment)
                  }
                >
                  <MenubarRadioItem value="studio">Studio</MenubarRadioItem>
                  <MenubarRadioItem value="soft">Soft</MenubarRadioItem>
                  <MenubarRadioItem value="none">None</MenubarRadioItem>
                </MenubarRadioGroup>
              </MenubarGroup>
              <MenubarSeparator />
              <MenubarGroup>
                <MenubarLabel className="text-xs text-muted-foreground">Exposure</MenubarLabel>
                <div className="px-1.5 py-1">
                  <div className="mb-2 flex items-center gap-1">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 px-2"
                      onClick={() => updateThreeExposure(threeExposure - 0.1)}
                    >
                      -0.1
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 px-2"
                      onClick={() => updateThreeExposure(DEFAULT_THREE_EXPOSURE)}
                    >
                      Reset
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 px-2"
                      onClick={() => updateThreeExposure(threeExposure + 0.1)}
                    >
                      +0.1
                    </Button>
                  </div>
                  <Input
                    type="number"
                    min={MIN_THREE_EXPOSURE}
                    max={MAX_THREE_EXPOSURE}
                    step={0.05}
                    value={formatSliderValue(threeExposure)}
                    onChange={(event) => {
                      const nextValue = Number.parseFloat(event.target.value);
                      if (Number.isFinite(nextValue)) {
                        setThreeExposure(nextValue);
                      }
                    }}
                    onBlur={(event) => {
                      updateThreeExposure(Number.parseFloat(event.target.value));
                    }}
                  />
                  <div className="mt-2 flex items-center justify-between text-[11px] text-muted-foreground">
                    <span>{formatSliderValue(MIN_THREE_EXPOSURE)}</span>
                    <span>{formatSliderValue(MAX_THREE_EXPOSURE)}</span>
                  </div>
                </div>
              </MenubarGroup>
            </MenubarContent>
          </MenubarMenu>
          {showToggleViewer ? (
            <MenubarMenu>
              <MenubarTrigger>{t("page.tools.model_viewer.menu.toggle")}</MenubarTrigger>
              <MenubarContent>
                <MenubarGroup>
                  <MenubarItem onClick={handleSaveTogglesToIni} disabled={isViewerBusy}>
                    <SaveIcon />
                    {t("page.tools.model_viewer.menu.save_to_ini")}
                  </MenubarItem>
                  <MenubarSeparator />
                  <MenubarItem onClick={handleResetToggles}>
                    <RotateCcwIcon />
                    {t("page.tools.model_viewer.menu.reset")}
                  </MenubarItem>
                </MenubarGroup>
              </MenubarContent>
            </MenubarMenu>
          ) : null}
        </Menubar>

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
                  <ActiveViewerComponent
                    key={index}
                    ref={(viewer) => {
                      viewerRefs.current[index] = viewer;
                    }}
                    className={cn(
                      "absolute inset-0 h-full w-full transition-opacity duration-200",
                      activeViewerIndex === index
                        ? "z-10 opacity-100"
                        : "pointer-events-none z-0 opacity-0",
                    )}
                    src={viewerUrls[index]}
                    orientation={modelOrientation}
                    variantState={viewerVariantState}
                    shapeKeys={shapeKeys}
                    threeToneMapping={threeToneMapping}
                    threeEnvironment={threeEnvironment}
                    threeExposure={threeExposure}
                    onLoad={() => handleViewerLoad(index)}
                    onError={(error) => handleViewerError(index, error)}
                  />
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

                  <div className="space-y-3">
                    {tileVariables.map((variable) => (
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
                    {sliderVariables.map((variable) => (
                      <VariantSlider
                        key={variable.id}
                        variable={variable}
                        activeValue={activeState[variable.id]}
                        disabled={isViewerBusy}
                        realtime={Boolean(
                          shapeKeys?.some((shapeKey) =>
                            shapeKey.dimensions.some(
                              (dimension) => dimension.variableId === variable.id,
                            ),
                          ) && renderer === "three",
                        )}
                        onSelect={handleSelectValue}
                      />
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
  disabled,
  onSelect,
}: {
  variable: ModelViewerVariantManifest["variables"][number];
  activeValue: VariableStateValue | undefined;
  slotPath?: string;
  slotHoverPath?: string;
  slotActivePath?: string;
  disabled?: boolean;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const isActive = String(activeValue) !== String(variable.defaultValue);
  const framePath = isActive ? slotActivePath || slotHoverPath || slotPath : slotPath;

  return (
    <button
      type="button"
      className={cn(
        "relative flex aspect-square min-h-20 items-end justify-center overflow-hidden rounded-md border bg-black/20 p-2 text-white transition",
        disabled ? "cursor-not-allowed opacity-60" : "hover:bg-black/30",
      )}
      disabled={disabled}
      aria-disabled={disabled}
      tabIndex={disabled ? -1 : 0}
      onClick={() => {
        if (disabled || variable.values.length === 0) {
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

function VariantSlider({
  variable,
  activeValue,
  disabled,
  realtime,
  onSelect,
}: {
  variable: ModelViewerVariantManifest["variables"][number];
  activeValue: VariableStateValue | undefined;
  disabled?: boolean;
  realtime?: boolean;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const slider = variable.slider;
  const fallbackValue =
    typeof variable.defaultValue === "number"
      ? variable.defaultValue
      : Number(variable.values[0]?.value ?? 0);
  const resolvedValue =
    typeof activeValue === "number" ? activeValue : Number(activeValue ?? fallbackValue);
  const [draftValue, setDraftValue] = useState(resolvedValue);
  const commitTimeoutRef = useRef<number | null>(null);

  useEffect(() => {
    setDraftValue(resolvedValue);
  }, [resolvedValue]);

  useEffect(() => {
    return () => {
      if (commitTimeoutRef.current !== null) {
        window.clearTimeout(commitTimeoutRef.current);
      }
    };
  }, []);

  if (!slider) {
    return null;
  }

  const scheduleCommit = (nextValue: number) => {
    if (commitTimeoutRef.current !== null) {
      window.clearTimeout(commitTimeoutRef.current);
    }
    commitTimeoutRef.current = window.setTimeout(() => {
      onSelect(variable.id, nextValue);
      commitTimeoutRef.current = null;
    }, 150);
  };

  return (
    <div className="rounded-md border bg-background/50 p-3">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {variable.iconPath ? (
            <img
              src={modelViewerSourceToUrl(variable.iconPath)}
              className="size-8 rounded object-contain"
            />
          ) : null}
          <div className="text-sm font-medium">{variable.label}</div>
        </div>
        <div className="text-xs tabular-nums text-muted-foreground">
          {formatSliderValue(draftValue)}
        </div>
      </div>
      <input
        type="range"
        min={slider.min}
        max={slider.max}
        step={slider.step}
        value={draftValue}
        disabled={disabled}
        className={cn("w-full accent-primary", disabled && "cursor-not-allowed opacity-60")}
        onChange={(event) => {
          const nextValue = Number(event.currentTarget.value);
          setDraftValue(nextValue);
          if (!disabled) {
            if (realtime) {
              onSelect(variable.id, nextValue);
            } else {
              scheduleCommit(nextValue);
            }
          }
        }}
      />
      <div className="mt-2 flex items-center justify-between text-[11px] tabular-nums text-muted-foreground">
        <span>{formatSliderValue(slider.min)}</span>
        <span>{formatSliderValue(slider.max)}</span>
      </div>
    </div>
  );
}

function formatSliderValue(value: number): string {
  if (Number.isInteger(value)) {
    return String(value);
  }

  return value.toFixed(2).replace(/\.?0+$/, "");
}

function stripRealtimeShapeKeyState(
  state: Record<string, VariableStateValue>,
  shapeKeys?: ModelViewerRealtimeShapeKey[],
): Record<string, VariableStateValue> {
  if (!shapeKeys?.length) {
    return state;
  }

  const stripped = { ...state };
  for (const shapeKey of shapeKeys) {
    for (const dimension of shapeKey.dimensions) {
      delete stripped[dimension.variableId];
    }
  }
  return stripped;
}

function normalizeRealtimeShapeKeyState(
  state: Record<string, VariableStateValue>,
  variables: ModelViewerVariantManifest["variables"],
  shapeKeys?: ModelViewerRealtimeShapeKey[],
): Record<string, VariableStateValue> {
  if (!shapeKeys?.length) {
    return state;
  }

  const normalized = { ...state };
  const realtimeVariableIds = new Set(
    shapeKeys.flatMap((shapeKey) => shapeKey.dimensions.map((dimension) => dimension.variableId)),
  );

  for (const variable of variables) {
    if (!realtimeVariableIds.has(variable.id) || !variable.slider) {
      continue;
    }

    const rawValue = normalized[variable.id];
    if (typeof rawValue !== "number") {
      continue;
    }

    const range = variable.slider.max - variable.slider.min;
    if (range <= 0) {
      normalized[variable.id] = 0.5;
      continue;
    }

    normalized[variable.id] = Math.min(1, Math.max(0, (rawValue - variable.slider.min) / range));
  }

  return normalized;
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

function getSourceSessionKey(source: ModelViewerDialogSource | null): string | null {
  if (!source) {
    return null;
  }

  if (source.mode === "single") {
    return `single:${source.glbPath}`;
  }

  return `variant:${source.manifestPath}`;
}

function clampThreeExposure(value: number): number {
  if (!Number.isFinite(value)) {
    return DEFAULT_THREE_EXPOSURE;
  }

  return Math.min(MAX_THREE_EXPOSURE, Math.max(MIN_THREE_EXPOSURE, Math.round(value * 100) / 100));
}

function normalizeThreeToneMapping(value: string | null | undefined): ModelViewerThreeToneMapping {
  return value === "aces" || value === "none" || value === "neutral" ? value : "neutral";
}

function normalizeThreeEnvironment(value: string | null | undefined): ModelViewerThreeEnvironment {
  return value === "soft" || value === "none" || value === "studio" ? value : "studio";
}

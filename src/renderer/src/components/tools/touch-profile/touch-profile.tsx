import {
  BodyShapeViewport,
  type BodyShapeViewportHandle,
} from "@renderer/components/tools/body-shape/body-shape-viewport";
import {
  formatOrientation,
  parseOrientation,
} from "@renderer/components/tools/model-viewer/model-viewer-contract";
import {
  DEFAULT_MODEL_ORIENTATION,
  DEFAULT_THREE_EXPOSURE,
} from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import { ModelViewerMenuBar } from "@renderer/components/tools/model-viewer/model-viewer-menu-bar";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Slider } from "@renderer/components/ui/slider";
import { Switch } from "@renderer/components/ui/switch";
import { cn } from "@renderer/lib/utils";
import type {
  TouchProfileComponentSummary,
  TouchProfileMeshPreview,
  TouchProfilePreview,
} from "@shared/touch-profile-preview";
import {
  TOUCH_PHYSICS_PRESETS,
  TOUCH_PROFILE_MASK_CURVE_RANGE,
  TOUCH_PROFILE_MASK_STRENGTH_RANGE,
  TOUCH_PROFILE_SETTING_RANGES,
  type TouchPhysicsPreset,
  type TouchProfileAdvancedSettings,
  type TouchZoneSettings,
  type TouchZoneStrengthPreset,
} from "@shared/touch-profile-settings";
import type { TouchProfileProgressEvent } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { FolderOpenIcon, Loader2Icon, RotateCcwIcon, SparklesIcon, Undo2Icon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const ALL_ZONES = "__all__";
const DEFAULT_MASK_STRENGTH = 1;
const DEFAULT_MASK_CURVE = 1;

type TouchDraft = {
  sessionId: string;
  sourceModRoot: string;
  canAutoApply: boolean;
  warnings: string[];
  analysis: { supportGrade: string };
  llm?: {
    protocol: string;
    endpoint: string;
    model: string;
    reasoning: string;
  };
  components: TouchProfileComponentSummary[];
};

type TouchModInspection = {
  sessionId: string;
  modRoot: string;
  supportGrade: string;
  supportReasons: string[];
  components: Array<{
    id: string;
    name: string;
    kind: string;
    supportGrade: string;
    interactiveCandidate: boolean;
    vertexCount: number;
    indexCount: number;
    variantKey?: string;
    variantCondition?: string;
  }>;
};

type TouchApplyResultState = {
  sessionId: string;
  outputModRoot: string;
  sourceModRoot: string;
  reenableSourceOnRollback: boolean;
};

type TouchProfileInputError = "already_touch" | "suspected_touch";

export default function TouchProfileTool({
  fixedTargetPath,
  modName,
  onApplied,
  onRolledBack,
}: {
  fixedTargetPath?: string;
  modName?: string;
  onApplied?: (result: { outputModRoot: string; sourceModRoot: string }) => void;
  onRolledBack?: (sourceModRoot: string) => void;
} = {}) {
  const { t } = useTranslation();
  const viewportRef = useRef<BodyShapeViewportHandle | null>(null);
  const draftSessionRef = useRef<string | null>(null);

  const [modPath, setModPath] = useState(fixedTargetPath ?? "");
  const [loading, setLoading] = useState(false);
  const [reanalyzing, setReanalyzing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [rollingBack, setRollingBack] = useState(false);
  const [draft, setDraft] = useState<TouchDraft | null>(null);
  const [inspection, setInspection] = useState<TouchModInspection | null>(null);
  const [phase, setPhase] = useState<"select" | "review">("select");
  const [selectedMeshIds, setSelectedMeshIds] = useState<Set<string>>(new Set());
  const [selectedMeshId, setSelectedMeshId] = useState("");
  const [meshPreview, setMeshPreview] = useState<TouchProfileMeshPreview | null>(null);
  const [meshPreviewLoading, setMeshPreviewLoading] = useState(false);
  const [meshPreviewError, setMeshPreviewError] = useState<string | null>(null);
  const [progress, setProgress] = useState<TouchProfileProgressEvent | null>(null);
  const [result, setResult] = useState<TouchApplyResultState | null>(null);
  const [inputError, setInputError] = useState<TouchProfileInputError | null>(null);

  const [selectedComponentId, setSelectedComponentId] = useState("");
  const [selectedZoneId, setSelectedZoneId] = useState(ALL_ZONES);
  const [linkedComponents, setLinkedComponents] = useState<Record<string, boolean>>({});
  const [preview, setPreview] = useState<TouchProfilePreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewReloadVersion, setPreviewReloadVersion] = useState(0);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const [pendingSettingsSaves, setPendingSettingsSaves] = useState(0);
  const settingsSaveQueueRef = useRef(Promise.resolve());
  const previewRefreshRequestRef = useRef(0);

  const reanalyzeTurn = async (componentId: string) => {
    if (!draft || reanalyzing) return;
    setReanalyzing(true);
    try {
      const next = await window.api.invoke("tools:touchProfileReanalyzeTurn", {
        sessionId: draft.sessionId,
        componentId,
      });
      setDraft(next);
      setPreviewReloadVersion((v) => v + 1);
      const updatedComponent = next.components.find((c) => c.componentId === componentId);
      toast.success(
        t("page.tools.touch_profile.toast.reanalyzed", {
          turn: updatedComponent?.currentTurn ?? 1,
        }),
      );
    } catch (error) {
      toast.error(t("page.tools.touch_profile.toast.reanalyze_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setReanalyzing(false);
    }
  };

  const selectTurn = async (componentId: string, turn: number) => {
    if (!draft) return;
    try {
      const next = await window.api.invoke("tools:touchProfileSelectTurn", {
        sessionId: draft.sessionId,
        componentId,
        turn,
      });
      setDraft(next);
      setPreviewReloadVersion((v) => v + 1);
      toast.success(t("page.tools.touch_profile.toast.turn_selected", { turn }));
    } catch (error) {
      toast.error(t("page.tools.touch_profile.toast.turn_select_failed"), {
        description: toErrorMessage(error),
      });
    }
  };

  const isFixedTarget = Boolean(fixedTargetPath);
  const selectedMeshComponentId = inspection?.components.find(
    (component) => component.id === selectedMeshId,
  )?.id;
  const interactiveComponents = useMemo(
    () =>
      (draft?.components ?? []).filter(
        (component) => component.interactive && component.zones.length > 0,
      ),
    [draft?.components],
  );
  const firstComponentId = interactiveComponents[0]?.componentId ?? "";
  const selectedComponent = interactiveComponents.find(
    (component) => component.componentId === selectedComponentId,
  );
  const selectedComponentIsValid = selectedComponent !== undefined;
  const selectedZone = selectedComponent?.zones.find((zone) => zone.id === selectedZoneId);
  const activePreview = preview?.componentId === selectedComponentId ? preview : null;

  useEffect(() => {
    return window.api.on("tools:touchProfileProgress", (event) => {
      setProgress(event);
    });
  }, []);

  useEffect(() => {
    draftSessionRef.current = draft?.sessionId ?? null;
  }, [draft?.sessionId]);

  useEffect(() => {
    return () => {
      const sessionId = draftSessionRef.current;
      if (!sessionId) return;
      void window.api.invoke("tools:touchProfileDiscardDraft", sessionId);
    };
  }, []);

  useEffect(() => {
    if (!fixedTargetPath) return;
    setModPath(fixedTargetPath);
    void loadMod(fixedTargetPath);
  }, [fixedTargetPath]);
  useEffect(() => {
    if (selectedComponentIsValid) return;
    setSelectedComponentId(firstComponentId);
  }, [firstComponentId, selectedComponentIsValid]);

  useEffect(() => {
    setSelectedZoneId(ALL_ZONES);
  }, [selectedComponentId]);

  useEffect(() => {
    let cancelled = false;
    if (!draft?.sessionId || !selectedComponentId) {
      if (!draft) return;
      setPreview(null);
      setPreviewError(null);
      setPreviewLoading(false);
      return;
    }

    setPreviewError(null);
    setPreviewLoading(true);

    void window.api
      .invoke("tools:touchProfileGetPreview", {
        sessionId: draft.sessionId,
        componentId: selectedComponentId,
      })
      .then((next) => {
        if (cancelled) return;
        setPreview(next);
      })
      .catch((error) => {
        if (cancelled) return;
        setPreviewError(toErrorMessage(error));
      })
      .finally(() => {
        if (!cancelled) setPreviewLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [draft?.sessionId, selectedComponentId, previewReloadVersion]);

  useEffect(() => {
    let cancelled = false;
    if (phase !== "select" || !inspection?.sessionId || !selectedMeshComponentId) {
      if (phase !== "select") return;
      setMeshPreview(null);
      setMeshPreviewError(null);
      setMeshPreviewLoading(false);
      return;
    }

    setMeshPreviewError(null);
    setMeshPreviewLoading(true);

    void window.api
      .invoke("tools:touchProfileGetMeshPreview", {
        sessionId: inspection.sessionId,
        componentId: selectedMeshComponentId,
      })
      .then((next) => {
        if (cancelled) return;
        setMeshPreview(next);
      })
      .catch((error) => {
        if (cancelled) return;
        setMeshPreviewError(toErrorMessage(error));
      })
      .finally(() => {
        if (!cancelled) setMeshPreviewLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [phase, inspection?.sessionId, selectedMeshComponentId]);

  const visibleZones = useMemo(() => {
    if (!activePreview) return [];
    if (selectedZoneId === ALL_ZONES) return activePreview.zones;
    return activePreview.zones.filter((zone) => zone.id === selectedZoneId);
  }, [activePreview, selectedZoneId]);

  const previewRegions = useMemo(
    () =>
      visibleZones.map((zone) => ({
        id: zone.id,
        weights: zone.weights,
        amount: 1,
        axisScale: [1, 1, 1] as [number, number, number],
        pivot: zone.center,
      })),
    [visibleZones],
  );

  const selectFolder = async () => {
    const selected = await window.api.invoke("util:showOpenDialog", {
      properties: ["openDirectory"],
    });
    if (selected && !selected.canceled && selected.filePaths[0]) {
      setModPath(selected.filePaths[0]);
    }
  };

  const loadMod = async (pathOverride?: string) => {
    const targetPath = pathOverride ?? modPath;
    if (!targetPath || loading) return;
    setLoading(true);
    setDraft(null);
    setResult(null);
    setInputError(null);
    setProgress(null);
    setPreview(null);
    setPreviewError(null);
    setMeshPreview(null);
    setMeshPreviewError(null);
    setSelectedComponentId("");
    setSelectedZoneId(ALL_ZONES);
    setLinkedComponents({});
    try {
      const next = await window.api.invoke("tools:touchProfilePrepare", {
        modPath: targetPath,
      });
      setInspection(next);
      setSelectedMeshIds(
        new Set(
          next.components.filter((component) => component.interactiveCandidate).map((c) => c.id),
        ),
      );
      setPhase("select");
    } catch (error) {
      const inputErrorCode = getTouchProfileInputError(toErrorMessage(error));
      if (inputErrorCode) {
        setInputError(inputErrorCode);
        toast.error(t(`page.tools.touch_profile.input_error.${inputErrorCode}.title`), {
          description: t(`page.tools.touch_profile.input_error.${inputErrorCode}.description`),
        });
      } else {
        toast.error(t("page.tools.touch_profile.toast.load_failed"), {
          description: toErrorMessage(error),
        });
      }
    } finally {
      setLoading(false);
    }
  };

  const analyzeSelected = async () => {
    if (!inspection || inspection.components.length === 0) return;
    if (selectedMeshIds.size === 0) {
      toast.error(t("page.tools.touch_profile.toast.no_components_selected"));
      return;
    }
    setLoading(true);
    setProgress(null);
    setInputError(null);
    try {
      const next = await window.api.invoke("tools:touchProfileAnalyzeComponents", {
        sessionId: inspection.sessionId,
        componentIds: [...selectedMeshIds],
      });
      setDraft(next);
      setPhase("review");
      toast.success(t("page.tools.touch_profile.toast.loaded"));
    } catch (error) {
      const inputErrorCode = getTouchProfileInputError(toErrorMessage(error));
      if (inputErrorCode) {
        setInputError(inputErrorCode);
        toast.error(t(`page.tools.touch_profile.input_error.${inputErrorCode}.title`), {
          description: t(`page.tools.touch_profile.input_error.${inputErrorCode}.description`),
        });
      } else {
        toast.error(t("page.tools.touch_profile.toast.load_failed"), {
          description: toErrorMessage(error),
        });
      }
    } finally {
      setLoading(false);
    }
  };

  const backToSelect = () => {
    setDraft(null);
    setPhase("select");
    setProgress(null);
    setPreview(null);
    setPreviewError(null);
    setSelectedComponentId("");
    setSelectedZoneId(ALL_ZONES);
  };

  const clearVisionCache = async () => {
    if (loading) return;
    try {
      await window.api.invoke("tools:touchProfileClearVisionCache");
      toast.success(t("page.tools.touch_profile.toast.vision_cache_cleared"));
    } catch (error) {
      toast.error(t("page.tools.touch_profile.toast.vision_cache_clear_failed"), {
        description: toErrorMessage(error),
      });
    }
  };

  const applyDraft = async (force = false) => {
    if (!draft || applying || pendingSettingsSaves > 0) return;
    const regenerating = result !== null;
    setApplying(true);
    try {
      const next = regenerating
        ? await window.api.invoke("tools:touchProfileRegenerate", {
            sessionId: draft.sessionId,
            force,
          })
        : await window.api.invoke("tools:touchProfileApply", {
            sessionId: draft.sessionId,
            force,
          });
      const applyResult = {
        sessionId: next.sessionId,
        outputModRoot: next.outputModRoot,
        sourceModRoot: next.sourceModRoot,
        reenableSourceOnRollback: next.reenableSourceOnRollback,
      };
      setResult(applyResult);
      toast.success(
        t(
          regenerating
            ? "page.tools.touch_profile.toast.regenerated"
            : "page.tools.touch_profile.toast.created",
        ),
        {
          description: next.outputModRoot,
        },
      );
      if (!regenerating) {
        onApplied?.({
          outputModRoot: next.outputModRoot,
          sourceModRoot: next.sourceModRoot,
        });
      }
    } catch (error) {
      toast.error(
        t(
          regenerating
            ? "page.tools.touch_profile.toast.regenerate_failed"
            : "page.tools.touch_profile.toast.create_failed",
        ),
        {
          description: toErrorMessage(error),
        },
      );
    } finally {
      setApplying(false);
    }
  };

  const updateZoneSettings = (
    componentId: string,
    zoneId: string,
    settings: TouchZoneSettings,
    options?: { refreshPreview?: boolean; zoneIds?: string[] },
  ) => {
    if (!draft || applying || rollingBack) return;
    const zoneIds = options?.zoneIds ?? [zoneId];
    if (zoneIds.length === 0) return;
    setDraft((current) => {
      if (!current) return current;
      return {
        ...current,
        components: current.components.map((component) =>
          component.componentId !== componentId
            ? component
            : {
                ...component,
                zones: component.zones.map((zone) =>
                  zoneIds.includes(zone.id) ? { ...zone, settings } : zone,
                ),
              },
        ),
      };
    });
    setPendingSettingsSaves((count) => count + zoneIds.length);
    const previewRefreshRequest = options?.refreshPreview
      ? ++previewRefreshRequestRef.current
      : undefined;
    settingsSaveQueueRef.current = zoneIds.reduce(
      (queue, nextZoneId, index) =>
        queue
          .catch(() => {})
          .then(async () => {
            await window.api.invoke("tools:touchProfileUpdateZoneSettings", {
              sessionId: draft.sessionId,
              componentId,
              zoneId: nextZoneId,
              settings,
            });
            if (
              index === zoneIds.length - 1 &&
              options?.refreshPreview &&
              previewRefreshRequest === previewRefreshRequestRef.current
            ) {
              setPreviewReloadVersion((version) => version + 1);
            }
          })
          .catch((error) => {
            toast.error(t("page.tools.touch_profile.toast.settings_save_failed"), {
              description: toErrorMessage(error),
            });
          })
          .finally(() => {
            setPendingSettingsSaves((count) => Math.max(0, count - 1));
          }),
      settingsSaveQueueRef.current,
    );
  };

  const setComponentLinked = (componentId: string, linked: boolean) => {
    setLinkedComponents((current) => ({ ...current, [componentId]: linked }));
    if (!linked || !draft) return;
    const component = draft.components.find((item) => item.componentId === componentId);
    const sourceZone = component?.zones[0];
    if (!component || !sourceZone || component.zones.length < 2) return;
    updateZoneSettings(componentId, sourceZone.id, sourceZone.settings, {
      refreshPreview: true,
      zoneIds: component.zones.map((zone) => zone.id),
    });
  };

  const updateZoneAdvancedSetting = <K extends keyof TouchProfileAdvancedSettings>(
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    key: K,
    value: number,
  ) => {
    updateZoneSettings(componentId, zone.id, {
      ...zone.settings,
      physicsPreset: "custom",
      advanced: { ...zone.settings.advanced, [key]: value },
    });
  };

  const updateZoneMaskStrength = (
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    value: number,
    options?: { zoneIds?: string[] },
  ) => {
    if (!Number.isFinite(value)) return;
    updateZoneSettings(
      componentId,
      zone.id,
      {
        ...zone.settings,
        maskStrength: Math.max(
          TOUCH_PROFILE_MASK_STRENGTH_RANGE.min,
          Math.min(TOUCH_PROFILE_MASK_STRENGTH_RANGE.max, value),
        ),
      },
      { refreshPreview: true, ...options },
    );
  };

  const resetZoneMaskStrength = (
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    options?: { zoneIds?: string[] },
  ) => {
    updateZoneMaskStrength(componentId, zone, DEFAULT_MASK_STRENGTH, options);
  };

  const updateZoneMaskCurve = (
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    value: number,
    options?: { zoneIds?: string[] },
  ) => {
    if (!Number.isFinite(value)) return;
    updateZoneSettings(
      componentId,
      zone.id,
      {
        ...zone.settings,
        maskCurve: Math.max(
          TOUCH_PROFILE_MASK_CURVE_RANGE.min,
          Math.min(TOUCH_PROFILE_MASK_CURVE_RANGE.max, value),
        ),
      },
      { refreshPreview: true, ...options },
    );
  };

  const resetZoneMaskCurve = (
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    options?: { zoneIds?: string[] },
  ) => {
    updateZoneMaskCurve(componentId, zone, DEFAULT_MASK_CURVE, options);
  };

  const discardDraft = async () => {
    if (!draft && !inspection) return;
    if (draft) {
      await window.api.invoke("tools:touchProfileDiscardDraft", draft.sessionId);
    }
    draftSessionRef.current = null;
    setDraft(null);
    setInspection(null);
    setPhase("select");
    setProgress(null);
    setPreview(null);
    setPreviewError(null);
    setMeshPreview(null);
    setMeshPreviewError(null);
    setInputError(null);
    setSelectedComponentId("");
    setSelectedZoneId(ALL_ZONES);
  };

  const openResult = async () => {
    if (!result) return;
    await window.api.invoke("util:openPath", result.outputModRoot);
  };

  const rollbackResult = async () => {
    if (!result || rollingBack) return;
    setRollingBack(true);
    try {
      const next = await window.api.invoke("tools:touchProfileRollback", {
        sessionId: result.sessionId,
        outputModRoot: result.outputModRoot,
        sourceModRoot: result.sourceModRoot,
        reenableSourceOnRollback: result.reenableSourceOnRollback,
      });
      setResult(null);
      setProgress(null);
      setPreviewError(null);
      setPreviewReloadVersion((version) => version + 1);
      setModPath(next.sourceModRoot);
      setDraft((current) =>
        current ? { ...current, sourceModRoot: next.sourceModRoot } : current,
      );
      toast.success(t("page.tools.touch_profile.toast.rolled_back"), {
        description: next.sourceModRoot,
      });
      onRolledBack?.(next.sourceModRoot);
    } catch (error) {
      toast.error(t("page.tools.touch_profile.toast.rollback_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setRollingBack(false);
    }
  };

  const rotateModel = (delta: [number, number, number]) => {
    setModelOrientation((current) => {
      const [roll, pitch, yaw] = parseOrientation(current);
      return formatOrientation([roll + delta[0], pitch + delta[1], yaw + delta[2]]);
    });
  };

  const resetView = () => {
    setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    viewportRef.current?.resetCamera();
  };

  const emptyMessage = loading
    ? t("page.tools.touch_profile.analyzing")
    : phase === "select"
      ? t("page.tools.touch_profile.mesh_select_hint")
      : previewLoading
        ? t("page.tools.touch_profile.preview_loading")
        : t("page.tools.touch_profile.preview_empty");

  const meshPreviewRegions = useMemo(
    () => [
      {
        id: "__mesh__",
        weights: new Float32Array(Math.floor((meshPreview?.positions.length ?? 0) / 3)),
        amount: 1,
        axisScale: [1, 1, 1] as [number, number, number],
        pivot: [0, 0, 0] as [number, number, number],
      },
    ],
    [meshPreview],
  );

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3">
      {(phase === "select" && meshPreview) || (phase === "review" && activePreview) ? (
        <ModelViewerMenuBar
          rotateModel={rotateModel}
          onResetView={resetView}
          doubleSidedEnabled={true}
          onDoubleSidedChange={() => {}}
          toneMapping="neutral"
          onToneMappingChange={() => {}}
          environment="studio"
          onEnvironmentChange={() => {}}
          exposure={DEFAULT_THREE_EXPOSURE}
          onExposureDraftChange={() => {}}
          onExposureCommit={() => {}}
          showToggleViewer={false}
          isViewerBusy={previewLoading || loading || meshPreviewLoading}
          onSaveTogglesToIni={() => {}}
          onResetToggles={() => {}}
          canSaveCapturedPreview={false}
          onCapturePreviewClick={() => {}}
          showTextureMenu={false}
          showRenderingMenu={false}
          showMiscMenu={false}
        />
      ) : null}

      <div className="grid min-h-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="relative min-h-80 overflow-hidden rounded-md border bg-muted/30">
          {phase === "select" && meshPreview ? (
            <BodyShapeViewport
              ref={viewportRef}
              originalPositions={meshPreview.positions}
              previewPositions={meshPreview.positions}
              regions={meshPreviewRegions}
              indices={meshPreview.indices}
              showOriginal={false}
              showWeights={false}
              weightVersion={0}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={meshPreview.sessionId}
            />
          ) : activePreview ? (
            <BodyShapeViewport
              ref={viewportRef}
              originalPositions={activePreview.positions}
              previewPositions={activePreview.positions}
              regions={previewRegions}
              indices={activePreview.indices}
              showOriginal={false}
              showWeights={true}
              weightVersion={previewReloadVersion}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={activePreview.sessionId}
            />
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {emptyMessage}
            </div>
          )}
          {loading || previewLoading || meshPreviewLoading ? (
            <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/20">
              <div className="inline-flex items-center gap-2 rounded-md border bg-background/90 px-3 py-2 text-sm shadow-sm">
                <Loader2Icon className="size-4 animate-spin" />
                {loading
                  ? t("page.tools.touch_profile.analyzing")
                  : meshPreviewLoading
                    ? t("page.tools.touch_profile.preview_loading")
                    : t("page.tools.touch_profile.preview_loading")}
              </div>
            </div>
          ) : null}
          {phase === "select" && meshPreviewError ? (
            <div className="absolute inset-x-0 bottom-0 p-2 text-center text-xs text-destructive">
              {meshPreviewError}
            </div>
          ) : null}
        </div>

        <div className="flex min-h-0 flex-col overflow-hidden rounded-md border bg-card/20">
          <div className="border-b px-4 py-3">
            <div className="text-sm font-medium">{t("page.tools.touch_profile.title")}</div>
            {modName ? (
              <div className="truncate text-xs text-muted-foreground">{modName}</div>
            ) : (
              <div className="mt-1 text-xs text-muted-foreground">
                {t("page.tools.touch_profile.description")}
              </div>
            )}
          </div>

          <ScrollArea className="min-h-0 flex-1">
            <div className="flex flex-col gap-4 p-4">
              {!isFixedTarget ? (
                <Field>
                  <FieldLabel>{t("page.tools.touch_profile.mod_path")}</FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      value={modPath}
                      onChange={(event) => setModPath(event.target.value)}
                      placeholder={t("page.tools.touch_profile.mod_path_placeholder")}
                    />
                    <Button type="button" variant="outline" size="icon" onClick={selectFolder}>
                      <FolderOpenIcon className="size-4" />
                    </Button>
                  </div>
                </Field>
              ) : null}

              {phase === "select" ? (
                <div className="flex space-x-2">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => void clearVisionCache()}
                    disabled={loading}
                  >
                    <RotateCcwIcon className="mr-2 size-4" />
                    {t("page.tools.touch_profile.vision_cache_clear_button")}
                  </Button>

                  <Button
                    type="button"
                    onClick={() => void loadMod()}
                    disabled={!modPath || loading || Boolean(result)}
                  >
                    {loading ? (
                      <Loader2Icon className="mr-2 size-4 animate-spin" />
                    ) : (
                      <SparklesIcon className="mr-2 size-4" />
                    )}
                    {loading
                      ? t("page.tools.touch_profile.analyzing")
                      : t("page.tools.touch_profile.analyze")}
                  </Button>
                </div>
              ) : (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void discardDraft()}
                  disabled={Boolean(result) || applying}
                >
                  {t("page.tools.touch_profile.discard")}
                </Button>
              )}

              {progress ? (
                <div className="rounded-md border border-border px-3 py-2 text-xs">
                  <div className="font-medium">
                    {t(`page.tools.touch_profile.stage.${progress.stage}`)}
                  </div>
                  <div className="mt-1 text-muted-foreground">{progress.message}</div>
                  <div className="mt-2 h-1.5 overflow-hidden rounded bg-muted">
                    <div
                      className="h-full bg-accent transition-all"
                      style={{ width: `${Math.round(progress.progress * 100)}%` }}
                    />
                  </div>
                </div>
              ) : null}

              {inputError ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                  <div className="font-medium">
                    {t(`page.tools.touch_profile.input_error.${inputError}.title`)}
                  </div>
                  <div className="mt-1">
                    {t(`page.tools.touch_profile.input_error.${inputError}.description`)}
                  </div>
                </div>
              ) : null}

              {phase === "select" && inspection ? (
                <>
                  <div>
                    <div className="text-sm font-medium">
                      {t("page.tools.touch_profile.mesh_select_title")}
                    </div>
                    <div className="mt-2 flex gap-3 text-xs text-muted-foreground">
                      <span>
                        {t("page.tools.touch_profile.support_grade")}: {inspection.supportGrade}
                      </span>
                      <span>
                        {t("page.tools.touch_profile.mesh_select_count", {
                          selected: selectedMeshIds.size,
                          total: inspection.components.length,
                        })}
                      </span>
                    </div>
                    <div className="mt-2 text-xs text-muted-foreground">
                      {t("page.tools.touch_profile.mesh_select_hint")}
                    </div>
                  </div>

                  <div className="flex gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 text-xs"
                      onClick={() => {
                        setSelectedMeshIds(
                          new Set(inspection.components.map((component) => component.id)),
                        );
                      }}
                    >
                      {t("page.tools.touch_profile.mesh_select_all")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 text-xs"
                      onClick={() => {
                        setSelectedMeshIds(
                          new Set(
                            inspection.components
                              .filter((component) => component.interactiveCandidate)
                              .map((component) => component.id),
                          ),
                        );
                      }}
                    >
                      {t("page.tools.touch_profile.mesh_select_candidates")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-7 text-xs"
                      onClick={() => setSelectedMeshIds(new Set())}
                    >
                      {t("page.tools.touch_profile.mesh_select_none")}
                    </Button>
                  </div>

                  <div className="space-y-2">
                    {inspection.components.map((component) => (
                      <div
                        key={component.id}
                        className={cn(
                          "w-full rounded-md border px-3 py-2 transition-colors",
                          selectedMeshId === component.id
                            ? "border-primary bg-primary/10"
                            : "border-border/60 hover:bg-muted/60",
                        )}
                      >
                        <div className="flex items-center gap-2">
                          <Checkbox
                            checked={selectedMeshIds.has(component.id)}
                            onCheckedChange={(checked) => {
                              setSelectedMeshIds((current) => {
                                const next = new Set(current);
                                if (checked) next.add(component.id);
                                else next.delete(component.id);
                                return next;
                              });
                            }}
                            aria-label={component.name}
                          />
                          <button
                            type="button"
                            className="min-w-0 flex-1 text-left"
                            onClick={() => {
                              setSelectedMeshId((current) =>
                                current === component.id ? "" : component.id,
                              );
                            }}
                          >
                            <div className="truncate text-sm font-medium">
                              {component.name}
                              {component.variantKey ? (
                                <span className="text-xs text-muted-foreground">
                                  {" "}
                                  (v{component.variantKey})
                                </span>
                              ) : null}
                            </div>
                            <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                              <Badge variant="outline">{component.kind}</Badge>
                              <Badge variant="outline">{component.supportGrade}</Badge>
                              <span>
                                {component.vertexCount.toLocaleString()} v ·{" "}
                                {component.indexCount.toLocaleString()} i
                              </span>
                              {!component.interactiveCandidate ? (
                                <Badge variant="secondary">
                                  {t("page.tools.touch_profile.mesh_non_candidate")}
                                </Badge>
                              ) : null}
                            </div>
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>

                  <Button
                    type="button"
                    onClick={() => void analyzeSelected()}
                    disabled={loading || selectedMeshIds.size === 0}
                  >
                    {loading ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
                    {loading
                      ? t("page.tools.touch_profile.analyzing")
                      : t("page.tools.touch_profile.mesh_next")}
                  </Button>
                </>
              ) : null}

              {draft ? (
                <>
                  <div>
                    <div className="text-sm font-medium">
                      {t("page.tools.touch_profile.preview_title")}
                    </div>
                    <div className="mt-2 flex gap-3 text-xs text-muted-foreground">
                      <span>
                        {t("page.tools.touch_profile.support_grade")}: {draft.analysis.supportGrade}
                      </span>
                      <span>
                        {t("page.tools.touch_profile.auto_apply")}:{" "}
                        {draft.canAutoApply
                          ? t("page.tools.touch_profile.yes")
                          : t("page.tools.touch_profile.no")}
                      </span>
                    </div>
                    {draft.llm ? (
                      <div
                        className="mt-2 truncate text-xs text-muted-foreground"
                        title={draft.llm.endpoint}
                      >
                        {t("page.tools.touch_profile.llm_summary")}: {draft.llm.protocol} /{" "}
                        {draft.llm.model}
                      </div>
                    ) : null}
                  </div>

                  <Field>
                    <FieldLabel>{t("page.tools.touch_profile.component")}</FieldLabel>
                    <Select
                      value={selectedComponentId}
                      onValueChange={(value) => {
                        if (value) setSelectedComponentId(value);
                      }}
                      disabled={interactiveComponents.length === 0 || previewLoading}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue
                          placeholder={t("page.tools.touch_profile.component_placeholder")}
                        />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {interactiveComponents.map((component) => (
                            <SelectItem key={component.componentId} value={component.componentId}>
                              {component.componentId} ({component.zones.length})
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>

                  <Field>
                    <FieldLabel>{t("page.tools.touch_profile.zone")}</FieldLabel>
                    <Select
                      value={selectedZoneId}
                      onValueChange={(value) => {
                        if (value) setSelectedZoneId(value);
                      }}
                      disabled={!activePreview || previewLoading}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder={t("page.tools.touch_profile.zone_placeholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value={ALL_ZONES}>
                            {t("page.tools.touch_profile.all_zones")}
                          </SelectItem>
                          {(activePreview?.zones ?? []).map((zone) => (
                            <SelectItem key={zone.id} value={zone.id}>
                              {zone.label || zone.id}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>

                  {selectedComponent ? (
                    <div className="space-y-2 rounded-md border border-border/80 p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="text-sm font-medium">{selectedComponent.componentId}</div>
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">
                            {t("page.tools.touch_profile.confidence")}{" "}
                            {(selectedComponent.confidence * 100).toFixed(0)}%
                          </Badge>
                          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                            <span>{t("page.tools.touch_profile.link_settings")}</span>
                            <Switch
                              size="sm"
                              checked={linkedComponents[selectedComponent.componentId] ?? true}
                              onCheckedChange={(checked) =>
                                setComponentLinked(selectedComponent.componentId, checked)
                              }
                              aria-label={t("page.tools.touch_profile.link_settings")}
                            />
                          </div>
                        </div>
                      </div>

                      <div className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/40 p-2 text-xs">
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-foreground">
                            {t("page.tools.touch_profile.turn_label")}: Turn{" "}
                            {selectedComponent.currentTurn ?? 1}
                          </span>
                          {selectedComponent.turnHistory &&
                          selectedComponent.turnHistory.length > 1 ? (
                            <Select
                              value={String(selectedComponent.currentTurn ?? 1)}
                              onValueChange={(val) => {
                                if (val)
                                  void selectTurn(selectedComponent.componentId, Number(val));
                              }}
                              disabled={reanalyzing || loading}
                            >
                              <SelectTrigger className="h-7 text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectGroup>
                                  {selectedComponent.turnHistory.map((item) => (
                                    <SelectItem key={item.turn} value={String(item.turn)}>
                                      Turn {item.turn} {item.approved ? " (LGTM)" : ""}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                          ) : null}
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => void reanalyzeTurn(selectedComponent.componentId)}
                          disabled={reanalyzing || loading}
                        >
                          {reanalyzing ? (
                            <Loader2Icon className="mr-1.5 size-3.5 animate-spin" />
                          ) : (
                            <SparklesIcon className="mr-1.5 size-3.5" />
                          )}
                          {reanalyzing
                            ? t("page.tools.touch_profile.reanalyzing")
                            : t("page.tools.touch_profile.reanalyze")}
                        </Button>
                      </div>
                      {selectedComponent.zones
                        .filter(
                          (_, index) =>
                            !(linkedComponents[selectedComponent.componentId] ?? true) ||
                            index === 0,
                        )
                        .map((zone) => {
                          const source = sourceLabel(zone.source, t);
                          const isSelected = selectedZoneId === zone.id;
                          const isLinked = linkedComponents[selectedComponent.componentId] ?? true;
                          const linkedZoneIds = isLinked
                            ? selectedComponent.zones.map((item) => item.id)
                            : undefined;
                          return (
                            <div
                              key={zone.id}
                              className={cn(
                                "w-full rounded-md border px-3 py-2 text-left transition-colors",
                                !isLinked && isSelected
                                  ? "border-primary bg-primary/10"
                                  : "border-border/60 hover:bg-muted/60",
                              )}
                            >
                              {isLinked ? (
                                <div className="text-xs text-muted-foreground">
                                  {selectedComponent.zones
                                    .map((item) => item.label || item.id)
                                    .join(" · ")}
                                </div>
                              ) : (
                                <button
                                  type="button"
                                  className="w-full text-left"
                                  onClick={() =>
                                    setSelectedZoneId(isSelected ? ALL_ZONES : zone.id)
                                  }
                                >
                                  <div className="flex items-center justify-between gap-2">
                                    <span className="text-sm font-medium">
                                      {zone.label || zone.id}
                                    </span>
                                    <Badge variant="outline">{source}</Badge>
                                  </div>
                                  <div className="mt-1 text-xs text-muted-foreground">
                                    {t("page.tools.touch_profile.confidence")}{" "}
                                    {(zone.confidence * 100).toFixed(0)}%
                                  </div>
                                </button>
                              )}
                              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                                <Field>
                                  <FieldLabel>
                                    {t("page.tools.touch_profile.touch_strength")}
                                  </FieldLabel>
                                  <Select
                                    value={zone.settings.strengthPreset}
                                    onValueChange={(value) => {
                                      if (
                                        typeof value === "string" &&
                                        isTouchZoneStrengthPreset(value)
                                      ) {
                                        updateZoneSettings(
                                          selectedComponent.componentId,
                                          zone.id,
                                          {
                                            ...zone.settings,
                                            strengthPreset: value,
                                          },
                                          { zoneIds: linkedZoneIds },
                                        );
                                      }
                                    }}
                                  >
                                    <SelectTrigger className="w-full">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectGroup>
                                        <SelectItem value="light">
                                          {t("page.tools.touch_profile.strength_light")}
                                        </SelectItem>
                                        <SelectItem value="normal">
                                          {t("page.tools.touch_profile.strength_normal")}
                                        </SelectItem>
                                        <SelectItem value="strong">
                                          {t("page.tools.touch_profile.strength_strong")}
                                        </SelectItem>
                                      </SelectGroup>
                                    </SelectContent>
                                  </Select>
                                </Field>
                                <Field>
                                  <FieldLabel>
                                    {t("page.tools.touch_profile.physics_preset")}
                                  </FieldLabel>
                                  <Select
                                    value={zone.settings.physicsPreset}
                                    onValueChange={(value) => {
                                      if (
                                        typeof value === "string" &&
                                        isTouchPhysicsPreset(value)
                                      ) {
                                        updateZoneSettings(
                                          selectedComponent.componentId,
                                          zone.id,
                                          createPresetSettings(
                                            value,
                                            zone.settings.strengthPreset,
                                            zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH,
                                            zone.settings.maskCurve ?? DEFAULT_MASK_CURVE,
                                          ),
                                          { zoneIds: linkedZoneIds },
                                        );
                                      }
                                    }}
                                  >
                                    <SelectTrigger className="w-full">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectGroup>
                                        <SelectItem value="soft">
                                          {t("page.tools.touch_profile.physics_soft")}
                                        </SelectItem>
                                        <SelectItem value="normal">
                                          {t("page.tools.touch_profile.physics_normal")}
                                        </SelectItem>
                                        <SelectItem value="firm">
                                          {t("page.tools.touch_profile.physics_firm")}
                                        </SelectItem>
                                        {zone.settings.physicsPreset === "custom" ? (
                                          <SelectItem value="custom">
                                            {t("page.tools.touch_profile.physics_custom")}
                                          </SelectItem>
                                        ) : null}
                                      </SelectGroup>
                                    </SelectContent>
                                  </Select>
                                </Field>
                              </div>
                              <Field className="mt-3">
                                <div className="flex items-center justify-between gap-2">
                                  <FieldLabel>
                                    {t("page.tools.touch_profile.mask_strength")}
                                  </FieldLabel>
                                  <div className="flex items-center gap-1">
                                    <span className="text-xs text-muted-foreground tabular-nums">
                                      {Math.round(
                                        (zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH) * 100,
                                      )}
                                      %
                                    </span>
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      className="size-6"
                                      title={t("page.tools.touch_profile.mask_strength_reset")}
                                      aria-label={t("page.tools.touch_profile.mask_strength_reset")}
                                      onClick={() =>
                                        resetZoneMaskStrength(selectedComponent.componentId, zone, {
                                          zoneIds: linkedZoneIds,
                                        })
                                      }
                                    >
                                      <RotateCcwIcon className="size-3.5" />
                                    </Button>
                                  </div>
                                </div>
                                <Slider
                                  min={TOUCH_PROFILE_MASK_STRENGTH_RANGE.min}
                                  max={TOUCH_PROFILE_MASK_STRENGTH_RANGE.max}
                                  step={TOUCH_PROFILE_MASK_STRENGTH_RANGE.step}
                                  value={zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH}
                                  onValueChange={(next) => {
                                    const nextValue = Array.isArray(next) ? next[0] : next;
                                    if (typeof nextValue === "number") {
                                      updateZoneMaskStrength(
                                        selectedComponent.componentId,
                                        zone,
                                        nextValue,
                                        { zoneIds: linkedZoneIds },
                                      );
                                    }
                                  }}
                                />
                              </Field>
                              <Field className="mt-3">
                                <div className="flex items-center justify-between gap-2">
                                  <FieldLabel>
                                    {t("page.tools.touch_profile.mask_curve")}
                                  </FieldLabel>
                                  <div className="flex items-center gap-1">
                                    <span className="text-xs text-muted-foreground tabular-nums">
                                      {formatTouchSettingValue(
                                        zone.settings.maskCurve ?? DEFAULT_MASK_CURVE,
                                      )}
                                      x
                                    </span>
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      className="size-6"
                                      title={t("page.tools.touch_profile.mask_curve_reset")}
                                      aria-label={t("page.tools.touch_profile.mask_curve_reset")}
                                      onClick={() =>
                                        resetZoneMaskCurve(selectedComponent.componentId, zone, {
                                          zoneIds: linkedZoneIds,
                                        })
                                      }
                                    >
                                      <RotateCcwIcon className="size-3.5" />
                                    </Button>
                                  </div>
                                </div>
                                <div className="mb-2 text-xs text-muted-foreground">
                                  {t("page.tools.touch_profile.mask_curve_hint")}
                                </div>
                                <Slider
                                  min={TOUCH_PROFILE_MASK_CURVE_RANGE.min}
                                  max={TOUCH_PROFILE_MASK_CURVE_RANGE.max}
                                  step={TOUCH_PROFILE_MASK_CURVE_RANGE.step}
                                  value={zone.settings.maskCurve ?? DEFAULT_MASK_CURVE}
                                  onValueChange={(next) => {
                                    const nextValue = Array.isArray(next) ? next[0] : next;
                                    if (typeof nextValue === "number") {
                                      updateZoneMaskCurve(
                                        selectedComponent.componentId,
                                        zone,
                                        nextValue,
                                        { zoneIds: linkedZoneIds },
                                      );
                                    }
                                  }}
                                />
                              </Field>
                            </div>
                          );
                        })}
                    </div>
                  ) : (
                    <div className="rounded-md border border-border/80 px-3 py-2 text-xs text-muted-foreground">
                      {t("page.tools.touch_profile.no_interactive_components")}
                    </div>
                  )}

                  {selectedZone &&
                  selectedZoneId !== ALL_ZONES &&
                  selectedComponent &&
                  !(linkedComponents[selectedComponent.componentId] ?? true) ? (
                    <div className="rounded-md border border-border/80 p-3">
                      <div className="text-sm font-medium">
                        {t("page.tools.touch_profile.advanced_title")}: {selectedZone.label}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {t("page.tools.touch_profile.advanced_description")}
                      </div>
                      <div className="mt-3 grid gap-4">
                        {advancedSettingEntries.map(([key, labelKey]) => {
                          const range = TOUCH_PROFILE_SETTING_RANGES[key];
                          const value = selectedZone.settings.advanced[key];
                          return (
                            <Field key={key}>
                              <div className="flex items-center justify-between gap-2">
                                <FieldLabel>{t(labelKey)}</FieldLabel>
                                <span className="text-xs text-muted-foreground tabular-nums">
                                  {formatTouchSettingValue(value)}
                                </span>
                              </div>
                              <Slider
                                min={range.min}
                                max={range.max}
                                step={range.step}
                                value={value}
                                onValueChange={(next) => {
                                  const nextValue = Array.isArray(next) ? next[0] : next;
                                  if (typeof nextValue === "number") {
                                    updateZoneAdvancedSetting(
                                      selectedComponent.componentId,
                                      selectedZone,
                                      key,
                                      nextValue,
                                    );
                                  }
                                }}
                              />
                            </Field>
                          );
                        })}
                      </div>
                    </div>
                  ) : null}

                  {previewError ? (
                    <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                      {t("page.tools.touch_profile.preview_failed")}: {previewError}
                    </div>
                  ) : null}

                  {selectedComponent?.warnings.length ? (
                    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
                      {selectedComponent.warnings.join(" · ")}
                    </div>
                  ) : null}

                  {draft.warnings.length > 0 ? (
                    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
                      {draft.warnings.join(" · ")}
                    </div>
                  ) : null}

                  <Button
                    type="button"
                    variant="outline"
                    onClick={backToSelect}
                    disabled={applying}
                  >
                    {t("page.tools.touch_profile.mesh_back")}
                  </Button>

                  <Button
                    type="button"
                    onClick={() => void applyDraft(!draft.canAutoApply)}
                    disabled={
                      applying || pendingSettingsSaves > 0 || interactiveComponents.length === 0
                    }
                  >
                    {applying ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
                    {result
                      ? t("page.tools.touch_profile.regenerate")
                      : draft.canAutoApply
                        ? t("page.tools.touch_profile.create")
                        : t("page.tools.touch_profile.force_create")}
                  </Button>
                </>
              ) : null}

              {result ? (
                <div className="rounded-md border border-border px-3 py-2 text-xs">
                  <div className="font-medium">{t("page.tools.touch_profile.result")}</div>
                  <div className="mt-1 break-all text-muted-foreground">{result.outputModRoot}</div>
                  <div className="mt-2 text-muted-foreground">
                    {t("page.tools.touch_profile.source")}:{" "}
                    <span className="break-all">{result.sourceModRoot}</span>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => void openResult()}
                      disabled={rollingBack}
                    >
                      {t("page.tools.touch_profile.open_folder")}
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() => void rollbackResult()}
                      disabled={rollingBack}
                    >
                      {rollingBack ? (
                        <Loader2Icon className="mr-2 size-4 animate-spin" />
                      ) : (
                        <Undo2Icon className="mr-2 size-4" />
                      )}
                      {rollingBack
                        ? t("page.tools.touch_profile.rolling_back")
                        : t("page.tools.touch_profile.rollback")}
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
          </ScrollArea>
        </div>
      </div>
    </div>
  );
}

function sourceLabel(source: "vision" | "manual", t: (key: string) => string) {
  if (source === "vision") return t("page.tools.touch_profile.vision_source");
  return t("page.tools.touch_profile.manual_source");
}

const advancedSettingEntries = [
  ["radius", "page.tools.touch_profile.advanced_radius"],
  ["strength", "page.tools.touch_profile.advanced_strength"],
  ["damping", "page.tools.touch_profile.advanced_damping"],
  ["spring", "page.tools.touch_profile.advanced_spring"],
  ["maxOffset", "page.tools.touch_profile.advanced_max_offset"],
  ["falloff", "page.tools.touch_profile.advanced_falloff"],
] as const satisfies ReadonlyArray<[keyof TouchProfileAdvancedSettings, string]>;

function isTouchZoneStrengthPreset(value: string): value is TouchZoneStrengthPreset {
  return value === "light" || value === "normal" || value === "strong";
}

function isTouchPhysicsPreset(value: string): value is Exclude<TouchPhysicsPreset, "custom"> {
  return value === "soft" || value === "normal" || value === "firm";
}

function createPresetSettings(
  preset: Exclude<TouchPhysicsPreset, "custom">,
  strengthPreset: TouchZoneStrengthPreset,
  maskStrength: number,
  maskCurve: number,
): TouchZoneSettings {
  return {
    maskStrength,
    maskCurve,
    strengthPreset,
    physicsPreset: preset,
    advanced: { ...TOUCH_PHYSICS_PRESETS[preset] },
  };
}

function formatTouchSettingValue(value: number) {
  return Number.isInteger(value)
    ? String(value)
    : value.toFixed(3).replace(/0+$/, "").replace(/\.$/, "");
}

function getTouchProfileInputError(message: string): TouchProfileInputError | null {
  if (message.includes("TOUCH_PROFILE_INPUT_ALREADY_TOUCH")) return "already_touch";
  if (message.includes("TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH")) return "suspected_touch";
  return null;
}

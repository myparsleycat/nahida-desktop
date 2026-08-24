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
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
} from "@renderer/components/ui/combobox";
import { Field, FieldDescription, FieldLabel } from "@renderer/components/ui/field";
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
import {
  computeBoundingCenter,
  computeRegionPivot,
  extractBoneWeights,
  type ActiveRegionDeform,
} from "@shared/body-shape";
import type {
  TouchBoneZoneSelection,
  TouchProfileComponentSummary,
  TouchProfileMeshPreview,
  TouchProfilePreview,
} from "@shared/touch-profile-preview";
import {
  TOUCH_PHYSICS_PRESETS,
  TOUCH_PROFILE_MASK_CURVE_RANGE,
  TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE,
  TOUCH_PROFILE_MASK_STRENGTH_RANGE,
  TOUCH_PROFILE_SETTING_RANGES,
  type TouchMaskCoreAttenuation,
  type TouchPhysicsPreset,
  type TouchProfileAdvancedSettings,
  type TouchZoneSettings,
  type TouchZoneStrengthPreset,
} from "@shared/touch-profile-settings";
// vision-llm disabled — progress event type no longer used
// import type { TouchProfileProgressEvent } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import {
  FolderOpenIcon,
  Loader2Icon,
  Maximize2Icon,
  Minimize2Icon,
  RotateCcwIcon,
  Undo2Icon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const ALL_ZONES = "__all__";
const DEFAULT_MASK_STRENGTH = 1;
const DEFAULT_MASK_CURVE = 1;
const DEFAULT_MASK_RADIUS_SCALE = 1;
const DEFAULT_MASK_CORE_ATTENUATION = "off" as const;
const BONE_WEIGHT_THRESHOLD_RANGE = { min: 0, max: 1, step: 0.005 } as const;
const DEFAULT_BONE_WEIGHT_THRESHOLD = 0.01;
const DEFAULT_BONE_WEIGHT_THRESHOLD_MAX = 1;
const TOUCH_ZONE_CHANNEL_COUNT = 12;
const CHANNEL_LABELS = [
  "L Breast",
  "R Breast",
  "L Butt/Thigh",
  "R Butt/Thigh",
  ...Array.from({ length: TOUCH_ZONE_CHANNEL_COUNT - 4 }, (_, i) => `Ch ${i + 4}`),
] as const;

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
    hasBlend: boolean;
    bones: Array<{ id: number; vertexCount: number }>;
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
  const bonePreviewRef = useRef<string | null>(null);

  const [modPath, setModPath] = useState(fixedTargetPath ?? "");
  const [loading, setLoading] = useState(false);
  // vision-llm disabled — reanalyzing state isolated (vision reanalyze only)
  // const [reanalyzing, setReanalyzing] = useState(false);
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
  const [result, setResult] = useState<TouchApplyResultState | null>(null);
  const [inputError, setInputError] = useState<TouchProfileInputError | null>(null);
  // vision-llm disabled — analysisMode fixed to bone
  const analysisMode = "bone" as const;
  const [weightThreshold, setWeightThreshold] = useState<[number, number]>([
    DEFAULT_BONE_WEIGHT_THRESHOLD,
    DEFAULT_BONE_WEIGHT_THRESHOLD_MAX,
  ]);
  const [boneZoneAssignments, setBoneZoneAssignments] = useState<
    Record<string, TouchBoneZoneSelection[]>
  >({});

  const [selectedComponentId, setSelectedComponentId] = useState("");
  const [selectedZoneId, setSelectedZoneId] = useState(ALL_ZONES);
  const [linkedComponents, setLinkedComponents] = useState<Record<string, boolean>>({});
  const [preview, setPreview] = useState<TouchProfilePreview | null>(null);
  const [lastValidPreview, setLastValidPreview] = useState<TouchProfilePreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewReloadVersion, setPreviewReloadVersion] = useState(0);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const [pendingSettingsSaves, setPendingSettingsSaves] = useState(0);
  const settingsSaveQueueRef = useRef(Promise.resolve());
  const previewRefreshRequestRef = useRef(0);

  // vision-llm disabled — reanalyzeTurn (vision-only) isolated
  // const reanalyzeTurn = async (componentId: string) => {
  //   if (!draft || reanalyzing) return;
  //   setReanalyzing(true);
  //   try {
  //     const next = await window.api.invoke("tools:touchProfileReanalyzeTurn", {
  //       sessionId: draft.sessionId,
  //       componentId,
  //     });
  //     setDraft(next as unknown as TouchDraft);
  //     setPreviewReloadVersion((v) => v + 1);
  //     const updatedComponent = next.components.find((c) => c.componentId === componentId);
  //     toast.success(
  //       t("page.tools.touch_profile.toast.reanalyzed", {
  //         turn: updatedComponent?.currentTurn ?? 1,
  //       }),
  //     );
  //   } catch (error) {
  //     toast.error(t("page.tools.touch_profile.toast.reanalyze_failed"), {
  //       description: toErrorMessage(error),
  //     });
  //   } finally {
  //     setReanalyzing(false);
  //   }
  // };

  // vision-llm disabled — selectTurn (vision turn history) isolated
  // const selectTurn = async (componentId: string, turn: number) => {
  //   if (!draft) return;
  //   try {
  //     const next = await window.api.invoke("tools:touchProfileSelectTurn", {
  //       sessionId: draft.sessionId,
  //       componentId,
  //       turn,
  //     });
  //     setDraft(next as unknown as TouchDraft);
  //     setPreviewReloadVersion((v) => v + 1);
  //     toast.success(t("page.tools.touch_profile.toast.turn_selected", { turn }));
  //   } catch (error) {
  //     toast.error(t("page.tools.touch_profile.toast.turn_select_failed"), {
  //       description: toErrorMessage(error),
  //     });
  //   }
  // };

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
  const activeComponentId =
    interactiveComponents.find((component) => component.componentId === selectedComponentId)
      ?.componentId ??
    interactiveComponents[0]?.componentId ??
    "";
  const selectedComponent = interactiveComponents.find(
    (component) => component.componentId === activeComponentId,
  );
  const activeZoneId =
    selectedZoneId === ALL_ZONES ||
    selectedComponent?.zones.some((zone) => zone.id === selectedZoneId)
      ? selectedZoneId
      : ALL_ZONES;
  const selectedZone = selectedComponent?.zones.find((zone) => zone.id === activeZoneId);
  const activePreview = preview?.componentId === activeComponentId ? preview : null;
  // While a new component's preview is loading, keep the previous component's
  // mesh on the Canvas instead of unmounting/remounting the WebGL context (which
  // forces shader recompilation and stalls the renderer main thread). Falls back
  // to the last successful preview; a stale-component overlay is shown via
  // previewLoading so the user knows the displayed mesh is not the current one.
  const displayPreview = activePreview ?? (previewLoading ? lastValidPreview : null);

  const loadMod = async (pathOverride?: string) => {
    const targetPath = pathOverride ?? modPath;
    if (!targetPath || loading) return;
    if (pathOverride) setModPath(pathOverride);
    setLoading(true);
    setDraft(null);
    setResult(null);
    setInputError(null);
    setPreview(null);
    setLastValidPreview(null);
    setPreviewError(null);
    setMeshPreview(null);
    setMeshPreviewError(null);
    setSelectedComponentId("");
    setSelectedZoneId(ALL_ZONES);
    setLinkedComponents({});
    setBoneZoneAssignments({});
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
    queueMicrotask(() => {
      void loadMod(fixedTargetPath);
    });
  }, [fixedTargetPath]);

  useEffect(() => {
    let cancelled = false;
    if (!draft?.sessionId || !activeComponentId) {
      return;
    }

    const fetchPreview = async () => {
      setPreviewError(null);
      setPreviewLoading(true);
      try {
        const next = await window.api.invoke("tools:touchProfileGetPreview", {
          sessionId: draft.sessionId,
          componentId: activeComponentId,
        });
        if (cancelled) return;
        setPreview(next);
        setLastValidPreview(next);
      } catch (error) {
        if (cancelled) return;
        setPreviewError(toErrorMessage(error));
      } finally {
        if (!cancelled) setPreviewLoading(false);
      }
    };

    void fetchPreview();

    return () => {
      cancelled = true;
    };
  }, [draft?.sessionId, activeComponentId, previewReloadVersion]);

  useEffect(() => {
    let cancelled = false;
    if (phase !== "select" || !inspection?.sessionId || !selectedMeshComponentId) {
      return;
    }

    const fetchMeshPreview = async () => {
      setMeshPreviewError(null);
      setMeshPreviewLoading(true);
      try {
        const next = await window.api.invoke("tools:touchProfileGetMeshPreview", {
          sessionId: inspection.sessionId,
          componentId: selectedMeshComponentId,
        });
        if (cancelled) return;
        setMeshPreview(next);
      } catch (error) {
        if (cancelled) return;
        setMeshPreviewError(toErrorMessage(error));
      } finally {
        if (!cancelled) setMeshPreviewLoading(false);
      }
    };

    void fetchMeshPreview();

    return () => {
      cancelled = true;
    };
  }, [phase, inspection?.sessionId, selectedMeshComponentId]);

  const visibleZones = useMemo(() => {
    if (!activePreview) return [];
    if (activeZoneId === ALL_ZONES) return activePreview.zones;
    return activePreview.zones.filter((zone) => zone.id === activeZoneId);
  }, [activePreview, activeZoneId]);

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

  const analyzeSelected = async () => {
    if (!inspection || inspection.components.length === 0) return;
    if (selectedMeshIds.size === 0) {
      toast.error(t("page.tools.touch_profile.toast.no_components_selected"));
      return;
    }
    setLoading(true);
    setInputError(null);
    try {
      const boneSelections =
        analysisMode === "bone"
          ? Object.entries(boneZoneAssignments)
              .filter(([, zones]) => zones.length > 0)
              .map(([componentId, zones]) => ({ componentId, zones }))
          : undefined;
      const next = await window.api.invoke("tools:touchProfileAnalyzeComponents", {
        sessionId: inspection.sessionId,
        componentIds: [...selectedMeshIds],
        mode: analysisMode,
        boneSelections,
        weightThreshold: analysisMode === "bone" ? weightThreshold : undefined,
      });
      setDraft(next as unknown as TouchDraft);
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
    setPreview(null);
    setLastValidPreview(null);
    setPreviewError(null);
    setSelectedComponentId("");
    setSelectedZoneId(ALL_ZONES);
    setBoneZoneAssignments({});
    bonePreviewRef.current = null;
    viewportRef.current?.updateColors([]);
  };

  const handleBoneHighlight = (boneId: number | null) => {
    bonePreviewRef.current = boneId !== null ? `bone:${boneId}` : null;
    if (!meshPreview || !meshPreview.blendBytes || meshPreview.blendStride === undefined) {
      viewportRef.current?.updateColors([]);
      return;
    }
    if (boneId === null) {
      viewportRef.current?.updateColors([]);
      return;
    }
    const weights = extractBoneWeights(
      meshPreview.blendBytes,
      boneId,
      meshPreview.vertexCount,
      meshPreview.blendStride,
    );
    for (let i = 0; i < weights.length; i++) {
      if (weights[i] < weightThreshold[0] || weights[i] > weightThreshold[1]) weights[i] = 0;
    }
    const boundsCenter = computeBoundingCenter(meshPreview.positions);
    const regions: ActiveRegionDeform[] = [
      {
        id: `bone:${boneId}`,
        weights,
        amount: 1,
        axisScale: [1, 1, 1],
        pivot: computeRegionPivot(meshPreview.positions, weights, boundsCenter),
      },
    ];
    viewportRef.current?.updateColors(regions);
  };

  // vision-llm disabled — clearVisionCache isolated
  // const clearVisionCache = async () => {
  //   if (loading) return;
  //   try {
  //     await window.api.invoke("tools:touchProfileClearVisionCache");
  //     toast.success(t("page.tools.touch_profile.toast.vision_cache_cleared"));
  //   } catch (error) {
  //     toast.error(t("page.tools.touch_profile.toast.vision_cache_clear_failed"), {
  //       description: toErrorMessage(error),
  //     });
  //   }
  // };

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

  const updateZoneMaskRadiusScale = (
    componentId: string,
    zone: TouchProfileComponentSummary["zones"][number],
    delta: number,
    options?: { zoneIds?: string[] },
  ) => {
    if (!Number.isFinite(delta)) return;
    const current = zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE;
    const next = Math.max(
      TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.min,
      Math.min(
        TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.max,
        Math.round((current + delta) * 100) / 100,
      ),
    );
    if (next === current) return;
    updateZoneSettings(
      componentId,
      zone.id,
      { ...zone.settings, maskRadiusScale: next },
      { refreshPreview: true, ...options },
    );
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
    setPreview(null);
    setLastValidPreview(null);
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
              showWeights
              weightVersion={0}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={meshPreview.sessionId}
            />
          ) : displayPreview ? (
            <BodyShapeViewport
              ref={viewportRef}
              originalPositions={displayPreview.positions}
              previewPositions={displayPreview.positions}
              regions={previewRegions}
              indices={displayPreview.indices}
              showOriginal={false}
              showWeights={true}
              weightVersion={previewReloadVersion}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={displayPreview.sessionId}
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
            <div className="text-sm font-medium">
              {t("page.tools.touch_profile.title")} ({t("g.beta")})
            </div>
            {modName ? (
              <div className="truncate text-xs text-muted-foreground">{modName}</div>
            ) : null}
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

              {phase === "select" ? null : (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void discardDraft()}
                  disabled={Boolean(result) || applying}
                >
                  {t("page.tools.touch_profile.discard")}
                </Button>
              )}

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

                  {analysisMode === "bone" ? (
                    <div className="space-y-3 rounded-md border border-border/60 p-3">
                      <div>
                        <div className="text-sm font-medium">
                          {t("page.tools.touch_profile.bone_select_title")}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {t("page.tools.touch_profile.bone_select_hint")}
                        </div>
                      </div>
                      <div>
                        <FieldLabel>{t("page.tools.touch_profile.weight_threshold")}</FieldLabel>
                        <div className="mt-1 flex items-center gap-3">
                          <Slider
                            value={weightThreshold}
                            min={BONE_WEIGHT_THRESHOLD_RANGE.min}
                            max={BONE_WEIGHT_THRESHOLD_RANGE.max}
                            step={BONE_WEIGHT_THRESHOLD_RANGE.step}
                            onValueChange={(values) => setWeightThreshold([values[0], values[1]])}
                            className="flex-1"
                          />
                          <span className="w-20 text-right text-xs tabular-nums">
                            {weightThreshold[0].toFixed(3)} ~ {weightThreshold[1].toFixed(3)}
                          </span>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {t("page.tools.touch_profile.weight_threshold_hint")}
                        </div>
                      </div>
                      {inspection.components
                        .filter((c) => selectedMeshIds.has(c.id))
                        .map((component) => {
                          const assignments = boneZoneAssignments[component.id] ?? [];
                          const hasBlend = component.hasBlend;
                          const boneItems = component.bones.map((bone) => ({
                            key: `bone:${bone.id}`,
                            label: t("page.tools.touch_profile.bone_label", {
                              id: bone.id,
                              count: bone.vertexCount,
                            }),
                          }));
                          const selectedBoneItems = boneItems.filter((item) =>
                            assignments.some((a) => `bone:${a.boneId}` === item.key),
                          );
                          return (
                            <div key={component.id} className="space-y-2">
                              <div className="flex items-center gap-2 text-sm font-medium">
                                <span>{component.name}</span>
                                {!hasBlend ? (
                                  <Badge variant="destructive">
                                    {t("page.tools.touch_profile.no_blend")}
                                  </Badge>
                                ) : (
                                  <Badge variant="outline">
                                    {component.bones.length}{" "}
                                    {t("page.tools.touch_profile.bones_count")}
                                  </Badge>
                                )}
                              </div>
                              {hasBlend ? (
                                <>
                                  <Combobox
                                    multiple
                                    autoHighlight
                                    items={boneItems}
                                    value={selectedBoneItems}
                                    onValueChange={(next) => {
                                      const nextKeys = (next ?? []).map((item) => item.key);
                                      const nextAssignments = nextKeys.map((key) => {
                                        const boneId = Number(key.slice(5));
                                        const existing = assignments.find(
                                          (a) => a.boneId === boneId,
                                        );
                                        if (existing) return existing;
                                        return { boneId, channel: null };
                                      });
                                      setBoneZoneAssignments((current) => ({
                                        ...current,
                                        [component.id]: nextAssignments,
                                      }));
                                    }}
                                    onItemHighlighted={(item) => {
                                      const boneId = item ? Number(item.key.slice(5)) : null;
                                      handleBoneHighlight(boneId);
                                    }}
                                    onOpenChange={(open) => {
                                      if (!open) handleBoneHighlight(null);
                                    }}
                                    itemToStringLabel={(item) => item.label}
                                    isItemEqualToValue={(a, b) => a.key === b.key}
                                  >
                                    <ComboboxChips>
                                      <ComboboxValue>
                                        {(value: typeof boneItems) => (
                                          <>
                                            {value.map((item) => {
                                              const boneId = Number(item.key.slice(5));
                                              return (
                                                <ComboboxChip
                                                  key={item.key}
                                                  onMouseEnter={() => handleBoneHighlight(boneId)}
                                                  onMouseLeave={() => handleBoneHighlight(null)}
                                                >
                                                  {item.label}
                                                </ComboboxChip>
                                              );
                                            })}
                                            <ComboboxInput
                                              placeholder={
                                                value.length > 0
                                                  ? ""
                                                  : t(
                                                      "page.tools.touch_profile.bone_select_placeholder",
                                                    )
                                              }
                                            />
                                          </>
                                        )}
                                      </ComboboxValue>
                                    </ComboboxChips>
                                    <ComboboxContent>
                                      <ComboboxEmpty>
                                        {t("page.tools.touch_profile.no_bones")}
                                      </ComboboxEmpty>
                                      <ComboboxList>
                                        {(item: (typeof boneItems)[number]) => (
                                          <ComboboxItem key={item.key} value={item}>
                                            {item.label}
                                          </ComboboxItem>
                                        )}
                                      </ComboboxList>
                                    </ComboboxContent>
                                  </Combobox>

                                  {assignments.length > 0 ? (
                                    <div className="space-y-1.5">
                                      {assignments.map((assignment, ai) => {
                                        return (
                                          <div
                                            key={`${assignment.boneId}-${ai}`}
                                            className="flex items-center gap-2 rounded-md border border-border/40 px-2 py-1.5"
                                          >
                                            <span className="min-w-0 flex-1 truncate text-xs font-medium">
                                              {t("page.tools.touch_profile.bone_label", {
                                                id: assignment.boneId,
                                                count:
                                                  component.bones.find(
                                                    (b) => b.id === assignment.boneId,
                                                  )?.vertexCount ?? 0,
                                              })}
                                            </span>
                                            <Select
                                              value={
                                                assignment.channel === null
                                                  ? null
                                                  : String(assignment.channel)
                                              }
                                              items={Array.from(
                                                { length: TOUCH_ZONE_CHANNEL_COUNT },
                                                (_, ch) => ({
                                                  value: String(ch),
                                                  label: CHANNEL_LABELS[ch],
                                                }),
                                              )}
                                              onValueChange={(value) => {
                                                const channel =
                                                  value === null ? null : Number(value);
                                                setBoneZoneAssignments((current) => ({
                                                  ...current,
                                                  [component.id]: (current[component.id] ?? []).map(
                                                    (a, idx) =>
                                                      idx === ai ? { ...a, channel } : a,
                                                  ),
                                                }));
                                              }}
                                            >
                                              <SelectTrigger className="h-7 w-28 text-xs">
                                                <SelectValue
                                                  placeholder={t(
                                                    "page.tools.touch_profile.channel_select_placeholder",
                                                  )}
                                                />
                                              </SelectTrigger>
                                              <SelectContent>
                                                {Array.from(
                                                  { length: TOUCH_ZONE_CHANNEL_COUNT },
                                                  (_, ch) => {
                                                    const otherBone = assignments.find(
                                                      (a, idx) => idx !== ai && a.channel === ch,
                                                    );
                                                    return (
                                                      <SelectItem
                                                        key={ch}
                                                        value={String(ch)}
                                                        className="text-xs"
                                                      >
                                                        {CHANNEL_LABELS[ch]}
                                                        {otherBone
                                                          ? ` (${t(
                                                              "page.tools.touch_profile.bone_label_short",
                                                              { id: otherBone.boneId },
                                                            )})`
                                                          : ""}
                                                      </SelectItem>
                                                    );
                                                  },
                                                )}
                                              </SelectContent>
                                            </Select>
                                            <Input
                                              value={assignment.label ?? ""}
                                              placeholder={t(
                                                "page.tools.touch_profile.zone_label_placeholder",
                                              )}
                                              onChange={(e) => {
                                                const label = e.target.value;
                                                setBoneZoneAssignments((current) => ({
                                                  ...current,
                                                  [component.id]: (current[component.id] ?? []).map(
                                                    (a, idx) =>
                                                      idx === ai
                                                        ? { ...a, label: label || undefined }
                                                        : a,
                                                  ),
                                                }));
                                              }}
                                              className="h-7 w-28 text-xs"
                                            />
                                          </div>
                                        );
                                      })}
                                    </div>
                                  ) : null}
                                </>
                              ) : (
                                <div className="text-xs text-muted-foreground">
                                  {t("page.tools.touch_profile.no_blend_hint")}
                                </div>
                              )}
                            </div>
                          );
                        })}
                    </div>
                  ) : null}

                  <Button
                    type="button"
                    onClick={() => void analyzeSelected()}
                    disabled={
                      loading ||
                      selectedMeshIds.size === 0 ||
                      (analysisMode === "bone" &&
                        (![...selectedMeshIds].some(
                          (id) => (boneZoneAssignments[id] ?? []).length > 0,
                        ) ||
                          [...selectedMeshIds].some((id) =>
                            (boneZoneAssignments[id] ?? []).some((a) => a.channel === null),
                          )))
                    }
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
                    {/* vision-llm disabled — LLM summary isolated */}
                    {/* {draft.llm ? (
                      <div
                        className="mt-2 truncate text-xs text-muted-foreground"
                        title={draft.llm.endpoint}
                      >
                        {t("page.tools.touch_profile.llm_summary")}: {draft.llm.protocol} /{" "}
                        {draft.llm.model}
                      </div>
                    ) : null} */}
                  </div>

                  <Field>
                    <FieldLabel>{t("page.tools.touch_profile.component")}</FieldLabel>
                    <Select
                      value={activeComponentId}
                      items={interactiveComponents.map((component) => ({
                        value: component.componentId,
                        label: `${component.componentId} (${component.zones.length})`,
                      }))}
                      onValueChange={(value) => {
                        if (value) {
                          setSelectedComponentId(value);
                          setSelectedZoneId(ALL_ZONES);
                        }
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
                      value={activeZoneId}
                      items={[
                        { value: ALL_ZONES, label: t("page.tools.touch_profile.all_zones") },
                        ...(activePreview?.zones ?? []).map((zone) => ({
                          value: zone.id,
                          label: zone.label || zone.id,
                        })),
                      ]}
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
                          {/* vision-llm disabled — turn history select isolated */}
                          {/* {selectedComponent.turnHistory &&
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
                          ) : null} */}
                        </div>
                        {/* vision-llm disabled — reanalyze button isolated */}
                        {/* <Button
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
                        </Button> */}
                      </div>
                      {selectedComponent.zones
                        .filter(
                          (_, index) =>
                            !(linkedComponents[selectedComponent.componentId] ?? true) ||
                            index === 0,
                        )
                        .map((zone) => {
                          const source = sourceLabel(zone.source, t);
                          const isSelected = activeZoneId === zone.id;
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
                                    items={[
                                      {
                                        value: "light",
                                        label: t("page.tools.touch_profile.strength_light"),
                                      },
                                      {
                                        value: "normal",
                                        label: t("page.tools.touch_profile.strength_normal"),
                                      },
                                      {
                                        value: "strong",
                                        label: t("page.tools.touch_profile.strength_strong"),
                                      },
                                    ]}
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
                                    items={[
                                      {
                                        value: "soft",
                                        label: t("page.tools.touch_profile.physics_soft"),
                                      },
                                      {
                                        value: "normal",
                                        label: t("page.tools.touch_profile.physics_normal"),
                                      },
                                      {
                                        value: "firm",
                                        label: t("page.tools.touch_profile.physics_firm"),
                                      },
                                      ...(zone.settings.physicsPreset === "custom"
                                        ? [
                                            {
                                              value: "custom",
                                              label: t("page.tools.touch_profile.physics_custom"),
                                            },
                                          ]
                                        : []),
                                    ]}
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
                                            zone.settings.maskRadiusScale ??
                                              DEFAULT_MASK_RADIUS_SCALE,
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
                              <div className="mt-3">
                                <div className="mb-1.5 flex items-center justify-between gap-2">
                                  <span className="text-sm font-medium">
                                    {t("page.tools.touch_profile.mask_radius")}
                                  </span>
                                  <span className="text-xs text-muted-foreground tabular-nums">
                                    {Math.round(
                                      (zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE) *
                                        100,
                                    )}
                                    %
                                  </span>
                                </div>
                                <div className="grid grid-cols-2 gap-2">
                                  <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    className="h-8 text-xs"
                                    onClick={() =>
                                      updateZoneMaskRadiusScale(
                                        selectedComponent.componentId,
                                        zone,
                                        -TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.step,
                                        { zoneIds: linkedZoneIds },
                                      )
                                    }
                                    disabled={
                                      applying ||
                                      rollingBack ||
                                      (zone.settings.maskRadiusScale ??
                                        DEFAULT_MASK_RADIUS_SCALE) <=
                                        TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.min
                                    }
                                  >
                                    <Minimize2Icon className="mr-1.5 size-3.5" />
                                    {t("page.tools.touch_profile.mask_radius_shrink")}
                                  </Button>
                                  <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    className="h-8 text-xs"
                                    onClick={() =>
                                      updateZoneMaskRadiusScale(
                                        selectedComponent.componentId,
                                        zone,
                                        TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.step,
                                        { zoneIds: linkedZoneIds },
                                      )
                                    }
                                    disabled={
                                      applying ||
                                      rollingBack ||
                                      (zone.settings.maskRadiusScale ??
                                        DEFAULT_MASK_RADIUS_SCALE) >=
                                        TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.max
                                    }
                                  >
                                    <Maximize2Icon className="mr-1.5 size-3.5" />
                                    {t("page.tools.touch_profile.mask_radius_grow")}
                                  </Button>
                                </div>
                              </div>
                              <Field className="mt-3">
                                <FieldLabel>
                                  {t("page.tools.touch_profile.mask_core_attenuation")}
                                </FieldLabel>
                                <Select
                                  value={
                                    zone.settings.maskCoreAttenuation ??
                                    DEFAULT_MASK_CORE_ATTENUATION
                                  }
                                  items={[
                                    {
                                      value: "off",
                                      label: t(
                                        "page.tools.touch_profile.mask_core_attenuation_off",
                                      ),
                                    },
                                    {
                                      value: "linear",
                                      label: t(
                                        "page.tools.touch_profile.mask_core_attenuation_linear",
                                      ),
                                    },
                                    {
                                      value: "sqrt",
                                      label: t(
                                        "page.tools.touch_profile.mask_core_attenuation_sqrt",
                                      ),
                                    },
                                    {
                                      value: "pow",
                                      label: t(
                                        "page.tools.touch_profile.mask_core_attenuation_pow",
                                      ),
                                    },
                                  ]}
                                  onValueChange={(value) => {
                                    if (isTouchMaskCoreAttenuation(value)) {
                                      updateZoneSettings(
                                        selectedComponent.componentId,
                                        zone.id,
                                        { ...zone.settings, maskCoreAttenuation: value },
                                        { zoneIds: linkedZoneIds, refreshPreview: true },
                                      );
                                    }
                                  }}
                                >
                                  <SelectTrigger className="w-full">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectGroup>
                                      <SelectItem value="off">
                                        {t("page.tools.touch_profile.mask_core_attenuation_off")}
                                      </SelectItem>
                                      <SelectItem value="linear">
                                        {t("page.tools.touch_profile.mask_core_attenuation_linear")}
                                      </SelectItem>
                                      <SelectItem value="sqrt">
                                        {t("page.tools.touch_profile.mask_core_attenuation_sqrt")}
                                      </SelectItem>
                                      <SelectItem value="pow">
                                        {t("page.tools.touch_profile.mask_core_attenuation_pow")}
                                      </SelectItem>
                                    </SelectGroup>
                                  </SelectContent>
                                </Select>
                                <FieldDescription>
                                  {t("page.tools.touch_profile.mask_core_attenuation_hint")}
                                </FieldDescription>
                              </Field>
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
                  activeZoneId !== ALL_ZONES &&
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

function sourceLabel(source: "vision" | "manual" | "bone", t: (key: string) => string) {
  // vision-llm disabled — vision source branch kept for type completeness
  if (source === "vision") return t("page.tools.touch_profile.vision_source");
  if (source === "bone") return t("page.tools.touch_profile.mode_bone");
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

function isTouchMaskCoreAttenuation(value: string | null): value is TouchMaskCoreAttenuation {
  return value === "off" || value === "linear" || value === "sqrt" || value === "pow";
}

function createPresetSettings(
  preset: Exclude<TouchPhysicsPreset, "custom">,
  strengthPreset: TouchZoneStrengthPreset,
  maskStrength: number,
  maskCurve: number,
  maskRadiusScale: number,
): TouchZoneSettings {
  return {
    maskStrength,
    maskCurve,
    maskRadiusScale,
    maskCoreAttenuation: "off",
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

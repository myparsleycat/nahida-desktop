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
import { Button } from "@renderer/components/ui/button";
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
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import {
  applyMultiRegionDeform,
  BODY_REGION_IDS,
  composeDisplayWeights,
  computeBoundingCenter,
  computeRegionPivot,
  DEFAULT_BLEND_STRIDE,
  displacementMetrics,
  extractBoneWeights,
  generateRegionWeights,
  REGION_PRESETS,
  type ActiveRegionDeform,
  type BlendBoneInfo,
  type BodyRegionId,
} from "@shared/body-shape";
import { toErrorMessage } from "@shared/utils";
import { FolderOpenIcon, Loader2Icon, SaveIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ScrollArea } from "../../ui/scroll-area";

const DEFAULT_AXIS_SCALE: [number, number, number] = [1, 0.15, 1];

type ControlMode = "unified" | "perItem";

type WeightSource = { kind: "bone"; boneId: number } | { kind: "region"; regionId: BodyRegionId };

type DeformControl = {
  amount: number;
  axisScale: [number, number, number];
};

const DEFAULT_UNIFIED_CONTROL: DeformControl = {
  amount: 0,
  axisScale: [...DEFAULT_AXIS_SCALE],
};

type LoadedMesh = {
  id: string;
  name: string;
  positionPath: string;
  positionRelativePath: string;
  positionStride: number;
  vertexCount: number;
  originalPositions: Float32Array;
  previewPositions: Float32Array;
  indices?: Uint32Array;
  vectorPath?: string;
  vectorLayout?: "snorm8-tangent-normal" | null;
  blendBytes?: Uint8Array;
  blendStride: number;
  bones: BlendBoneInfo[];
  weightCache: Map<string, Float32Array>;
};

type LoadResult = {
  modRoot: string;
  iniPath: string;
  meshes: LoadedMesh[];
};

function toFloat32(value: unknown): Float32Array {
  if (value instanceof Float32Array) return value;
  if (ArrayBuffer.isView(value)) {
    return new Float32Array(value.buffer, value.byteOffset, value.byteLength / 4);
  }
  if (value instanceof ArrayBuffer) return new Float32Array(value);
  if (Array.isArray(value)) return Float32Array.from(value as number[]);
  throw new Error("Invalid positions payload from main process");
}

function toUint32(value: unknown): Uint32Array | undefined {
  if (value == null) return undefined;
  if (value instanceof Uint32Array) return value;
  if (ArrayBuffer.isView(value)) {
    return new Uint32Array(value.buffer, value.byteOffset, value.byteLength / 4);
  }
  if (value instanceof ArrayBuffer) return new Uint32Array(value);
  if (Array.isArray(value)) return Uint32Array.from(value as number[]);
  return undefined;
}

function toUint8(value: unknown): Uint8Array | undefined {
  if (value == null) return undefined;
  if (value instanceof Uint8Array) return value;
  if (ArrayBuffer.isView(value)) {
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  }
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (Array.isArray(value)) return Uint8Array.from(value as number[]);
  return undefined;
}

function weightKey(source: WeightSource): string {
  return source.kind === "bone" ? `bone:${source.boneId}` : `region:${source.regionId}`;
}

function parseWeightKey(key: string): WeightSource | null {
  if (key.startsWith("bone:")) {
    const boneId = Number(key.slice(5));
    if (!Number.isInteger(boneId) || boneId < 0) return null;
    return { kind: "bone", boneId };
  }
  if (key.startsWith("region:")) {
    const regionId = key.slice(7) as BodyRegionId;
    if (!BODY_REGION_IDS.includes(regionId)) return null;
    return { kind: "region", regionId };
  }
  return null;
}

function defaultControlFor(source: WeightSource): DeformControl {
  if (source.kind === "region") {
    return {
      amount: 0,
      axisScale: [...REGION_PRESETS[source.regionId].defaultAxisScale] as [number, number, number],
    };
  }
  return { amount: 0, axisScale: [...DEFAULT_AXIS_SCALE] };
}

function getOrCreateWeights(mesh: LoadedMesh, source: WeightSource): Float32Array {
  const key = weightKey(source);
  const cached = mesh.weightCache.get(key);
  if (cached) return cached;

  const weights =
    source.kind === "bone"
      ? mesh.blendBytes
        ? extractBoneWeights(mesh.blendBytes, source.boneId, mesh.vertexCount, mesh.blendStride)
        : new Float32Array(mesh.vertexCount)
      : generateRegionWeights(mesh.originalPositions, source.regionId);

  mesh.weightCache.set(key, weights);
  return weights;
}

function buildHighlightRegions(mesh: LoadedMesh, previewKey: string | null): ActiveRegionDeform[] {
  if (!previewKey) return [];
  const source = parseWeightKey(previewKey);
  if (!source) return [];
  const weights = getOrCreateWeights(mesh, source);
  const boundsCenter = computeBoundingCenter(mesh.originalPositions);
  return [
    {
      id: previewKey,
      weights,
      amount: 1,
      axisScale: [1, 1, 1],
      pivot: computeRegionPivot(mesh.originalPositions, weights, boundsCenter),
    },
  ];
}

export default function BodyShapeTool({
  fixedTargetPath,
  modName,
  onExported,
}: {
  fixedTargetPath?: string;
  modName?: string;
  onExported?: (result: { modRoot?: string; sourceModPath?: string }) => void;
} = {}) {
  const { t } = useTranslation();
  const [modPath, setModPath] = useState(fixedTargetPath ?? "");
  const [loading, setLoading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [loaded, setLoaded] = useState<LoadResult | null>(null);
  const [selectedMeshId, setSelectedMeshId] = useState<string>("");
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [controls, setControls] = useState<Record<string, DeformControl>>({});
  const [unifiedControl, setUnifiedControl] = useState<DeformControl>(DEFAULT_UNIFIED_CONTROL);
  const [controlMode, setControlMode] = useState<ControlMode>("unified");
  const [showOriginal, setShowOriginal] = useState(false);
  const [showWeights, setShowWeights] = useState(true);
  const [weightVersion, setWeightVersion] = useState(0);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
  const isFixedTarget = Boolean(fixedTargetPath);
  const simpleViewportRef = useRef<BodyShapeViewportHandle | null>(null);
  const prevPositionInputsRef = useRef<{
    regions: readonly ActiveRegionDeform[] | null;
    showOriginal: boolean;
  }>({ regions: null, showOriginal: false });
  const previewKeyRef = useRef<string | null>(null);

  const selectedMesh = useMemo(() => {
    if (!loaded) return null;
    return loaded.meshes.find((mesh) => mesh.id === selectedMeshId) ?? loaded.meshes[0] ?? null;
  }, [loaded, selectedMeshId]);

  const useBones = !!selectedMesh && selectedMesh.bones.length > 0;

  const selectableItems = useMemo(() => {
    if (!selectedMesh) return [] as { key: string; label: string }[];
    if (useBones) {
      return selectedMesh.bones.map((bone) => ({
        key: weightKey({ kind: "bone", boneId: bone.id }),
        label: t("page.tools.body_shape.bone_label", {
          id: bone.id,
          count: bone.vertexCount,
        }),
      }));
    }
    return BODY_REGION_IDS.map((id) => ({
      key: weightKey({ kind: "region", regionId: id }),
      label: t(`page.tools.body_shape.regions.${id}`),
    }));
  }, [selectedMesh, useBones, t]);

  const activeRegions: ActiveRegionDeform[] = useMemo(() => {
    if (!selectedMesh) return [];
    const boundsCenter = computeBoundingCenter(selectedMesh.originalPositions);
    return selectedKeys.flatMap((key) => {
      const source = parseWeightKey(key);
      if (!source) return [];
      const weights = getOrCreateWeights(selectedMesh, source);
      const control =
        controlMode === "unified" ? unifiedControl : (controls[key] ?? defaultControlFor(source));
      return [
        {
          id: key,
          weights,
          amount: control.amount,
          axisScale: control.axisScale,
          pivot: computeRegionPivot(selectedMesh.originalPositions, weights, boundsCenter),
        },
      ];
    });
  }, [selectedMesh, selectedKeys, controls, unifiedControl, controlMode, weightVersion]);

  const metrics = useMemo(() => {
    if (!selectedMesh) return null;
    if (showOriginal || activeRegions.length === 0) {
      selectedMesh.previewPositions.set(selectedMesh.originalPositions);
      return displacementMetrics(selectedMesh.originalPositions, selectedMesh.previewPositions);
    }
    applyMultiRegionDeform({
      originalPositions: selectedMesh.originalPositions,
      previewPositions: selectedMesh.previewPositions,
      regions: activeRegions,
    });
    return displacementMetrics(selectedMesh.originalPositions, selectedMesh.previewPositions);
  }, [selectedMesh, activeRegions, showOriginal, weightVersion]);

  const positionsChanged =
    prevPositionInputsRef.current.regions !== activeRegions ||
    prevPositionInputsRef.current.showOriginal !== showOriginal;
  useEffect(() => {
    prevPositionInputsRef.current = { regions: activeRegions, showOriginal };
  }, [activeRegions, showOriginal]);

  const selectFolder = async () => {
    const selected = await window.api.invoke("util:showOpenDialog", {
      properties: ["openDirectory"],
    });
    if (selected.canceled || !selected.filePaths[0]) return;
    setModPath(selected.filePaths[0]);
  };

  const loadMod = async (path = modPath) => {
    if (!path || loading) return;
    setLoading(true);
    try {
      const result = await window.api.invoke("tools:bodyShapeLoadMod", path);
      const meshes: LoadedMesh[] = result.meshes.map((mesh) => {
        const originalPositions = toFloat32(mesh.positions);
        return {
          id: mesh.id,
          name: mesh.name,
          positionPath: mesh.positionPath,
          positionRelativePath: mesh.positionRelativePath,
          positionStride: mesh.positionStride,
          vertexCount: mesh.vertexCount,
          originalPositions,
          previewPositions: new Float32Array(originalPositions),
          indices: toUint32(mesh.indices),
          vectorPath: mesh.vectorPath,
          vectorLayout: mesh.vectorLayout ?? null,
          blendBytes: toUint8(mesh.blendBytes),
          blendStride: mesh.blendStride ?? DEFAULT_BLEND_STRIDE,
          bones: mesh.bones ?? [],
          weightCache: new Map(),
        };
      });
      setLoaded({
        modRoot: result.modRoot,
        iniPath: result.iniPath,
        meshes,
      });
      setSelectedMeshId(meshes[0]?.id ?? "");
      setSelectedKeys([]);
      previewKeyRef.current = null;
      setControls({});
      setUnifiedControl(DEFAULT_UNIFIED_CONTROL);
      setControlMode("unified");
      setWeightVersion((v) => v + 1);
      setModelOrientation(DEFAULT_MODEL_ORIENTATION);
      setShowWeights(true);

      toast.success(t("page.tools.body_shape.toast.loaded"), {
        description: t("page.tools.body_shape.toast.loaded_description", {
          count: meshes.length,
        }),
      });
    } catch (error) {
      toast.error(t("page.tools.body_shape.toast.load_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!fixedTargetPath) return;
    setModPath(fixedTargetPath);
    void loadMod(fixedTargetPath);
  }, [fixedTargetPath]);

  const applySelectedKeys = (nextKeys: string[]) => {
    setSelectedKeys(nextKeys);
    setControls((prevControls) => {
      const updated = { ...prevControls };
      for (const key of nextKeys) {
        if (updated[key]) continue;
        const source = parseWeightKey(key);
        if (!source) continue;
        updated[key] = defaultControlFor(source);
        if (selectedMesh) getOrCreateWeights(selectedMesh, source);
      }
      return updated;
    });
    setWeightVersion((v) => v + 1);
  };

  const handleItemHighlighted = (item: { key: string; label: string } | undefined) => {
    const next = item?.key ?? null;
    previewKeyRef.current = next;
    if (!selectedMesh) return;
    const regions = buildHighlightRegions(selectedMesh, next);
    simpleViewportRef.current?.updateColors(regions);
  };

  const updateAmount = (key: string, amount: number) => {
    if (controlMode === "unified") {
      setUnifiedControl((prev) => ({ ...prev, amount }));
    } else {
      const source = parseWeightKey(key);
      if (!source) return;
      setControls((prev) => ({
        ...prev,
        [key]: { ...(prev[key] ?? defaultControlFor(source)), amount },
      }));
    }
    setWeightVersion((v) => v + 1);
  };

  const updateAxis = (key: string, axis: 0 | 1 | 2, value: number) => {
    if (controlMode === "unified") {
      setUnifiedControl((prev) => {
        const nextScale = [...prev.axisScale] as [number, number, number];
        nextScale[axis] = value;
        return { ...prev, axisScale: nextScale };
      });
    } else {
      const source = parseWeightKey(key);
      if (!source) return;
      setControls((prev) => {
        const current = prev[key] ?? defaultControlFor(source);
        const nextScale = [...current.axisScale] as [number, number, number];
        nextScale[axis] = value;
        return { ...prev, [key]: { ...current, axisScale: nextScale } };
      });
    }
    setWeightVersion((v) => v + 1);
  };

  const resetSelected = () => {
    if (controlMode === "unified") {
      setUnifiedControl(DEFAULT_UNIFIED_CONTROL);
    } else {
      setControls((prev) => {
        const next = { ...prev };
        for (const key of selectedKeys) {
          const source = parseWeightKey(key);
          if (!source) continue;
          next[key] = defaultControlFor(source);
        }
        return next;
      });
    }
    setWeightVersion((v) => v + 1);
  };

  const rotateModel = (delta: [number, number, number]) => {
    setModelOrientation((current) => {
      const [roll, pitch, yaw] = parseOrientation(current);
      return formatOrientation([roll + delta[0], pitch + delta[1], yaw + delta[2]]);
    });
  };

  const handleResetView = () => {
    setModelOrientation(DEFAULT_MODEL_ORIENTATION);
    simpleViewportRef.current?.resetCamera();
  };

  const exportMesh = async () => {
    if (!loaded || !selectedMesh || exporting) return;
    setExporting(true);
    try {
      applyMultiRegionDeform({
        originalPositions: selectedMesh.originalPositions,
        previewPositions: selectedMesh.previewPositions,
        regions: activeRegions,
      });
      const metricsNow = displacementMetrics(
        selectedMesh.originalPositions,
        selectedMesh.previewPositions,
      );
      const displayWeights = composeDisplayWeights(selectedMesh.vertexCount, activeRegions, {
        ignoreAmount: true,
      });
      const primary = activeRegions.find((r) => r.amount !== 0);
      const result = await window.api.invoke("tools:bodyShapeExport", {
        modRoot: loaded.modRoot,
        positionPath: selectedMesh.positionPath,
        positionStride: selectedMesh.positionStride,
        positions: selectedMesh.previewPositions,
        vectorPath: selectedMesh.vectorPath,
        vectorLayout: selectedMesh.vectorLayout,
        weights: displayWeights,
        amount: primary?.amount ?? 0,
        axisScale: primary ? [...primary.axisScale] : [1, 1, 1],
        writeChangeLog: true,
        changeSummary: {
          amount: primary?.amount ?? 0,
          axisScale: primary ? ([...primary.axisScale] as [number, number, number]) : [1, 1, 1],
          movedVertices: metricsNow.movedVertices,
          maxDisplacement: metricsNow.maxDisplacement,
        },
      });
      toast.success(t("page.tools.body_shape.toast.exported"), {
        description: result.modRoot
          ? t("page.tools.body_shape.toast.exported_as_mod", {
              path: result.modRoot,
            })
          : result.changeLogPath
            ? t("page.tools.body_shape.toast.exported_with_log")
            : t("page.tools.body_shape.toast.exported_description", {
                bytes: result.positionBytes,
              }),
      });
      onExported?.(result);
    } catch (error) {
      toast.error(t("page.tools.body_shape.toast.export_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setExporting(false);
    }
  };

  const selectedItems = selectableItems.filter((item) => selectedKeys.includes(item.key));

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3">
      {selectedMesh ? (
        <ModelViewerMenuBar
          rotateModel={rotateModel}
          onResetView={handleResetView}
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
          isViewerBusy={loading}
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
          {selectedMesh ? (
            <BodyShapeViewport
              ref={simpleViewportRef}
              originalPositions={selectedMesh.originalPositions}
              previewPositions={selectedMesh.previewPositions}
              regions={activeRegions}
              indices={selectedMesh.indices}
              showOriginal={showOriginal}
              showWeights={showWeights}
              weightVersion={weightVersion}
              positionsChanged={positionsChanged}
              orientation={modelOrientation}
            />
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {loading ? t("page.tools.body_shape.loading") : t("page.tools.body_shape.empty")}
            </div>
          )}
          {loading && selectedMesh ? (
            <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/20">
              <div className="inline-flex items-center gap-2 rounded-md border bg-background/90 px-3 py-2 text-sm shadow-sm">
                <Loader2Icon className="size-4 animate-spin" />
                {t("page.tools.body_shape.loading")}
              </div>
            </div>
          ) : null}
        </div>

        <div className="flex min-h-0 flex-col overflow-hidden rounded-md border bg-card/20">
          <div className="border-b px-4 py-3">
            <div className="text-sm font-medium">{t("page.tools.body_shape.title")}</div>
            {modName ? (
              <div className="truncate text-xs text-muted-foreground">{modName}</div>
            ) : null}
          </div>
          <ScrollArea className="min-h-0 flex-1">
            <div className="flex flex-col gap-4 p-4">
              {!isFixedTarget ? (
                <>
                  <Field>
                    <FieldLabel>{t("page.tools.body_shape.mod_path")}</FieldLabel>
                    <div className="flex gap-2">
                      <Input
                        value={modPath}
                        onChange={(event) => setModPath(event.target.value)}
                        placeholder={t("page.tools.body_shape.mod_path_placeholder")}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        onClick={() => void selectFolder()}
                      >
                        <FolderOpenIcon className="size-4" />
                      </Button>
                    </div>
                  </Field>

                  <Button
                    type="button"
                    onClick={() => void loadMod()}
                    disabled={!modPath || loading}
                  >
                    {loading ? <Loader2Icon className="size-4 animate-spin" /> : null}
                    {loading ? t("page.tools.body_shape.loading") : t("page.tools.body_shape.load")}
                  </Button>
                </>
              ) : null}

              {loaded && selectedMesh ? (
                <>
                  <Field>
                    <FieldLabel>{t("page.tools.body_shape.mesh")}</FieldLabel>
                    <Select
                      value={selectedMesh.id}
                      onValueChange={(value) => {
                        if (value) {
                          setSelectedMeshId(value);
                          setSelectedKeys([]);
                          previewKeyRef.current = null;
                          setControls({});
                          setUnifiedControl(DEFAULT_UNIFIED_CONTROL);
                          setWeightVersion((v) => v + 1);
                        }
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {loaded.meshes.map((mesh) => (
                            <SelectItem key={mesh.id} value={mesh.id}>
                              {mesh.name} ({mesh.vertexCount})
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>

                  <Field>
                    <FieldLabel>
                      {useBones
                        ? t("page.tools.body_shape.select_bones")
                        : t("page.tools.body_shape.select_regions")}
                    </FieldLabel>
                    {useBones ? (
                      <p className="mb-2 text-[11px] text-muted-foreground">
                        {t("page.tools.body_shape.pick_bones_hint")}
                      </p>
                    ) : (
                      <p className="mb-2 text-[11px] text-muted-foreground">
                        {t("page.tools.body_shape.no_blend_fallback")}
                      </p>
                    )}
                    <Combobox
                      multiple
                      autoHighlight
                      items={selectableItems}
                      value={selectedItems}
                      onValueChange={(next) => {
                        applySelectedKeys((next ?? []).map((item) => item.key));
                      }}
                      onItemHighlighted={handleItemHighlighted}
                      onOpenChange={(open) => {
                        if (!open) {
                          previewKeyRef.current = null;
                          if (selectedMesh) {
                            simpleViewportRef.current?.updateColors(activeRegions);
                          }
                        }
                      }}
                      itemToStringLabel={(item) => item.label}
                      isItemEqualToValue={(a, b) => a.key === b.key}
                    >
                      <ComboboxChips>
                        <ComboboxValue>
                          {(value: typeof selectableItems) => (
                            <>
                              {value.map((item) => (
                                <ComboboxChip key={item.key}>{item.label}</ComboboxChip>
                              ))}
                              <ComboboxInput
                                placeholder={
                                  value.length > 0
                                    ? ""
                                    : useBones
                                      ? t("page.tools.body_shape.select_bones_placeholder")
                                      : t("page.tools.body_shape.select_regions_placeholder")
                                }
                              />
                            </>
                          )}
                        </ComboboxValue>
                      </ComboboxChips>
                      <ComboboxContent>
                        <ComboboxEmpty>
                          {useBones
                            ? t("page.tools.body_shape.no_bones")
                            : t("page.tools.body_shape.no_regions")}
                        </ComboboxEmpty>
                        <ComboboxList>
                          {(item: (typeof selectableItems)[number]) => (
                            <ComboboxItem key={item.key} value={item}>
                              {item.label}
                            </ComboboxItem>
                          )}
                        </ComboboxList>
                      </ComboboxContent>
                    </Combobox>
                  </Field>

                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground">
                      {t("page.tools.body_shape.show_weights")}
                    </span>
                    <Switch checked={showWeights} onCheckedChange={setShowWeights} />
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground">
                      {t("page.tools.body_shape.show_original")}
                    </span>
                    <Switch checked={showOriginal} onCheckedChange={setShowOriginal} />
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground">
                      {t("page.tools.body_shape.unified_controls")}
                    </span>
                    <Switch
                      checked={controlMode === "unified"}
                      onCheckedChange={(checked) => {
                        setControlMode(checked ? "unified" : "perItem");
                        setWeightVersion((v) => v + 1);
                      }}
                    />
                  </div>

                  {selectedKeys.length === 0 ? (
                    <p className="text-xs text-muted-foreground">
                      {useBones
                        ? t("page.tools.body_shape.select_bones_hint")
                        : t("page.tools.body_shape.select_regions_hint")}
                    </p>
                  ) : controlMode === "unified" ? (
                    <div className="space-y-2 rounded-md border bg-background/50 p-3">
                      <div className="text-sm font-medium">
                        {t("page.tools.body_shape.unified_controls_title", {
                          count: selectedKeys.length,
                        })}
                      </div>
                      <RangeField
                        label={t("page.tools.body_shape.deform_amount")}
                        value={unifiedControl.amount}
                        defaultValue={DEFAULT_UNIFIED_CONTROL.amount}
                        min={-0.5}
                        max={0.5}
                        step={0.01}
                        display={`${(unifiedControl.amount * 100).toFixed(0)}%`}
                        onChange={(value) => updateAmount(selectedKeys[0]!, value)}
                      />
                      <RangeField
                        label={t("page.tools.body_shape.axis_x")}
                        value={unifiedControl.axisScale[0]}
                        defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[0]}
                        min={0}
                        max={2}
                        step={0.05}
                        display={unifiedControl.axisScale[0].toFixed(2)}
                        onChange={(value) => updateAxis(selectedKeys[0]!, 0, value)}
                      />
                      <RangeField
                        label={t("page.tools.body_shape.axis_y")}
                        value={unifiedControl.axisScale[1]}
                        defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[1]}
                        min={0}
                        max={2}
                        step={0.05}
                        display={unifiedControl.axisScale[1].toFixed(2)}
                        onChange={(value) => updateAxis(selectedKeys[0]!, 1, value)}
                      />
                      <RangeField
                        label={t("page.tools.body_shape.axis_z")}
                        value={unifiedControl.axisScale[2]}
                        defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[2]}
                        min={0}
                        max={2}
                        step={0.05}
                        display={unifiedControl.axisScale[2].toFixed(2)}
                        onChange={(value) => updateAxis(selectedKeys[0]!, 2, value)}
                      />
                    </div>
                  ) : (
                    <div className="space-y-3">
                      {selectedKeys.map((key) => {
                        const source = parseWeightKey(key);
                        if (!source) return null;
                        const control = controls[key] ?? defaultControlFor(source);
                        const label =
                          source.kind === "bone"
                            ? t("page.tools.body_shape.bone_id", { id: source.boneId })
                            : t(`page.tools.body_shape.regions.${source.regionId}`);
                        return (
                          <div
                            key={key}
                            className="space-y-2 rounded-md border bg-background/50 p-3"
                          >
                            <div className="text-sm font-medium">{label}</div>
                            <RangeField
                              label={t("page.tools.body_shape.deform_amount")}
                              value={control.amount}
                              defaultValue={defaultControlFor(source).amount}
                              min={-0.5}
                              max={0.5}
                              step={0.01}
                              display={`${(control.amount * 100).toFixed(0)}%`}
                              onChange={(value) => updateAmount(key, value)}
                            />
                            <RangeField
                              label={t("page.tools.body_shape.axis_x")}
                              value={control.axisScale[0]}
                              defaultValue={defaultControlFor(source).axisScale[0]}
                              min={0}
                              max={2}
                              step={0.05}
                              display={control.axisScale[0].toFixed(2)}
                              onChange={(value) => updateAxis(key, 0, value)}
                            />
                            <RangeField
                              label={t("page.tools.body_shape.axis_y")}
                              value={control.axisScale[1]}
                              defaultValue={defaultControlFor(source).axisScale[1]}
                              min={0}
                              max={2}
                              step={0.05}
                              display={control.axisScale[1].toFixed(2)}
                              onChange={(value) => updateAxis(key, 1, value)}
                            />
                            <RangeField
                              label={t("page.tools.body_shape.axis_z")}
                              value={control.axisScale[2]}
                              defaultValue={defaultControlFor(source).axisScale[2]}
                              min={0}
                              max={2}
                              step={0.05}
                              display={control.axisScale[2].toFixed(2)}
                              onChange={(value) => updateAxis(key, 2, value)}
                            />
                          </div>
                        );
                      })}
                    </div>
                  )}

                  <Button
                    type="button"
                    variant="outline"
                    onClick={resetSelected}
                    disabled={selectedKeys.length === 0}
                  >
                    {t("page.tools.body_shape.reset_selected")}
                  </Button>

                  {metrics ? (
                    <div className="space-y-1 rounded-md border bg-background/50 p-3 text-xs text-muted-foreground">
                      <div>
                        {t("page.tools.body_shape.metrics.vertices")}: {metrics.vertexCount}
                      </div>
                      <div>
                        {t("page.tools.body_shape.metrics.moved")}: {metrics.movedVertices}
                      </div>
                      <div>
                        {t("page.tools.body_shape.metrics.max_disp")}:{" "}
                        {metrics.maxDisplacement.toFixed(5)}
                      </div>
                      {useBones ? (
                        <div>
                          {t("page.tools.body_shape.metrics.bones")}: {selectedMesh.bones.length}
                        </div>
                      ) : null}
                    </div>
                  ) : null}

                  <Button type="button" onClick={() => void exportMesh()} disabled={exporting}>
                    {exporting ? (
                      <Loader2Icon className="size-4 animate-spin" />
                    ) : (
                      <SaveIcon className="size-4" />
                    )}
                    {exporting
                      ? t("page.tools.body_shape.exporting")
                      : t("page.tools.body_shape.export")}
                  </Button>
                </>
              ) : null}
            </div>
          </ScrollArea>
        </div>
      </div>
    </div>
  );
}

function RangeField({
  label,
  value,
  defaultValue,
  min,
  max,
  step,
  display,
  onChange,
}: {
  label: string;
  value: number;
  defaultValue: number;
  min: number;
  max: number;
  step: number;
  display: string;
  onChange: (value: number) => void;
}) {
  return (
    <Field>
      <div className="mb-1 flex items-center justify-between gap-2">
        <FieldLabel className="mb-0">{label}</FieldLabel>
        <span className="text-[11px] text-muted-foreground tabular-nums">{display}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        className="w-full accent-primary"
        onPointerDown={(event) => {
          if (!(event.ctrlKey || event.metaKey)) return;
          event.preventDefault();
          onChange(defaultValue);
        }}
        onChange={(event) => onChange(Number(event.currentTarget.value))}
      />
    </Field>
  );
}

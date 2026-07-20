import { BodyShapeViewport } from "@renderer/components/tools/body-shape/body-shape-viewport";
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
  displacementMetrics,
  generateRegionWeights,
  REGION_PRESETS,
  type ActiveRegionDeform,
  type BodyRegionId,
} from "@shared/body-shape";
import { toErrorMessage } from "@shared/utils";
import { FolderOpenIcon, Loader2Icon, SaveIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ScrollArea } from "../../ui/scroll-area";

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
  regionWeightCache: Partial<Record<BodyRegionId, Float32Array>>;
};

type LoadResult = {
  modRoot: string;
  iniPath: string;
  meshes: LoadedMesh[];
};

type RegionControl = {
  amount: number;
  axisScale: [number, number, number];
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

function defaultControls(): Record<BodyRegionId, RegionControl> {
  return Object.fromEntries(
    BODY_REGION_IDS.map((id) => [
      id,
      {
        amount: 0,
        axisScale: [...REGION_PRESETS[id].defaultAxisScale] as [number, number, number],
      },
    ]),
  ) as Record<BodyRegionId, RegionControl>;
}

function getOrCreateRegionWeights(mesh: LoadedMesh, regionId: BodyRegionId): Float32Array {
  const cached = mesh.regionWeightCache[regionId];
  if (cached) return cached;
  const weights = generateRegionWeights(mesh.originalPositions, regionId);
  mesh.regionWeightCache[regionId] = weights;
  return weights;
}

export default function BodyShapeTool({
  fixedTargetPath,
  modName,
}: {
  fixedTargetPath?: string;
  modName?: string;
} = {}) {
  const { t } = useTranslation();
  const [modPath, setModPath] = useState(fixedTargetPath ?? "");
  const [loading, setLoading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [loaded, setLoaded] = useState<LoadResult | null>(null);
  const [selectedMeshId, setSelectedMeshId] = useState<string>("");
  const [selectedRegions, setSelectedRegions] = useState<BodyRegionId[]>([]);
  const [regionControls, setRegionControls] = useState(defaultControls);
  const [showOriginal, setShowOriginal] = useState(false);
  const [showWeights, setShowWeights] = useState(true);
  const [weightVersion, setWeightVersion] = useState(0);
  const isFixedTarget = Boolean(fixedTargetPath);

  const selectedMesh = useMemo(() => {
    if (!loaded) return null;
    return loaded.meshes.find((mesh) => mesh.id === selectedMeshId) ?? loaded.meshes[0] ?? null;
  }, [loaded, selectedMeshId]);

  const activeRegions: ActiveRegionDeform[] = useMemo(() => {
    if (!selectedMesh) return [];
    const boundsCenter = computeBoundingCenter(selectedMesh.originalPositions);
    return selectedRegions.map((regionId) => {
      const weights = getOrCreateRegionWeights(selectedMesh, regionId);
      const control = regionControls[regionId];
      return {
        regionId,
        weights,
        amount: control.amount,
        axisScale: control.axisScale,
        pivot: computeRegionPivot(selectedMesh.originalPositions, weights, boundsCenter),
      };
    });
  }, [selectedMesh, selectedRegions, regionControls, weightVersion]);

  const metrics = useMemo(() => {
    if (!selectedMesh) return null;
    if (showOriginal || activeRegions.length === 0) {
      selectedMesh.previewPositions.set(selectedMesh.originalPositions);
    } else {
      applyMultiRegionDeform({
        originalPositions: selectedMesh.originalPositions,
        previewPositions: selectedMesh.previewPositions,
        regions: activeRegions,
      });
    }
    return displacementMetrics(selectedMesh.originalPositions, selectedMesh.previewPositions);
  }, [selectedMesh, activeRegions, showOriginal, weightVersion]);

  const regionItems = useMemo(
    () =>
      BODY_REGION_IDS.map((id) => ({
        id,
        label: t(`page.tools.body_shape.regions.${id}`),
      })),
    [t],
  );

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
          regionWeightCache: {},
        };
      });
      setLoaded({
        modRoot: result.modRoot,
        iniPath: result.iniPath,
        meshes,
      });
      setSelectedMeshId(meshes[0]?.id ?? "");
      setSelectedRegions([]);
      setRegionControls(defaultControls());
      setWeightVersion((v) => v + 1);
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

  const updateRegionAmount = (regionId: BodyRegionId, amount: number) => {
    setRegionControls((prev) => ({
      ...prev,
      [regionId]: { ...prev[regionId], amount },
    }));
    setWeightVersion((v) => v + 1);
  };

  const updateRegionAxis = (regionId: BodyRegionId, axis: 0 | 1 | 2, value: number) => {
    setRegionControls((prev) => {
      const nextScale = [...prev[regionId].axisScale] as [number, number, number];
      nextScale[axis] = value;
      return {
        ...prev,
        [regionId]: { ...prev[regionId], axisScale: nextScale },
      };
    });
    setWeightVersion((v) => v + 1);
  };

  const resetSelected = () => {
    setRegionControls((prev) => {
      const next = { ...prev };
      for (const id of selectedRegions) {
        next[id] = {
          amount: 0,
          axisScale: [...REGION_PRESETS[id].defaultAxisScale] as [number, number, number],
        };
      }
      return next;
    });
    setWeightVersion((v) => v + 1);
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
      const displayWeights = composeDisplayWeights(selectedMesh.vertexCount, activeRegions);
      // Export uses a representative amount/axis for vector correction when a single
      // non-uniform region dominates; multi-region vector rewrite is skipped if complex.
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
        description: result.changeLogPath
          ? t("page.tools.body_shape.toast.exported_with_log")
          : t("page.tools.body_shape.toast.exported_description", {
              bytes: result.positionBytes,
            }),
      });
    } catch (error) {
      toast.error(t("page.tools.body_shape.toast.export_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="grid h-full min-h-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_360px]">
      <div className="relative min-h-80 overflow-hidden rounded-md border bg-muted/30">
        {selectedMesh ? (
          <BodyShapeViewport
            originalPositions={selectedMesh.originalPositions}
            previewPositions={selectedMesh.previewPositions}
            regions={activeRegions}
            indices={selectedMesh.indices}
            showOriginal={showOriginal}
            showWeights={showWeights}
            weightVersion={weightVersion}
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
          {modName ? <div className="truncate text-xs text-muted-foreground">{modName}</div> : null}
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

                <Button type="button" onClick={() => void loadMod()} disabled={!modPath || loading}>
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
                        setSelectedRegions([]);
                        setRegionControls(defaultControls());
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
                  <FieldLabel>{t("page.tools.body_shape.select_regions")}</FieldLabel>
                  <Combobox
                    multiple
                    items={regionItems}
                    value={regionItems.filter((item) => selectedRegions.includes(item.id))}
                    onValueChange={(next) => {
                      const ids = (next ?? []).map((item) => item.id);
                      setSelectedRegions(ids);
                      for (const id of ids) {
                        getOrCreateRegionWeights(selectedMesh, id);
                      }
                      setWeightVersion((v) => v + 1);
                    }}
                    itemToStringLabel={(item) => item.label}
                    isItemEqualToValue={(a, b) => a.id === b.id}
                  >
                    <ComboboxChips>
                      <ComboboxValue>
                        {(value: typeof regionItems) => (
                          <>
                            {value.map((item) => (
                              <ComboboxChip key={item.id}>{item.label}</ComboboxChip>
                            ))}
                            <ComboboxInput
                              placeholder={
                                value.length > 0
                                  ? ""
                                  : t("page.tools.body_shape.select_regions_placeholder")
                              }
                            />
                          </>
                        )}
                      </ComboboxValue>
                    </ComboboxChips>
                    <ComboboxContent>
                      <ComboboxEmpty>{t("page.tools.body_shape.no_regions")}</ComboboxEmpty>
                      <ComboboxList>
                        {(item: (typeof regionItems)[number]) => (
                          <ComboboxItem key={item.id} value={item}>
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

                {selectedRegions.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    {t("page.tools.body_shape.select_regions_hint")}
                  </p>
                ) : (
                  <div className="space-y-3">
                    {selectedRegions.map((regionId) => {
                      const control = regionControls[regionId];
                      return (
                        <div
                          key={regionId}
                          className="space-y-2 rounded-md border bg-background/50 p-3"
                        >
                          <div className="text-sm font-medium">
                            {t(`page.tools.body_shape.regions.${regionId}`)}
                          </div>
                          <RangeField
                            label={t("page.tools.body_shape.deform_amount")}
                            value={control.amount}
                            min={-0.5}
                            max={0.5}
                            step={0.01}
                            display={`${(control.amount * 100).toFixed(0)}%`}
                            onChange={(value) => updateRegionAmount(regionId, value)}
                          />
                          <RangeField
                            label={t("page.tools.body_shape.axis_x")}
                            value={control.axisScale[0]}
                            min={0}
                            max={2}
                            step={0.05}
                            display={control.axisScale[0].toFixed(2)}
                            onChange={(value) => updateRegionAxis(regionId, 0, value)}
                          />
                          <RangeField
                            label={t("page.tools.body_shape.axis_y")}
                            value={control.axisScale[1]}
                            min={0}
                            max={2}
                            step={0.05}
                            display={control.axisScale[1].toFixed(2)}
                            onChange={(value) => updateRegionAxis(regionId, 1, value)}
                          />
                          <RangeField
                            label={t("page.tools.body_shape.axis_z")}
                            value={control.axisScale[2]}
                            min={0}
                            max={2}
                            step={0.05}
                            display={control.axisScale[2].toFixed(2)}
                            onChange={(value) => updateRegionAxis(regionId, 2, value)}
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
                  disabled={selectedRegions.length === 0}
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
  );
}

function RangeField({
  label,
  value,
  min,
  max,
  step,
  display,
  onChange,
}: {
  label: string;
  value: number;
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
        onChange={(event) => onChange(Number(event.currentTarget.value))}
      />
    </Field>
  );
}

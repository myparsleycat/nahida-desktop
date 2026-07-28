import {
  BodyShapeViewport,
  type BodyShapeViewportHandle,
  type BrushStrokeInput,
} from "@renderer/components/tools/body-shape/body-shape-viewport";
import {
  formatOrientation,
  parseOrientation,
} from "@renderer/components/tools/model-viewer/model-viewer-contract";
import {
  DEFAULT_MODEL_ORIENTATION,
  DEFAULT_THREE_EXPOSURE,
} from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import {
  ModelViewerMenuBar,
  type BrushMode,
  type BrushProps,
} from "@renderer/components/tools/model-viewer/model-viewer-menu-bar";
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
  applyBrushStroke,
  applyGeodesicBrush,
  applyMultiRegionDeform,
  buildConnectedComponents,
  buildSymmetryMap,
  buildVertexAdjacency,
  composeDisplayWeights,
  computeBoundingCenter,
  computeMeshBounds,
  computeRegionPivot,
  computeVertexNormals,
  DEFAULT_BLEND_STRIDE,
  displacementMetrics,
  extractBoneWeights,
  growSelectionWeights,
  mirrorWeightsAcrossX,
  selectConnectedComponent,
  shrinkSelectionWeights,
  smoothSelectionWeights,
  type ActiveRegionDeform,
  type BlendBoneInfo,
} from "@shared/body-shape";
import { toErrorMessage } from "@shared/utils";
import {
  FolderOpenIcon,
  Loader2Icon,
  Maximize2Icon,
  Minimize2Icon,
  Redo2Icon,
  SaveIcon,
  Undo2Icon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ScrollArea } from "../../ui/scroll-area";

const DEFAULT_AXIS_SCALE: [number, number, number] = [1, 0.15, 1];

type WeightSource = { kind: "bone"; boneId: number };

type DeformOperation = "scale" | "inflate" | "translate" | "taper";

type DeformControl = {
  amount: number;
  axisScale: [number, number, number];
  translation: [number, number, number];
  taperFactor: number;
};

const DEFAULT_UNIFIED_CONTROL: DeformControl = {
  amount: 0,
  axisScale: [...DEFAULT_AXIS_SCALE],
  translation: [0, 0, 0],
  taperFactor: 0.5,
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
  selectionWeights: Float32Array;
  adjacency?: readonly Uint32Array[];
  originalNormals?: Float32Array;
  componentIds?: Uint32Array;
  symmetryMap?: Int32Array;
};

type LoadResult = {
  modRoot: string;
  iniPath: string;
  meshes: LoadedMesh[];
};

type SelectionHistoryEntry = {
  before: Float32Array;
  after: Float32Array;
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

function nearestVertexToPoint(
  positions: Float32Array,
  candidates: readonly number[],
  point: readonly [number, number, number],
): number | undefined {
  return candidates.reduce<number | undefined>((nearest, candidate) => {
    if (candidate * 3 + 2 >= positions.length) return nearest;
    if (nearest === undefined) return candidate;
    const candidateOffset = candidate * 3;
    const nearestOffset = nearest * 3;
    const candidateDistance =
      (positions[candidateOffset] - point[0]) ** 2 +
      (positions[candidateOffset + 1] - point[1]) ** 2 +
      (positions[candidateOffset + 2] - point[2]) ** 2;
    const nearestDistance =
      (positions[nearestOffset] - point[0]) ** 2 +
      (positions[nearestOffset + 1] - point[1]) ** 2 +
      (positions[nearestOffset + 2] - point[2]) ** 2;
    return candidateDistance < nearestDistance ? candidate : nearest;
  }, undefined);
}

function weightKey(source: WeightSource): string {
  return `bone:${source.boneId}`;
}

function parseWeightKey(key: string): WeightSource | null {
  if (key.startsWith("bone:")) {
    const boneId = Number(key.slice(5));
    if (!Number.isInteger(boneId) || boneId < 0) return null;
    return { kind: "bone", boneId };
  }
  return null;
}

function getOrCreateWeights(
  mesh: LoadedMesh,
  source: WeightSource,
  customWeightsMap?: Map<string, Float32Array>,
): Float32Array {
  const key = weightKey(source);
  if (customWeightsMap?.has(key)) {
    return customWeightsMap.get(key)!;
  }
  const cached = mesh.weightCache.get(key);
  if (cached) return cached;

  const weights = mesh.blendBytes
    ? extractBoneWeights(mesh.blendBytes, source.boneId, mesh.vertexCount, mesh.blendStride)
    : new Float32Array(mesh.vertexCount);

  mesh.weightCache.set(key, weights);
  return weights;
}

function buildHighlightRegions(
  mesh: LoadedMesh,
  previewKey: string | null,
  customWeightsMap?: Map<string, Float32Array>,
): ActiveRegionDeform[] {
  if (!previewKey) return [];
  const source = parseWeightKey(previewKey);
  if (!source) return [];
  const weights = getOrCreateWeights(mesh, source, customWeightsMap);
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
  const [unifiedControl, setUnifiedControl] = useState<DeformControl>(DEFAULT_UNIFIED_CONTROL);
  const [deformOperation, setDeformOperation] = useState<DeformOperation>("scale");
  const [showOriginal, setShowOriginal] = useState(false);
  const [showWeights, setShowWeights] = useState(true);
  const [weightVersion, setWeightVersion] = useState(0);
  const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);

  /* Brush state */
  const [brushEnabled, setBrushEnabled] = useState(false);
  const [brushMode, setBrushMode] = useState<BrushMode>("paint");
  const [selectionToolMode, setSelectionToolMode] = useState<"brush" | "component">("brush");
  const [brushReachMode, setBrushReachMode] = useState<"surface" | "through">("surface");
  const [brushRadius, setBrushRadius] = useState(0.15);
  const [brushStrength, setBrushStrength] = useState(0.1);
  const [brushMirrorX, setBrushMirrorX] = useState(true);

  const isFixedTarget = Boolean(fixedTargetPath);
  const simpleViewportRef = useRef<BodyShapeViewportHandle | null>(null);
  const prevPositionInputsRef = useRef<{
    regions: readonly ActiveRegionDeform[] | null;
    showOriginal: boolean;
  }>({ regions: null, showOriginal: false });
  const previewKeyRef = useRef<string | null>(null);
  const selectionStrokeBeforeRef = useRef<Float32Array | null>(null);
  const selectionHistoryRef = useRef<{
    undo: SelectionHistoryEntry[];
    redo: SelectionHistoryEntry[];
  }>({ undo: [], redo: [] });
  const [, setHistoryVersion] = useState(0);

  const selectedMesh = useMemo(() => {
    if (!loaded) return null;
    return loaded.meshes.find((mesh) => mesh.id === selectedMeshId) ?? loaded.meshes[0] ?? null;
  }, [loaded, selectedMeshId]);

  const useBones = !!selectedMesh && selectedMesh.bones.length > 0;

  const selectableItems = useMemo(() => {
    if (!selectedMesh) return [] as { key: string; label: string }[];
    return selectedMesh.bones.map((bone) => ({
      key: weightKey({ kind: "bone", boneId: bone.id }),
      label: t("page.tools.body_shape.bone_label", {
        id: bone.id,
        count: bone.vertexCount,
      }),
    }));
  }, [selectedMesh, t]);

  const hasSelection = useMemo(
    () => selectedMesh?.selectionWeights.some((weight) => weight > 0) ?? false,
    [selectedMesh, weightVersion],
  );

  const meshExtent = useMemo(() => {
    if (!selectedMesh) return 1;
    const size = computeMeshBounds(selectedMesh.originalPositions).size;
    return Math.max(size[0], size[1], size[2], 1e-3);
  }, [selectedMesh]);

  const activeRegions: ActiveRegionDeform[] = useMemo(() => {
    if (!selectedMesh || !hasSelection) return [];
    const isTranslate = deformOperation === "translate";
    return [
      {
        id: "selection",
        weights: selectedMesh.selectionWeights,
        amount: isTranslate ? 1 : unifiedControl.amount,
        axisScale: unifiedControl.axisScale,
        operation: deformOperation,
        normals: selectedMesh.originalNormals,
        translation: unifiedControl.translation,
        taperFactor: unifiedControl.taperFactor,
        pivot: computeRegionPivot(
          selectedMesh.originalPositions,
          selectedMesh.selectionWeights,
          computeBoundingCenter(selectedMesh.originalPositions),
        ),
      },
    ];
  }, [selectedMesh, hasSelection, unifiedControl, deformOperation, weightVersion]);

  const resetSelectionHistory = () => {
    selectionHistoryRef.current = { undo: [], redo: [] };
    selectionStrokeBeforeRef.current = null;
    setHistoryVersion((version) => version + 1);
  };

  const commitSelectionHistory = (before: Float32Array) => {
    if (!selectedMesh) return;
    const after = new Float32Array(selectedMesh.selectionWeights);
    if (before.every((weight, index) => weight === after[index])) return;
    selectionHistoryRef.current.undo.push({ before, after });
    if (selectionHistoryRef.current.undo.length > 50) {
      selectionHistoryRef.current.undo.shift();
    }
    selectionHistoryRef.current.redo = [];
    setHistoryVersion((version) => version + 1);
  };

  const handleBrushStroke = (stroke: BrushStrokeInput) => {
    if (!selectedMesh) return;

    if (selectionToolMode === "component") {
      if (!selectedMesh.componentIds || stroke.vertexIndices === undefined) return;
      const seedVertex = nearestVertexToPoint(
        selectedMesh.previewPositions,
        stroke.vertexIndices,
        stroke.localPoint,
      );
      if (seedVertex === undefined) return;
      const componentId = selectedMesh.componentIds[seedVertex];
      if (componentId === undefined) return;

      const changedIndices = selectConnectedComponent({
        weights: selectedMesh.selectionWeights,
        componentIds: selectedMesh.componentIds,
        targetComponentId: componentId,
        mode: brushMode === "erase" ? "subtract" : "add",
      });
      if (changedIndices.length > 0) {
        if (brushMirrorX) {
          mirrorWeightsAcrossX(
            selectedMesh.originalPositions,
            selectedMesh.selectionWeights,
            changedIndices,
            1e-3,
            brushMode,
            selectedMesh.symmetryMap,
          );
        }
        setWeightVersion((version) => version + 1);
      }
      return;
    }

    const seedVertex = stroke.vertexIndices
      ? nearestVertexToPoint(selectedMesh.previewPositions, stroke.vertexIndices, stroke.localPoint)
      : undefined;
    const useSurfaceBrush = brushReachMode === "surface";
    const changedIndices =
      useSurfaceBrush && selectedMesh.adjacency && seedVertex !== undefined
        ? applyGeodesicBrush({
            positions: selectedMesh.originalPositions,
            adjacency: selectedMesh.adjacency,
            weights: selectedMesh.selectionWeights,
            seedVertexIndices: [seedVertex],
            radius: brushRadius,
            strength: brushStrength,
            mode: brushMode,
            frontFacingOnly: true,
            normals: selectedMesh.originalNormals,
            hitNormal: stroke.localNormal,
          })
        : (() => {
            const before = new Float32Array(selectedMesh.selectionWeights);
            applyBrushStroke({
              positions: showOriginal
                ? selectedMesh.originalPositions
                : selectedMesh.previewPositions,
              weights: selectedMesh.selectionWeights,
              hitPoint: stroke.localPoint,
              hitNormal: useSurfaceBrush ? stroke.localNormal : undefined,
              radius: brushRadius,
              strength: brushStrength,
              mode: brushMode,
              normals: useSurfaceBrush ? selectedMesh.originalNormals : undefined,
            });
            return Uint32Array.from(
              Array.from(before.keys()).filter(
                (index) => before[index] !== selectedMesh.selectionWeights[index],
              ),
            );
          })();

    if (changedIndices.length === 0) return;
    if (brushMirrorX) {
      mirrorWeightsAcrossX(
        selectedMesh.originalPositions,
        selectedMesh.selectionWeights,
        changedIndices,
        1e-3,
        brushMode,
        selectedMesh.symmetryMap,
      );
    }
    setWeightVersion((version) => version + 1);
  };

  const handleBrushStrokeStart = () => {
    if (!selectedMesh || selectionStrokeBeforeRef.current) return;
    selectionStrokeBeforeRef.current = new Float32Array(selectedMesh.selectionWeights);
  };

  const handleBrushStrokeEnd = () => {
    const before = selectionStrokeBeforeRef.current;
    selectionStrokeBeforeRef.current = null;
    if (before) commitSelectionHistory(before);
  };

  const handleResetPaintedWeights = () => {
    if (!selectedMesh) return;
    const before = new Float32Array(selectedMesh.selectionWeights);
    selectedMesh.selectionWeights.fill(0);
    setSelectedKeys([]);
    commitSelectionHistory(before);
    setWeightVersion((version) => version + 1);
    toast.success(t("page.tools.body_shape.brush_reset"));
  };

  const smoothSelection = () => {
    if (!selectedMesh?.adjacency) return;
    const before = new Float32Array(selectedMesh.selectionWeights);
    smoothSelectionWeights(selectedMesh.selectionWeights, selectedMesh.adjacency, 0.5, 2);
    commitSelectionHistory(before);
    setWeightVersion((version) => version + 1);
  };

  const growSelection = () => {
    if (!selectedMesh?.adjacency) return;
    const before = new Float32Array(selectedMesh.selectionWeights);
    growSelectionWeights(selectedMesh.selectionWeights, selectedMesh.adjacency, 1.0, 1);
    commitSelectionHistory(before);
    setWeightVersion((version) => version + 1);
  };

  const shrinkSelection = () => {
    if (!selectedMesh?.adjacency) return;
    const before = new Float32Array(selectedMesh.selectionWeights);
    shrinkSelectionWeights(selectedMesh.selectionWeights, selectedMesh.adjacency, 1.0, 1);
    commitSelectionHistory(before);
    setWeightVersion((version) => version + 1);
  };

  const undoSelection = () => {
    if (!selectedMesh) return;
    const entry = selectionHistoryRef.current.undo.pop();
    if (!entry) return;
    selectedMesh.selectionWeights.set(entry.before);
    selectionHistoryRef.current.redo.push(entry);
    setSelectedKeys([]);
    setHistoryVersion((version) => version + 1);
    setWeightVersion((version) => version + 1);
  };

  const redoSelection = () => {
    if (!selectedMesh) return;
    const entry = selectionHistoryRef.current.redo.pop();
    if (!entry) return;
    selectedMesh.selectionWeights.set(entry.after);
    selectionHistoryRef.current.undo.push(entry);
    setSelectedKeys([]);
    setHistoryVersion((version) => version + 1);
    setWeightVersion((version) => version + 1);
  };

  const hasPaintedWeights = hasSelection;

  const brushProps: BrushProps = {
    enabled: brushEnabled,
    onEnabledChange: setBrushEnabled,
    mode: brushMode,
    onModeChange: setBrushMode,
    radius: brushRadius,
    onRadiusChange: setBrushRadius,
    strength: brushStrength,
    onStrengthChange: setBrushStrength,
    mirrorX: brushMirrorX,
    onMirrorXChange: setBrushMirrorX,
    onResetPaintedWeights: handleResetPaintedWeights,
    hasPaintedWeights,
  };

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
        const indices = toUint32(mesh.indices);
        const adjacency = indices ? buildVertexAdjacency(mesh.vertexCount, indices) : undefined;
        const componentIds = adjacency
          ? buildConnectedComponents(mesh.vertexCount, adjacency)
          : undefined;
        const symmetryMap = buildSymmetryMap(originalPositions, "x", 1e-3);
        return {
          id: mesh.id,
          name: mesh.name,
          positionPath: mesh.positionPath,
          positionRelativePath: mesh.positionRelativePath,
          positionStride: mesh.positionStride,
          vertexCount: mesh.vertexCount,
          originalPositions,
          previewPositions: new Float32Array(originalPositions),
          indices,
          vectorPath: mesh.vectorPath,
          vectorLayout: mesh.vectorLayout ?? null,
          blendBytes: toUint8(mesh.blendBytes),
          blendStride: mesh.blendStride ?? DEFAULT_BLEND_STRIDE,
          bones: mesh.bones ?? [],
          weightCache: new Map(),
          selectionWeights: new Float32Array(mesh.vertexCount),
          adjacency,
          originalNormals: indices ? computeVertexNormals(originalPositions, indices) : undefined,
          componentIds,
          symmetryMap,
        };
      });
      setLoaded({
        modRoot: result.modRoot,
        iniPath: result.iniPath,
        meshes,
      });
      setSelectedMeshId(meshes[0]?.id ?? "");
      setSelectedKeys([]);
      resetSelectionHistory();
      previewKeyRef.current = null;
      setUnifiedControl(DEFAULT_UNIFIED_CONTROL);
      setDeformOperation("scale");
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
    if (!selectedMesh) return;
    const before = new Float32Array(selectedMesh.selectionWeights);
    selectedMesh.selectionWeights.fill(0);
    for (const key of nextKeys) {
      const source = parseWeightKey(key);
      if (!source) continue;
      const boneWeights = getOrCreateWeights(selectedMesh, source);
      for (let index = 0; index < selectedMesh.vertexCount; index++) {
        selectedMesh.selectionWeights[index] = Math.max(
          selectedMesh.selectionWeights[index],
          boneWeights[index] ?? 0,
        );
      }
    }
    commitSelectionHistory(before);
    setWeightVersion((version) => version + 1);
  };

  const handleItemHighlighted = (item: { key: string; label: string } | undefined) => {
    const next = item?.key ?? null;
    previewKeyRef.current = next;
    if (!selectedMesh) return;
    const regions = buildHighlightRegions(selectedMesh, next);
    simpleViewportRef.current?.updateColors(regions);
  };

  const updateAmount = (amount: number) => {
    setUnifiedControl((previous) => ({ ...previous, amount }));
    setWeightVersion((version) => version + 1);
  };

  const updateAxis = (axis: 0 | 1 | 2, value: number) => {
    setUnifiedControl((previous) => {
      const axisScale = [...previous.axisScale] as [number, number, number];
      axisScale[axis] = value;
      return { ...previous, axisScale };
    });
    setWeightVersion((version) => version + 1);
  };

  const updateTranslation = (axis: 0 | 1 | 2, value: number) => {
    setUnifiedControl((previous) => {
      const translation = [...previous.translation] as [number, number, number];
      translation[axis] = value;
      return { ...previous, translation };
    });
    setWeightVersion((version) => version + 1);
  };

  const updateTaperFactor = (taperFactor: number) => {
    setUnifiedControl((previous) => ({ ...previous, taperFactor }));
    setWeightVersion((version) => version + 1);
  };

  const resetSelected = () => {
    setUnifiedControl({
      ...DEFAULT_UNIFIED_CONTROL,
      axisScale: [...DEFAULT_AXIS_SCALE],
      translation: [0, 0, 0],
    });
    setWeightVersion((version) => version + 1);
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
        weights: deformOperation === "scale" ? displayWeights : undefined,
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
  const canUndoSelection = selectionHistoryRef.current.undo.length > 0;
  const canRedoSelection = selectionHistoryRef.current.redo.length > 0;

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
          brushProps={brushProps}
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
              brushEnabled={brushEnabled}
              brushMode={brushMode}
              brushRadius={brushRadius}
              brushStrength={brushStrength}
              brushMirrorX={brushMirrorX}
              onBrushStroke={handleBrushStroke}
              onBrushStrokeStart={handleBrushStrokeStart}
              onBrushStrokeEnd={handleBrushStrokeEnd}
              onBrushRadiusChange={setBrushRadius}
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
                          resetSelectionHistory();
                          previewKeyRef.current = null;
                          setUnifiedControl(DEFAULT_UNIFIED_CONTROL);
                          setDeformOperation("scale");
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
                    <FieldLabel>{t("page.tools.body_shape.selection_mask")}</FieldLabel>
                    <p className="mb-2 text-[11px] text-muted-foreground">
                      {t("page.tools.body_shape.selection_mask_hint")}
                    </p>
                    {useBones ? (
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
                            simpleViewportRef.current?.updateColors(activeRegions);
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
                                      : t("page.tools.body_shape.import_bones_placeholder")
                                  }
                                />
                              </>
                            )}
                          </ComboboxValue>
                        </ComboboxChips>
                        <ComboboxContent>
                          <ComboboxEmpty>{t("page.tools.body_shape.no_bones")}</ComboboxEmpty>
                          <ComboboxList>
                            {(item: (typeof selectableItems)[number]) => (
                              <ComboboxItem key={item.key} value={item}>
                                {item.label}
                              </ComboboxItem>
                            )}
                          </ComboboxList>
                        </ComboboxContent>
                      </Combobox>
                    ) : null}
                  </Field>

                  <Field>
                    <FieldLabel>{t("page.tools.body_shape.selection_tool")}</FieldLabel>
                    <Select
                      value={selectionToolMode}
                      onValueChange={(value) => {
                        if (value === "brush" || value === "component") {
                          setSelectionToolMode(value);
                        }
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="brush">
                            {t("page.tools.body_shape.tool_mode_brush")}
                          </SelectItem>
                          <SelectItem value="component" disabled={!selectedMesh.componentIds}>
                            {t("page.tools.body_shape.tool_mode_component")}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>

                  {selectionToolMode === "brush" ? (
                    <Field>
                      <FieldLabel>{t("page.tools.body_shape.brush_reach")}</FieldLabel>
                      <Select
                        value={brushReachMode}
                        onValueChange={(value) => {
                          if (value === "surface" || value === "through") {
                            setBrushReachMode(value);
                          }
                        }}
                      >
                        <SelectTrigger className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="surface">
                              {t("page.tools.body_shape.brush_reach_surface")}
                            </SelectItem>
                            <SelectItem value="through">
                              {t("page.tools.body_shape.brush_reach_through")}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">
                        {brushReachMode === "surface"
                          ? t("page.tools.body_shape.brush_reach_surface_hint")
                          : t("page.tools.body_shape.brush_reach_through_hint")}
                      </p>
                    </Field>
                  ) : null}

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
                  {!hasSelection ? (
                    <p className="text-xs text-muted-foreground">
                      {t("page.tools.body_shape.selection_empty_hint")}
                    </p>
                  ) : (
                    <div className="space-y-2 rounded-md border bg-background/50 p-3">
                      <div className="text-sm font-medium">
                        {t("page.tools.body_shape.selection_controls")}
                      </div>
                      <Field>
                        <FieldLabel>{t("page.tools.body_shape.deform_operation")}</FieldLabel>
                        <Select
                          value={deformOperation}
                          onValueChange={(value) => {
                            if (
                              value !== "scale" &&
                              value !== "inflate" &&
                              value !== "translate" &&
                              value !== "taper"
                            ) {
                              return;
                            }
                            setDeformOperation(value);
                            setUnifiedControl((previous) => ({
                              ...previous,
                              amount: 0,
                              translation: [0, 0, 0],
                            }));
                            setWeightVersion((version) => version + 1);
                          }}
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              <SelectItem value="scale">
                                {t("page.tools.body_shape.deform_scale")}
                              </SelectItem>
                              {selectedMesh.originalNormals ? (
                                <SelectItem value="inflate">
                                  {t("page.tools.body_shape.deform_inflate")}
                                </SelectItem>
                              ) : null}
                              <SelectItem value="translate">
                                {t("page.tools.body_shape.deform_translate")}
                              </SelectItem>
                              <SelectItem value="taper">
                                {t("page.tools.body_shape.deform_taper")}
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      </Field>
                      {deformOperation === "translate" ? (
                        <>
                          <RangeField
                            label={t("page.tools.body_shape.translate_x")}
                            value={unifiedControl.translation[0]}
                            defaultValue={0}
                            min={-meshExtent * 0.5}
                            max={meshExtent * 0.5}
                            step={meshExtent * 0.005}
                            display={unifiedControl.translation[0].toFixed(3)}
                            onChange={(value) => updateTranslation(0, value)}
                          />
                          <RangeField
                            label={t("page.tools.body_shape.translate_y")}
                            value={unifiedControl.translation[1]}
                            defaultValue={0}
                            min={-meshExtent * 0.5}
                            max={meshExtent * 0.5}
                            step={meshExtent * 0.005}
                            display={unifiedControl.translation[1].toFixed(3)}
                            onChange={(value) => updateTranslation(1, value)}
                          />
                          <RangeField
                            label={t("page.tools.body_shape.translate_z")}
                            value={unifiedControl.translation[2]}
                            defaultValue={0}
                            min={-meshExtent * 0.5}
                            max={meshExtent * 0.5}
                            step={meshExtent * 0.005}
                            display={unifiedControl.translation[2].toFixed(3)}
                            onChange={(value) => updateTranslation(2, value)}
                          />
                        </>
                      ) : (
                        <>
                          <RangeField
                            label={t("page.tools.body_shape.deform_amount")}
                            value={unifiedControl.amount}
                            defaultValue={DEFAULT_UNIFIED_CONTROL.amount}
                            min={
                              deformOperation === "inflate"
                                ? -0.25
                                : deformOperation === "taper"
                                  ? -1
                                  : -0.5
                            }
                            max={
                              deformOperation === "inflate"
                                ? 0.25
                                : deformOperation === "taper"
                                  ? 1
                                  : 0.5
                            }
                            step={0.01}
                            display={
                              deformOperation === "inflate"
                                ? unifiedControl.amount.toFixed(3)
                                : `${(unifiedControl.amount * 100).toFixed(0)}%`
                            }
                            onChange={updateAmount}
                          />
                          {deformOperation === "scale" ? (
                            <>
                              <RangeField
                                label={t("page.tools.body_shape.axis_x")}
                                value={unifiedControl.axisScale[0]}
                                defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[0]}
                                min={0}
                                max={2}
                                step={0.05}
                                display={unifiedControl.axisScale[0].toFixed(2)}
                                onChange={(value) => updateAxis(0, value)}
                              />
                              <RangeField
                                label={t("page.tools.body_shape.axis_y")}
                                value={unifiedControl.axisScale[1]}
                                defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[1]}
                                min={0}
                                max={2}
                                step={0.05}
                                display={unifiedControl.axisScale[1].toFixed(2)}
                                onChange={(value) => updateAxis(1, value)}
                              />
                              <RangeField
                                label={t("page.tools.body_shape.axis_z")}
                                value={unifiedControl.axisScale[2]}
                                defaultValue={DEFAULT_UNIFIED_CONTROL.axisScale[2]}
                                min={0}
                                max={2}
                                step={0.05}
                                display={unifiedControl.axisScale[2].toFixed(2)}
                                onChange={(value) => updateAxis(2, value)}
                              />
                            </>
                          ) : null}
                          {deformOperation === "taper" ? (
                            <RangeField
                              label={t("page.tools.body_shape.taper_factor")}
                              value={unifiedControl.taperFactor}
                              defaultValue={DEFAULT_UNIFIED_CONTROL.taperFactor}
                              min={0}
                              max={2}
                              step={0.05}
                              display={unifiedControl.taperFactor.toFixed(2)}
                              onChange={updateTaperFactor}
                            />
                          ) : null}
                        </>
                      )}
                    </div>
                  )}

                  <div className="grid grid-cols-2 gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      onClick={undoSelection}
                      disabled={!canUndoSelection}
                    >
                      <Undo2Icon className="size-4" />
                      {t("page.tools.body_shape.undo_selection")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={redoSelection}
                      disabled={!canRedoSelection}
                    >
                      <Redo2Icon className="size-4" />
                      {t("page.tools.body_shape.redo_selection")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={growSelection}
                      disabled={!hasSelection || !selectedMesh.adjacency}
                    >
                      <Maximize2Icon className="size-4" />
                      {t("page.tools.body_shape.grow_selection")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={shrinkSelection}
                      disabled={!hasSelection || !selectedMesh.adjacency}
                    >
                      <Minimize2Icon className="size-4" />
                      {t("page.tools.body_shape.shrink_selection")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={smoothSelection}
                      disabled={!hasSelection || !selectedMesh.adjacency}
                    >
                      {t("page.tools.body_shape.smooth_selection")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={resetSelected}
                      disabled={!hasSelection}
                    >
                      {t("page.tools.body_shape.reset_selected")}
                    </Button>
                  </div>

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

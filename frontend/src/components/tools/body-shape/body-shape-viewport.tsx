import { OrbitControls } from "@react-three/drei";
import { Canvas, useThree } from "@react-three/fiber";
import { parseOrientation } from "@renderer/components/tools/model-viewer/model-viewer-contract";
import type { BrushMode } from "@renderer/components/tools/model-viewer/model-viewer-menu-bar";
import { createThreeRenderer } from "@renderer/components/tools/three-renderer";
import {
  composeDisplayWeightsInto,
  writeWeightColors,
  type ActiveRegionDeform,
} from "@shared/body-shape";
import {
  type ElementRef,
  forwardRef,
  type MutableRefObject,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";
import {
  BufferAttribute,
  BufferGeometry,
  Color,
  DoubleSide,
  Euler,
  LineBasicMaterial,
  LineLoop,
  MathUtils,
  Mesh,
  MeshStandardMaterial,
  Quaternion,
  Vector2,
  Vector3,
} from "three";

export type BrushStrokeInput = {
  localPoint: [number, number, number];
  localNormal?: [number, number, number];
  vertexIndices?: [number, number, number];
};

export type BrushPointerSample = {
  clientX: number;
  clientY: number;
  paint: boolean;
};

export function enqueueBrushPointerSample(
  pending: BrushPointerSample[],
  sample: BrushPointerSample,
): void {
  if (sample.paint || pending.at(-1)?.paint) {
    pending.push(sample);
    return;
  }
  if (pending.length === 0) {
    pending.push(sample);
    return;
  }
  pending[pending.length - 1] = sample;
}

export type BodyShapeViewportHandle = {
  resetCamera: () => void;
  /** Imperatively refresh the weight heatmap without triggering a React re-render. */
  updateColors: (regions: ActiveRegionDeform[]) => void;
};

export type BodyShapeViewportProps = {
  originalPositions: Float32Array;
  previewPositions: Float32Array;
  /** Regions used for deformation and weight heatmap. */
  regions: ActiveRegionDeform[];
  indices?: Uint32Array;
  showOriginal: boolean;
  showWeights: boolean;
  /** Bumped when region selection or amounts change. */
  weightVersion: number;
  /** Bumped only after the parent has written new preview positions. */
  positionsVersion?: number;
  /** Whether the parent preview currently differs from the original geometry. */
  positionsChanged: boolean;
  /** Stable identity for the mesh frame; changing mask data should not reframe the camera. */
  frameKey?: string;
  orientation?: string;

  /* Brush props */
  brushEnabled?: boolean;
  brushMode?: BrushMode;
  brushRadius?: number;
  brushStrength?: number;
  brushMirrorX?: boolean;
  onBrushStroke?: (stroke: BrushStrokeInput) => void;
  onBrushStrokeStart?: () => void;
  onBrushStrokeEnd?: () => void;
  onBrushRadiusChange?: (radius: number) => void;
};

type OrbitControlsImpl = ElementRef<typeof OrbitControls>;

const bodyShapeRenderer = createThreeRenderer({ alpha: true, antialias: true });

export const BodyShapeViewport = forwardRef<BodyShapeViewportHandle, BodyShapeViewportProps>(
  function BodyShapeViewport(props, ref) {
    const controlsRef = useRef<OrbitControlsImpl | null>(null);
    const resetCameraRef = useRef<(() => void) | null>(null);
    const updateColorsRef = useRef<((regions: ActiveRegionDeform[]) => void) | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        resetCamera: () => resetCameraRef.current?.(),
        updateColors: (regions) => updateColorsRef.current?.(regions),
      }),
      [],
    );

    const rotation = useMemo(() => {
      const [roll, pitch, yaw] = parseOrientation(props.orientation ?? "0deg 0deg 0deg");
      return new Euler(
        MathUtils.degToRad(pitch),
        MathUtils.degToRad(yaw),
        MathUtils.degToRad(roll),
        "YXZ",
      );
    }, [props.orientation]);

    return (
      <div className="h-full min-h-80 w-full rounded-md border border-border bg-background">
        <Canvas
          frameloop="demand"
          camera={{ position: [0, 1.2, 2.4], fov: 45, near: 0.01, far: 500 }}
          dpr={Math.min(window.devicePixelRatio, 2)}
          gl={bodyShapeRenderer}
        >
          <color attach="background" args={["#12141a"]} />
          <ambientLight intensity={0.55} />
          <hemisphereLight intensity={0.7} groundColor="#3a3f4a" position={[0, 1, 0]} />
          <directionalLight intensity={1.4} position={[4, 6, 5]} />
          <directionalLight intensity={0.5} position={[-4, 2, -3]} />
          <group rotation={rotation}>
            <BodyShapeMesh
              {...props}
              controlsRef={controlsRef}
              onRegisterReset={(reset) => {
                resetCameraRef.current = reset;
              }}
              onRegisterUpdateColors={(update) => {
                updateColorsRef.current = update;
              }}
            />
          </group>
          <DemandOrbitControls controlsRef={controlsRef} />
        </Canvas>
      </div>
    );
  },
);

function DemandOrbitControls({
  controlsRef,
}: {
  controlsRef: MutableRefObject<OrbitControlsImpl | null>;
}) {
  const invalidate = useThree((state) => state.invalidate);
  return (
    <OrbitControls
      ref={controlsRef}
      makeDefault
      enableDamping
      dampingFactor={0.12}
      onChange={() => invalidate()}
    />
  );
}

function BodyShapeMesh({
  originalPositions,
  previewPositions,
  regions,
  indices,
  showOriginal,
  showWeights,
  weightVersion,
  positionsVersion = weightVersion,
  positionsChanged,
  frameKey,
  brushEnabled = false,
  brushRadius = 0.15,
  brushMode = "paint",
  onBrushStroke,
  onBrushStrokeStart,
  onBrushStrokeEnd,
  onBrushRadiusChange,
  controlsRef,
  onRegisterReset,
  onRegisterUpdateColors,
}: BodyShapeViewportProps & {
  controlsRef: MutableRefObject<OrbitControlsImpl | null>;
  onRegisterReset: (reset: () => void) => void;
  onRegisterUpdateColors: (update: (regions: ActiveRegionDeform[]) => void) => void;
}) {
  const meshRef = useRef<Mesh>(null);
  const brushRingRef = useRef<LineLoop>(null);
  const framedKeyRef = useRef<string | Float32Array | null>(null);
  const isPaintingRef = useRef(false);
  const pointerFrameRef = useRef<number | null>(null);
  const pendingPointersRef = useRef<BrushPointerSample[]>([]);
  const finishStrokeRef = useRef(false);
  const { camera, raycaster, gl, invalidate } = useThree();
  const appliedPositionsVersionRef = useRef<number | null>(null);
  const hadPositionDeformationRef = useRef(false);
  const heatmapRegionsRef = useRef<ActiveRegionDeform[]>(regions);
  const onBrushStrokeRef = useRef(onBrushStroke);
  const onBrushStrokeStartRef = useRef(onBrushStrokeStart);
  const onBrushStrokeEndRef = useRef(onBrushStrokeEnd);
  const brushRadiusRef = useRef(brushRadius);
  const onBrushRadiusChangeRef = useRef(onBrushRadiusChange);

  const initialColors = useMemo(
    () => new Float32Array(Math.floor(originalPositions.length / 3) * 3),
    [originalPositions],
  );
  const colorsRef = useRef(initialColors);
  const displayWeightsRef = useRef(new Float32Array(initialColors.length / 3));
  useLayoutEffect(() => {
    colorsRef.current = initialColors;
    displayWeightsRef.current = new Float32Array(initialColors.length / 3);
  }, [initialColors]);

  useEffect(() => {
    heatmapRegionsRef.current = regions;
    onBrushStrokeRef.current = onBrushStroke;
    onBrushStrokeStartRef.current = onBrushStrokeStart;
    onBrushStrokeEndRef.current = onBrushStrokeEnd;
    brushRadiusRef.current = brushRadius;
    onBrushRadiusChangeRef.current = onBrushRadiusChange;
  });

  const writeColors = (regions: ActiveRegionDeform[]) => {
    const vertexCount = Math.floor(originalPositions.length / 3);
    composeDisplayWeightsInto(displayWeightsRef.current, vertexCount, showOriginal ? [] : regions, {
      ignoreAmount: true,
    });
    writeWeightColors(displayWeightsRef.current, colorsRef.current);
    const colorAttr = meshRef.current?.geometry.getAttribute("color") as
      | BufferAttribute
      | undefined;
    if (colorAttr) colorAttr.needsUpdate = true;
    invalidate();
  };

  useEffect(() => {
    onRegisterUpdateColors(writeColors);
  });

  const geometry = useMemo(() => {
    const geo = new BufferGeometry();
    const positionAttr = new BufferAttribute(previewPositions, 3);
    positionAttr.setUsage(35048);
    geo.setAttribute("position", positionAttr);

    const colorAttr = new BufferAttribute(initialColors, 3);
    colorAttr.setUsage(35048);
    geo.setAttribute("color", colorAttr);

    if (indices && indices.length > 0) {
      geo.setIndex(new BufferAttribute(indices, 1));
    }
    geo.computeVertexNormals();
    geo.computeBoundingSphere();
    return geo;
  }, [indices, initialColors, originalPositions, previewPositions]);

  useEffect(() => {
    const versionChanged = appliedPositionsVersionRef.current !== positionsVersion;
    const isPositionsChanged =
      versionChanged && (positionsChanged || hadPositionDeformationRef.current);
    appliedPositionsVersionRef.current = positionsVersion;
    hadPositionDeformationRef.current = positionsChanged;

    const vertexCount = Math.floor(originalPositions.length / 3);
    if (isPositionsChanged) {
      const positionAttr = geometry.getAttribute("position") as BufferAttribute;
      positionAttr.needsUpdate = true;
    }

    composeDisplayWeightsInto(
      displayWeightsRef.current,
      vertexCount,
      showOriginal ? [] : heatmapRegionsRef.current,
      { ignoreAmount: true },
    );
    writeWeightColors(displayWeightsRef.current, colorsRef.current);
    const colorAttr = geometry.getAttribute("color") as BufferAttribute;
    colorAttr.needsUpdate = true;

    if (isPositionsChanged) {
      geometry.computeVertexNormals();
      geometry.computeBoundingSphere();
    }
    invalidate();
  }, [
    geometry,
    initialColors,
    originalPositions,
    previewPositions,
    regions,
    showOriginal,
    weightVersion,
    positionsVersion,
    positionsChanged,
    invalidate,
  ]);

  const material = useMemo(() => {
    return new MeshStandardMaterial({
      vertexColors: showWeights,
      color: showWeights ? new Color("#ffffff") : new Color("#9aa4b2"),
      roughness: 0.55,
      metalness: 0.05,
      side: DoubleSide,
      flatShading: false,
    });
  }, [showWeights]);

  const brushRingGeometry = useMemo(() => {
    const segments = 32;
    const ringGeo = new BufferGeometry();
    const positions = new Float32Array((segments + 1) * 3);
    for (let i = 0; i <= segments; i++) {
      const theta = (i / segments) * Math.PI * 2;
      positions[i * 3] = Math.cos(theta) * brushRadius;
      positions[i * 3 + 1] = Math.sin(theta) * brushRadius;
      positions[i * 3 + 2] = 0;
    }
    ringGeo.setAttribute("position", new BufferAttribute(positions, 3));
    return ringGeo;
  }, [brushRadius]);

  const brushRingMaterial = useMemo(() => {
    return new LineBasicMaterial({
      color: brushMode === "erase" ? 0xef4444 : 0x3b82f6,
      linewidth: 2,
      depthTest: false,
      transparent: true,
      opacity: 0.85,
    });
  }, [brushMode]);

  useEffect(() => () => geometry.dispose(), [geometry]);
  useEffect(() => () => material.dispose(), [material]);
  useEffect(() => () => brushRingGeometry.dispose(), [brushRingGeometry]);
  useEffect(() => () => brushRingMaterial.dispose(), [brushRingMaterial]);

  const frameCamera = () => {
    if (!geometry.boundingSphere) return;
    const center = geometry.boundingSphere.center;
    const radius = Math.max(geometry.boundingSphere.radius, 0.1);
    camera.position.copy(new Vector3(radius * 1.6, radius * 1.1, radius * 2.2).add(center));
    camera.lookAt(center);
    camera.updateProjectionMatrix();
    const controls = controlsRef.current;
    if (controls) {
      controls.target.copy(center);
      controls.update();
    }
    invalidate();
  };

  useEffect(() => {
    onRegisterReset(frameCamera);
  });

  useEffect(() => {
    const nextFrameKey = frameKey ?? originalPositions;
    if (framedKeyRef.current === nextFrameKey) return;
    framedKeyRef.current = nextFrameKey;
    frameCamera();
  }, [camera, frameKey, geometry, originalPositions, invalidate]);

  /* Brush Raycasting & Pointer Handlers */
  useEffect(() => {
    if (!brushEnabled) {
      if (brushRingRef.current) brushRingRef.current.visible = false;
      return;
    }

    const canvasElement = gl.domElement;

    const performStrokeAtPointer = (pointer: BrushPointerSample) => {
      if (!meshRef.current || !onBrushStrokeRef.current) return;
      const rect = canvasElement.getBoundingClientRect();
      const x = ((pointer.clientX - rect.left) / rect.width) * 2 - 1;
      const y = -((pointer.clientY - rect.top) / rect.height) * 2 + 1;

      raycaster.setFromCamera(new Vector2(x, y), camera);
      const intersects = raycaster.intersectObject(meshRef.current);

      if (intersects.length > 0) {
        const hit = intersects[0]!;
        const localPointVector = meshRef.current.worldToLocal(hit.point.clone());
        const localPoint: [number, number, number] = [
          localPointVector.x,
          localPointVector.y,
          localPointVector.z,
        ];
        const localNormal: [number, number, number] | undefined = hit.face
          ? [hit.face.normal.x, hit.face.normal.y, hit.face.normal.z]
          : undefined;

        if (brushRingRef.current) {
          brushRingRef.current.visible = true;
          brushRingRef.current.position.copy(localPointVector);
          if (hit.face) {
            const up = new Vector3(0, 0, 1);
            const q = new Quaternion().setFromUnitVectors(up, hit.face.normal);
            brushRingRef.current.quaternion.copy(q);
            brushRingRef.current.position.addScaledVector(hit.face.normal, 0.001);
          }
          invalidate();
        }

        if (pointer.paint) {
          onBrushStrokeRef.current({
            localPoint,
            localNormal,
            vertexIndices: hit.face ? [hit.face.a, hit.face.b, hit.face.c] : undefined,
          });
        }
      } else {
        if (brushRingRef.current) brushRingRef.current.visible = false;
        invalidate();
      }
    };

    const flushPointer = () => {
      pointerFrameRef.current = null;
      const pointers = pendingPointersRef.current.splice(0);
      for (const pointer of pointers) performStrokeAtPointer(pointer);
      if (finishStrokeRef.current) {
        finishStrokeRef.current = false;
        onBrushStrokeEndRef.current?.();
      }
    };

    const queuePointer = (event: PointerEvent) => {
      enqueueBrushPointerSample(pendingPointersRef.current, {
        clientX: event.clientX,
        clientY: event.clientY,
        paint: isPaintingRef.current,
      });
      if (pointerFrameRef.current === null) {
        pointerFrameRef.current = requestAnimationFrame(flushPointer);
      }
    };

    const handlePointerDown = (event: PointerEvent) => {
      if (event.button === 0 && !event.altKey) {
        if (finishStrokeRef.current) {
          if (pointerFrameRef.current !== null) cancelAnimationFrame(pointerFrameRef.current);
          flushPointer();
        }
        isPaintingRef.current = true;
        onBrushStrokeStartRef.current?.();
        if (controlsRef.current) controlsRef.current.enabled = false;
        queuePointer(event);
      }
    };

    const handlePointerMove = (event: PointerEvent) => {
      queuePointer(event);
    };

    const handlePointerUp = () => {
      if (isPaintingRef.current) {
        if (pendingPointersRef.current.length > 0 || pointerFrameRef.current !== null) {
          finishStrokeRef.current = true;
        } else {
          onBrushStrokeEndRef.current?.();
        }
      }
      isPaintingRef.current = false;
      if (controlsRef.current) controlsRef.current.enabled = true;
    };

    const handleWheel = (event: WheelEvent) => {
      const updateBrushRadius = onBrushRadiusChangeRef.current;
      if (brushEnabled && event.ctrlKey && updateBrushRadius) {
        event.preventDefault();
        const delta = event.deltaY < 0 ? 0.01 : -0.01;
        const nextRadius = Math.max(0.01, Math.min(2.0, brushRadiusRef.current + delta));
        updateBrushRadius(nextRadius);
      }
    };

    canvasElement.addEventListener("pointerdown", handlePointerDown);
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    canvasElement.addEventListener("wheel", handleWheel, { passive: false });

    return () => {
      canvasElement.removeEventListener("pointerdown", handlePointerDown);
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      canvasElement.removeEventListener("wheel", handleWheel);
      if (pointerFrameRef.current !== null) cancelAnimationFrame(pointerFrameRef.current);
      pointerFrameRef.current = null;
      pendingPointersRef.current.length = 0;
      finishStrokeRef.current = false;
      isPaintingRef.current = false;
      if (controlsRef.current) controlsRef.current.enabled = true;
    };
  }, [brushEnabled, camera, frameKey, gl, raycaster, controlsRef, invalidate]);

  return (
    <>
      <mesh ref={meshRef} geometry={geometry} material={material} dispose={null} />
      <lineLoop
        ref={brushRingRef}
        geometry={brushRingGeometry}
        material={brushRingMaterial}
        visible={false}
        dispose={null}
      />
    </>
  );
}

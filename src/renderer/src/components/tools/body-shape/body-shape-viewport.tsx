import { OrbitControls } from "@react-three/drei";
import { Canvas, useThree } from "@react-three/fiber";
import { parseOrientation } from "@renderer/components/tools/model-viewer/model-viewer-contract";
import type { BrushMode } from "@renderer/components/tools/model-viewer/model-viewer-menu-bar";
import {
  applyMultiRegionDeform,
  composeDisplayWeights,
  writeWeightColors,
  type ActiveRegionDeform,
} from "@shared/body-shape";
import {
  type ElementRef,
  forwardRef,
  type MutableRefObject,
  useEffect,
  useImperativeHandle,
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
  Matrix3,
  Mesh,
  MeshStandardMaterial,
  Quaternion,
  Vector2,
  Vector3,
} from "three";

export type BrushStrokeInput = {
  hitPoint: [number, number, number];
  hitNormal?: [number, number, number];
};

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
  /** When false, positions are unchanged since the last apply — skip deform and normal recomputation. */
  positionsChanged: boolean;
  orientation?: string;

  /* Brush props */
  brushEnabled?: boolean;
  brushMode?: BrushMode;
  brushRadius?: number;
  brushStrength?: number;
  brushMirrorX?: boolean;
  onBrushStroke?: (stroke: BrushStrokeInput) => void;
  onBrushRadiusChange?: (radius: number) => void;
};

type OrbitControlsImpl = ElementRef<typeof OrbitControls>;

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
      <div className="h-full min-h-[320px] w-full rounded-md border border-border bg-background">
        <Canvas
          camera={{ position: [0, 1.2, 2.4], fov: 45, near: 0.01, far: 500 }}
          dpr={Math.min(window.devicePixelRatio, 2)}
          gl={{ antialias: true, alpha: true }}
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
          <OrbitControls ref={controlsRef} makeDefault enableDamping dampingFactor={0.12} />
        </Canvas>
      </div>
    );
  },
);

function BodyShapeMesh({
  originalPositions,
  previewPositions,
  regions,
  indices,
  showOriginal,
  showWeights,
  weightVersion,
  positionsChanged,
  brushEnabled = false,
  brushRadius = 0.15,
  brushMode = "paint",
  onBrushStroke,
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
  const colorsRef = useRef(new Float32Array(Math.floor(originalPositions.length / 3) * 3));
  const framedKeyRef = useRef<Float32Array | null>(null);
  const isPaintingRef = useRef(false);
  const { camera, raycaster, gl } = useThree();
  const heatmapRegionsRef = useRef<ActiveRegionDeform[]>(regions);
  heatmapRegionsRef.current = regions;
  const onBrushStrokeRef = useRef(onBrushStroke);
  onBrushStrokeRef.current = onBrushStroke;

  const writeColors = (regions: ActiveRegionDeform[]) => {
    const vertexCount = Math.floor(originalPositions.length / 3);
    const displayWeights = composeDisplayWeights(vertexCount, showOriginal ? [] : regions, {
      ignoreAmount: true,
    });
    writeWeightColors(displayWeights, colorsRef.current);
    const colorAttr = meshRef.current?.geometry.getAttribute("color") as
      | BufferAttribute
      | undefined;
    if (colorAttr) colorAttr.needsUpdate = true;
  };

  useEffect(() => {
    onRegisterUpdateColors(writeColors);
  });

  const geometry = useMemo(() => {
    const geo = new BufferGeometry();
    const positionAttr = new BufferAttribute(previewPositions, 3);
    positionAttr.setUsage(35048);
    geo.setAttribute("position", positionAttr);

    const colorAttr = new BufferAttribute(colorsRef.current, 3);
    colorAttr.setUsage(35048);
    geo.setAttribute("color", colorAttr);

    if (indices && indices.length > 0) {
      geo.setIndex(new BufferAttribute(indices, 1));
    }
    geo.computeVertexNormals();
    geo.computeBoundingSphere();
    return geo;
  }, [indices, originalPositions, previewPositions]);

  useEffect(() => {
    const vertexCount = Math.floor(originalPositions.length / 3);
    if (colorsRef.current.length !== vertexCount * 3) {
      colorsRef.current = new Float32Array(vertexCount * 3);
      geometry.setAttribute("color", new BufferAttribute(colorsRef.current, 3));
    }

    if (positionsChanged) {
      if (showOriginal || regions.length === 0) {
        previewPositions.set(originalPositions);
      } else {
        applyMultiRegionDeform({
          originalPositions,
          previewPositions,
          regions,
        });
      }

      const positionAttr = geometry.getAttribute("position") as BufferAttribute;
      positionAttr.needsUpdate = true;
    }

    const displayWeights = composeDisplayWeights(
      vertexCount,
      showOriginal ? [] : heatmapRegionsRef.current,
      { ignoreAmount: true },
    );
    writeWeightColors(displayWeights, colorsRef.current);
    const colorAttr = geometry.getAttribute("color") as BufferAttribute;
    colorAttr.needsUpdate = true;

    if (positionsChanged) {
      geometry.computeVertexNormals();
      geometry.computeBoundingSphere();
    }
  }, [
    geometry,
    originalPositions,
    previewPositions,
    regions,
    showOriginal,
    weightVersion,
    positionsChanged,
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

  useEffect(() => {
    return () => {
      geometry.dispose();
      material.dispose();
      brushRingGeometry.dispose();
      brushRingMaterial.dispose();
    };
  }, [geometry, material, brushRingGeometry, brushRingMaterial]);

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
  };

  useEffect(() => {
    onRegisterReset(frameCamera);
  });

  useEffect(() => {
    if (framedKeyRef.current === originalPositions) return;
    framedKeyRef.current = originalPositions;
    frameCamera();
  }, [camera, geometry, originalPositions]);

  /* Brush Raycasting & Pointer Handlers */
  useEffect(() => {
    if (!brushEnabled) {
      if (brushRingRef.current) brushRingRef.current.visible = false;
      return;
    }

    const canvasElement = gl.domElement;

    const performStrokeAtPointer = (event: PointerEvent) => {
      if (!meshRef.current || !onBrushStrokeRef.current) return;
      const rect = canvasElement.getBoundingClientRect();
      const x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      const y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

      raycaster.setFromCamera(new Vector2(x, y), camera);
      const intersects = raycaster.intersectObject(meshRef.current);

      if (intersects.length > 0) {
        const hit = intersects[0]!;
        const hitPoint: [number, number, number] = [hit.point.x, hit.point.y, hit.point.z];
        let hitNormal: [number, number, number] | undefined;
        if (hit.face) {
          const normalMatrix = new Matrix3().getNormalMatrix(meshRef.current.matrixWorld);
          const worldNormal = hit.face.normal.clone().applyNormalMatrix(normalMatrix).normalize();
          hitNormal = [worldNormal.x, worldNormal.y, worldNormal.z];
        }

        if (brushRingRef.current) {
          brushRingRef.current.visible = true;
          brushRingRef.current.position.copy(hit.point);
          if (hit.face) {
            const normalMatrix = new Matrix3().getNormalMatrix(meshRef.current.matrixWorld);
            const worldNormal = hit.face.normal.clone().applyNormalMatrix(normalMatrix).normalize();
            const up = new Vector3(0, 0, 1);
            const q = new Quaternion().setFromUnitVectors(up, worldNormal);
            brushRingRef.current.quaternion.copy(q);
            brushRingRef.current.position.addScaledVector(worldNormal, 0.001);
          }
        }

        if (isPaintingRef.current) {
          onBrushStrokeRef.current({ hitPoint, hitNormal });
        }
      } else {
        if (brushRingRef.current) brushRingRef.current.visible = false;
      }
    };

    const handlePointerDown = (event: PointerEvent) => {
      if (event.button === 0 && !event.altKey) {
        isPaintingRef.current = true;
        if (controlsRef.current) controlsRef.current.enabled = false;
        performStrokeAtPointer(event);
      }
    };

    const handlePointerMove = (event: PointerEvent) => {
      performStrokeAtPointer(event);
    };

    const handlePointerUp = () => {
      isPaintingRef.current = false;
      if (controlsRef.current) controlsRef.current.enabled = true;
    };

    const handleWheel = (event: WheelEvent) => {
      if (brushEnabled && event.ctrlKey && onBrushRadiusChange) {
        event.preventDefault();
        const delta = event.deltaY < 0 ? 0.01 : -0.01;
        const nextRadius = Math.max(0.01, Math.min(2.0, brushRadius + delta));
        onBrushRadiusChange(nextRadius);
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
      if (controlsRef.current) controlsRef.current.enabled = true;
    };
  }, [brushEnabled, brushRadius, camera, gl, raycaster, controlsRef, onBrushRadiusChange]);

  return (
    <>
      <mesh ref={meshRef} geometry={geometry} material={material} />
      <lineLoop
        ref={brushRingRef}
        geometry={brushRingGeometry}
        material={brushRingMaterial}
        visible={false}
      />
    </>
  );
}

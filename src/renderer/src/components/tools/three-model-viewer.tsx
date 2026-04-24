import { OrbitControls } from "@react-three/drei";
import { Canvas, useThree } from "@react-three/fiber";
import { cn } from "@renderer/lib/utils";
import {
  type MutableRefObject,
  forwardRef,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  BufferAttribute,
  BufferGeometry,
  Box3,
  type Camera,
  DoubleSide,
  Euler,
  FrontSide,
  Group,
  MathUtils,
  Mesh,
  MeshStandardMaterial,
  Object3D,
  PerspectiveCamera,
  SRGBColorSpace,
  Texture,
  Vector3,
} from "three";
import type { OrbitControls as OrbitControlsImpl } from "three-stdlib";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type {
  ModelViewerHandle,
  ModelViewerRealtimeShapeKey,
  ModelViewerSurfaceProps,
} from "./model-viewer-contract";
import { parseOrientation } from "./model-viewer-contract";
import type { ModelViewerCameraState } from "./model-viewer-contract";
import { modelViewerSourceToUrl } from "./model-viewer-session";

const DEFAULT_CAMERA_POSITION = new Vector3(0, 0, 4);

type LoadedShapeKey = {
  metadata: ModelViewerRealtimeShapeKey;
  base: Float32Array;
  dimensions: Array<{
    variableId: string;
    smaller: Float32Array;
    bigger: Float32Array;
  }>;
};

export const ThreeModelViewer = forwardRef<ModelViewerHandle, ModelViewerSurfaceProps>(
  function ThreeModelViewer(
    { className, onError, onLoad, orientation, shapeKeys, src, variantState },
    ref,
  ) {
    const controllerRef = useRef<ModelViewerHandle | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        captureCameraState: () => controllerRef.current?.captureCameraState() ?? null,
        restoreCameraState: (state, options) =>
          controllerRef.current?.restoreCameraState(state, options),
        setDoubleSided: (doubleSided) => controllerRef.current?.setDoubleSided(doubleSided),
        updateFraming: () => controllerRef.current?.updateFraming(),
      }),
      [],
    );

    return (
      <div className={cn("h-full w-full", className)}>
        <Canvas
          style={{ background: "transparent" }}
          camera={{ far: 1000, fov: 45, near: 0.01, position: DEFAULT_CAMERA_POSITION.toArray() }}
          dpr={window.devicePixelRatio}
          gl={{ alpha: true, antialias: true }}
        >
          <ambientLight intensity={1.8} />
          <directionalLight intensity={2.8} position={[6, 8, 10]} />
          <directionalLight intensity={1.2} position={[-6, 4, -8]} />
          <ThreeModelScene
            controllerRef={controllerRef}
            orientation={orientation}
            shapeKeys={shapeKeys}
            src={src}
            onError={onError}
            onLoad={onLoad}
            variantState={variantState}
          />
        </Canvas>
      </div>
    );
  },
);

function ThreeModelScene({
  controllerRef,
  onError,
  onLoad,
  orientation,
  shapeKeys,
  src,
  variantState,
}: ModelViewerSurfaceProps & {
  controllerRef: MutableRefObject<ModelViewerHandle | null>;
}) {
  const { camera, gl, invalidate } = useThree();
  const [modelRoot, setModelRoot] = useState<Object3D | null>(null);
  const controlsRef = useRef<OrbitControlsImpl | null>(null);
  const groupRef = useRef<Group | null>(null);
  const activeObjectRef = useRef<Object3D | null>(null);
  const materialRef = useRef<MeshStandardMaterial[]>([]);
  const orientedCenterRef = useRef<Vector3 | null>(null);
  const pendingLoadIdRef = useRef(0);
  const onLoadRef = useRef(onLoad);
  const onErrorRef = useRef(onError);
  const shapeKeyCacheRef = useRef<Map<string, Promise<Float32Array>>>(new Map());
  const lastAppliedVariantSnapshotRef = useRef<Record<string, number | string> | null>(null);
  const lastAppliedModelRootRef = useRef<Object3D | null>(null);
  const lastAppliedShapeKeysRef = useRef<ModelViewerRealtimeShapeKey[] | undefined>(undefined);

  const rotation = useMemo(() => {
    const [roll, pitch, yaw] = parseOrientation(orientation);
    return new Euler(
      MathUtils.degToRad(pitch),
      MathUtils.degToRad(yaw),
      MathUtils.degToRad(roll),
      "YXZ",
    );
  }, [orientation]);

  useEffect(() => {
    gl.outputColorSpace = SRGBColorSpace;
    gl.setClearAlpha(0);
  }, [gl]);

  useEffect(() => {
    onLoadRef.current = onLoad;
  }, [onLoad]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  useEffect(() => {
    const loadId = pendingLoadIdRef.current + 1;
    pendingLoadIdRef.current = loadId;

    const loader = new GLTFLoader();
    let disposed = false;

    if (!src) {
      if (activeObjectRef.current) {
        disposeObjectTree(activeObjectRef.current);
      }
      setModelRoot(null);
      activeObjectRef.current = null;
      orientedCenterRef.current = null;
      materialRef.current = [];
      return () => {
        disposed = true;
      };
    }

    setModelRoot((current) => {
      if (current) {
        disposeObjectTree(current);
      }
      orientedCenterRef.current = null;
      return null;
    });

    loader.load(
      src,
      (gltf) => {
        if (disposed || pendingLoadIdRef.current !== loadId) {
          disposeObjectTree(gltf.scene);
          return;
        }

        const nextRoot = gltf.scene;
        materialRef.current = collectStandardMaterials(nextRoot);
        activeObjectRef.current = nextRoot;
        setModelRoot(nextRoot);
      },
      undefined,
      (error) => {
        if (disposed || pendingLoadIdRef.current !== loadId) {
          return;
        }

        activeObjectRef.current = null;
        materialRef.current = [];
        onErrorRef.current?.(error);
      },
    );

    return () => {
      disposed = true;
    };
  }, [src]);

  useLayoutEffect(() => {
    if (!modelRoot) {
      return;
    }

    if (!onLoadRef.current) {
      void fitCameraToObject({
        camera,
        controls: controlsRef.current,
        object: groupRef.current,
      }).then((center) => {
        orientedCenterRef.current = center;
      });
    }
    invalidate();
    onLoadRef.current?.();
  }, [camera, invalidate, modelRoot]);

  useEffect(() => {
    invalidate();
  }, [invalidate, rotation]);

  useLayoutEffect(() => {
    if (!modelRoot || !groupRef.current || !controlsRef.current) {
      return;
    }

    groupRef.current.updateMatrixWorld(true);
    const nextCenter = getObjectCenter(groupRef.current);
    if (!nextCenter) {
      return;
    }

    const previousCenter = orientedCenterRef.current;
    orientedCenterRef.current = nextCenter.clone();
    if (!previousCenter) {
      return;
    }

    const delta = nextCenter.clone().sub(previousCenter);
    if (delta.lengthSq() === 0) {
      return;
    }

    controlsRef.current.target.add(delta);
    camera.position.add(delta);
    controlsRef.current.update();
    invalidate();
  }, [camera, invalidate, modelRoot, rotation]);

  useEffect(() => {
    controllerRef.current = {
      captureCameraState: () =>
        captureThreeCameraState(camera, controlsRef.current, groupRef.current),
      restoreCameraState: (state, options) =>
        restoreThreeCameraState(camera, controlsRef.current, groupRef.current, state, options),
      setDoubleSided: (doubleSided) => {
        for (const material of materialRef.current) {
          material.side = doubleSided ? DoubleSide : FrontSide;
          material.needsUpdate = true;
        }
        invalidate();
      },
      updateFraming: async () => {
        const center = await fitCameraToObject({
          camera,
          controls: controlsRef.current,
          object: groupRef.current,
        });
        orientedCenterRef.current = center;
        invalidate();
      },
    };

    return () => {
      controllerRef.current = null;
    };
  }, [camera, controllerRef, invalidate]);

  useEffect(() => {
    return () => {
      if (activeObjectRef.current) {
        disposeObjectTree(activeObjectRef.current);
        activeObjectRef.current = null;
      }
      materialRef.current = [];
      lastAppliedVariantSnapshotRef.current = null;
      lastAppliedModelRootRef.current = null;
      lastAppliedShapeKeysRef.current = undefined;
    };
  }, []);

  useEffect(() => {
    if (!modelRoot || !shapeKeys?.length) {
      lastAppliedVariantSnapshotRef.current = null;
      lastAppliedModelRootRef.current = modelRoot;
      lastAppliedShapeKeysRef.current = shapeKeys;
      return;
    }

    const currentVariantSnapshot = variantState ? { ...variantState } : null;
    const didApplySameInputs =
      lastAppliedModelRootRef.current === modelRoot &&
      lastAppliedShapeKeysRef.current === shapeKeys &&
      areVariantSnapshotsEqual(lastAppliedVariantSnapshotRef.current, currentVariantSnapshot);
    if (didApplySameInputs) {
      return;
    }

    let cancelled = false;

    void (async () => {
      const loadedShapeKeys = (
        await Promise.all(
          shapeKeys.map((shapeKey) => loadShapeKey(shapeKey, shapeKeyCacheRef.current)),
        )
      ).filter((shapeKey): shapeKey is LoadedShapeKey => !!shapeKey);

      if (cancelled || loadedShapeKeys.length === 0) {
        return;
      }

      applyShapeKeysToScene(modelRoot, loadedShapeKeys, variantState);
      lastAppliedVariantSnapshotRef.current = currentVariantSnapshot;
      lastAppliedModelRootRef.current = modelRoot;
      lastAppliedShapeKeysRef.current = shapeKeys;
      invalidate();
    })().catch((error) => {
      if (!cancelled) {
        onErrorRef.current?.(error);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [invalidate, modelRoot, shapeKeys, variantState]);

  return (
    <>
      <OrbitControls ref={controlsRef} dampingFactor={0.08} enableDamping makeDefault />
      <group ref={groupRef} rotation={rotation}>
        {modelRoot ? <primitive object={modelRoot} /> : null}
      </group>
    </>
  );
}

function areVariantSnapshotsEqual(
  left: Record<string, number | string> | null,
  right: Record<string, number | string> | null,
): boolean {
  if (left === right) {
    return true;
  }

  if (!left || !right) {
    return false;
  }

  const leftEntries = Object.entries(left);
  const rightEntries = Object.entries(right);
  if (leftEntries.length !== rightEntries.length) {
    return false;
  }

  for (const [key, value] of leftEntries) {
    if (right[key] !== value) {
      return false;
    }
  }

  return true;
}

async function loadShapeKey(
  shapeKey: ModelViewerRealtimeShapeKey,
  cache: Map<string, Promise<Float32Array>>,
): Promise<LoadedShapeKey | null> {
  const [base, ...dimensionBuffers] = await Promise.all([
    loadFloatBuffer(shapeKey.basePath, cache),
    ...shapeKey.dimensions.flatMap((dimension) => [
      loadFloatBuffer(dimension.smallerPath, cache),
      loadFloatBuffer(dimension.biggerPath, cache),
    ]),
  ]);

  if (!base) {
    return null;
  }

  const dimensions = shapeKey.dimensions.map((dimension, index) => ({
    variableId: dimension.variableId,
    smaller: dimensionBuffers[index * 2]!,
    bigger: dimensionBuffers[index * 2 + 1]!,
  }));

  return {
    metadata: shapeKey,
    base,
    dimensions,
  };
}

async function loadFloatBuffer(
  sourcePath: string,
  cache: Map<string, Promise<Float32Array>>,
): Promise<Float32Array> {
  const existing = cache.get(sourcePath);
  if (existing) {
    return existing;
  }

  const request = fetch(modelViewerSourceToUrl(sourcePath))
    .then(async (response) => {
      if (!response.ok) {
        throw new Error(`Failed to load shape key buffer: ${sourcePath}`);
      }
      return new Float32Array(await response.arrayBuffer());
    })
    .catch((error) => {
      cache.delete(sourcePath);
      throw error;
    });
  cache.set(sourcePath, request);
  return request;
}

function applyShapeKeysToScene(
  root: Object3D,
  shapeKeys: LoadedShapeKey[],
  variantState?: Record<string, number | string>,
) {
  root.traverse((object) => {
    if (!(object instanceof Mesh) || !(object.geometry instanceof BufferGeometry)) {
      return;
    }

    const shapeKey = shapeKeys.find((candidate) =>
      candidate.metadata.targetMeshPrefixes.some((prefix) => object.name.startsWith(prefix)),
    );
    if (!shapeKey) {
      return;
    }

    applyShapeKeyToGeometry(object.geometry, shapeKey, variantState);
  });
}

function applyShapeKeyToGeometry(
  geometry: BufferGeometry,
  shapeKey: LoadedShapeKey,
  variantState?: Record<string, number | string>,
) {
  const positionAttribute = geometry.getAttribute("position");
  const normalAttribute = geometry.getAttribute("normal");
  const tangentAttribute = geometry.getAttribute("tangent");
  if (!(positionAttribute instanceof BufferAttribute)) {
    return;
  }

  const positionArray = positionAttribute.array as Float32Array;
  const normalArray =
    normalAttribute instanceof BufferAttribute ? (normalAttribute.array as Float32Array) : null;
  const tangentArray =
    tangentAttribute instanceof BufferAttribute ? (tangentAttribute.array as Float32Array) : null;
  const stride = shapeKey.metadata.vertexStride / 4;
  const base = shapeKey.base;
  const vertexCount = positionAttribute.count;
  if (base.length < vertexCount * stride) {
    return;
  }

  for (let index = 0; index < vertexCount; index += 1) {
    const baseOffset = index * stride;
    const tangentIndex = index * 4;
    let sumPosX = 0;
    let sumPosY = 0;
    let sumPosZ = 0;
    let sumNormX = 0;
    let sumNormY = 0;
    let sumNormZ = 0;
    let sumTangX = 0;
    let sumTangY = 0;
    let sumTangZ = 0;
    let sumTangW = 0;
    let preservedTangW = tangentArray ? Math.sign(tangentArray[tangentIndex + 3]) || 1 : 1;
    let contributionCount = 0;

    for (const dimension of shapeKey.dimensions) {
      const rawValue = Number(variantState?.[dimension.variableId] ?? 0.5);
      const value = Math.min(1, Math.max(0, rawValue));
      const useSmaller = value <= 0.5;
      const t = useSmaller ? value / 0.5 : (value - 0.5) / 0.5;
      const source = useSmaller ? dimension.smaller : dimension.bigger;
      const left = useSmaller ? source : base;
      const right = useSmaller ? base : source;

      const pos = lerpVec3FromBuffers(
        left,
        right,
        baseOffset,
        shapeKey.metadata.positionOffset / 4,
        t,
      );
      const norm = normalizeVec3(
        lerpVec3FromBuffers(left, right, baseOffset, shapeKey.metadata.normalOffset / 4, t),
      );
      const tang = lerpVec4FromBuffers(
        left,
        right,
        baseOffset,
        shapeKey.metadata.tangentOffset / 4,
        t,
      );
      const normalizedTang = normalizeVec3([tang[0], tang[1], tang[2]]);
      if (tang[3] !== 0) {
        preservedTangW = tang[3] < 0 ? -1 : 1;
      }

      sumPosX += pos[0];
      sumPosY += pos[1];
      sumPosZ += pos[2];
      sumNormX += norm[0];
      sumNormY += norm[1];
      sumNormZ += norm[2];
      sumTangX += normalizedTang[0];
      sumTangY += normalizedTang[1];
      sumTangZ += normalizedTang[2];
      sumTangW += tang[3];
      contributionCount += 1;
    }

    const positionIndex = index * 3;
    if (contributionCount === 0) {
      positionArray[positionIndex] = base[baseOffset + shapeKey.metadata.positionOffset / 4];
      positionArray[positionIndex + 1] =
        base[baseOffset + shapeKey.metadata.positionOffset / 4 + 1];
      positionArray[positionIndex + 2] =
        base[baseOffset + shapeKey.metadata.positionOffset / 4 + 2];
      continue;
    }

    positionArray[positionIndex] = sumPosX / contributionCount;
    positionArray[positionIndex + 1] = sumPosY / contributionCount;
    positionArray[positionIndex + 2] = sumPosZ / contributionCount;

    if (normalArray) {
      const normalized = normalizeVec3([sumNormX, sumNormY, sumNormZ]);
      normalArray[positionIndex] = normalized[0];
      normalArray[positionIndex + 1] = normalized[1];
      normalArray[positionIndex + 2] = normalized[2];
    }

    if (tangentArray) {
      const normalized = normalizeVec3([sumTangX, sumTangY, sumTangZ]);
      tangentArray[tangentIndex] = normalized[0];
      tangentArray[tangentIndex + 1] = normalized[1];
      tangentArray[tangentIndex + 2] = normalized[2];
      tangentArray[tangentIndex + 3] = Math.sign(sumTangW) || preservedTangW;
    }
  }

  positionAttribute.needsUpdate = true;
  if (normalAttribute instanceof BufferAttribute) {
    normalAttribute.needsUpdate = true;
  }
  if (tangentAttribute instanceof BufferAttribute) {
    tangentAttribute.needsUpdate = true;
  }
  geometry.computeBoundingBox();
  geometry.computeBoundingSphere();
}

function lerpVec3FromBuffers(
  left: Float32Array,
  right: Float32Array,
  baseOffset: number,
  offset: number,
  t: number,
): [number, number, number] {
  const index = baseOffset + offset;
  return [
    left[index] + (right[index] - left[index]) * t,
    left[index + 1] + (right[index + 1] - left[index + 1]) * t,
    left[index + 2] + (right[index + 2] - left[index + 2]) * t,
  ];
}

function lerpVec4FromBuffers(
  left: Float32Array,
  right: Float32Array,
  baseOffset: number,
  offset: number,
  t: number,
): [number, number, number, number] {
  const index = baseOffset + offset;
  return [
    left[index] + (right[index] - left[index]) * t,
    left[index + 1] + (right[index + 1] - left[index + 1]) * t,
    left[index + 2] + (right[index + 2] - left[index + 2]) * t,
    left[index + 3] + (right[index + 3] - left[index + 3]) * t,
  ];
}

function normalizeVec3(value: [number, number, number]): [number, number, number] {
  const length = Math.hypot(value[0], value[1], value[2]);
  if (!length) {
    return [0, 0, 0];
  }
  return [value[0] / length, value[1] / length, value[2] / length];
}

function captureThreeCameraState(
  camera: Camera,
  controls: OrbitControlsImpl | null,
  object: Object3D | null,
): ModelViewerCameraState | null {
  if (!(camera instanceof PerspectiveCamera) || !controls) {
    return null;
  }

  const offset = camera.position.clone().sub(controls.target);
  const radius = offset.length();
  if (!Number.isFinite(radius) || radius <= 0) {
    return null;
  }

  const theta = Math.atan2(offset.x, offset.z);
  const phi = Math.acos(MathUtils.clamp(offset.y / radius, -1, 1));
  const anchor = getObjectCenter(object);

  return {
    orbit: `${theta}rad ${phi}rad ${radius}m`,
    target: `${controls.target.x}m ${controls.target.y}m ${controls.target.z}m`,
    fieldOfView: `${camera.fov}deg`,
    position: `${camera.position.x}m ${camera.position.y}m ${camera.position.z}m`,
    anchor: anchor ? `${anchor.x}m ${anchor.y}m ${anchor.z}m` : undefined,
  };
}

function restoreThreeCameraState(
  camera: Camera,
  controls: OrbitControlsImpl | null,
  object: Object3D | null,
  state: ModelViewerCameraState | null,
  options?: {
    includeFieldOfView?: boolean;
  },
) {
  if (!(camera instanceof PerspectiveCamera) || !controls || !state) {
    return;
  }

  const orbit = parseThreeOrbit(state.orbit);
  const target = parseThreeTarget(state.target);
  const position = state.position ? parseThreeTarget(state.position) : null;
  const previousAnchor = state.anchor ? parseThreeTarget(state.anchor) : null;
  const nextAnchor = getObjectCenter(object);
  if (!target) {
    return;
  }

  const anchorDelta =
    previousAnchor && nextAnchor ? nextAnchor.clone().sub(previousAnchor) : new Vector3();
  const nextTarget = target.clone().add(anchorDelta);
  controls.target.copy(nextTarget);
  if (position) {
    camera.position.copy(position.clone().add(anchorDelta));
  } else {
    if (!orbit) {
      return;
    }

    const sphericalOffset = new Vector3().setFromSphericalCoords(
      orbit.radius,
      orbit.phi,
      orbit.theta,
    );
    camera.position.copy(nextTarget.clone().add(sphericalOffset));
  }

  if (options?.includeFieldOfView !== false) {
    const fov = Number.parseFloat(state.fieldOfView);
    if (Number.isFinite(fov)) {
      camera.fov = fov;
      camera.updateProjectionMatrix();
    }
  }

  camera.lookAt(nextTarget);
  controls.update();
}

async function fitCameraToObject({
  camera,
  controls,
  object,
}: {
  camera: Camera;
  controls: OrbitControlsImpl | null;
  object: Object3D | null;
}): Promise<Vector3 | null> {
  if (!(camera instanceof PerspectiveCamera) || !controls || !object) {
    return null;
  }

  object.updateMatrixWorld(true);
  const bounds = new Box3().setFromObject(object);
  if (bounds.isEmpty()) {
    return null;
  }

  const center = bounds.getCenter(new Vector3());
  const size = bounds.getSize(new Vector3());
  const radius = Math.max(size.x, size.y, size.z) * 0.5 || 1;
  const fov = MathUtils.degToRad(camera.fov);
  const distance = Math.max(radius / Math.sin(fov / 2), radius * 1.8);

  controls.target.copy(center);
  camera.position.copy(center.clone().add(new Vector3(distance * 0.45, distance * 0.15, distance)));
  camera.near = Math.max(distance / 100, 0.01);
  camera.far = Math.max(distance * 20, 100);
  camera.updateProjectionMatrix();
  controls.update();
  return center.clone();
}

function collectStandardMaterials(root: Object3D): MeshStandardMaterial[] {
  const materials: MeshStandardMaterial[] = [];
  root.traverse((child) => {
    if (!(child instanceof Mesh)) {
      return;
    }

    const entries = Array.isArray(child.material) ? child.material : [child.material];
    for (const material of entries) {
      if (material instanceof MeshStandardMaterial && !materials.includes(material)) {
        materials.push(material);
      }
    }
  });
  return materials;
}

function disposeObjectTree(root: Object3D) {
  root.traverse((child) => {
    if (!(child instanceof Mesh)) {
      return;
    }

    child.geometry.dispose();

    const materials = Array.isArray(child.material) ? child.material : [child.material];
    for (const material of materials) {
      disposeMaterialTextures(material);
      material.dispose();
    }
  });
}

function disposeMaterialTextures(material: MeshStandardMaterial) {
  for (const value of Object.values(material)) {
    if (value instanceof Texture) {
      value.dispose();
    }
  }
}

function parseThreeOrbit(value: string): { phi: number; radius: number; theta: number } | null {
  const [thetaValue, phiValue, radiusValue] = value.split(/\s+/);
  const theta = Number.parseFloat(thetaValue);
  const phi = Number.parseFloat(phiValue);
  const radius = Number.parseFloat(radiusValue);
  if (![theta, phi, radius].every((entry) => Number.isFinite(entry))) {
    return null;
  }

  return { phi, radius, theta };
}

function parseThreeTarget(value: string): Vector3 | null {
  const [xValue, yValue, zValue] = value.split(/\s+/);
  const x = Number.parseFloat(xValue);
  const y = Number.parseFloat(yValue);
  const z = Number.parseFloat(zValue);
  if (![x, y, z].every((entry) => Number.isFinite(entry))) {
    return null;
  }

  return new Vector3(x, y, z);
}

function getObjectCenter(object: Object3D | null): Vector3 | null {
  if (!object) {
    return null;
  }

  const bounds = new Box3().setFromObject(object);
  if (bounds.isEmpty()) {
    return null;
  }

  return bounds.getCenter(new Vector3());
}

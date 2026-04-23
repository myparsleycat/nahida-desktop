import { Canvas, useThree } from "@react-three/fiber";
import { OrbitControls } from "@react-three/drei";
import { cn } from "@renderer/lib/utils";
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { OrbitControls as OrbitControlsImpl } from "three-stdlib";
import {
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
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type { MutableRefObject } from "react";
import type { ModelViewerHandle, ModelViewerSurfaceProps } from "./model-viewer-contract";
import { parseOrientation } from "./model-viewer-contract";
import type { ModelViewerCameraState } from "./model-viewer-contract";

const DEFAULT_CAMERA_POSITION = new Vector3(0, 0, 4);

export const ThreeModelViewer = forwardRef<ModelViewerHandle, ModelViewerSurfaceProps>(
  function ThreeModelViewer({ className, onError, onLoad, orientation, src }, ref) {
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
            src={src}
            onError={onError}
            onLoad={onLoad}
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
  src,
}: ModelViewerSurfaceProps & {
  controllerRef: MutableRefObject<ModelViewerHandle | null>;
}) {
  const { camera, gl, invalidate } = useThree();
  const [modelRoot, setModelRoot] = useState<Object3D | null>(null);
  const controlsRef = useRef<OrbitControlsImpl | null>(null);
  const groupRef = useRef<Group | null>(null);
  const activeObjectRef = useRef<Object3D | null>(null);
  const materialRef = useRef<MeshStandardMaterial[]>([]);
  const pendingLoadIdRef = useRef(0);
  const onLoadRef = useRef(onLoad);
  const onErrorRef = useRef(onError);

  const rotation = useMemo(() => {
    const [x, y, z] = parseOrientation(orientation);
    return new Euler(MathUtils.degToRad(x), MathUtils.degToRad(y), MathUtils.degToRad(z), "XYZ");
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
      materialRef.current = [];
      return () => {
        disposed = true;
      };
    }

    setModelRoot((current) => {
      if (current) {
        disposeObjectTree(current);
      }
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
      });
    }
    invalidate();
    onLoadRef.current?.();
  }, [camera, invalidate, modelRoot]);

  useEffect(() => {
    invalidate();
  }, [invalidate, rotation]);

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
        await fitCameraToObject({
          camera,
          controls: controlsRef.current,
          object: groupRef.current,
        });
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
    };
  }, []);

  return (
    <>
      <OrbitControls
        ref={controlsRef}
        dampingFactor={0.08}
        enableDamping
        makeDefault
      />
      <group ref={groupRef} rotation={rotation}>
        {modelRoot ? <primitive object={modelRoot} /> : null}
      </group>
    </>
  );
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
}) {
  if (!(camera instanceof PerspectiveCamera) || !controls || !object) {
    return;
  }

  const bounds = new Box3().setFromObject(object);
  if (bounds.isEmpty()) {
    return;
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

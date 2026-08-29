import { OrbitControls } from "@react-three/drei";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { cn } from "@renderer/lib/utils";
import { evaluateViewerState } from "@shared/mod-viewer/eval";
import {
  type ElementRef,
  forwardRef,
  memo,
  type MutableRefObject,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ACESFilmicToneMapping,
  Box3,
  BufferAttribute,
  BufferGeometry,
  type Camera,
  DoubleSide,
  Euler,
  FrontSide,
  Group,
  MathUtils,
  Mesh,
  MeshStandardMaterial,
  NeutralToneMapping,
  NoToneMapping,
  Object3D,
  PerspectiveCamera,
  PMREMGenerator,
  Scene,
  SRGBColorSpace,
  Texture,
  Vector3,
} from "three";
import { RoomEnvironment } from "three/examples/jsm/environments/RoomEnvironment.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";

import type {
  ModelViewerAnimationClip,
  ModelViewerAnimationFrame,
  ModelViewerBodyShapeOverride,
  ModelViewerCameraState,
  ModelViewerHandle,
  ModelViewerRealtimeShapeKey,
  ModelViewerSurfaceProps,
} from "./model-viewer-contract";

import { parseOrientation } from "./model-viewer-contract";
import { applyPayloadEval, buildPayloadModel } from "./model-viewer-payload";
import { modelViewerSourceToUrl } from "./model-viewer-session";
import { MODEL_VIEWER_UPRIGHT_ROTATION, needsUprightCorrection } from "./model-viewer-upright";

type BodyShapeBaseline = {
  positions: Float32Array;
  colors?: Float32Array;
  vertexColorsEnabled: boolean;
};

type OrbitControlsImpl = ElementRef<typeof OrbitControls>;

const DEFAULT_CAMERA_POSITION = new Vector3(0, 0, 4);
const ORBIT_CONTROLS_ZOOM_SPEED = 1.5;
const SMOOTH_ZOOM_DAMPING = 0.16;
const SMOOTH_ZOOM_DELTA_SCALE = 0.0015;

type LoadedShapeKey = {
  metadata: ModelViewerRealtimeShapeKey;
  base: Float32Array;
  dimensions: Array<{
    variableId: string;
    smaller: Float32Array;
    bigger: Float32Array;
  }>;
};

type LoadedAnimationFrame = {
  index: number;
  meshes: Array<{
    meshName: string;
    indices?: Uint32Array;
    position: Float32Array;
    normal?: Float32Array;
    tangent?: Float32Array;
    texcoord0?: Float32Array;
  }>;
};

export const ThreeModelViewer = memo(
  forwardRef<ModelViewerHandle, ModelViewerSurfaceProps>(function ThreeModelViewer(
    {
      className,
      animationClip,
      animationFrame,
      bodyShapeOverrides,
      onError,
      onLoad,
      orientation,
      payloadEval,
      payloadTransport,
      shapeKeys,
      src,
      threeEnvironment = "studio",
      threeExposure = 1,
      threeToneMapping = "neutral",
      variantState,
    },
    ref,
  ) {
    const controllerRef = useRef<ModelViewerHandle | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        captureCameraState: () => controllerRef.current?.captureCameraState() ?? null,
        captureSquarePngDataUrl: async () =>
          (await controllerRef.current?.captureSquarePngDataUrl()) ?? null,
        restoreCameraState: (state, options) =>
          controllerRef.current?.restoreCameraState(state, options),
        setDoubleSided: (doubleSided) => controllerRef.current?.setDoubleSided(doubleSided),
        updateFraming: () => controllerRef.current?.updateFraming(),
        setAnimationFrame: (index) => controllerRef.current?.setAnimationFrame(index),
      }),
      [],
    );

    const lighting = useMemo(() => {
      switch (threeEnvironment) {
        case "none":
          return {
            ambient: 0.45,
            directionalKey: 1.35,
            directionalFill: 0.45,
            hemisphere: 0,
          };
        case "soft":
          return {
            ambient: 0.5,
            directionalKey: 1.5,
            directionalFill: 0.6,
            hemisphere: 0.55,
          };
        case "studio":
        default:
          return {
            ambient: 0.6,
            directionalKey: 1.8,
            directionalFill: 0.8,
            hemisphere: 0.9,
          };
      }
    }, [threeEnvironment]);

    return (
      <div className={cn("h-full w-full", className)}>
        <Canvas
          style={{ background: "transparent" }}
          camera={{
            far: 1000,
            fov: 45,
            near: 0.01,
            position: DEFAULT_CAMERA_POSITION.toArray(),
          }}
          dpr={window.devicePixelRatio}
          gl={{ alpha: true, antialias: true, preserveDrawingBuffer: true }}
        >
          <ambientLight intensity={lighting.ambient} />
          {lighting.hemisphere > 0 ? (
            <hemisphereLight
              intensity={lighting.hemisphere}
              groundColor="#b9bec7"
              position={[0, 1, 0]}
            />
          ) : null}
          <directionalLight intensity={lighting.directionalKey} position={[6, 8, 10]} />
          <directionalLight intensity={lighting.directionalFill} position={[-6, 4, -8]} />
          <ThreeModelScene
            controllerRef={controllerRef}
            animationClip={animationClip}
            animationFrame={animationFrame}
            bodyShapeOverrides={bodyShapeOverrides}
            orientation={orientation}
            shapeKeys={shapeKeys}
            src={src}
            threeEnvironment={threeEnvironment}
            threeExposure={threeExposure}
            threeToneMapping={threeToneMapping}
            onError={onError}
            onLoad={onLoad}
            payloadEval={payloadEval}
            payloadTransport={payloadTransport}
            variantState={variantState}
          />
        </Canvas>
      </div>
    );
  }),
);

function ThreeModelScene({
  controllerRef,
  animationClip,
  animationFrame,
  bodyShapeOverrides,
  onError,
  onLoad,
  orientation,
  payloadEval,
  payloadTransport,
  shapeKeys,
  src,
  threeEnvironment = "studio",
  threeExposure = 1,
  threeToneMapping = "neutral",
  variantState,
}: ModelViewerSurfaceProps & {
  controllerRef: MutableRefObject<ModelViewerHandle | null>;
}) {
  const { camera, gl, invalidate, scene } = useThree();
  const [modelRoot, setModelRoot] = useState<Object3D | null>(null);
  const controlsRef = useRef<OrbitControlsImpl | null>(null);
  const groupRef = useRef<Group | null>(null);
  const activeObjectRef = useRef<Object3D | null>(null);
  const materialRef = useRef<MeshStandardMaterial[]>([]);
  const orientedCenterRef = useRef<Vector3 | null>(null);
  const pendingLoadIdRef = useRef(0);
  const onLoadRef = useRef(onLoad);
  const onErrorRef = useRef(onError);
  const payloadEvalRef = useRef(payloadEval);
  const payloadTransportRef = useRef(payloadTransport);
  const animationClipRef = useRef(animationClip);
  const animationValuesRef = useRef<Record<string, string | number>>({});
  const modelRootRef = useRef<Object3D | null>(null);
  const applyPayloadVisualsRef = useRef(() => {});
  const floatBufferCacheRef = useRef<Map<string, Promise<Float32Array>>>(new Map());
  const uint32BufferCacheRef = useRef<Map<string, Promise<Uint32Array>>>(new Map());
  const lastAppliedVariantSnapshotRef = useRef<Record<string, number | string> | null>(null);
  const lastAppliedModelRootRef = useRef<Object3D | null>(null);
  const lastAppliedShapeKeysRef = useRef<ModelViewerRealtimeShapeKey[] | undefined>(undefined);
  const lastAppliedAnimationSignatureRef = useRef<string | null>(null);
  const bodyShapeBaselineRef = useRef<WeakMap<Mesh, BodyShapeBaseline>>(new WeakMap());
  const desiredCameraDistanceRef = useRef<number | null>(null);
  const hasBodyShapeOverrides = !!bodyShapeOverrides?.length;

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
    // oxlint-disable-next-line react/immutability
    gl.outputColorSpace = SRGBColorSpace;
    // oxlint-disable-next-line react/immutability
    gl.toneMapping =
      threeToneMapping === "aces"
        ? ACESFilmicToneMapping
        : threeToneMapping === "none"
          ? NoToneMapping
          : NeutralToneMapping;
    // oxlint-disable-next-line react/immutability
    gl.toneMappingExposure = Number.isFinite(threeExposure) ? threeExposure : 1;
    gl.setClearAlpha(0);

    if (threeEnvironment === "none") {
      // oxlint-disable-next-line react/immutability
      scene.environment = null;
      invalidate();
      return;
    }

    const environmentScene = new Scene();
    const pmremGenerator = new PMREMGenerator(gl);
    const roomEnvironment = new RoomEnvironment();
    roomEnvironment.scale.setScalar(threeEnvironment === "soft" ? 0.85 : 1);
    const environmentTarget = pmremGenerator.fromScene(environmentScene.add(roomEnvironment));

    // oxlint-disable-next-line react/immutability
    scene.environment = environmentTarget.texture;
    invalidate();

    return () => {
      if (scene.environment === environmentTarget.texture) {
        // oxlint-disable-next-line react/immutability
        scene.environment = null;
      }
      environmentTarget.dispose();
      roomEnvironment.dispose();
      pmremGenerator.dispose();
    };
  }, [gl, invalidate, scene, threeEnvironment, threeExposure, threeToneMapping]);

  useEffect(() => {
    onLoadRef.current = onLoad;
  }, [onLoad]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  useEffect(() => {
    payloadEvalRef.current = payloadEval;
  }, [payloadEval]);

  useEffect(() => {
    payloadTransportRef.current = payloadTransport;
  }, [payloadTransport]);

  useEffect(() => {
    animationClipRef.current = animationClip;
  }, [animationClip]);

  useEffect(() => {
    modelRootRef.current = modelRoot;
  }, [modelRoot]);

  useEffect(() => {
    applyPayloadVisualsRef.current = () => {
      const root = modelRootRef.current;
      const transport = payloadTransportRef.current;
      const toggleEval = payloadEvalRef.current;
      if (!root || !transport || !toggleEval) {
        return;
      }
      const animationValues = animationValuesRef.current;
      const evalResult = Object.keys(animationValues).length
        ? evaluateViewerState(transport, { ...toggleEval.state, ...animationValues })
        : toggleEval;
      applyPayloadEval(root, evalResult);
      invalidate();
    };
  }, [invalidate]);

  useEffect(() => {
    const loadId = pendingLoadIdRef.current + 1;
    pendingLoadIdRef.current = loadId;

    let disposed = false;

    if (activeObjectRef.current) {
      disposeObjectTree(activeObjectRef.current);
      activeObjectRef.current = null;
    }
    orientedCenterRef.current = null;
    materialRef.current = [];
    // oxlint-disable-next-line react/set-state-in-effect
    setModelRoot((current) => (current ? null : current));

    if (!payloadTransport && !src) {
      return () => {
        disposed = true;
      };
    }

    if (payloadTransport) {
      void buildPayloadModel(
        payloadTransport,
        payloadEvalRef.current ?? { state: {}, meshes: [] },
        true,
      )
        .then((nextRoot) => {
          if (disposed || pendingLoadIdRef.current !== loadId) {
            disposeObjectTree(nextRoot);
            return;
          }
          if (needsUprightCorrection(nextRoot)) {
            nextRoot.rotation.copy(MODEL_VIEWER_UPRIGHT_ROTATION);
          }
          materialRef.current = collectStandardMaterials(nextRoot);
          activeObjectRef.current = nextRoot;
          // oxlint-disable-next-line react/set-state-in-effect
          setModelRoot(nextRoot);
        })
        .catch((error) => {
          if (disposed || pendingLoadIdRef.current !== loadId) {
            return;
          }
          activeObjectRef.current = null;
          materialRef.current = [];
          onErrorRef.current?.(error);
        });
      return () => {
        disposed = true;
      };
    }

    const glbSrc = src;
    if (!glbSrc) {
      return () => {
        disposed = true;
      };
    }

    const loader = new GLTFLoader();
    loader.load(
      glbSrc,
      (gltf) => {
        if (disposed || pendingLoadIdRef.current !== loadId) {
          disposeObjectTree(gltf.scene);
          return;
        }

        const nextRoot = gltf.scene;
        if (needsUprightCorrection(nextRoot)) {
          nextRoot.rotation.copy(MODEL_VIEWER_UPRIGHT_ROTATION);
        }
        materialRef.current = collectStandardMaterials(nextRoot);
        activeObjectRef.current = nextRoot;
        // oxlint-disable-next-line react/set-state-in-effect
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
  }, [payloadTransport, src]);

  useEffect(() => {
    if (!modelRoot || !payloadEval || !payloadTransport) {
      return;
    }
    applyPayloadVisualsRef.current();
  }, [modelRoot, payloadEval, payloadTransport]);

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
        desiredCameraDistanceRef.current = getPerspectiveCameraDistance(
          camera,
          controlsRef.current,
        );
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
    desiredCameraDistanceRef.current = getPerspectiveCameraDistance(camera, controlsRef.current);
    invalidate();
  }, [camera, invalidate, modelRoot, rotation]);

  useEffect(() => {
    // oxlint-disable-next-line react/immutability
    controllerRef.current = {
      captureCameraState: () =>
        captureThreeCameraState(camera, controlsRef.current, groupRef.current),
      captureSquarePngDataUrl: async () => {
        const dataUrl = await captureSquareCanvasPngDataUrl(gl.domElement, invalidate);
        return dataUrl;
      },
      restoreCameraState: (state, options) => {
        restoreThreeCameraState(camera, controlsRef.current, groupRef.current, state, options);
        desiredCameraDistanceRef.current = getPerspectiveCameraDistance(
          camera,
          controlsRef.current,
        );
      },
      setDoubleSided: (doubleSided) => {
        for (const material of materialRef.current) {
          material.side = doubleSided ? DoubleSide : FrontSide;
          material.needsUpdate = true;
        }
        invalidate();
      },
      setAnimationFrame: (index) => {
        const clip = animationClipRef.current;
        if (!clip?.frames.length) {
          animationValuesRef.current = {};
        } else {
          const frame = clip.frames[Math.min(Math.max(index, 0), clip.frames.length - 1)];
          animationValuesRef.current = frame?.values ?? {};
        }
        applyPayloadVisualsRef.current();
      },
      updateFraming: async () => {
        const center = await fitCameraToObject({
          camera,
          controls: controlsRef.current,
          object: groupRef.current,
        });
        orientedCenterRef.current = center;
        desiredCameraDistanceRef.current = getPerspectiveCameraDistance(
          camera,
          controlsRef.current,
        );
        invalidate();
      },
    };

    return () => {
      controllerRef.current = null;
    };
  }, [camera, controllerRef, invalidate]);

  useEffect(() => {
    desiredCameraDistanceRef.current = getPerspectiveCameraDistance(camera, controlsRef.current);
  }, [camera]);

  useEffect(() => {
    const controls = controlsRef.current;
    const domElement = gl.domElement;
    if (!(camera instanceof PerspectiveCamera) || !controls || !domElement) {
      return;
    }

    const onWheel = (event: WheelEvent) => {
      if (!controls.enabled) {
        return;
      }

      event.preventDefault();
      const currentDistance =
        desiredCameraDistanceRef.current ?? getPerspectiveCameraDistance(camera, controls);
      if (!currentDistance) {
        return;
      }

      const zoomFactor = Math.exp(
        event.deltaY * SMOOTH_ZOOM_DELTA_SCALE * ORBIT_CONTROLS_ZOOM_SPEED,
      );
      desiredCameraDistanceRef.current = MathUtils.clamp(
        currentDistance * zoomFactor,
        Math.max(controls.minDistance, 0.01),
        controls.maxDistance,
      );
      invalidate();
    };

    domElement.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      domElement.removeEventListener("wheel", onWheel);
    };
  }, [camera, gl, invalidate]);

  useFrame((_, delta) => {
    const controls = controlsRef.current;
    const desiredDistance = desiredCameraDistanceRef.current;
    if (!(camera instanceof PerspectiveCamera) || !controls || !desiredDistance) {
      return;
    }

    const offset = camera.position.clone().sub(controls.target);
    const currentDistance = offset.length();
    if (!Number.isFinite(currentDistance) || currentDistance <= 0) {
      return;
    }

    const step = 1 - Math.pow(1 - SMOOTH_ZOOM_DAMPING, delta * 60);
    const nextDistance = MathUtils.lerp(currentDistance, desiredDistance, step);
    if (Math.abs(nextDistance - currentDistance) < 0.0001) {
      if (Math.abs(desiredDistance - currentDistance) < 0.0001) {
        desiredCameraDistanceRef.current = currentDistance;
      }
      return;
    }

    camera.position.copy(
      controls.target.clone().add(offset.normalize().multiplyScalar(nextDistance)),
    );
    controls.update();
    invalidate();
  });

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
      lastAppliedAnimationSignatureRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!modelRoot) {
      return;
    }

    if (!bodyShapeOverrides?.length) {
      restoreBodyShapeBaselines(modelRoot, bodyShapeBaselineRef.current);
      invalidate();
      return;
    }

    applyBodyShapeOverridesToScene(
      modelRoot,
      bodyShapeOverrides,
      bodyShapeBaselineRef.current,
      (error) => onErrorRef.current?.(error),
    );
    invalidate();
  }, [bodyShapeOverrides, invalidate, modelRoot]);

  useEffect(() => {
    if (
      payloadTransport ||
      !modelRoot ||
      hasBodyShapeOverrides ||
      !animationClip ||
      animationClip.frames.length === 0
    ) {
      lastAppliedAnimationSignatureRef.current = null;
      return;
    }

    const normalizedFrame = Math.min(
      Math.max(animationFrame ?? 0, 0),
      animationClip.frames.length - 1,
    );
    const signature = `${animationClip.id}:${normalizedFrame}`;
    if (lastAppliedAnimationSignatureRef.current === signature) {
      return;
    }

    let cancelled = false;
    void (async () => {
      const nextFrame = await loadAnimationFrame(
        animationClip.frames[normalizedFrame]!,
        animationClip,
        floatBufferCacheRef.current,
        uint32BufferCacheRef.current,
      );
      if (cancelled) {
        return;
      }

      applyAnimationFrameToScene(modelRoot, nextFrame);
      lastAppliedAnimationSignatureRef.current = signature;
      invalidate();
    })().catch((error) => {
      if (!cancelled) {
        onErrorRef.current?.(error);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [
    animationClip,
    animationFrame,
    hasBodyShapeOverrides,
    invalidate,
    modelRoot,
    payloadTransport,
  ]);

  useEffect(() => {
    if (!modelRoot || hasBodyShapeOverrides || !shapeKeys?.length) {
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
          shapeKeys.map((shapeKey) => loadShapeKey(shapeKey, floatBufferCacheRef.current)),
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
  }, [hasBodyShapeOverrides, invalidate, modelRoot, shapeKeys, variantState]);

  return (
    <>
      <OrbitControls
        ref={controlsRef}
        dampingFactor={0.08}
        enableDamping
        enableZoom={false}
        makeDefault
        zoomSpeed={ORBIT_CONTROLS_ZOOM_SPEED}
      />
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

async function loadUint32Buffer(
  sourcePath: string,
  cache: Map<string, Promise<Uint32Array>>,
): Promise<Uint32Array> {
  const existing = cache.get(sourcePath);
  if (existing) {
    return existing;
  }

  const request = fetch(modelViewerSourceToUrl(sourcePath))
    .then(async (response) => {
      if (!response.ok) {
        throw new Error(`Failed to load animation index buffer: ${sourcePath}`);
      }
      return new Uint32Array(await response.arrayBuffer());
    })
    .catch((error) => {
      cache.delete(sourcePath);
      throw error;
    });
  cache.set(sourcePath, request);
  return request;
}

async function loadAnimationFrame(
  frame: ModelViewerAnimationFrame,
  clip: ModelViewerAnimationClip,
  floatCache: Map<string, Promise<Float32Array>>,
  uint32Cache: Map<string, Promise<Uint32Array>>,
): Promise<LoadedAnimationFrame> {
  const sharedBufferPathMap = new Map(
    (clip.sharedBuffers ?? []).map((buffer) => [buffer.id, buffer.path]),
  );
  const meshes = await Promise.all(
    frame.meshes.map(async (mesh) => ({
      meshName: mesh.meshName,
      indices: await loadOptionalUint32AnimationBuffer(
        mesh.indicesBufferId,
        mesh.indicesPath,
        sharedBufferPathMap,
        uint32Cache,
      ),
      position: await loadRequiredFloatAnimationBuffer(
        mesh.positionBufferId,
        mesh.positionPath,
        sharedBufferPathMap,
        floatCache,
        `animation position buffer for ${mesh.meshName}`,
      ),
      normal: await loadOptionalFloatAnimationBuffer(
        mesh.normalBufferId,
        mesh.normalPath,
        sharedBufferPathMap,
        floatCache,
      ),
      tangent: await loadOptionalFloatAnimationBuffer(
        mesh.tangentBufferId,
        mesh.tangentPath,
        sharedBufferPathMap,
        floatCache,
      ),
      texcoord0: await loadOptionalFloatAnimationBuffer(
        mesh.texcoord0BufferId,
        mesh.texcoord0Path,
        sharedBufferPathMap,
        floatCache,
      ),
    })),
  );

  return {
    index: frame.index,
    meshes,
  };
}

function resolveAnimationBufferPath(
  bufferId: string | undefined,
  sourcePath: string | undefined,
  sharedBufferPathMap: Map<string, string>,
): string | undefined {
  if (bufferId) {
    return sharedBufferPathMap.get(bufferId);
  }

  return sourcePath;
}

async function loadRequiredFloatAnimationBuffer(
  bufferId: string | undefined,
  sourcePath: string | undefined,
  sharedBufferPathMap: Map<string, string>,
  cache: Map<string, Promise<Float32Array>>,
  label: string,
): Promise<Float32Array> {
  const resolvedPath = resolveAnimationBufferPath(bufferId, sourcePath, sharedBufferPathMap);
  if (!resolvedPath) {
    throw new Error(`Missing ${label}`);
  }

  return await loadFloatBuffer(resolvedPath, cache);
}

async function loadOptionalFloatAnimationBuffer(
  bufferId: string | undefined,
  sourcePath: string | undefined,
  sharedBufferPathMap: Map<string, string>,
  cache: Map<string, Promise<Float32Array>>,
): Promise<Float32Array | undefined> {
  const resolvedPath = resolveAnimationBufferPath(bufferId, sourcePath, sharedBufferPathMap);
  return resolvedPath ? await loadFloatBuffer(resolvedPath, cache) : undefined;
}

async function loadOptionalUint32AnimationBuffer(
  bufferId: string | undefined,
  sourcePath: string | undefined,
  sharedBufferPathMap: Map<string, string>,
  cache: Map<string, Promise<Uint32Array>>,
): Promise<Uint32Array | undefined> {
  const resolvedPath = resolveAnimationBufferPath(bufferId, sourcePath, sharedBufferPathMap);
  return resolvedPath ? await loadUint32Buffer(resolvedPath, cache) : undefined;
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

function applyAnimationFrameToScene(root: Object3D, frame: LoadedAnimationFrame) {
  root.traverse((object) => {
    if (!(object instanceof Mesh) || !(object.geometry instanceof BufferGeometry)) {
      return;
    }

    const mesh = frame.meshes.find((entry) => entry.meshName === object.name);
    if (!mesh) {
      return;
    }

    applyAnimationMeshToGeometry(object.geometry, mesh);
  });
}

function applyBodyShapeOverridesToScene(
  root: Object3D,
  overrides: ModelViewerBodyShapeOverride[],
  baselines: WeakMap<Mesh, BodyShapeBaseline>,
  onError?: (error: Error) => void,
) {
  const nameSetByOverride = overrides.map((override) => new Set(override.meshNames));
  const activeNames = new Set(overrides.flatMap((override) => override.meshNames));

  root.traverse((object) => {
    if (!(object instanceof Mesh) || !(object.geometry instanceof BufferGeometry)) {
      return;
    }

    if (!activeNames.has(object.name)) {
      restoreBodyShapeBaselineMesh(object, baselines);
      return;
    }

    const overrideIndex = nameSetByOverride.findIndex((names) => names.has(object.name));
    if (overrideIndex < 0) {
      return;
    }

    const override = overrides[overrideIndex]!;
    cacheBodyShapeBaseline(object, baselines);

    const positionsChanged = override.positionsChanged !== false;

    if (positionsChanged) {
      const positionAttribute = object.geometry.getAttribute("position");
      if (!(positionAttribute instanceof BufferAttribute)) {
        return;
      }

      const positionArray = positionAttribute.array as Float32Array;
      if (positionArray.length !== override.positions.length) {
        onError?.(
          new Error(
            `Body shape vertex mismatch for ${object.name}: glb=${positionArray.length / 3} edit=${override.positions.length / 3}`,
          ),
        );
        return;
      }

      positionArray.set(override.positions);
      positionAttribute.needsUpdate = true;
      object.geometry.computeVertexNormals();
      object.geometry.computeBoundingBox();
      object.geometry.computeBoundingSphere();
    }

    applyBodyShapeVertexColors(object, override.vertexColors, baselines.get(object));
  });
}

function restoreBodyShapeBaselines(root: Object3D, baselines: WeakMap<Mesh, BodyShapeBaseline>) {
  root.traverse((object) => {
    if (!(object instanceof Mesh) || !(object.geometry instanceof BufferGeometry)) {
      return;
    }
    restoreBodyShapeBaselineMesh(object, baselines);
  });
}

function restoreBodyShapeBaselineMesh(mesh: Mesh, baselines: WeakMap<Mesh, BodyShapeBaseline>) {
  if (!(mesh.geometry instanceof BufferGeometry)) {
    return;
  }

  const baseline = baselines.get(mesh);
  if (!baseline) {
    return;
  }

  const positionAttribute = mesh.geometry.getAttribute("position");
  if (positionAttribute instanceof BufferAttribute) {
    const positionArray = positionAttribute.array as Float32Array;
    if (positionArray.length === baseline.positions.length) {
      positionArray.set(baseline.positions);
      positionAttribute.needsUpdate = true;
    }
  }

  if (baseline.colors) {
    const colorAttribute = mesh.geometry.getAttribute("color");
    if (colorAttribute instanceof BufferAttribute) {
      const colorArray = colorAttribute.array as Float32Array;
      if (colorArray.length === baseline.colors.length) {
        colorArray.set(baseline.colors);
        colorAttribute.needsUpdate = true;
      }
    }
  } else {
    mesh.geometry.deleteAttribute("color");
  }

  for (const material of collectMeshMaterials(mesh)) {
    material.vertexColors = baseline.vertexColorsEnabled;
    material.needsUpdate = true;
  }

  mesh.geometry.computeVertexNormals();
  mesh.geometry.computeBoundingBox();
  mesh.geometry.computeBoundingSphere();
}

function cacheBodyShapeBaseline(mesh: Mesh, baselines: WeakMap<Mesh, BodyShapeBaseline>) {
  if (baselines.has(mesh) || !(mesh.geometry instanceof BufferGeometry)) {
    return;
  }

  const positionAttribute = mesh.geometry.getAttribute("position");
  if (!(positionAttribute instanceof BufferAttribute)) {
    return;
  }

  const colorsAttribute = mesh.geometry.getAttribute("color");
  const materials = collectMeshMaterials(mesh);
  baselines.set(mesh, {
    positions: new Float32Array(positionAttribute.array as Float32Array),
    colors:
      colorsAttribute instanceof BufferAttribute
        ? new Float32Array(colorsAttribute.array as Float32Array)
        : undefined,
    vertexColorsEnabled: materials.some((material) => material.vertexColors),
  });
}

function applyBodyShapeVertexColors(
  mesh: Mesh,
  vertexColors: Float32Array | undefined,
  baseline?: BodyShapeBaseline,
) {
  if (!(mesh.geometry instanceof BufferGeometry)) {
    return;
  }

  const materials = collectMeshMaterials(mesh);

  if (!vertexColors) {
    if (baseline?.colors) {
      const colorAttribute = mesh.geometry.getAttribute("color");
      if (colorAttribute instanceof BufferAttribute) {
        const colorArray = colorAttribute.array as Float32Array;
        if (colorArray.length === baseline.colors.length) {
          colorArray.set(baseline.colors);
          colorAttribute.needsUpdate = true;
        }
      }
    } else {
      mesh.geometry.deleteAttribute("color");
    }
    for (const material of materials) {
      if (material.vertexColors !== (baseline?.vertexColorsEnabled ?? false)) {
        material.vertexColors = baseline?.vertexColorsEnabled ?? false;
        material.needsUpdate = true;
      }
    }
    return;
  }

  const existing = mesh.geometry.getAttribute("color");
  if (existing instanceof BufferAttribute && existing.array.length === vertexColors.length) {
    (existing.array as Float32Array).set(vertexColors);
    existing.needsUpdate = true;
  } else {
    mesh.geometry.setAttribute("color", new BufferAttribute(vertexColors.slice(), 3));
  }

  for (const material of materials) {
    if (!material.vertexColors) {
      material.vertexColors = true;
      material.needsUpdate = true;
    }
  }
}

function collectMeshMaterials(mesh: Mesh): MeshStandardMaterial[] {
  const list = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
  return list.filter((material): material is MeshStandardMaterial => {
    return !!material && "vertexColors" in material;
  });
}

function applyAnimationMeshToGeometry(
  geometry: BufferGeometry,
  mesh: LoadedAnimationFrame["meshes"][number],
) {
  const positionAttribute = geometry.getAttribute("position");
  const normalAttribute = geometry.getAttribute("normal");
  const tangentAttribute = geometry.getAttribute("tangent");
  const uvAttribute = geometry.getAttribute("uv");
  if (mesh.indices) {
    applyIndexToGeometry(geometry, mesh.indices);
  }

  if (mesh.position.length % 3 !== 0) {
    return;
  }

  if (positionAttribute instanceof BufferAttribute) {
    const positionArray = positionAttribute.array as Float32Array;
    if (positionArray.length === mesh.position.length) {
      positionArray.set(mesh.position);
      positionAttribute.needsUpdate = true;
    } else {
      geometry.setAttribute("position", new BufferAttribute(mesh.position.slice(), 3));
    }
  } else {
    geometry.setAttribute("position", new BufferAttribute(mesh.position.slice(), 3));
  }

  if (normalAttribute instanceof BufferAttribute && mesh.normal) {
    const normalArray = normalAttribute.array as Float32Array;
    if (normalArray.length === mesh.normal.length) {
      normalArray.set(mesh.normal);
      normalAttribute.needsUpdate = true;
    } else if (mesh.normal.length % 3 === 0) {
      geometry.setAttribute("normal", new BufferAttribute(mesh.normal.slice(), 3));
    }
  } else if (mesh.normal && mesh.normal.length % 3 === 0) {
    geometry.setAttribute("normal", new BufferAttribute(mesh.normal.slice(), 3));
  }

  if (tangentAttribute instanceof BufferAttribute && mesh.tangent) {
    const tangentArray = tangentAttribute.array as Float32Array;
    if (tangentArray.length === mesh.tangent.length) {
      tangentArray.set(mesh.tangent);
      tangentAttribute.needsUpdate = true;
    } else if (mesh.tangent.length % 4 === 0) {
      geometry.setAttribute("tangent", new BufferAttribute(mesh.tangent.slice(), 4));
    }
  } else if (mesh.tangent && mesh.tangent.length % 4 === 0) {
    geometry.setAttribute("tangent", new BufferAttribute(mesh.tangent.slice(), 4));
  }

  if (uvAttribute instanceof BufferAttribute && mesh.texcoord0) {
    const uvArray = uvAttribute.array as Float32Array;
    if (uvArray.length === mesh.texcoord0.length) {
      uvArray.set(mesh.texcoord0);
      uvAttribute.needsUpdate = true;
    } else if (mesh.texcoord0.length % 2 === 0) {
      geometry.setAttribute("uv", new BufferAttribute(mesh.texcoord0.slice(), 2));
    }
  } else if (mesh.texcoord0 && mesh.texcoord0.length % 2 === 0) {
    geometry.setAttribute("uv", new BufferAttribute(mesh.texcoord0.slice(), 2));
  }

  geometry.computeBoundingBox();
  geometry.computeBoundingSphere();
}

function applyIndexToGeometry(geometry: BufferGeometry, indices: Uint32Array) {
  const currentIndex = geometry.getIndex();
  if (currentIndex instanceof BufferAttribute && currentIndex.array.length === indices.length) {
    const indexArray = currentIndex.array;
    if (indexArray instanceof Uint32Array) {
      indexArray.set(indices);
      currentIndex.needsUpdate = true;
      return;
    }

    if (indexArray instanceof Uint16Array && maxUint32(indices) <= 65535) {
      indexArray.set(indices);
      currentIndex.needsUpdate = true;
      return;
    }
  }

  const nextIndices = maxUint32(indices) <= 65535 ? Uint16Array.from(indices) : indices.slice();
  geometry.setIndex(new BufferAttribute(nextIndices, 1));
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

function maxUint32(values: Uint32Array): number {
  let max = 0;
  for (const value of values) {
    if (value > max) {
      max = value;
    }
  }
  return max;
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

async function captureSquareCanvasPngDataUrl(
  sourceCanvas: HTMLCanvasElement | null,
  invalidate?: () => void,
): Promise<string | null> {
  if (!sourceCanvas) {
    return null;
  }

  invalidate?.();
  await waitForNextFrame();

  const width = sourceCanvas.width;
  const height = sourceCanvas.height;
  const size = Math.min(width, height);
  if (!width || !height || !size) {
    return null;
  }

  const cropX = Math.floor((width - size) / 2);
  const cropY = Math.floor((height - size) / 2);
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;

  const context = canvas.getContext("2d");
  if (!context) {
    return null;
  }

  try {
    context.drawImage(sourceCanvas, cropX, cropY, size, size, 0, 0, size, size);
    return canvas.toDataURL("image/png");
  } catch {
    return null;
  }
}

function waitForNextFrame(): Promise<void> {
  return new Promise((resolve) => {
    requestAnimationFrame(() => resolve());
  });
}

function getPerspectiveCameraDistance(
  camera: Camera,
  controls: OrbitControlsImpl | null,
): number | null {
  if (!(camera instanceof PerspectiveCamera) || !controls) {
    return null;
  }

  const distance = camera.position.distanceTo(controls.target);
  return Number.isFinite(distance) && distance > 0 ? distance : null;
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
      (value as Texture).dispose();
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

import { OrbitControls } from "@react-three/drei";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { createThreeRenderer } from "@renderer/components/tools/three-renderer";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import { evaluateViewerState } from "@shared/mod-viewer/eval";
import { isEqual } from "es-toolkit";
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
  Box3,
  BufferAttribute,
  BufferGeometry,
  type Camera,
  type DirectionalLight,
  DoubleSide,
  Euler,
  FrontSide,
  Group,
  MathUtils,
  Mesh,
  MeshStandardMaterial,
  Object3D,
  PerspectiveCamera,
  Spherical,
  SRGBColorSpace,
  Texture,
  Vector3,
} from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";

import type {
  ModelViewerAnimationClip,
  ModelViewerAnimationFrame,
  ModelViewerBodyShapeOverride,
  ModelViewerCameraState,
  ModelViewerHandle,
  ModelViewerSurfaceProps,
  ModelViewerToonStyle,
} from "./model-viewer-contract";

import {
  ModelViewerAxisGizmo,
  type ModelViewerAxisGizmoHandle,
  type ModelViewerAxisView,
} from "./model-viewer-axis-gizmo";
import { ModelViewerComputeController } from "./model-viewer-compute-controller";
import {
  DEFAULT_MODEL_VIEWER_GIMI_TOON_STYLE,
  DEFAULT_MODEL_VIEWER_TOON_STYLE,
  parseOrientation,
} from "./model-viewer-contract";
import { createModelViewerMipmapper, getModelViewerTextureCapabilities } from "./model-viewer-dds";
import { createGenshinToonTextureCache } from "./model-viewer-genshin-toon";
import {
  buildPayloadModel,
  clearPayloadModelData,
  commitPayloadEval,
  finalizePayloadGeometry,
  type PreparedPayloadEval,
  preparePayloadEval,
  setPayloadGameTextureHighlight,
  setPayloadGIMIToon,
  setPayloadNormalMaps,
  setPayloadToonShadows,
  setPayloadToonStyle,
} from "./model-viewer-payload";
import { ModelViewerPositionLoader } from "./model-viewer-position-loader";
import {
  aimKeyLight,
  createModelViewerEnvironment,
  fitKeyLightShadowToObject,
  frameCameraOnObject,
  MODEL_VIEWER_FILL_LIGHT_POSITION,
  MODEL_VIEWER_HEMISPHERE_GROUND_COLOR,
  MODEL_VIEWER_KEY_LIGHT_POSITION,
  modelViewerKeyLightDirection,
  modelViewerLighting,
  modelViewerToneMapping,
  modelViewerToonLightDirection,
} from "./model-viewer-scene";
import { modelViewerSourceToUrl } from "./model-viewer-session";
import { ShapeWavePlayback } from "./model-viewer-shape-wave";
import {
  raycastModelViewerSurface,
  resolveModelViewerSurfacePick,
} from "./model-viewer-surface-pick";
import { applyModelViewerInitialRotation } from "./model-viewer-upright";

type BodyShapeBaseline = {
  positions: Float32Array;
  colors?: Float32Array;
  vertexColorsEnabled: boolean;
};

type OrbitControlsImpl = ElementRef<typeof OrbitControls>;

type ThreeModelController = ModelViewerHandle & {
  viewAlongAxis: (view: ModelViewerAxisView) => void;
};

type AxisViewTurn = {
  phi: number;
  started: boolean;
  theta: number;
};

const DEFAULT_CAMERA_POSITION = new Vector3(0, 0, 4);
const toonForward = new Vector3();
const toonDirection = new Vector3();
const ORBIT_CONTROLS_ZOOM_SPEED = 1.5;
const SMOOTH_ZOOM_DAMPING = 0.16;
const SMOOTH_ZOOM_DELTA_SCALE = 0.0015;
const SMOOTH_ZOOM_MAX_DELTA = 1 / 30;
const AXIS_VIEW_TURN_DAMPING = 0.2;
const AXIS_VIEW_TURN_EPSILON = 0.001;
const INTERACTIVE_COMMIT_WINDOW_MS = 150;
const MODEL_VIEWER_FIELD_OF_VIEW = 45;

const modelViewerRenderer = createThreeRenderer({
  mode: "webgl",
  alpha: true,
  antialias: true,
});

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
      computeState,
      bodyShapeOverrides,
      onError,
      onLoad,
      orientation,
      pixelRatio = window.devicePixelRatio,
      payloadEval,
      payloadTransport,
      form,
      src,
      threeEnvironment = "studio",
      threeExposure = 1,
      threeToneMapping = "neutral",
      toonShadows = false,
      toonStyle = DEFAULT_MODEL_VIEWER_TOON_STYLE,
      gimiToonStyle = DEFAULT_MODEL_VIEWER_GIMI_TOON_STYLE,
      genshinAssets,
      normalMaps = true,
      highlightGameTextures = false,
      onSurfacePick,
      onVisibleMeshesChange,
      overlay,
    },
    ref,
  ) {
    const controllerRef = useRef<ThreeModelController | null>(null);
    const keyLightRef = useRef<DirectionalLight | null>(null);
    const axisGizmoRef = useRef<ModelViewerAxisGizmoHandle | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        captureCameraState: () => controllerRef.current?.captureCameraState() ?? null,
        captureSquarePngBlob: async () =>
          (await controllerRef.current?.captureSquarePngBlob()) ?? null,
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

    const lighting = useMemo(() => modelViewerLighting(threeEnvironment), [threeEnvironment]);

    return (
      <div className={cn("relative h-full w-full", className)}>
        <ModelViewerAxisGizmo
          ref={axisGizmoRef}
          onSelect={(view) => controllerRef.current?.viewAlongAxis(view)}
        />
        <Canvas
          frameloop="demand"
          style={{ background: "transparent" }}
          camera={{
            far: 1000,
            fov: MODEL_VIEWER_FIELD_OF_VIEW,
            near: 0.01,
            position: DEFAULT_CAMERA_POSITION.toArray(),
          }}
          dpr={pixelRatio}
          gl={modelViewerRenderer}
        >
          <ambientLight intensity={lighting.ambient} />
          {lighting.hemisphere > 0 ? (
            <hemisphereLight
              intensity={lighting.hemisphere}
              groundColor={MODEL_VIEWER_HEMISPHERE_GROUND_COLOR}
              position={[0, 1, 0]}
            />
          ) : null}
          <directionalLight
            ref={keyLightRef}
            intensity={lighting.directionalKey}
            position={MODEL_VIEWER_KEY_LIGHT_POSITION}
          />
          <directionalLight
            intensity={lighting.directionalFill}
            position={MODEL_VIEWER_FILL_LIGHT_POSITION}
          />
          <ThreeModelScene
            axisGizmoRef={axisGizmoRef}
            controllerRef={controllerRef}
            keyLightRef={keyLightRef}
            animationClip={animationClip}
            animationFrame={animationFrame}
            computeState={computeState}
            bodyShapeOverrides={bodyShapeOverrides}
            orientation={orientation}
            src={src}
            threeEnvironment={threeEnvironment}
            threeExposure={threeExposure}
            threeToneMapping={threeToneMapping}
            toonShadows={toonShadows}
            toonStyle={toonStyle}
            gimiToonStyle={gimiToonStyle}
            genshinAssets={genshinAssets}
            normalMaps={normalMaps}
            highlightGameTextures={highlightGameTextures}
            onError={onError}
            onLoad={onLoad}
            payloadEval={payloadEval}
            payloadTransport={payloadTransport}
            form={form}
            onSurfacePick={onSurfacePick}
            onVisibleMeshesChange={onVisibleMeshesChange}
            overlay={overlay}
          />
        </Canvas>
      </div>
    );
  }),
);

function ThreeModelScene({
  axisGizmoRef,
  controllerRef,
  keyLightRef,
  animationClip,
  animationFrame,
  computeState,
  bodyShapeOverrides,
  onError,
  onLoad,
  orientation,
  payloadEval,
  payloadTransport,
  form,
  src,
  threeEnvironment = "studio",
  threeExposure = 1,
  threeToneMapping = "neutral",
  toonShadows = false,
  toonStyle = DEFAULT_MODEL_VIEWER_TOON_STYLE,
  gimiToonStyle = DEFAULT_MODEL_VIEWER_GIMI_TOON_STYLE,
  genshinAssets,
  normalMaps = true,
  highlightGameTextures = false,
  onSurfacePick,
  onVisibleMeshesChange,
  overlay,
}: ModelViewerSurfaceProps & {
  axisGizmoRef: MutableRefObject<ModelViewerAxisGizmoHandle | null>;
  controllerRef: MutableRefObject<ThreeModelController | null>;
  keyLightRef: MutableRefObject<DirectionalLight | null>;
  toonStyle?: ModelViewerToonStyle;
}) {
  const { camera, gl, invalidate, scene } = useThree();
  const axisViewTurnRef = useRef<AxisViewTurn | null>(null);
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
  const formRef = useRef(form);
  const toonShadowsRef = useRef(toonShadows);
  const animationClipRef = useRef(animationClip);
  const animationFrameRef = useRef(animationFrame);
  const animationValuesRef = useRef<Record<string, string | number>>({});
  const shapeWavePlaybackRef = useRef<ShapeWavePlayback | null>(null);
  const modelRootRef = useRef<Object3D | null>(null);
  const applyPayloadVisualsRef = useRef<(options?: { interactive?: boolean }) => void>(() => {});
  const lastInteractiveCommitAtRef = useRef(Number.NEGATIVE_INFINITY);
  const geometryFinalizeTimerRef = useRef<number | undefined>(undefined);
  const positionLoaderRef = useRef<ModelViewerPositionLoader | null>(null);
  const computeControllerRef = useRef<ModelViewerComputeController | null>(null);
  const computeStateRef = useRef(computeState);
  const payloadEvalAbortRef = useRef<AbortController | null>(null);
  const payloadEvalGenerationRef = useRef(0);
  const animationFrameIndexRef = useRef<number | null>(null);
  const preparedAnimationRingRef = useRef<
    Array<{ signature: string; prepared: PreparedPayloadEval }>
  >([]);
  const floatBufferCacheRef = useRef<Map<string, Promise<Float32Array>>>(new Map());
  const uint32BufferCacheRef = useRef<Map<string, Promise<Uint32Array>>>(new Map());
  const lastAppliedAnimationSignatureRef = useRef<string | null>(null);
  const bodyShapeBaselineRef = useRef<WeakMap<Mesh, BodyShapeBaseline>>(new WeakMap());
  const desiredCameraDistanceRef = useRef<number | null>(null);
  const zoomActiveRef = useRef(false);
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
    gl.toneMapping = modelViewerToneMapping(threeToneMapping);
    // oxlint-disable-next-line react/immutability
    gl.toneMappingExposure = Number.isFinite(threeExposure) ? threeExposure : 1;
    gl.setClearAlpha(0);
    invalidate();
  }, [gl, invalidate, threeExposure, threeToneMapping]);

  // The PMREM environment is a GPU prefilter pass; keep it off the exposure and
  // tone-mapping path so dragging the exposure slider does not rebuild it.
  useEffect(() => {
    if (threeEnvironment === "none") {
      // oxlint-disable-next-line react/immutability
      scene.environment = null;
      invalidate();
      return;
    }

    const environment = createModelViewerEnvironment(gl, threeEnvironment);

    // oxlint-disable-next-line react/immutability
    scene.environment = environment.texture;
    invalidate();

    return () => {
      if (scene.environment === environment.texture) {
        // oxlint-disable-next-line react/immutability
        scene.environment = null;
      }
      environment.dispose();
    };
  }, [gl, invalidate, scene, threeEnvironment]);

  useEffect(() => {
    onLoadRef.current = onLoad;
  }, [onLoad]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const onSurfacePickRef = useRef(onSurfacePick);
  useEffect(() => {
    onSurfacePickRef.current = onSurfacePick;
  }, [onSurfacePick]);

  const onVisibleMeshesChangeRef = useRef(onVisibleMeshesChange);
  useEffect(() => {
    onVisibleMeshesChangeRef.current = onVisibleMeshesChange;
  }, [onVisibleMeshesChange]);
  // Animation frames commit at playback rate, so only a changed set reports.
  const reportedVisibleMeshesRef = useRef<string | null>(null);

  const pickEnabled = !!onSurfacePick;
  useEffect(() => {
    if (!pickEnabled || !modelRoot) {
      return;
    }
    const element = gl.domElement;
    let pressed: { x: number; y: number } | null = null;
    // A pick resolves after its index buffer loads, so only the latest click of
    // this model may report.
    let latestPick = 0;
    const handleDown = (event: PointerEvent) => {
      pressed = event.button === 0 ? { x: event.clientX, y: event.clientY } : null;
    };
    // A drag rotates the camera; only a click that stays in place picks.
    const handleUp = (event: PointerEvent) => {
      const start = pressed;
      pressed = null;
      if (!start || Math.hypot(event.clientX - start.x, event.clientY - start.y) > 4) {
        return;
      }
      const pick = ++latestPick;
      const rect = element.getBoundingClientRect();
      const hit = raycastModelViewerSurface(
        modelRoot,
        camera,
        ((event.clientX - rect.left) / rect.width) * 2 - 1,
        -((event.clientY - rect.top) / rect.height) * 2 + 1,
      );
      if (!hit) {
        onSurfacePickRef.current?.(null);
        return;
      }
      void resolveModelViewerSurfacePick(hit, uint32BufferCacheRef.current)
        .then((resolved) => {
          if (pick === latestPick) onSurfacePickRef.current?.(resolved);
        })
        .catch((error: unknown) => {
          if (pick === latestPick) onErrorRef.current?.(error);
        });
    };
    element.addEventListener("pointerdown", handleDown);
    element.addEventListener("pointerup", handleUp);
    return () => {
      latestPick = -1;
      element.removeEventListener("pointerdown", handleDown);
      element.removeEventListener("pointerup", handleUp);
    };
  }, [camera, gl, modelRoot, pickEnabled]);

  useEffect(() => {
    payloadEvalRef.current = payloadEval;
    formRef.current = form;
  }, [form, payloadEval]);

  useEffect(() => {
    payloadTransportRef.current = payloadTransport;
    const waves = new ShapeWavePlayback(
      payloadTransport?.meshes.flatMap((mesh) => mesh.shapeTargets) ?? [],
    );
    shapeWavePlaybackRef.current = waves.empty ? null : waves;
  }, [payloadTransport]);

  useEffect(() => {
    animationClipRef.current = animationClip;
    animationFrameRef.current = animationFrame;
  }, [animationClip, animationFrame]);

  useEffect(() => {
    modelRootRef.current = modelRoot;
  }, [modelRoot]);

  useEffect(() => {
    applyPayloadVisualsRef.current = (options) => {
      const root = modelRootRef.current;
      const transport = payloadTransportRef.current;
      const toggleEval = payloadEvalRef.current;
      if (!root || !transport || !toggleEval) {
        return;
      }
      const interactive = options?.interactive ?? false;
      const animationValues = animationValuesRef.current;
      const evalResult = Object.keys(animationValues).length
        ? evaluateViewerState(
            transport,
            { ...toggleEval.state, ...animationValues },
            formRef.current,
          )
        : toggleEval;
      const loader = positionLoaderRef.current;
      if (!loader) {
        return;
      }
      payloadEvalAbortRef.current?.abort();
      const controller = new AbortController();
      payloadEvalAbortRef.current = controller;
      const generation = ++payloadEvalGenerationRef.current;
      const signature = payloadEvalSignature(evalResult);
      const cachedIndex = preparedAnimationRingRef.current.findIndex(
        (entry) => entry.signature === signature,
      );
      const cached =
        cachedIndex < 0 ? undefined : preparedAnimationRingRef.current.splice(cachedIndex, 1)[0];
      const commitLatest = (prepared: PreparedPayloadEval) => {
        if (
          controller.signal.aborted ||
          generation !== payloadEvalGenerationRef.current ||
          root !== modelRootRef.current
        ) {
          return;
        }
        // A slider drag commits on every pointer tick; recompute normals and
        // bounds only once the ticks settle instead of on each one.
        const now = performance.now();
        const deferGeometryUpdates =
          interactive && now - lastInteractiveCommitAtRef.current < INTERACTIVE_COMMIT_WINDOW_MS;
        if (interactive) {
          lastInteractiveCommitAtRef.current = now;
        }
        window.clearTimeout(geometryFinalizeTimerRef.current);
        commitPayloadEval(root, prepared, { deferGeometryUpdates });
        if (onVisibleMeshesChangeRef.current) {
          const visible = prepared.evalResult.meshes
            .filter((mesh) => mesh.visible)
            .map((mesh) => mesh.id);
          const signature = visible.join("\n");
          if (signature !== reportedVisibleMeshesRef.current) {
            reportedVisibleMeshesRef.current = signature;
            onVisibleMeshesChangeRef.current(new Set(visible));
          }
        }
        if (deferGeometryUpdates) {
          geometryFinalizeTimerRef.current = window.setTimeout(() => {
            if (root === modelRootRef.current && finalizePayloadGeometry(root)) {
              invalidate();
            }
          }, INTERACTIVE_COMMIT_WINDOW_MS);
        } else {
          // The cleared timer may have owned a mesh whose shape signature this
          // commit left unchanged, so nothing else would refresh it.
          finalizePayloadGeometry(root);
        }
        invalidate();
        // Defined below to keep the state-transition path together; refs make this
        // independent of render-time initialization order.
        // oxlint-disable-next-line react/immutability
        prefetchAnimationPositions({
          controller,
          generation,
          loader,
          root,
          toggleEval,
          transport,
        });
      };
      if (cached) {
        commitLatest(cached.prepared);
        return;
      }
      void preparePayloadEval(root, evalResult, loader, controller.signal)
        .then(commitLatest)
        .catch((error: unknown) => {
          if (error instanceof DOMException && error.name === "AbortError") {
            return;
          }
          onErrorRef.current?.(error);
        });
    };
  }, [invalidate]);

  useEffect(() => {
    const loadId = pendingLoadIdRef.current + 1;
    pendingLoadIdRef.current = loadId;

    let disposed = false;
    payloadEvalAbortRef.current?.abort();
    payloadEvalAbortRef.current = null;
    window.clearTimeout(geometryFinalizeTimerRef.current);
    preparedAnimationRingRef.current = [];
    reportedVisibleMeshesRef.current = null;
    positionLoaderRef.current?.dispose();
    positionLoaderRef.current = null;
    computeControllerRef.current?.dispose(false);
    computeControllerRef.current = null;

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
      const loadController = new AbortController();
      const positionLoader = new ModelViewerPositionLoader();
      positionLoaderRef.current = positionLoader;
      void buildPayloadModel(
        payloadTransport,
        payloadEvalRef.current ?? { state: {}, meshes: [] },
        true,
        positionLoader,
        toonShadowsRef.current,
        loadController.signal,
        getModelViewerTextureCapabilities(gl),
        createModelViewerMipmapper(gl),
      )
        .then((nextRoot) => {
          if (disposed || pendingLoadIdRef.current !== loadId) {
            disposeObjectTree(nextRoot);
            return;
          }
          applyModelViewerInitialRotation(nextRoot, payloadTransport.defaultYaw);
          materialRef.current = collectStandardMaterials(nextRoot);
          activeObjectRef.current = nextRoot;
          // oxlint-disable-next-line react/set-state-in-effect
          setModelRoot(nextRoot);
        })
        .catch((error) => {
          if (disposed || pendingLoadIdRef.current !== loadId) {
            return;
          }
          if (positionLoaderRef.current === positionLoader) {
            positionLoaderRef.current = null;
          }
          positionLoader.dispose();
          activeObjectRef.current = null;
          materialRef.current = [];
          onErrorRef.current?.(error);
        });
      return () => {
        disposed = true;
        loadController.abort();
        if (positionLoaderRef.current === positionLoader) {
          positionLoaderRef.current = null;
          positionLoader.dispose();
        }
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
        applyModelViewerInitialRotation(nextRoot);
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

  const computeDeformerId = animationClip?.deformerId;
  useEffect(() => {
    computeControllerRef.current?.dispose();
    computeControllerRef.current = null;
    const clip = animationClipRef.current;
    const deformer = payloadTransport?.computeDeformers.find(
      (candidate) => candidate.id === computeDeformerId,
    );
    if (
      !modelRoot ||
      modelRoot !== activeObjectRef.current ||
      !payloadTransport ||
      !deformer ||
      !clip
    ) {
      return;
    }
    const controller = new ModelViewerComputeController(
      modelRoot,
      deformer,
      payloadTransport.meshes,
      invalidate,
      (error) => onErrorRef.current?.(error),
    );
    computeControllerRef.current = controller;
    controller.setVariables(computeStateRef.current ?? {});
    const frameIndex = Math.min(
      Math.max(animationFrameIndexRef.current ?? animationFrameRef.current ?? 0, 0),
      Math.max(clip.frames.length - 1, 0),
    );
    controller.request({ clip, frameIndex });
    return () => {
      if (computeControllerRef.current === controller) {
        computeControllerRef.current = null;
      }
      controller.dispose();
    };
  }, [computeDeformerId, modelRoot, payloadTransport, invalidate]);

  useEffect(() => {
    computeStateRef.current = computeState;
    computeControllerRef.current?.setVariables(computeState ?? {});
  }, [computeState]);

  useEffect(() => {
    if (!animationClip?.deformerId) {
      return;
    }
    computeControllerRef.current?.request({
      clip: animationClip,
      frameIndex: Math.min(
        Math.max(animationFrame ?? 0, 0),
        Math.max(animationClip.frames.length - 1, 0),
      ),
    });
  }, [animationClip, animationFrame]);

  useEffect(() => {
    toonShadowsRef.current = toonShadows;
    if (!modelRoot) {
      return;
    }
    setPayloadToonShadows(modelRoot, toonShadows);
    invalidate();
  }, [invalidate, modelRoot, toonShadows]);

  useEffect(() => {
    if (!modelRoot) {
      return;
    }
    setPayloadToonStyle(modelRoot, toonStyle);
    invalidate();
  }, [invalidate, modelRoot, toonStyle]);

  const [genshinTextures] = useState(createGenshinToonTextureCache);
  useEffect(() => () => genshinTextures.dispose(), [genshinTextures]);

  const gimi = payloadTransport?.materialProfile === "gimi";
  useEffect(() => {
    if (!modelRoot || !gimi) {
      return;
    }
    let cancelled = false;
    void genshinTextures.resolve(genshinAssets, gimiToonStyle).then((textures) => {
      if (cancelled) {
        return;
      }
      setPayloadGIMIToon(modelRoot, { style: gimiToonStyle, textures });
      invalidate();
    });
    return () => {
      cancelled = true;
    };
  }, [genshinAssets, genshinTextures, gimi, gimiToonStyle, invalidate, modelRoot]);

  const profileToon = !toonShadows
    ? undefined
    : payloadTransport?.materialProfile === "efmi"
      ? toonStyle
      : gimi
        ? gimiToonStyle
        : undefined;
  const toonLighting = Boolean(profileToon);
  const selfShadow = profileToon?.selfShadow ?? false;
  useEffect(() => {
    const light = keyLightRef.current;
    if (!toonLighting || !modelRoot || !light) {
      return;
    }
    fitKeyLightShadowToObject(light, modelRoot);
    invalidate();
    return () => {
      light.target.position.set(0, 0, 0);
      light.target.updateMatrixWorld();
      light.position.set(...MODEL_VIEWER_KEY_LIGHT_POSITION);
      invalidate();
    };
  }, [toonLighting, invalidate, keyLightRef, modelRoot, orientation]);

  useEffect(() => {
    const light = keyLightRef.current;
    if (!selfShadow || !modelRoot || !light) {
      return;
    }
    light.castShadow = true;
    // oxlint-disable-next-line react/immutability
    gl.shadowMap.enabled = true;
    invalidate();
    return () => {
      light.castShadow = false;
      // oxlint-disable-next-line react/immutability
      gl.shadowMap.enabled = false;
      invalidate();
    };
  }, [gl, invalidate, keyLightRef, modelRoot, selfShadow]);

  const fieldOfView = profileToon?.fieldOfView ?? MODEL_VIEWER_FIELD_OF_VIEW;
  useEffect(() => {
    const controls = controlsRef.current;
    if (!(camera instanceof PerspectiveCamera) || camera.fov === fieldOfView) {
      return;
    }

    // Dolly along with the lens so the model keeps its size on screen.
    const scale =
      Math.tan(MathUtils.degToRad(camera.fov / 2)) / Math.tan(MathUtils.degToRad(fieldOfView / 2));
    if (controls) {
      camera.position.sub(controls.target).multiplyScalar(scale).add(controls.target);
      controls.update();
    }
    if (desiredCameraDistanceRef.current) {
      desiredCameraDistanceRef.current *= scale;
    }
    // oxlint-disable-next-line react/immutability
    camera.fov = fieldOfView;
    camera.updateProjectionMatrix();
    invalidate();
  }, [camera, fieldOfView, invalidate, modelRoot]);

  useFrame(() => {
    const light = keyLightRef.current;
    if (!profileToon || !light) {
      return;
    }
    aimKeyLight(
      light,
      profileToon.lightFollowsCamera
        ? modelViewerToonLightDirection(camera.getWorldDirection(toonForward), toonDirection)
        : modelViewerKeyLightDirection(toonDirection),
    );
  });

  useEffect(() => {
    if (!modelRoot) {
      return;
    }
    setPayloadNormalMaps(modelRoot, normalMaps);
    invalidate();
  }, [invalidate, modelRoot, normalMaps]);

  useEffect(() => {
    if (!modelRoot) {
      return;
    }
    setPayloadGameTextureHighlight(modelRoot, highlightGameTextures);
    invalidate();
  }, [highlightGameTextures, invalidate, modelRoot]);

  useEffect(() => {
    if (!modelRoot || !payloadEval || !payloadTransport) {
      return;
    }
    applyPayloadVisualsRef.current({ interactive: true });
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
    const renderNow = () => gl.render(scene, camera);
    // oxlint-disable-next-line react/immutability
    controllerRef.current = {
      captureCameraState: () =>
        captureThreeCameraState(camera, controlsRef.current, groupRef.current),
      captureSquarePngBlob: () => captureSquareCanvasPngBlob(gl.domElement, renderNow),
      captureSquarePngDataUrl: () => captureSquareCanvasPngDataUrl(gl.domElement, renderNow),
      restoreCameraState: (state, options) => {
        axisViewTurnRef.current = null;
        restoreThreeCameraState(camera, controlsRef.current, groupRef.current, state, options);
        desiredCameraDistanceRef.current = getPerspectiveCameraDistance(
          camera,
          controlsRef.current,
        );
        invalidate();
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
          animationFrameIndexRef.current = null;
        } else {
          const frameIndex = Math.min(Math.max(index, 0), clip.frames.length - 1);
          const frame = clip.frames[frameIndex];
          const waves = shapeWavePlaybackRef.current;
          animationFrameIndexRef.current = frameIndex;
          if (clip.deformerId) {
            computeControllerRef.current?.request({ clip, frameIndex });
          }
          if (waves && frame) {
            const values = {
              ...frame.values,
              ...waves.advance(clip.id, frame.time, clip.frames.length / clip.fps, {
                ...payloadEvalRef.current?.state,
                ...frame.values,
              }),
            };
            // A wave clip plays even while every wave is off, which leaves nothing to redraw.
            const unchanged = isEqual(values, animationValuesRef.current);
            animationValuesRef.current = values;
            if (unchanged) {
              return;
            }
          } else {
            animationValuesRef.current = frame?.values ?? {};
          }
        }
        applyPayloadVisualsRef.current();
      },
      updateFraming: async () => {
        axisViewTurnRef.current = null;
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
      viewAlongAxis: (view) => {
        axisViewTurnRef.current = { phi: view.phi, started: false, theta: view.theta };
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
  }, [camera, gl, invalidate, scene]);

  useFrame((_, delta) => {
    const controls = controlsRef.current;
    const turn = axisViewTurnRef.current;
    if (!controls || !turn) {
      return;
    }

    const spherical = new Spherical().setFromVector3(camera.position.clone().sub(controls.target));
    const thetaError =
      MathUtils.euclideanModulo(turn.theta - spherical.theta + Math.PI, Math.PI * 2) - Math.PI;
    const phiError = turn.phi - spherical.phi;
    const turnDelta = turn.started ? delta : Math.min(delta, SMOOTH_ZOOM_MAX_DELTA);
    turn.started = true;

    const finished =
      Math.abs(thetaError) < AXIS_VIEW_TURN_EPSILON && Math.abs(phiError) < AXIS_VIEW_TURN_EPSILON;
    const step = finished ? 1 : 1 - Math.pow(1 - AXIS_VIEW_TURN_DAMPING, turnDelta * 60);
    spherical.theta += thetaError * step;
    spherical.phi += phiError * step;
    camera.position.setFromSpherical(spherical).add(controls.target);
    controls.update();
    if (finished) {
      axisViewTurnRef.current = null;
    }
    invalidate();
  });

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

    // Cap only the first delta after idle; active zoom still follows elapsed
    // time at low frame rates, matching continuous rendering.
    const zoomDelta = zoomActiveRef.current ? delta : Math.min(delta, SMOOTH_ZOOM_MAX_DELTA);
    const step = 1 - Math.pow(1 - SMOOTH_ZOOM_DAMPING, zoomDelta * 60);
    const nextDistance = MathUtils.lerp(currentDistance, desiredDistance, step);
    if (Math.abs(nextDistance - currentDistance) < 0.0001) {
      zoomActiveRef.current = false;
      if (Math.abs(desiredDistance - currentDistance) < 0.0001) {
        desiredCameraDistanceRef.current = currentDistance;
      }
      return;
    }

    zoomActiveRef.current = true;
    camera.position.copy(
      controls.target.clone().add(offset.normalize().multiplyScalar(nextDistance)),
    );
    controls.update();
    invalidate();
  });

  useFrame(() => {
    axisGizmoRef.current?.update(camera.quaternion);
  });

  useEffect(() => {
    return () => {
      payloadEvalAbortRef.current?.abort();
      payloadEvalAbortRef.current = null;
      window.clearTimeout(geometryFinalizeTimerRef.current);
      positionLoaderRef.current?.dispose();
      positionLoaderRef.current = null;
      computeControllerRef.current?.dispose(false);
      computeControllerRef.current = null;
      preparedAnimationRingRef.current = [];
      floatBufferCacheRef.current.clear();
      uint32BufferCacheRef.current.clear();
      gl.renderLists.dispose();
      if (activeObjectRef.current) {
        disposeObjectTree(activeObjectRef.current);
        activeObjectRef.current = null;
      }
      materialRef.current = [];
      lastAppliedAnimationSignatureRef.current = null;
    };
  }, [gl]);

  function prefetchAnimationPositions({
    controller,
    generation,
    loader,
    root,
    toggleEval,
    transport,
  }: {
    controller: AbortController;
    generation: number;
    loader: ModelViewerPositionLoader;
    root: Object3D;
    toggleEval: NonNullable<typeof payloadEval>;
    transport: NonNullable<typeof payloadTransport>;
  }) {
    const clip = animationClipRef.current;
    const currentIndex = animationFrameIndexRef.current;
    // A wave phase depends on the frames before it, which a frame alone does not tell.
    if (!clip?.frames.length || currentIndex === null || shapeWavePlaybackRef.current) {
      preparedAnimationRingRef.current = [];
      return;
    }
    const upcoming = [1, 2].flatMap((offset) => {
      const candidate = currentIndex + offset;
      const index =
        candidate < clip.frames.length
          ? candidate
          : clip.loop
            ? candidate % clip.frames.length
            : -1;
      const frame = index < 0 ? undefined : clip.frames[index];
      if (!frame) {
        return [];
      }
      const evaluated = evaluateViewerState(
        transport,
        { ...toggleEval.state, ...frame.values },
        formRef.current,
      );
      return [{ evaluated, signature: payloadEvalSignature(evaluated) }];
    });
    const seen = new Set<string>();
    for (const { evaluated, signature } of upcoming) {
      if (
        seen.has(signature) ||
        preparedAnimationRingRef.current.some((entry) => entry.signature === signature)
      ) {
        continue;
      }
      seen.add(signature);
      void preparePayloadEval(root, evaluated, loader, controller.signal, true)
        .then((prepared) => {
          if (
            controller.signal.aborted ||
            generation !== payloadEvalGenerationRef.current ||
            root !== modelRootRef.current
          ) {
            return;
          }
          preparedAnimationRingRef.current = [
            ...preparedAnimationRingRef.current.filter((entry) => entry.signature !== signature),
            { signature, prepared },
          ].slice(-2);
        })
        .catch((error: unknown) => {
          if (!(error instanceof DOMException && error.name === "AbortError")) {
            onErrorRef.current?.(error);
          }
        });
    }
  }

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

  return (
    <>
      <OrbitControls
        ref={controlsRef}
        dampingFactor={0.08}
        enableDamping
        enableZoom={false}
        makeDefault
        onStart={() => {
          axisViewTurnRef.current = null;
        }}
        zoomSpeed={ORBIT_CONTROLS_ZOOM_SPEED}
      />
      <group ref={groupRef} rotation={rotation}>
        {modelRoot ? <primitive object={modelRoot} /> : null}
        {modelRoot && overlay ? (
          <group
            position={modelRoot.position}
            quaternion={modelRoot.quaternion}
            scale={modelRoot.scale}
          >
            {overlay}
          </group>
        ) : null}
      </group>
    </>
  );
}

async function loadFloatBuffer(
  sourcePath: string,
  cache: Map<string, Promise<Float32Array>>,
): Promise<Float32Array> {
  const existing = cache.get(sourcePath);
  if (existing) {
    return existing;
  }

  const request = fetchFloat32(modelViewerSourceToUrl(sourcePath)).catch((error) => {
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

  const request = fetchUint32(modelViewerSourceToUrl(sourcePath)).catch((error) => {
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
    (frame.meshes ?? []).map(async (mesh) => {
      const [indices, position, normal, tangent, texcoord0] = await Promise.all([
        loadOptionalUint32AnimationBuffer(
          mesh.indicesBufferId,
          mesh.indicesPath,
          sharedBufferPathMap,
          uint32Cache,
        ),
        loadRequiredFloatAnimationBuffer(
          mesh.positionBufferId,
          mesh.positionPath,
          sharedBufferPathMap,
          floatCache,
          `animation position buffer for ${mesh.meshName}`,
        ),
        loadOptionalFloatAnimationBuffer(
          mesh.normalBufferId,
          mesh.normalPath,
          sharedBufferPathMap,
          floatCache,
        ),
        loadOptionalFloatAnimationBuffer(
          mesh.tangentBufferId,
          mesh.tangentPath,
          sharedBufferPathMap,
          floatCache,
        ),
        loadOptionalFloatAnimationBuffer(
          mesh.texcoord0BufferId,
          mesh.texcoord0Path,
          sharedBufferPathMap,
          floatCache,
        ),
      ]);
      return { meshName: mesh.meshName, indices, position, normal, tangent, texcoord0 };
    }),
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

  const center = frameCameraOnObject(camera, object);
  if (!center) {
    return null;
  }

  controls.target.copy(center);
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
  render: () => void,
): Promise<string | null> {
  const canvas = await copySquareCanvas(sourceCanvas, render);
  if (!canvas) {
    return null;
  }

  try {
    return canvas.toDataURL("image/png");
  } catch (error) {
    Logger.capture("model-viewer:capture-preview", error);
    return null;
  }
}

async function captureSquareCanvasPngBlob(
  sourceCanvas: HTMLCanvasElement | null,
  render: () => void,
): Promise<Blob | null> {
  const canvas = await copySquareCanvas(sourceCanvas, render);
  if (!canvas) {
    return null;
  }

  return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
}

async function copySquareCanvas(
  sourceCanvas: HTMLCanvasElement | null,
  render: () => void,
): Promise<HTMLCanvasElement | null> {
  if (!sourceCanvas) {
    return null;
  }

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

  // The drawing buffer is not preserved across frames, so the copy must
  // follow a render in the same task.
  try {
    render();
    context.drawImage(sourceCanvas, cropX, cropY, size, size, 0, 0, size, size);
    return canvas;
  } catch (error) {
    Logger.capture("model-viewer:capture-preview", error);
    return null;
  }
}

function waitForNextFrame(): Promise<void> {
  return new Promise((resolve) => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => resolve());
    });
  });
}

function payloadEvalSignature(evalResult: NonNullable<ModelViewerSurfaceProps["payloadEval"]>) {
  return JSON.stringify(
    evalResult.meshes.map((mesh) => ({
      id: mesh.id,
      visible: mesh.visible,
      texKey: mesh.texKey,
      normalMapKey: mesh.normalMapKey,
      lightMapKey: mesh.lightMapKey,
      materialMapKey: mesh.materialMapKey,
      cutoutMapKey: mesh.cutoutMapKey,
      highlightMapKey: mesh.highlightMapKey,
      rampMapKey: mesh.rampMapKey,
      shapeWeights: mesh.shapeWeights,
      positionVariantIndex: mesh.positionVariantIndex,
    })),
  );
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
  clearPayloadModelData(root);
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

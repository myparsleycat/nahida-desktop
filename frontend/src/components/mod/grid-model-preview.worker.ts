import { getModelViewerTextureCapabilities } from "@renderer/components/tools/model-viewer/model-viewer-dds";
import {
    buildPayloadModel,
    clearPayloadModelData,
} from "@renderer/components/tools/model-viewer/model-viewer-payload";
import { ModelViewerPositionLoader } from "@renderer/components/tools/model-viewer/model-viewer-position-loader";
import {
    createModelViewerEnvironment,
    frameCameraOnObject,
    MODEL_VIEWER_FILL_LIGHT_POSITION,
    MODEL_VIEWER_HEMISPHERE_GROUND_COLOR,
    MODEL_VIEWER_KEY_LIGHT_POSITION,
    modelViewerLighting,
    modelViewerToneMapping,
} from "@renderer/components/tools/model-viewer/model-viewer-scene";
import {
    MODEL_VIEWER_UPRIGHT_ROTATION,
    needsUprightCorrection,
} from "@renderer/components/tools/model-viewer/model-viewer-upright";
import { serializeDiagnostic } from "@shared/diagnostic";
import type { EvaluatedViewerState, ModViewerTransport } from "@shared/mod-viewer/types";
import {
    AmbientLight,
    DirectionalLight,
    HemisphereLight,
    Mesh,
    type Object3D,
    PerspectiveCamera,
    Scene,
    type Texture,
    WebGLRenderer,
} from "three";

import type { ModelPreviewRenderSettings } from "./grid-model-preview";

export type GridModelPreviewRenderRequest = {
    id: number;
    size: number;
    transport: ModViewerTransport;
    evaluated: EvaluatedViewerState;
    settings: ModelPreviewRenderSettings;
};

export type GridModelPreviewRenderResponse =
    | { id: number; blob: Blob }
    | { id: number; blob?: undefined; message: string; diagnostic: unknown };

type WorkerScope = {
    onmessage: ((event: MessageEvent<GridModelPreviewRenderRequest>) => void) | null;
    postMessage(message: GridModelPreviewRenderResponse): void;
};

type Stage = {
    canvas: OffscreenCanvas;
    renderer: WebGLRenderer;
    environment?: {
        name: ModelPreviewRenderSettings["environment"];
        texture: Texture;
        dispose: () => void;
    };
};

const scope = self as unknown as WorkerScope;
let stage: Stage | undefined;

scope.onmessage = (event) => {
    const request = event.data;
    render(request).then(
        (blob) => scope.postMessage({ id: request.id, blob }),
        (error: unknown) =>
            scope.postMessage({
                id: request.id,
                message: error instanceof Error ? error.message : String(error),
                diagnostic: serializeDiagnostic({ error }),
            }),
    );
};

async function render(request: GridModelPreviewRenderRequest): Promise<Blob> {
    const { settings } = request;
    stage ??= createStage(request.size);
    const { canvas, renderer } = stage;
    renderer.toneMapping = modelViewerToneMapping(settings.toneMapping);
    renderer.toneMappingExposure = Number.isFinite(settings.exposure) ? settings.exposure : 1;

    const scene = createScene(settings.environment);
    scene.environment = environmentTexture(stage, settings.environment);
    const camera = new PerspectiveCamera(45, 1, 0.01, 1000);
    camera.position.set(0, 0, 4);

    const positionLoader = new ModelViewerPositionLoader();
    let root: Object3D | undefined;
    try {
        root = await buildPayloadModel(
            visibleTransport(request.transport, request.evaluated),
            request.evaluated,
            true,
            positionLoader,
            settings.toonShadows,
            undefined,
            getModelViewerTextureCapabilities(renderer),
        );
        if (needsUprightCorrection(root)) {
            root.rotation.copy(MODEL_VIEWER_UPRIGHT_ROTATION);
        }
        scene.add(root);
        const center = frameCameraOnObject(camera, root);
        if (center) {
            camera.lookAt(center);
        }

        // Linking in parallel keeps the GPU process, which also composites the
        // main window, from blocking on this context's shader programs.
        await renderer.compileAsync(scene, camera);
        renderer.render(scene, camera);
        if (renderer.getContext().isContextLost()) {
            throw new Error("Model preview WebGL context was lost.");
        }
        return await canvas.convertToBlob({ type: "image/png" });
    } finally {
        positionLoader.dispose();
        if (root) {
            disposeModel(root);
        }
        renderer.renderLists.dispose();
    }
}

// A still image shows one toggle state, so meshes it hides and textures only
// other states use are never fetched.
function visibleTransport(
    transport: ModViewerTransport,
    evaluated: EvaluatedViewerState,
): ModViewerTransport {
    const hidden = new Set(evaluated.meshes.filter((mesh) => !mesh.visible).map((mesh) => mesh.id));
    const used = new Set(
        evaluated.meshes
            .filter((mesh) => mesh.visible)
            .flatMap((mesh) => [
                mesh.texKey,
                mesh.normalMapKey,
                mesh.lightMapKey,
                mesh.materialMapKey,
            ]),
    );
    return {
        ...transport,
        meshes: transport.meshes.filter((mesh) => !hidden.has(mesh.id)),
        textures: Object.fromEntries(
            Object.entries(transport.textures).filter(([key]) => used.has(key)),
        ),
    };
}

function createStage(size: number): Stage {
    const canvas = new OffscreenCanvas(size, size);
    const renderer = new WebGLRenderer({
        canvas,
        alpha: true,
        antialias: true,
        powerPreference: "high-performance",
    });
    renderer.setClearAlpha(0);
    return { canvas, renderer };
}

function createScene(environment: ModelPreviewRenderSettings["environment"]): Scene {
    const lighting = modelViewerLighting(environment);
    const scene = new Scene();
    scene.add(new AmbientLight(0xffffff, lighting.ambient));
    if (lighting.hemisphere > 0) {
        scene.add(
            new HemisphereLight(
                0xffffff,
                MODEL_VIEWER_HEMISPHERE_GROUND_COLOR,
                lighting.hemisphere,
            ),
        );
    }

    const key = new DirectionalLight(0xffffff, lighting.directionalKey);
    key.position.set(...MODEL_VIEWER_KEY_LIGHT_POSITION);
    const fill = new DirectionalLight(0xffffff, lighting.directionalFill);
    fill.position.set(...MODEL_VIEWER_FILL_LIGHT_POSITION);
    return scene.add(key, fill);
}

function environmentTexture(
    current: Stage,
    name: ModelPreviewRenderSettings["environment"],
): Texture | null {
    if (current.environment?.name !== name) {
        current.environment?.dispose();
        current.environment =
            name === "none"
                ? undefined
                : { name, ...createModelViewerEnvironment(current.renderer, name) };
    }
    return current.environment?.texture ?? null;
}

function disposeModel(root: Object3D) {
    root.traverse((child) => {
        if (!(child instanceof Mesh)) {
            return;
        }
        child.geometry.dispose();
        for (const material of Array.isArray(child.material) ? child.material : [child.material]) {
            material.dispose();
        }
    });
    clearPayloadModelData(root);
}

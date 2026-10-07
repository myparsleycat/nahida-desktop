import {
    ACESFilmicToneMapping,
    Box3,
    NeutralToneMapping,
    NoToneMapping,
    type Object3D,
    type PerspectiveCamera,
    PMREMGenerator,
    Scene,
    type Texture,
    type ToneMapping,
    Vector3,
    type WebGLRenderer,
} from "three";
import { RoomEnvironment } from "three/examples/jsm/environments/RoomEnvironment.js";

import type {
    ModelViewerThreeEnvironment,
    ModelViewerThreeToneMapping,
} from "./model-viewer-contract";

// Shared by the interactive viewer and the grid preview worker so a preview
// image matches what the viewer shows for the same settings.
export function modelViewerLighting(environment: ModelViewerThreeEnvironment) {
    switch (environment) {
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
}

export const MODEL_VIEWER_HEMISPHERE_GROUND_COLOR = "#b9bec7";
export const MODEL_VIEWER_KEY_LIGHT_POSITION = [6, 8, 10] as const;
export const MODEL_VIEWER_FILL_LIGHT_POSITION = [-6, 4, -8] as const;

export function modelViewerToneMapping(toneMapping: ModelViewerThreeToneMapping): ToneMapping {
    if (toneMapping === "aces") {
        return ACESFilmicToneMapping;
    }
    return toneMapping === "none" ? NoToneMapping : NeutralToneMapping;
}

export function createModelViewerEnvironment(
    renderer: WebGLRenderer,
    environment: Exclude<ModelViewerThreeEnvironment, "none">,
): { texture: Texture; dispose: () => void } {
    const pmremGenerator = new PMREMGenerator(renderer);
    const roomEnvironment = new RoomEnvironment();
    roomEnvironment.scale.setScalar(environment === "soft" ? 0.85 : 1);
    const target = pmremGenerator.fromScene(new Scene().add(roomEnvironment));

    return {
        texture: target.texture,
        dispose: () => {
            target.dispose();
            roomEnvironment.dispose();
            pmremGenerator.dispose();
        },
    };
}

// Moves the camera to the default three-quarter view of the object and returns
// the point it should look at, or null when the object has no geometry.
export function frameCameraOnObject(camera: PerspectiveCamera, object: Object3D): Vector3 | null {
    object.updateMatrixWorld(true);
    const bounds = new Box3().setFromObject(object);
    if (bounds.isEmpty()) {
        return null;
    }

    const center = bounds.getCenter(new Vector3());
    const size = bounds.getSize(new Vector3());
    const radius = Math.max(size.x, size.y, size.z) * 0.5 || 1;
    const distance = Math.max(radius / Math.sin((camera.fov * Math.PI) / 360), radius * 1.8);

    camera.position.copy(
        center.clone().add(new Vector3(distance * 0.45, distance * 0.15, distance)),
    );
    camera.near = Math.max(distance / 100, 0.01);
    camera.far = Math.max(distance * 20, 100);
    camera.updateProjectionMatrix();
    return center;
}

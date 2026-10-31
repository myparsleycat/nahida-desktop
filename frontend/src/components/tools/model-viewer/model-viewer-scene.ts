import {
    ACESFilmicToneMapping,
    Box3,
    type DirectionalLight,
    MathUtils,
    NeutralToneMapping,
    NoToneMapping,
    type Object3D,
    type PerspectiveCamera,
    PMREMGenerator,
    Scene,
    Sphere,
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

// The game aims its menu light from the camera, turned 10 degrees and kept at least
// 32 degrees above the horizon.
export function modelViewerToonLightDirection(forward: Vector3, target: Vector3): Vector3 {
    const yaw = Math.atan2(-forward.x, forward.z) + MathUtils.degToRad(10);
    const pitch = Math.max(MathUtils.degToRad(32), Math.asin(MathUtils.clamp(-forward.y, -1, 1)));
    return target.set(
        Math.sin(yaw) * Math.cos(pitch),
        Math.sin(pitch),
        -Math.cos(yaw) * Math.cos(pitch),
    );
}

export function modelViewerKeyLightDirection(target: Vector3): Vector3 {
    return target.set(...MODEL_VIEWER_KEY_LIGHT_POSITION).normalize();
}

export function aimKeyLight(light: DirectionalLight, direction: Vector3): void {
    light.position
        .copy(light.target.position)
        .addScaledVector(direction, (light.shadow.camera.far / 4) * 2 || 1);
}

export function fitKeyLightShadowToObject(light: DirectionalLight, object: Object3D): void {
    const bounds = new Box3().setFromObject(object);
    if (bounds.isEmpty()) {
        return;
    }

    const sphere = bounds.getBoundingSphere(new Sphere());
    const radius = sphere.radius * 1.2 || 1;
    light.target.position.copy(sphere.center);
    light.target.updateMatrixWorld();

    const camera = light.shadow.camera;
    camera.left = -radius;
    camera.right = radius;
    camera.top = radius;
    camera.bottom = -radius;
    camera.near = radius * 0.1;
    camera.far = radius * 4;
    camera.updateProjectionMatrix();
    light.shadow.mapSize.set(4096, 4096);
    light.shadow.bias = -0.0005;
    light.shadow.normalBias = radius * 0.003;
}

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

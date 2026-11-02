import type {
    EvaluatedViewerState,
    ModViewerTransport,
    ViewerAnimationClip,
    ViewerAnimationFrame,
    ViewerStateValue,
} from "@shared/mod-viewer/types";
import type { ReactNode } from "react";

export type ModelViewerThreeToneMapping = "neutral" | "aces" | "none";
export type ModelViewerThreeEnvironment = "studio" | "soft" | "none";
export type ModelViewerVariantStateValue = ViewerStateValue;

export type ModelViewerToonStyle = {
    rangeBias: number;
    softness: number;
    shadowBrightness: number;
    shadowSaturation: number;
    ambient: number;
    lightStrength: number;
    brightness: number;
    specular: number;
    rimStrength: number;
    rimWidth: number;
    outlines: boolean;
    outlineWidth: number;
    outlineBrightness: number;
    selfShadow: boolean;
    lightFollowsCamera: boolean;
    flatFace: boolean;
    fieldOfView: number;
};

export const DEFAULT_MODEL_VIEWER_TOON_STYLE: ModelViewerToonStyle = {
    rangeBias: -0.4,
    softness: 0.02,
    shadowBrightness: 0.5,
    shadowSaturation: 1,
    ambient: 0.8,
    lightStrength: 0.9,
    // Warfarin defaults to 2 and clips less because its materials carry tints that mods
    // lack. This value approximates its output for the same textures.
    brightness: 1.5,
    specular: 1,
    rimStrength: 0.5,
    rimWidth: 0.3,
    outlines: true,
    outlineWidth: 0.5,
    outlineBrightness: 0.5,
    selfShadow: false,
    lightFollowsCamera: true,
    flatFace: true,
    fieldOfView: 20,
};

export const MODEL_VIEWER_TOON_RANGES = {
    rangeBias: { min: -1, max: 1, step: 0.05 },
    softness: { min: 0, max: 0.25, step: 0.01 },
    shadowBrightness: { min: 0, max: 1, step: 0.05 },
    shadowSaturation: { min: 0, max: 2, step: 0.05 },
    ambient: { min: 0, max: 1.5, step: 0.05 },
    lightStrength: { min: 0, max: 2, step: 0.05 },
    brightness: { min: 0.5, max: 3, step: 0.05 },
    specular: { min: 0, max: 2, step: 0.05 },
    rimStrength: { min: 0, max: 2, step: 0.05 },
    rimWidth: { min: 0, max: 1, step: 0.05 },
    outlineWidth: { min: 0, max: 5, step: 0.25 },
    outlineBrightness: { min: 0, max: 1, step: 0.05 },
    fieldOfView: { min: 10, max: 45, step: 1 },
} as const;

export function parseModelViewerToonStyle(stored: string): ModelViewerToonStyle {
    const parsed = parseToonStyleRecord(stored);
    const number = (key: keyof typeof MODEL_VIEWER_TOON_RANGES) => {
        const value = parsed[key];
        const range = MODEL_VIEWER_TOON_RANGES[key];
        return typeof value === "number" && Number.isFinite(value)
            ? Math.min(Math.max(value, range.min), range.max)
            : DEFAULT_MODEL_VIEWER_TOON_STYLE[key];
    };
    const flag = (key: "outlines" | "selfShadow" | "lightFollowsCamera" | "flatFace") => {
        const value = parsed[key];
        return typeof value === "boolean" ? value : DEFAULT_MODEL_VIEWER_TOON_STYLE[key];
    };
    return {
        rangeBias: number("rangeBias"),
        softness: number("softness"),
        shadowBrightness: number("shadowBrightness"),
        shadowSaturation: number("shadowSaturation"),
        ambient: number("ambient"),
        lightStrength: number("lightStrength"),
        brightness: number("brightness"),
        specular: number("specular"),
        rimStrength: number("rimStrength"),
        rimWidth: number("rimWidth"),
        outlines: flag("outlines"),
        outlineWidth: number("outlineWidth"),
        outlineBrightness: number("outlineBrightness"),
        selfShadow: flag("selfShadow"),
        lightFollowsCamera: flag("lightFollowsCamera"),
        flatFace: flag("flatFace"),
        fieldOfView: number("fieldOfView"),
    };
}

function parseToonStyleRecord(stored: string): Record<string, unknown> {
    try {
        const parsed: unknown = JSON.parse(stored);
        return typeof parsed === "object" && parsed !== null
            ? (parsed as Record<string, unknown>)
            : {};
    } catch {
        return {};
    }
}

export type ModelViewerRealtimeShapeKey = {
    targetMeshPrefixes: string[];
    basePath: string;
    vertexStride: number;
    positionOffset: number;
    normalOffset: number;
    tangentOffset: number;
    dimensions: Array<{
        variableId: string;
        smallerPath: string;
        biggerPath: string;
    }>;
};

export type ModelViewerAnimationFrameMesh = {
    meshName: string;
    indicesBufferId?: string;
    indicesPath?: string;
    positionBufferId?: string;
    positionPath?: string;
    normalBufferId?: string;
    normalPath?: string;
    tangentBufferId?: string;
    tangentPath?: string;
    texcoord0BufferId?: string;
    texcoord0Path?: string;
};

export type ModelViewerAnimationFrame = ViewerAnimationFrame & {
    meshes?: ModelViewerAnimationFrameMesh[];
};

export type ModelViewerAnimationSharedBuffer = {
    id: string;
    path: string;
};

export type ModelViewerAnimationClip = Omit<ViewerAnimationClip, "frames"> & {
    sharedBuffers?: ModelViewerAnimationSharedBuffer[];
    frames: ModelViewerAnimationFrame[];
};

export type ModelViewerCameraState = {
    orbit: string;
    target: string;
    fieldOfView: string;
    position?: string;
    anchor?: string;
};

export type ModelViewerHandle = {
    captureCameraState: () => ModelViewerCameraState | null;
    captureSquarePngBlob: () => Promise<Blob | null>;
    captureSquarePngDataUrl: () => Promise<string | null>;
    restoreCameraState: (
        state: ModelViewerCameraState | null,
        options?: {
            includeFieldOfView?: boolean;
        },
    ) => void;
    updateFraming: () => Promise<void> | void;
    setDoubleSided: (doubleSided: boolean) => Promise<void> | void;
    setAnimationFrame: (index: number) => void;
};

export type ModelViewerBodyShapeOverride = {
    meshNames: string[];
    positions: Float32Array;
    /** When set, enables vertexColors and writes RGB colors (vertexCount * 3). */
    vertexColors?: Float32Array;
    /**
     * When false, positions are unchanged since the last apply — callers should skip
     * position writeback, normal recomputation, and bounding volume updates, only
     * refreshing vertex colors. Defaults to true (treat as changed when unknown).
     */
    positionsChanged?: boolean;
};

export type ModelViewerVec3 = [number, number, number];

/** A clicked mesh face. Vectors are in the mesh's own (source buffer) space. */
export type ModelViewerSurfacePick = {
    meshId: string;
    faceIndex: number;
    /** Source vertex buffer indices of the face corners, in viewer corner order. */
    vertices: ModelViewerVec3;
    barycentric: ModelViewerVec3;
    triangle: [ModelViewerVec3, ModelViewerVec3, ModelViewerVec3];
    point: ModelViewerVec3;
    normal: ModelViewerVec3;
};

export type ModelViewerSurfaceProps = {
    className?: string;
    pixelRatio?: number;
    orientation: string;
    src?: string;
    payloadTransport?: ModViewerTransport;
    payloadEval?: EvaluatedViewerState;
    /** The form payloadEval was evaluated for; animation frames keep it. */
    form?: string;
    variantState?: Record<string, ModelViewerVariantStateValue>;
    shapeKeys?: ModelViewerRealtimeShapeKey[];
    animationClip?: ModelViewerAnimationClip;
    animationFrame?: number;
    /** The variables a compute deformer reads while it plays. */
    computeState?: Record<string, ModelViewerVariantStateValue>;
    /** Live position overrides for body-shape editing (skips shape keys / animation). */
    bodyShapeOverrides?: ModelViewerBodyShapeOverride[];
    threeToneMapping?: ModelViewerThreeToneMapping;
    threeEnvironment?: ModelViewerThreeEnvironment;
    threeExposure?: number;
    toonShadows?: boolean;
    toonStyle?: ModelViewerToonStyle;
    normalMaps?: boolean;
    /** Tints the visible meshes the mod binds no diffuse texture to. */
    highlightGameTextures?: boolean;
    /** Called for a left click without drag; null when the click hit no visible mesh. */
    onSurfacePick?: (pick: ModelViewerSurfacePick | null) => void;
    /**
     * Called with the IDs of the payload meshes drawn, after toggles, form and
     * the current animation frame are applied; only when that set changes.
     */
    onVisibleMeshesChange?: (meshIds: ReadonlySet<string>) => void;
    /** Rendered in the model's space, so it can use the coordinates of a surface pick. */
    overlay?: ReactNode;
    onError?: (error: unknown) => void;
    onLoad?: () => void;
};

export function parseOrientation(orientation: string): [number, number, number] {
    const [roll = "0deg", pitch = "0deg", yaw = "0deg"] = orientation.split(/\s+/);
    return [roll, pitch, yaw].map((value) => Number.parseFloat(value) || 0) as [
        number,
        number,
        number,
    ];
}

export function formatOrientation([roll, pitch, yaw]: [number, number, number]): string {
    return [roll, pitch, yaw].map((value) => `${normalizeDegrees(value)}deg`).join(" ");
}

function normalizeDegrees(value: number): number {
    const normalized = ((value % 360) + 360) % 360;
    return normalized > 180 ? normalized - 360 : normalized;
}

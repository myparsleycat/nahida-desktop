import type { GenshinToonAssetsTransport } from "@bindings/tools/model_viewer";
import type {
    EvaluatedViewerState,
    ModViewerTransport,
    ViewerAnimationClip,
    ViewerAnimationFrame,
    ViewerStateValue,
} from "@shared/mod-viewer/types";
import { pick } from "es-toolkit";
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
    selfShadow: true,
    lightFollowsCamera: true,
    flatFace: true,
    fieldOfView: 20,
};

// Only the flags are user settings. Shading values stay at the defaults even when
// older versions stored edited values, because the menu no longer shows them.
export function parseModelViewerToonStyle(stored: string): ModelViewerToonStyle {
    const parsed = parseToonStyleRecord(stored);
    const flag = (key: "outlines" | "selfShadow" | "lightFollowsCamera" | "flatFace") => {
        const value = parsed[key];
        return typeof value === "boolean" ? value : DEFAULT_MODEL_VIEWER_TOON_STYLE[key];
    };
    return {
        ...DEFAULT_MODEL_VIEWER_TOON_STYLE,
        outlines: flag("outlines"),
        selfShadow: flag("selfShadow"),
        lightFollowsCamera: flag("lightFollowsCamera"),
        flatFace: flag("flatFace"),
    };
}

export function serializeModelViewerToonStyle(style: ModelViewerToonStyle): string {
    return JSON.stringify(
        pick(style, ["outlines", "selfShadow", "lightFollowsCamera", "flatFace"]),
    );
}

export const GENSHIN_FACE_TYPES = ["boy", "girl", "lady", "loli", "male"] as const;
export type ModelViewerGenshinFaceType = (typeof GENSHIN_FACE_TYPES)[number];

export type ModelViewerGIMIToonStyle = {
    daytime: boolean;
    shadowOffset: number;
    shadowSmoothness: number;
    shadowBrightness: number;
    brightness: number;
    specular: number;
    metallic: number;
    rimStrength: number;
    emission: number;
    outlines: boolean;
    outlineWidth: number;
    selfShadow: boolean;
    lightFollowsCamera: boolean;
    toneMapping: boolean;
    fieldOfView: number;
    /** Name of a ramp in the user's Genshin texture folder; empty uses the built-in ramp. */
    ramp: string;
    /** "auto" takes the body type from the ramp name, such as Avatar_Loli_... */
    faceType: "auto" | ModelViewerGenshinFaceType;
};

// Shading values follow kaze-mio's Nahida material in UnityGenshinToonShader, except
// the shadow offset: with its 0.1, frontal light shades limbs that the game keeps lit
// to the silhouette.
export const DEFAULT_MODEL_VIEWER_GIMI_TOON_STYLE: ModelViewerGIMIToonStyle = {
    daytime: true,
    shadowOffset: 0,
    shadowSmoothness: 0.4,
    shadowBrightness: 1.1,
    brightness: 1,
    specular: 0.3,
    metallic: 8,
    rimStrength: 0.3,
    emission: 0.2,
    outlines: true,
    outlineWidth: 1.6,
    selfShadow: true,
    lightFollowsCamera: true,
    // The curve compresses bright skin channels unevenly and bleaches it.
    toneMapping: false,
    fieldOfView: 20,
    ramp: "",
    faceType: "auto",
};

// Like parseModelViewerToonStyle, keeps everything but the menu's flags and asset
// choices at the defaults.
export function parseModelViewerGIMIToonStyle(stored: string): ModelViewerGIMIToonStyle {
    const parsed = parseToonStyleRecord(stored);
    const defaults = DEFAULT_MODEL_VIEWER_GIMI_TOON_STYLE;
    const flag = (key: "outlines" | "selfShadow" | "lightFollowsCamera") => {
        const value = parsed[key];
        return typeof value === "boolean" ? value : defaults[key];
    };
    const faceType = parsed.faceType;
    return {
        ...defaults,
        outlines: flag("outlines"),
        selfShadow: flag("selfShadow"),
        lightFollowsCamera: flag("lightFollowsCamera"),
        ramp: typeof parsed.ramp === "string" ? parsed.ramp : defaults.ramp,
        faceType:
            faceType === "auto" ||
            GENSHIN_FACE_TYPES.includes(faceType as ModelViewerGenshinFaceType)
                ? (faceType as ModelViewerGIMIToonStyle["faceType"])
                : defaults.faceType,
    };
}

export function serializeModelViewerGIMIToonStyle(style: ModelViewerGIMIToonStyle): string {
    return JSON.stringify(
        pick(style, ["outlines", "selfShadow", "lightFollowsCamera", "ramp", "faceType"]),
    );
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
    gimiToonStyle?: ModelViewerGIMIToonStyle;
    /** Textures from the user's Genshin folder; the built-in substitutes are used without them. */
    genshinAssets?: GenshinToonAssetsTransport;
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

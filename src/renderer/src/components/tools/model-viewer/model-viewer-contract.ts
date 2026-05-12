export type ModelViewerThreeToneMapping = "neutral" | "aces" | "none";
export type ModelViewerThreeEnvironment = "studio" | "soft" | "none";
export type ModelViewerVariantStateValue = number | string;

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

export type ModelViewerAnimationFrame = {
    index: number;
    time: number;
    values: Record<string, ModelViewerVariantStateValue>;
    meshes: ModelViewerAnimationFrameMesh[];
};

export type ModelViewerAnimationSharedBuffer = {
    id: string;
    path: string;
};

export type ModelViewerAnimationClip = {
    id: string;
    label: string;
    variableIds: string[];
    fps: number;
    frameStart: number;
    frameEnd: number;
    loop: boolean;
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
    captureSquarePngDataUrl: () => Promise<string | null>;
    restoreCameraState: (
        state: ModelViewerCameraState | null,
        options?: {
            includeFieldOfView?: boolean;
        },
    ) => void;
    updateFraming: () => Promise<void> | void;
    setDoubleSided: (doubleSided: boolean) => Promise<void> | void;
};

export type ModelViewerSurfaceProps = {
    className?: string;
    orientation: string;
    src: string;
    variantState?: Record<string, ModelViewerVariantStateValue>;
    shapeKeys?: ModelViewerRealtimeShapeKey[];
    animationClip?: ModelViewerAnimationClip;
    animationFrame?: number;
    threeToneMapping?: ModelViewerThreeToneMapping;
    threeEnvironment?: ModelViewerThreeEnvironment;
    threeExposure?: number;
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

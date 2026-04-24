export type ModelViewerRenderer = "google" | "three";
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

export type ModelViewerCameraState = {
    orbit: string;
    target: string;
    fieldOfView: string;
    position?: string;
    anchor?: string;
};

export type ModelViewerHandle = {
    captureCameraState: () => ModelViewerCameraState | null;
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

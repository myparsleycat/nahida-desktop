import type { TouchZoneSettings } from "./touch-profile-settings";

export type TouchProfileZoneSource = "vision" | "manual";

export type TouchProfileZoneMetadata = {
    id: string;
    label: string;
    channel: number;
    confidence: number;
    center: [number, number, number];
    radius: [number, number, number];
    source: TouchProfileZoneSource;
    settings: TouchZoneSettings;
};

export type TouchProfileTurnSummary = {
    turn: number;
    confidence: number;
    approved: boolean;
    warnings: string[];
};

export type TouchProfileComponentSummary = {
    componentId: string;
    interactive: boolean;
    confidence: number;
    zones: TouchProfileZoneMetadata[];
    warnings: string[];
    currentTurn?: number;
    turnHistory?: TouchProfileTurnSummary[];
};

export type TouchProfilePreviewInput = {
    sessionId: string;
    componentId: string;
};

export type TouchProfileMeshPreview = {
    sessionId: string;
    componentId: string;
    vertexCount: number;
    positions: Float32Array;
    indices: Uint32Array;
};

export type TouchProfileAnalyzeComponentsInput = {
    sessionId: string;
    componentIds: string[];
};

export type TouchProfilePreviewZone = TouchProfileZoneMetadata & {
    weights: Float32Array;
};

export type TouchProfilePreview = {
    sessionId: string;
    componentId: string;
    vertexCount: number;
    positions: Float32Array;
    indices: Uint32Array;
    zones: TouchProfilePreviewZone[];
};

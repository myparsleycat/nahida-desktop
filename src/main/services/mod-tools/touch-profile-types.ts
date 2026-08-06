import type { BlendBoneInfo } from "@shared/body-shape";
import type { TouchProfileLlmSettings } from "@shared/touch-profile-llm";
import type { TouchZoneSettings } from "@shared/touch-profile-settings";

export const TOUCH_RUNTIME_VERSION = "1";
export const TOUCH_PROMPT_VERSION = "17";
export const TOUCH_VISION_CACHE_VERSION = "15";
export const TOUCH_VISION_EVALUATOR_VERSION = "1";
export const TOUCH_VISION_MASK_TUNING_VERSION = "2";
export const TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE = {
    min: 0.65,
    max: 1.25,
    default: 1,
    step: 0.05,
} as const;
export const TOUCH_VISION_MASK_TUNING_RANGES = {
    radiusPadding: { min: 0.95, max: 1.12, step: 0.01 },
    maskCutoffD2: { min: 1, max: 3.5, step: 0.05 },
    maskEdgeFadeD2: { min: 0.05, max: 1.25, step: 0.05 },
    colocatedLayerRatio: { min: 0, max: 0.5, step: 0.01 },
    colocatedLayerMin: { min: 0, max: 0.05, step: 0.001 },
    sideHeightPadRatio: { min: 0, max: 0.25, step: 0.01 },
    sideHeightPadMin: { min: 0, max: 0.08, step: 0.001 },
    seedRadiusScale: { min: 0.9, max: 1.15, step: 0.01 },
} as const;
export const DEFAULT_TOUCH_VISION_MASK_TUNING = {
    radiusPadding: 1.02,
    maskCutoffD2: 2.25,
    maskEdgeFadeD2: 0.55,
    seedInfluenceScale: TOUCH_VISION_SEED_INFLUENCE_SCALE_RANGE.default,
    colocatedLayerRatio: 0.28,
    colocatedLayerMin: 0.012,
    sideHeightPadRatio: 0.12,
    sideHeightPadMin: 0.02,
    seedRadiusScale: 1.05,
} as const;
/** Maximum number of position Vision pipelines running at once. */
export const TOUCH_VISION_CONCURRENCY = 3;
export const TOUCH_PROFILE_MANIFEST_FILE = ".nahida-touch-profile.json";
export const TOUCH_PROFILE_MANIFEST_KIND = "nahida-touch-profile";
export const TOUCH_OBJECT_MODE = 7;
export const TOUCH_POSITION_STRIDE = 40;
export const TOUCH_MASK_BANDS = 3;
export const TOUCH_ZONE_CHANNELS = 12;
export const TOUCH_BAKE_SAMPLES = 8;
export const TOUCH_FOLDER_SUFFIX = " (Touch)";
/** Minimum confidence on every interactive component. */
export const TOUCH_CONFIDENCE_AUTO_APPLY_MIN = 0.55;
/** Average confidence across interactive components. */
export const TOUCH_CONFIDENCE_AUTO_APPLY_AVG = 0.65;

export const TOUCH_SHADER_FILES = [
    "rzm_gs_probe.hlsl",
    "rzm_object_detect.hlsl",
    "rzm_pin_detected.hlsl",
    "rzm_jiggle_screen_state.hlsl",
    "rzm_jiggle_interaction.hlsl",
] as const;

export type TouchSupportGrade = "A" | "B" | "C";
export type TouchComponentKind = "body" | "legs" | "hair" | "accessory" | "unknown";
export type TouchProgressStage =
    | "scan"
    | "preview"
    | "vision"
    | "assets"
    | "ini"
    | "validate"
    | "complete";

export type TouchDrawRange = {
    firstIndex: number;
    indexCount: number;
    baseVertex: number;
    label?: string;
    conditionText?: string;
};

export type TouchObjectMapEntry = {
    firstIndex: number;
    indexCount: number;
    objectMode: number;
    objectId: number;
    label: string;
    /** TextureOverride section that binds this IB at draw time (multi-IB GIMI). */
    ibSectionName?: string;
    /** Index buffer resource used when this section draws. */
    indexResourceName?: string;
};

/** One GIMI/part IB sharing a position VB. Ranges use two coordinate spaces. */
export type TouchIbPart = {
    ibSectionName: string;
    indexResourceName: string;
    /** Local range inside the part IB (runtime detect / object map). */
    localIndexCount: number;
    /** Start offset in the combined index buffer (mask / mesh analysis). */
    combinedFirstIndex: number;
    kindHint: "head" | "body" | "dress" | "other";
    /** True when this part should run Bake/Detect against its local IB. */
    detect: boolean;
};

export type TouchComponentAnalysis = {
    id: string;
    name: string;
    kind: TouchComponentKind;
    interactiveCandidate: boolean;
    supportGrade: TouchSupportGrade;
    supportReasons: string[];
    positionResourceName: string;
    positionRelativePath: string;
    positionPath: string;
    positionStride: number;
    vertexCount: number;
    indexResourceName?: string;
    indexResourceNames?: string[];
    indexResourceCounts?: Record<string, number>;
    indexRelativePath?: string;
    indexPath?: string;
    indexRelativePaths?: string[];
    indexPaths?: string[];
    indexFormat?: string;
    indexCount: number;
    blendSectionName?: string;
    ibSectionName?: string;
    ibSectionNames?: string[];
    ibHash?: string;
    variantKey?: string;
    variantCondition?: string;
    /**
     * Combined-index space for mask building / analysis.
     * Multi-IB GIMI: one range per part with combined offsets and section labels.
     */
    drawRanges: TouchDrawRange[];
    /**
     * Local-IB space for runtime object detect.
     * Must never span past the IB bound when Detect runs.
     */
    objectMaps: TouchObjectMapEntry[];
    /** Per-part IB metadata for multi-IB GIMI mods (optional for single-IB). */
    ibParts?: TouchIbPart[];
    blendRelativePath?: string;
    blendPath?: string;
    blendStride?: number;
    bones: BlendBoneInfo[];
};

export type TouchModAnalysis = {
    /** Directory containing the resolved INI (may be a body/face subfolder). */
    modRoot: string;
    /** Directory the user selected; used for output naming and source disabling. */
    sourceRoot: string;
    /** modRoot relative to sourceRoot; reapplies to the renamed source after disable. */
    modRootRelativeToSource: string;
    iniPath: string;
    iniRelativePath: string;
    sourceFilesRelativePaths: string[];
    supportGrade: TouchSupportGrade;
    supportReasons: string[];
    components: TouchComponentAnalysis[];
    meshHash: string;
    iniHash: string;
};

export type TouchIbPartInspection = {
    ibSectionName: string;
    kindHint: TouchIbPart["kindHint"];
    localIndexCount: number;
    /** Default checkbox state in the mesh-select UI (body-hint parts). */
    defaultSelected: boolean;
};

export type TouchComponentInspection = {
    id: string;
    name: string;
    kind: TouchComponentKind;
    supportGrade: TouchSupportGrade;
    interactiveCandidate: boolean;
    vertexCount: number;
    indexCount: number;
    variantKey?: string;
    variantCondition?: string;
    objectMaps: TouchObjectMapEntry[];
    /** Multi-IB GIMI parts sharing this position VB; empty/undefined for single-IB. */
    ibParts?: TouchIbPartInspection[];
    hasBlend: boolean;
    bones: BlendBoneInfo[];
};

export type TouchModInspection = {
    sessionId: string;
    modRoot: string;
    iniRelativePath: string;
    sourceFilesRelativePaths: string[];
    supportGrade: TouchSupportGrade;
    supportReasons: string[];
    components: TouchComponentInspection[];
};

export type TouchVisionZone = {
    id: string;
    label: string;
    confidence: number;
    maskTuning?: Partial<TouchVisionMaskTuning>;
    include: Record<string, Array<Array<[number, number]>>>;
    exclude: Record<string, Array<Array<[number, number]>>>;
};

export type TouchVisionMaskTuning = {
    radiusPadding: number;
    maskCutoffD2: number;
    maskEdgeFadeD2: number;
    seedInfluenceScale: number;
    colocatedLayerRatio: number;
    colocatedLayerMin: number;
    sideHeightPadRatio: number;
    sideHeightPadMin: number;
    seedRadiusScale: number;
};

export type TouchVisionResult = {
    componentId: string;
    isHumanBody: boolean;
    approved: boolean;
    interactive: boolean;
    zones: TouchVisionZone[];
    excludedRegions: string[];
    warnings: string[];
};

export type TouchZoneSpec = {
    id: string;
    label: string;
    channel: number;
    confidence: number;
    center: [number, number, number];
    radius: [number, number, number];
    source: "vision" | "manual" | "bone";
    settings: TouchZoneSettings;
    /**
     * Vertices selected from vision polygons (any view). When present, soft masks are
     * built from distance-to-seed rather than a pure geometric ellipsoid.
     */
    seedVertices?: number[];
    /** Bounded Vision-only mask tuning; manual zones leave this unset. */
    visionMaskTuning?: Partial<TouchVisionMaskTuning>;
};

export type TouchTurnRecord = {
    turn: number;
    vision: TouchVisionResult;
    zones: TouchZoneSpec[];
    confidence: number;
    warnings: string[];
    approved: boolean;
};

export type TouchComponentDraft = {
    componentId: string;
    interactive: boolean;
    objectId: number;
    zones: TouchZoneSpec[];
    /**
     * TextureOverride IB sections to patch (jiggle + TempVB + detect).
     * Multi-IB GIMI: user-selected parts only. Omitted on legacy drafts → all ibSectionNames.
     */
    selectedIbSectionNames?: string[];
    vision?: TouchVisionResult;
    visionApproved?: boolean;
    previewImageRelativePath?: string;
    confidence: number;
    warnings: string[];
    currentTurn?: number;
    turnHistory?: TouchTurnRecord[];
};

/** IB section names available for a component (multi-IB or single). */
export function listIbSectionNames(component: TouchComponentAnalysis): string[] {
    if (component.ibSectionNames && component.ibSectionNames.length > 0) {
        return component.ibSectionNames;
    }
    if (component.ibSectionName) return [component.ibSectionName];
    return [];
}

/**
 * Default IB selection for the mesh-select UI / analyze.
 * Multi-IB: body-hint parts only (name is a hint; user can change).
 * Single-IB / no body hint: every listed section.
 */
export function defaultSelectedIbSectionNames(component: TouchComponentAnalysis): string[] {
    const all = listIbSectionNames(component);
    if (all.length <= 1) return all;
    const body = (component.ibParts ?? [])
        .filter((part) => part.kindHint === "body")
        .map((part) => part.ibSectionName)
        .filter((name) => all.includes(name));
    return body.length > 0 ? body : all;
}

/**
 * IB sections that receive touch INI injection for this draft.
 * Explicit draft selection wins; legacy drafts without the field keep prior "all parts" behavior.
 */
export function resolveIbSectionsToPatch(
    component: TouchComponentAnalysis,
    draft?: Pick<TouchComponentDraft, "selectedIbSectionNames"> | null,
): string[] {
    const all = listIbSectionNames(component);
    const selected = draft?.selectedIbSectionNames;
    if (selected && selected.length > 0) {
        return selected.filter((name) => all.includes(name));
    }
    return all;
}

export type TouchDraft = {
    sessionId: string;
    createdAt: string;
    sourceModRoot: string;
    analysis: TouchModAnalysis;
    components: TouchComponentDraft[];
    visionUsed: boolean;
    modelName: string;
    llm: TouchProfileLlmSettings;
    promptVersion: string;
    runtimeVersion: string;
    canAutoApply: boolean;
    warnings: string[];
};

export type TouchProgressEvent = {
    sessionId?: string;
    stage: TouchProgressStage;
    progress: number;
    message: string;
    componentId?: string;
};

export type TouchApplyResult = {
    sessionId: string;
    outputModRoot: string;
    /** Source mod path after apply (disabled when reenableSourceOnRollback is true). */
    sourceModRoot: string;
    /** True when apply disabled the source; rollback should re-enable it. */
    reenableSourceOnRollback: boolean;
    disabled: boolean;
    validation: TouchValidationResult;
    warnings: string[];
};

export type TouchRollbackResult = {
    outputModRoot: string;
    sourceModRoot: string;
    removedOutput: boolean;
    reenabledSource: boolean;
};

export type TouchValidationIssue = {
    level: "error" | "warning";
    code: string;
    message: string;
    componentId?: string;
};

export type TouchValidationResult = {
    ok: boolean;
    issues: TouchValidationIssue[];
};

export type TouchJiggleParams = {
    objectId: number;
    radius: number;
    strength: number;
    falloff: number;
    dragScale: number;
    grabDamping: number;
    grabSpring: number;
    releaseDamping: number;
    releaseSpring: number;
    releaseKick: number;
    maxOffset: number;
    targetFollow: number;
    mouseYDirection: number;
    mouseXDirection: number;
};

export const DEFAULT_TOUCH_JIGGLE_PARAMS: Omit<TouchJiggleParams, "objectId"> = {
    radius: 0.2,
    strength: 1.15,
    falloff: 1.8,
    dragScale: 1,
    grabDamping: 0.86,
    grabSpring: 0.176,
    releaseDamping: 0.96,
    releaseSpring: 0.055,
    releaseKick: 1.18,
    maxOffset: 0.065,
    targetFollow: 0.12,
    mouseYDirection: 1,
    mouseXDirection: 1,
};

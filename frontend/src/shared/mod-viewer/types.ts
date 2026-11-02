import type {
    ModelViewerBounds,
    ModelViewerComputeDeformerKind,
    ModelViewerComputeSourceEncoding,
    ModelViewerDualQuaternionVariant,
    ModelViewerMaterialProfile,
    ModelViewerShapeTargetMode,
} from "@bindings/tools/model_viewer";

/** The values of a generated Go string enum, without the Go zero value. */
export type GoEnumValue<T extends string> = Exclude<`${T}`, "">;

export type DnfClause = {
    var: string;
    value: string;
    negate: boolean;
};

export type Dnf = DnfClause[][];

export type TextureVariant = {
    conditions: Dnf;
    texKey: string;
};

export type PositionVariant = {
    conditions: Dnf;
};

export type ViewerTextureRole =
    | "diffuse"
    | "normal_map"
    | "light_map"
    | "material_map"
    | "cutout_map"
    | "highlight_map"
    | "ramp_map";

export type ViewerDDSFormat =
    | "bc1-unorm"
    | "bc1-unorm-srgb"
    | "bc2-unorm"
    | "bc2-unorm-srgb"
    | "bc3-unorm"
    | "bc3-unorm-srgb"
    | "bc4-unorm"
    | "bc4-snorm"
    | "bc5-unorm"
    | "bc5-snorm"
    | "bc6h-ufloat"
    | "bc6h-sfloat"
    | "bc7-unorm"
    | "bc7-unorm-srgb";
export type ViewerMaterialProfile = GoEnumValue<ModelViewerMaterialProfile>;

export type ViewerVariableValue = {
    value: string | number;
    label: string;
};

export type ViewerMenuGuard = {
    var: string;
    op: string;
    value: string;
};

export type ViewerMenuEffect = {
    when?: ViewerMenuGuard | null;
    /** Guards of the conditionals enclosing `when`, which must pass too. */
    also?: ViewerMenuGuard[];
    var: string;
    value: string;
};

export type ViewerVariable = {
    id: string;
    label: string;
    defaultValue: string | number;
    values: ViewerVariableValue[];
    order: number;
    slot?: number;
    iconPath?: string;
    controlType?: "buttons" | "slider";
    slider?: {
        min: number;
        max: number;
        step: number;
    };
    effects?: ViewerMenuEffect[];
    /** The form whose INI declares the variable; see ModViewerTransport.forms. */
    form?: string;
};

export type ViewerOverlayQuad = {
    path: string;
    x: number;
    y: number;
    width: number;
    height: number;
    opacity: number;
    flipX?: boolean;
    flipY?: boolean;
    flipbook?: {
        columns: number;
        rows: number;
        frameStart: number;
        frameEnd: number;
        fps: number;
    };
    /** How the quad looks while a variable holds a value other than its default. */
    states?: Array<{ variable: string; value: number; path: string; opacity: number }>;
};

export type ViewerOverlayButton = {
    x: number;
    y: number;
    width: number;
    height: number;
    variable?: string;
    transitions: Array<{ from: number; to: number }>;
    /** The index in ViewerOverlayMenu.pages this button leads to. */
    page?: number;
    hover?: Array<{ quad: number; path: string }>;
    /** Names a variable the regular controls do not list. */
    label?: string;
};

export type ViewerOverlaySlider = {
    /** The index in ViewerOverlayPage.quads of the part that takes the drag. */
    handle: number;
    /** The quads that move with the slider, as they stand at both ends of the track. */
    parts: Array<{
        quad: number;
        startX: number;
        startWidth: number;
        endX: number;
        endWidth: number;
    }>;
    /** What the slider sets, chosen by the value of whenVariable. */
    modes: Array<{
        whenVariable: string;
        whenValue: number;
        variable: string;
        label: string;
        min: number;
        max: number;
    }>;
};

export type ViewerOverlayPage = {
    quads: ViewerOverlayQuad[];
    buttons: ViewerOverlayButton[];
    sliders?: ViewerOverlaySlider[];
};

export type ViewerOverlayMenu = {
    /** Where the mod draws the menu's corner in the 1920x1080 reference window. */
    x: number;
    y: number;
    width: number;
    height: number;
    /** The form whose INI declares the menu; see ModViewerTransport.forms. */
    form?: string;
    /** The first page is the one the mod opens on. */
    pages: ViewerOverlayPage[];
};

export type ViewerINI = {
    path: string;
    prefix: string;
};

/** A folder or named INI draw group; :: separates a group from its shared parent folder. */
export type ViewerForm = {
    id: string;
    label: string;
    /** Heading of a named draw group: its folder name, or its INI path when that name is shared. */
    group?: string;
};

export type ViewerStateRule = {
    var: string;
    value: string;
    conditions: Dnf;
};

export type ViewerUiAssets = {
    backgroundPath?: string;
    slotPath?: string;
    slotHoverPath?: string;
    slotActivePath?: string;
};

export type ViewerStateValue = string | number;

export type ViewerAnimationFrame = {
    index: number;
    time: number;
    values: Record<string, ViewerStateValue>;
};

export type ViewerAnimationClip = {
    id: string;
    label: string;
    deformerId?: string;
    variableIds: string[];
    fps: number;
    frameStart: number;
    frameEnd: number;
    loop: boolean;
    frames: ViewerAnimationFrame[];
};

export type ViewerComputeBinarySource = {
    encoding?: GoEnumValue<ModelViewerComputeSourceEncoding>;
    url: string;
    byteLength: number;
    stride: number;
};

export type ViewerComputeShapePass = {
    target: ViewerComputeBinarySource;
    phaseRate: number;
    wrapAt?: number;
    phaseStart?: number;
    phaseOffset: number;
    angularScale: number;
    amplitude: number;
    bias: number;
};

export type ViewerComputeShapeStage = {
    base: ViewerComputeBinarySource;
    target: ViewerComputeBinarySource;
    phaseRate: number;
    wrapAt?: number;
    phaseStart: number;
    phaseOffset: number;
    angularScale: number;
    amplitude: number;
    bias: number;
    duration: number;
};

export type ViewerComputeDeformerKind = GoEnumValue<ModelViewerComputeDeformerKind>;

export type ViewerComputeDeformer = {
    kind: ViewerComputeDeformerKind;
    meshSourceIndices?: Record<string, string | undefined>;
    id: string;
    meshIds: string[];
    vertexCount: number;
    base: ViewerComputeBinarySource;
    shapePasses: ViewerComputeShapePass[];
    shapeStages: ViewerComputeShapeStage[];
    pose?: {
        dualQuaternionVariant?: GoEnumValue<ModelViewerDualQuaternionVariant>;
        blend: ViewerComputeBinarySource;
        frames: ViewerComputeBinarySource;
        boneCount: number;
        frameCount: number;
    };
    mergedDeltas?: {
        url: string;
        /** While a group plays, its frame runs back and forth between 0 and frameEnd, or wraps. */
        groups: Array<{
            autoPlayVar: string;
            speedVar: string;
            weightVar: string;
            /** Frames per game frame at 100% speed while the weight is below until. */
            steps: Array<{ until: number; step: number }>;
            stepLimit: number;
            frameEnd: number;
            wrap?: boolean;
        }>;
        /** Weight is the group weight times scale minus offset, clamped to 0..1. */
        keys: Array<{ group: number; scale: number; offset: number; weight?: number }>;
    };
};

export type EvaluatedViewerMesh = {
    id: string;
    visible: boolean;
    texKey: string | null;
    normalMapKey: string | null;
    lightMapKey: string | null;
    materialMapKey: string | null;
    cutoutMapKey: string | null;
    highlightMapKey?: string | null;
    rampMapKey?: string | null;
    shapeWeights: Record<string, number>;
    positionVariantIndex: number | null;
};

export type EvaluatedViewerState = {
    state: Record<string, ViewerStateValue>;
    meshes: EvaluatedViewerMesh[];
};

export type ViewerEvalMesh = {
    id: string;
    /** The form whose INI draws the mesh; see ModViewerTransport.forms. */
    form?: string;
    conditions: Dnf;
    texKey: string | null;
    textureVariants: TextureVariant[];
    normalMapKey: string | null;
    normalMapVariants: TextureVariant[];
    lightMapKey: string | null;
    lightMapVariants: TextureVariant[];
    materialMapKey: string | null;
    materialMapVariants: TextureVariant[];
    cutoutMapKey: string | null;
    cutoutMapVariants: TextureVariant[];
    highlightMapKey?: string | null;
    rampMapKey?: string | null;
    shapeTargets: Array<{ var: string; divisor?: number; wave?: ViewerShapeWave }>;
    positionVariants: PositionVariant[];
};

/**
 * Weighs a shape target as amplitude * sin(phase * angularScale) + bias while `when`
 * holds, and zero otherwise; the phase in the target's var advances by rate, times
 * speedVar when set, per second of playback.
 */
export type ViewerShapeWave = {
    speedVar?: string;
    rate: number;
    angularScale: number;
    amplitude: number;
    bias: number;
    when: Dnf;
};

export type ViewerEvalInput = {
    meshes: ViewerEvalMesh[];
    defaultState: Record<string, ViewerStateValue>;
    stateRules: ViewerStateRule[];
};

export type ViewerMeshTransport = Omit<ViewerEvalMesh, "shapeTargets" | "positionVariants"> & {
    component: string;
    geometryUrl: string;
    sourceIndicesUrl?: string;
    drawKey?: string;
    bounds?: ModelViewerBounds;
    shapeTargets: Array<{
        var: string;
        positionsUrl: string;
        mode?: GoEnumValue<ModelViewerShapeTargetMode>;
        lowPositionsUrl?: string;
        divisor?: number;
        wave?: ViewerShapeWave;
    }>;
    positionVariants: Array<PositionVariant & { geometryUrl: string }>;
};

export type ModViewerTransport = {
    previewPath?: string;
    memorySessionId: string;
    iniPath: string;
    modPath: string;
    name: string;
    /** Rotation in degrees about viewer Y after upright correction. */
    defaultYaw?: number;
    materialProfile?: ViewerMaterialProfile;
    meshes: ViewerMeshTransport[];
    textures: Record<
        string,
        {
            url: string;
            fallbackUrl?: string;
            role: ViewerTextureRole;
            encoding: "image" | "dds";
            format?: ViewerDDSFormat;
            width?: number;
            height?: number;
            mipCount?: number;
            invertAlpha?: boolean;
            coverageMask?: boolean;
            outlineMask?: boolean;
        }
    >;
    variables: ViewerVariable[];
    defaultState: Record<string, ViewerStateValue>;
    stateRules: ViewerStateRule[];
    uiAssets: ViewerUiAssets;
    /** Menus the mod draws over the game, reproduced from its INI. */
    overlayMenus?: ViewerOverlayMenu[];
    animations: ViewerAnimationClip[];
    computeDeformers: ViewerComputeDeformer[];
    /** Present with two or more entries when the mod's meshes come from several folders. */
    forms: ViewerForm[];
    /** The loaded INIs; a variable ID is the prefix of the INI declaring it followed by its INI name. */
    inis: ViewerINI[];
};

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

export type ViewerShapeTarget = {
    var: string;
    positions: Float32Array;
    mode?: "midpoint_pair";
    lowPositions?: Float32Array;
};

export type ViewerMesh = {
    id: string;
    component: string;
    positions: Float32Array;
    uvs?: Float32Array;
    indices: Uint32Array;
    conditions: Dnf;
    texKey: string | null;
    textureVariants: TextureVariant[];
    normalMapKey: string | null;
    normalMapVariants: TextureVariant[];
    lightMapKey: string | null;
    lightMapVariants: TextureVariant[];
    materialMapKey: string | null;
    materialMapVariants: TextureVariant[];
    shapeTargets: ViewerShapeTarget[];
};

export type ViewerTextureRole = "diffuse" | "normal_map" | "light_map" | "material_map";

export type ViewerTexture = {
    texKey: string;
    role: ViewerTextureRole;
    bytes: Buffer;
    mimeType: "image/png" | "image/jpeg";
    relativePath: string;
};

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

export type ModViewerPayload = {
    iniPath: string;
    modDir: string;
    meshes: ViewerMesh[];
    textures: Record<string, ViewerTexture>;
    variables: ViewerVariable[];
    defaultState: Record<string, ViewerStateValue>;
    stateRules: ViewerStateRule[];
    uiAssets: ViewerUiAssets;
};

export type EvaluatedViewerMesh = {
    id: string;
    visible: boolean;
    texKey: string | null;
    normalMapKey: string | null;
    lightMapKey: string | null;
    materialMapKey: string | null;
    shapeWeights: Record<string, number>;
};

export type EvaluatedViewerState = {
    state: Record<string, ViewerStateValue>;
    meshes: EvaluatedViewerMesh[];
};

export type ViewerEvalMesh = {
    id: string;
    conditions: Dnf;
    texKey: string | null;
    textureVariants: TextureVariant[];
    normalMapKey: string | null;
    normalMapVariants: TextureVariant[];
    lightMapKey: string | null;
    lightMapVariants: TextureVariant[];
    materialMapKey: string | null;
    materialMapVariants: TextureVariant[];
    shapeTargets: Array<{ var: string }>;
};

export type ViewerEvalInput = {
    meshes: ViewerEvalMesh[];
    defaultState: Record<string, ViewerStateValue>;
    stateRules: ViewerStateRule[];
};

export type ViewerMeshTransport = Omit<ViewerEvalMesh, "shapeTargets"> & {
    component: string;
    positionsUrl: string;
    uvsUrl?: string;
    indicesUrl: string;
    shapeTargets: Array<{
        var: string;
        positionsUrl: string;
        mode?: "midpoint_pair";
        lowPositionsUrl?: string;
    }>;
};

export type ModViewerTransport = {
    memorySessionId: string;
    iniPath: string;
    modPath: string;
    name: string;
    meshes: ViewerMeshTransport[];
    textures: Record<string, { url: string; role: ViewerTextureRole }>;
    variables: ViewerVariable[];
    defaultState: Record<string, ViewerStateValue>;
    stateRules: ViewerStateRule[];
    uiAssets: ViewerUiAssets;
};

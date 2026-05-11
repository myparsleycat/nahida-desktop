import type { Logger } from "../../internal/logger";
import type { PreparedTexture, StaticGlbTextureFormat } from "./texture-utils";

export type IniSection = {
    header: string;
    name: string;
    lines: string[];
    values: Record<string, string>;
};

export type Resource = {
    name: string;
    filename?: string;
    stride?: number;
    format?: string;
    values: Record<string, string>;
};

export type FmtElement = {
    semanticName: string;
    semanticIndex: number;
    format: string;
    inputSlot: number;
    alignedByteOffset: number;
    inputSlotClass: string;
    instanceDataStepRate: number;
};

export type FmtLayout = {
    stride: number;
    topology: string;
    indexFormat: string;
    elements: FmtElement[];
};

export type BufferGroup = {
    key: string;
    vbFilename: string;
    vbBytes: Buffer;
    stride: number;
};

export type StaticGlbModLayout = "mihoyo" | "wwmi" | "efmi";

export type MihoyoBufferResourceGroup = {
    position?: Resource;
    blend?: Resource;
    texcoord?: Resource;
    single?: Resource;
};

export type WwmiBufferResourceGroup = {
    position?: Resource;
    vector?: Resource;
    blend?: Resource;
    color?: Resource;
    texcoord?: Resource;
};

export type EfmiBufferResourceGroup = {
    vb0?: Resource;
    vb1?: Resource;
    vb2?: Resource;
};

export type IbResource = {
    name: string;
    filename: string;
    format: string;
    key: string;
    overrideHash?: string;
    overrideHashes?: string[];
};

export type TextureBinding = {
    ibResourceName: string;
    diffuseResourceName?: string;
    textureResourceNames?: string[];
    overrideHash?: string;
};

export type MaterialBinding = {
    materialIndex: number;
    textureResourceName: string;
    imagePath?: string;
    mimeType: "image/png" | "image/jpeg";
};

export type VariableStateValue = number | string;
export type VariableStateMap = Record<string, VariableStateValue>;

export type StaticGlbVariantValue = {
    value: VariableStateValue;
    label: string;
};

export type StaticGlbVariantSlider = {
    min: number;
    max: number;
    step: number;
};

export type StaticGlbRealtimeShapeKeyDimension = {
    variableId: string;
    smallerPath: string;
    biggerPath: string;
};

export type StaticGlbRealtimeShapeKey = {
    shaderPath: string;
    targetMeshPrefixes: string[];
    basePath: string;
    vertexStride: number;
    positionOffset: number;
    normalOffset: number;
    tangentOffset: number;
    dimensions: StaticGlbRealtimeShapeKeyDimension[];
};

export type StaticGlbVariantVariable = {
    id: string;
    label: string;
    defaultValue: VariableStateValue;
    values: StaticGlbVariantValue[];
    order: number;
    slot?: number;
    iconPath?: string;
    controlType?: "buttons" | "slider";
    slider?: StaticGlbVariantSlider;
};

export type StaticGlbViewerUiAssets = {
    backgroundPath?: string;
    slotPath?: string;
    slotHoverPath?: string;
    slotActivePath?: string;
};

export type StaticGlbAnimationFrameMesh = {
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

export type StaticGlbAnimationFrame = {
    index: number;
    time: number;
    values: VariableStateMap;
    meshes: StaticGlbAnimationFrameMesh[];
};

export type StaticGlbAnimationSharedBuffer = {
    id: string;
    path: string;
};

export type StaticGlbAnimationClip = {
    id: string;
    label: string;
    variableIds: string[];
    fps: number;
    frameStart: number;
    frameEnd: number;
    loop: boolean;
    sharedBuffers?: StaticGlbAnimationSharedBuffer[];
    frames: StaticGlbAnimationFrame[];
};

export type StaticGlbVariantManifest = {
    version: 1;
    name: string;
    modPath: string;
    iniPath: string;
    defaultState: VariableStateMap;
    variables: StaticGlbVariantVariable[];
    uiAssets: StaticGlbViewerUiAssets;
    shapeKeys?: StaticGlbRealtimeShapeKey[];
    animations?: StaticGlbAnimationClip[];
    states: Array<{
        key: string;
        values: VariableStateMap;
        glbPath: string;
    }>;
};

export type PreparedAnimationClip = Omit<StaticGlbAnimationClip, "frames"> & {
    frames: Array<{
        index: number;
        time: number;
        values: VariableStateMap;
    }>;
};

export type MeshFrameGeometry = {
    name: string;
    indices: Uint32Array;
    position: Float32Array;
    normal?: Float32Array;
    tangent?: Float32Array;
    texcoord0?: Float32Array;
    vertexCount: number;
};

export type DrawInstruction = {
    ibResourceName?: string;
    indexCount: number;
    startIndex: number;
    baseVertex: number;
    condition?: IniConditionClause[];
};

export type IniConditionClause = {
    expression: string;
    expected: boolean;
};

export type IniBranchFrame = {
    activeClauses: IniConditionClause[];
    inverseClauses: IniConditionClause[];
};

export type TextureOverrideBinding = TextureBinding & {
    sectionName: string;
    draws: DrawInstruction[];
};

export type SlotVariableBinding = {
    slot: number;
    variable: string;
    values: VariableStateValue[];
};

export type PresentAnimationPattern = {
    variableId: string;
    speedToken: string;
    frameStartToken: string;
    frameEndToken: string;
};

export type StaticGlbAnimationBufferWriter = (bufferId: string, buffer: Buffer) => Promise<string>;
export type StaticGlbArtifactBufferWriter = (
    bufferId: string,
    buffer: Buffer,
    options?: {
        contentType?: string;
        fileName?: string;
    },
) => Promise<string>;

export type StaticGlbBuildContext = {
    iniPath: string;
    sections: IniSection[];
    modDir: string;
    defaultVariables: Map<string, number | string>;
    resources: Resource[];
    layout: StaticGlbModLayout;
    sectionByFullName: Map<string, IniSection>;
    bufferGroups: BufferGroup[];
    drawBindings: TextureOverrideBinding[];
    drawBindingsByIbName: Map<string, TextureOverrideBinding[]>;
    fmtByIbKey: Map<string, Promise<FmtLayout>>;
    indicesByIbKey: Map<string, Promise<Uint32Array>>;
};

export type ConvertModVariantArtifactsResult = {
    iniPath: string;
    artifactRoot: string;
    defaultGlbPath: string;
    meshCount: number;
    warningCount: number;
    manifestPath: string;
    manifest: StaticGlbVariantManifest;
};

export type ConvertModToGlbOptions = {
    modPath: string;
    assetPath: string;
    outputPath: string;
    textureFormat?: StaticGlbTextureFormat;
    jpegQuality?: number;
    includeTangents?: boolean;
    debug?: boolean;
    logger?: Logger;
    onWarning?: (message: string) => void;
};

export type ConvertModToGlbBufferOptions = Omit<ConvertModToGlbOptions, "outputPath"> & {
    textureCacheDir?: string;
    useTextureCache?: boolean;
    variableState?: VariableStateMap;
};

export type ConvertModToGlbResult = {
    iniPath: string;
    outputPath: string;
    meshCount: number;
    warningCount: number;
};

export type ConvertModToGlbBufferResult = {
    iniPath: string;
    glb: Buffer;
    meshCount: number;
    warningCount: number;
};

export type BuildMaterialOptions = ConvertModToGlbBufferOptions;

export type TexturePreparationResult = PreparedTexture | null;

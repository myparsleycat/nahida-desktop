import type { ModViewerTransport, ViewerMaterialProfile } from "@shared/mod-viewer/types";
import type { Mesh, MeshStandardMaterial, Texture } from "three";

/** The part of a toon style the viewer itself reads to aim the light, cast shadows and set the lens. */
export type ModelViewerToonStyleBase = {
    outlines: boolean;
    selfShadow: boolean;
    lightFollowsCamera: boolean;
    fieldOfView: number;
    /** Folder of game assets, for profiles whose menu lists them. */
    assetDir?: string;
};

export type ModelViewerToonStyles = Partial<
    Record<ViewerMaterialProfile, ModelViewerToonStyleBase>
>;

/** Game assets listed for one profile; only that profile's resource loader reads them. */
export type ModelViewerToonAssets = {
    profile: ViewerMaterialProfile;
    assets: unknown;
};

export type MaterialProfileMapKeys = {
    texKey: string | null;
    normalMapKey: string | null;
    lightMapKey: string | null;
    materialMapKey: string | null;
    cutoutMapKey: string | null;
    highlightMapKey?: string | null;
    rampMapKey?: string | null;
};

/** Mesh userData that payload code and every profile share. Profiles add prefixed fields of their own. */
export type MaterialProfileMeshData = {
    materialProfile?: ModViewerTransport["materialProfile"];
    toonShadows: boolean;
    /** The style of the mesh's own profile. */
    toonStyle?: ModelViewerToonStyleBase;
    /** What the profile's resource loader resolved, such as textures from a game folder. */
    toonResources?: unknown;
    tangentFrameUsable?: boolean;
    /** Drawn only while game textures are highlighted. */
    overlayShadow?: boolean;
};

export type ModelViewerToonFlag = {
    key: string;
    /** Key under page.tools.model_viewer.menu. */
    label: string;
};

/** A flag whose key names a boolean field of Style; check a profile's flags with `satisfies`. */
export type ModelViewerToonFlagOf<Style> = ModelViewerToonFlag & {
    key: { [Key in keyof Style]-?: Style[Key] extends boolean ? Key : never }[keyof Style] & string;
};

export const BASE_TOON_FLAGS = [
    { key: "outlines", label: "toon_outlines" },
    { key: "selfShadow", label: "toon_self_shadow" },
    { key: "lightFollowsCamera", label: "toon_light_follows_camera" },
] as const satisfies readonly ModelViewerToonFlagOf<ModelViewerToonStyleBase>[];

export type ModelViewerToonResourceLoader<Style> = {
    resolve(assets: unknown, style: Style): Promise<unknown>;
    dispose(): void;
};

export type ModelViewerToonDefinition<Style extends ModelViewerToonStyleBase> = {
    defaultStyle: Style;
    /** Reads a stored record; values the menu does not show stay at the defaults. */
    parse(record: Record<string, unknown>): Style;
    serialize(style: Style): Record<string, unknown>;
    flags: readonly ModelViewerToonFlag[];
    createResourceLoader?(): ModelViewerToonResourceLoader<Style>;
};

// Methods use shorthand syntax so a profile typed with its own style still
// assigns to the default, which the registry stores.
export type ModelViewerMaterialProfile<
    Style extends ModelViewerToonStyleBase = ModelViewerToonStyleBase,
> = {
    baseMaterial: { metalness: number; roughness: number };
    /** Normal maps may use a tangent frame derived in the shader when the mesh has no usable one. */
    derivativeTangentFrame: boolean;
    /** Shows the Toon Shadows toggle. */
    toonShadows: boolean;
    toon?: ModelViewerToonDefinition<Style>;
    markMeshes?(transport: ModViewerTransport, objects: Mesh[]): void;
    configureMaterial?(
        material: MeshStandardMaterial,
        object: Mesh,
        data: MaterialProfileMeshData,
    ): void;
    /** Binds the profile's own maps after the diffuse, normal and base PBR values are set. */
    bindMaps?(
        material: MeshStandardMaterial,
        maps: MaterialProfileMapKeys,
        textures: ReadonlyMap<string, Texture>,
    ): void;
    /** Applies toon state, style and resources; called whenever any of them or the maps change. */
    sync?(object: Mesh, material: MeshStandardMaterial, data: MaterialProfileMeshData): void;
};

/** Copies the boolean values of keys from a stored record over the defaults. */
export function readToonFlags<Style extends ModelViewerToonStyleBase>(
    record: Record<string, unknown>,
    defaults: Style,
    keys: readonly (keyof Style & string)[],
): Style {
    const style = { ...defaults };
    for (const key of keys) {
        const value = record[key];
        if (typeof value === "boolean" && typeof defaults[key] === "boolean") {
            style[key] = value as Style[typeof key];
        }
    }
    return style;
}

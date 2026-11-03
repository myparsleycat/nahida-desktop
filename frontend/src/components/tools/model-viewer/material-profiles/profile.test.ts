import type { ModViewerTransport } from "@shared/mod-viewer/types";
import {
    Mesh,
    MeshStandardMaterial,
    NoColorSpace,
    PlaneGeometry,
    ShaderLib,
    Texture,
    TextureLoader,
} from "three";
import { describe, expect, it, vi } from "vitest";

import type { MaterialProfileMeshData, ModelViewerMaterialProfile } from "./profile";

import { gimiMaterialProfile } from "./gimi";
import { wwmiMaterialProfile } from "./wwmi";

function prepare(profile: ModelViewerMaterialProfile, component: string) {
    const material = new MeshStandardMaterial();
    const object = new Mesh(new PlaneGeometry(), material);
    const data: MaterialProfileMeshData = { toonShadows: true, tangentFrameUsable: true };
    object.userData = data;
    profile.markMeshes?.({ meshes: [{ component, id: component }] } as ModViewerTransport, [
        object,
    ]);
    profile.configureMaterial?.(material, object, data);
    const shader = {
        vertexShader: ShaderLib.standard.vertexShader,
        fragmentShader: ShaderLib.standard.fragmentShader,
        uniforms: {} as Record<string, { value: unknown }>,
    };
    material.onBeforeCompile(shader as never, {} as never);
    const bind = (light?: Texture, type?: Texture) => {
        profile.bindMaps?.(
            material,
            {
                texKey: null,
                normalMapKey: null,
                lightMapKey: light ? "light" : null,
                materialMapKey: type ? "type" : null,
                cutoutMapKey: null,
            },
            new Map([
                ...(light ? [["light", light] as const] : []),
                ...(type ? [["type", type] as const] : []),
            ]),
        );
        profile.sync?.(object, material, data);
    };
    return { material, object, data, uniforms: shader.uniforms, bind };
}

describe("toon material inputs", () => {
    it("keeps the GIMI face branch when an ordinary light map is also bound", () => {
        const { data, uniforms, bind } = prepare(gimiMaterialProfile, "AvatarFace");
        const faceLightMap = new Texture();
        data.toonResources = { faceLightMap };
        bind(new Texture());
        expect(uniforms.genshinFace.value).toBe(1);
        expect(uniforms.genshinFaceLightMap.value).toBe(faceLightMap);
        expect(uniforms.genshinFaceLightMapEnabled.value).toBe(1);
        bind();
        expect(uniforms.genshinFace.value).toBe(1);
    });

    it("loads GIMI metal data without sRGB conversion", async () => {
        const texture = new Texture();
        const load = vi.spyOn(TextureLoader.prototype, "loadAsync").mockResolvedValue(texture);
        const loader = gimiMaterialProfile.toon!.createResourceLoader!();
        try {
            await loader.resolve(
                { metalMapUrl: "metal.png", ramps: [] },
                gimiMaterialProfile.toon!.defaultStyle,
            );
            expect(load).toHaveBeenCalledWith("metal.png");
            expect(texture.colorSpace).toBe(NoColorSpace);
        } finally {
            loader.dispose();
            load.mockRestore();
        }
    });

    it.each([
        ["AvatarFace", 1],
        ["AvatarFaceHeadA", 1],
        ["AvatarHair2", 3],
        ["AvatarBangs", 3],
        ["AvatarEyes", 2],
        ["AvatarEyeA", 2],
        ["HairClip", 0],
        ["Headwear", 0],
        ["Component0", 0],
    ])("classifies WWMI %s without guessing anonymous components", (component, type) => {
        const { uniforms, bind } = prepare(wwmiMaterialProfile, component);
        bind();
        expect(uniforms.wwmiMaterialType?.value).toBe(type);
    });

    it("disables WWMI packed mask channels for BC4 and restores them after a map change", () => {
        const { data, object, material, uniforms, bind } = prepare(
            wwmiMaterialProfile,
            "AvatarFace",
        );
        const rgba = new Texture();
        const bc4 = new Texture();
        bc4.userData.modelViewerDDS = { format: "bc4-unorm", direct: true };
        bind(rgba, rgba);
        expect(uniforms.wwmiPackedMaskEnabled?.value).toBe(1);
        expect(uniforms.wwmiTypeMaskEnabled?.value).toBe(1);
        bind(bc4, bc4);
        expect(uniforms.wwmiPackedMaskEnabled?.value).toBe(0);
        expect(uniforms.wwmiLightBC4.value).toBe(1);
        expect(uniforms.wwmiTypeMaskEnabled?.value).toBe(0);
        bind();
        expect(uniforms.wwmiPackedMaskEnabled?.value).toBe(0);
        bind(rgba);
        expect(uniforms.wwmiPackedMaskEnabled?.value).toBe(1);
        expect(uniforms.wwmiMaterialType?.value).toBe(1);
        data.toonShadows = false;
        wwmiMaterialProfile.sync?.(object, material, data);
        expect(uniforms.wwmiToonEnabled.value).toBe(0);
        expect(uniforms.wwmiMaterialType.value).toBe(1);
    });
});

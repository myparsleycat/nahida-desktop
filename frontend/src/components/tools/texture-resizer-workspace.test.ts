import { isCurrentRequest, resolveIfCurrent } from "@renderer/lib/generation-gate";
import type { TextureResizeListItem, TextureResizeSettings } from "@shared/types";
import { describe, expect, it } from "vitest";

const DEFAULT_SETTINGS: TextureResizeSettings = {
    mode: "custom",
    operation: "resize",
    percent: 50,
    customWidth: 2048,
    customHeight: 2048,
    outputFormat: "",
    backup: true,
    upscaleScale: 2,
    upscaleModel: "realesr-animevideov3",
};

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((res) => {
        resolve = res;
    });
    return { promise, resolve };
}

function textureItem(filePath: string): TextureResizeListItem {
    return {
        filePath,
        relativePath: filePath,
        fileName: filePath,
        fileSize: 1,
        format: "png",
        colorSpace: "srgb",
        layerCount: 1,
        mipLevelCount: 1,
        originalWidth: 64,
        originalHeight: 64,
        targetWidth: 64,
        targetHeight: 64,
        canResize: true,
        canUpscale: true,
        canConvertFormat: true,
        canProcess: true,
        availableOutputFormats: ["png"],
        outputFormatDefault: "png",
    };
}

async function runFixedTargetTextureLoad(
    latestId: { current: number },
    requestId: number,
    getSettings: () => Promise<TextureResizeSettings>,
    list: (settings: TextureResizeSettings) => Promise<TextureResizeListItem[]>,
) {
    const settings = await resolveIfCurrent(requestId, latestId, getSettings);
    if (settings === undefined || !isCurrentRequest(requestId, latestId)) {
        return;
    }
    const textures = await resolveIfCurrent(requestId, latestId, () => list(settings));
    if (textures === undefined) {
        return;
    }
    return { settings, textures };
}

describe("texture-resizer-workspace load ordering", () => {
    it("ignores superseded settings and out-of-order A/B list responses", async () => {
        const latestId = { current: 0 };
        const settingsA = deferred<TextureResizeSettings>();
        const settingsB = deferred<TextureResizeSettings>();
        const listA = deferred<TextureResizeListItem[]>();
        const listB = deferred<TextureResizeListItem[]>();
        const settingsForA: TextureResizeSettings = { ...DEFAULT_SETTINGS, percent: 25 };
        const settingsForB: TextureResizeSettings = { ...DEFAULT_SETTINGS, percent: 75 };
        const texturesA = [textureItem("a.png")];
        const texturesB = [textureItem("b.png")];

        const requestA = ++latestId.current;
        const loadA = runFixedTargetTextureLoad(
            latestId,
            requestA,
            () => settingsA.promise,
            () => listA.promise,
        );
        const requestB = ++latestId.current;
        const loadB = runFixedTargetTextureLoad(
            latestId,
            requestB,
            () => settingsB.promise,
            () => listB.promise,
        );

        settingsA.resolve(settingsForA);
        listB.resolve(texturesB);
        settingsB.resolve(settingsForB);
        listA.resolve(texturesA);

        expect(await loadA).toBeUndefined();
        expect(await loadB).toEqual({ settings: settingsForB, textures: texturesB });
    });

    it("ignores a slower list response from an earlier A request", async () => {
        const latestId = { current: 0 };
        const listA = deferred<TextureResizeListItem[]>();
        const listB = deferred<TextureResizeListItem[]>();
        const texturesA = [textureItem("a.png")];
        const texturesB = [textureItem("b.png")];

        const requestA = ++latestId.current;
        const loadA = resolveIfCurrent(requestA, latestId, () => listA.promise);
        const requestB = ++latestId.current;
        const loadB = resolveIfCurrent(requestB, latestId, () => listB.promise);

        listB.resolve(texturesB);
        listA.resolve(texturesA);

        expect(await loadA).toBeUndefined();
        expect(await loadB).toEqual(texturesB);
    });
});

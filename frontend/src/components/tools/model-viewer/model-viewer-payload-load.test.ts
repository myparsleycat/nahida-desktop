import { fetchBinaryBytes, fetchFloat32 } from "@renderer/wails/binary-memory";
import type { ModViewerTransport, ViewerMeshTransport } from "@shared/mod-viewer/types";
import {
    BufferGeometry,
    Mesh,
    MeshStandardMaterial,
    Texture,
    TextureLoader,
    SRGBColorSpace,
} from "three";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import {
    buildPayloadModel,
    clearPayloadModelData,
    commitPayloadEval,
} from "./model-viewer-payload";

vi.mock("@renderer/wails/binary-memory", () => ({
    fetchBinaryBytes: vi.fn(),
    fetchFloat32: vi.fn(),
}));
beforeEach(() => {
    vi.mocked(fetchBinaryBytes)
        .mockReset()
        .mockImplementation(async () => geometryBytes());
    vi.mocked(fetchFloat32).mockReset();
});
afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

function geometryBytes(
    positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]),
    normals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]),
) {
    const buffer = new ArrayBuffer(24 + (positions.length + normals.length + 3) * 4);
    new Uint32Array(buffer, 0, 6).set([0x3147564d, positions.length, normals.length, 0, 0, 3]);
    new Float32Array(buffer, 24, positions.length).set(positions);
    new Float32Array(buffer, 24 + positions.byteLength, normals.length).set(normals);
    new Uint32Array(buffer, 24 + positions.byteLength + normals.byteLength, 3).set([0, 1, 2]);
    return new Uint8Array(buffer);
}

function mesh(id: string): ViewerMeshTransport {
    return {
        id,
        component: "body",
        geometryUrl: id,
        conditions: [[]],
        texKey: null,
        textureVariants: [],
        normalMapKey: null,
        normalMapVariants: [],
        lightMapKey: null,
        lightMapVariants: [],
        materialMapKey: null,
        materialMapVariants: [],
        shapeTargets: [],
        positionVariants: [],
    };
}

function transport(meshes: ViewerMeshTransport[]): ModViewerTransport {
    return {
        memorySessionId: "test",
        iniPath: "mod.ini",
        modPath: "mod",
        name: "model",
        meshes,
        textures: {},
        variables: [],
        defaultState: {},
        stateRules: [],
        uiAssets: {},
        animations: [],
        computeDeformers: [],
    };
}

it("preserves backend normals and bounds through initial load and variant restoration", async () => {
    const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
    const authored = new Float32Array([1, 0, 0, 1, 0, 0, 1, 0, 0]);
    vi.mocked(fetchBinaryBytes).mockImplementation(async () => geometryBytes(positions, authored));
    const normals = vi.spyOn(BufferGeometry.prototype, "computeVertexNormals");
    const sphere = vi.spyOn(BufferGeometry.prototype, "computeBoundingSphere");
    const bounds = { min: [0, 0, 0], max: [1, 1, 0], center: [0.5, 0.5, 0], radius: Math.SQRT1_2 };
    const mesh = {
        id: "mesh",
        component: "body",
        geometryUrl: "geometry",
        bounds,
        conditions: [[]],
        texKey: null,
        textureVariants: [],
        normalMapKey: null,
        normalMapVariants: [],
        lightMapKey: null,
        lightMapVariants: [],
        materialMapKey: null,
        materialMapVariants: [],
        shapeTargets: [],
        positionVariants: [],
    };
    const transport: ModViewerTransport = {
        memorySessionId: "test",
        iniPath: "mod.ini",
        modPath: "mod",
        name: "model",
        meshes: [mesh],
        textures: {},
        variables: [],
        defaultState: {},
        stateRules: [],
        uiAssets: {},
        animations: [],
        computeDeformers: [],
    };
    const evaluated = {
        state: {},
        meshes: [
            {
                id: "mesh",
                visible: true,
                texKey: null,
                normalMapKey: null,
                lightMapKey: null,
                materialMapKey: null,
                shapeWeights: {},
                positionVariantIndex: null,
            },
        ],
    };
    const root = await buildPayloadModel(transport, evaluated, true, { load: vi.fn() });
    const geometry = (root.children[0] as Mesh).geometry;
    expect(geometry.attributes.normal.array).toEqual(authored);
    expect(geometry.boundingSphere?.radius).toBe(bounds.radius);
    commitPayloadEval(root, {
        evalResult: { state: {}, meshes: [{ ...evaluated.meshes[0], positionVariantIndex: 0 }] },
        positions: new Map([
            [
                "mesh",
                {
                    variantIndex: 0,
                    positions: new Float32Array(9),
                    normals: new Float32Array(9),
                    bounds: { ...bounds, radius: 9 },
                },
            ],
        ]),
    });
    expect(geometry.boundingSphere?.radius).toBe(9);
    commitPayloadEval(root, { evalResult: evaluated, positions: new Map() });
    expect(geometry.attributes.normal.array).toEqual(authored);
    expect(geometry.boundingSphere?.radius).toBe(bounds.radius);
    expect(fetchBinaryBytes).toHaveBeenCalledOnce();
    expect(fetchFloat32).not.toHaveBeenCalled();
    expect(normals).not.toHaveBeenCalled();
    expect(sphere).not.toHaveBeenCalled();
    clearPayloadModelData(root);
});

it("loads other meshes while a shape target is pending and preserves mesh order", async () => {
    const resolvers = new Map<string, (value: Uint8Array) => void>();
    vi.mocked(fetchBinaryBytes).mockImplementation(
        (url) => new Promise((resolve) => resolvers.set(url, resolve)),
    );
    let resolveShape!: (value: Float32Array) => void;
    vi.mocked(fetchFloat32).mockImplementation(
        () =>
            new Promise((resolve) => {
                resolveShape = resolve;
            }),
    );
    const input = transport([
        { ...mesh("first"), shapeTargets: [{ var: "shape", positionsUrl: "first.shape" }] },
        mesh("second"),
    ]);
    const pending = buildPayloadModel(input, { state: {}, meshes: [] }, true, { load: vi.fn() });
    expect([...resolvers.keys()]).toEqual(["first", "second"]);
    resolvers.get("second")!(geometryBytes());
    resolvers.get("first")!(geometryBytes());
    await vi.waitFor(() => expect(fetchFloat32).toHaveBeenCalledOnce());
    resolveShape(new Float32Array(9));
    const root = await pending;
    expect(root.children.map((child) => child.name)).toEqual(["first", "second"]);
    clearPayloadModelData(root);
});

it("bounds requests for 3000 meshes, refills free slots, and cancels queued work", async () => {
    const controller = new AbortController();
    const active = new Map<string, (value: Uint8Array) => void>();
    const signals: AbortSignal[] = [];
    vi.mocked(fetchBinaryBytes).mockImplementation((url, _expected, signal) => {
        signals.push(signal!);
        return new Promise((resolve, reject) => {
            active.set(url, resolve);
            signal!.addEventListener("abort", () => reject(signal!.reason), { once: true });
        });
    });
    const pending = buildPayloadModel(
        transport(Array.from({ length: 3000 }, (_, index) => mesh(String(index)))),
        { state: {}, meshes: [] },
        true,
        { load: vi.fn() },
        false,
        controller.signal,
    );
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchBinaryBytes).toHaveBeenCalledTimes(8);
    active.get("7")!(geometryBytes());
    await vi.waitFor(() => expect(active.has("8")).toBe(true));
    expect(fetchBinaryBytes).toHaveBeenCalledTimes(9);
    controller.abort();
    await rejected;
    expect(signals.every((signal) => signal.aborted)).toBe(true);
    expect(fetchBinaryBytes).toHaveBeenCalledTimes(9);
});

it("aborts siblings on failure and disposes completed meshes after late work settles", async () => {
    const failure = new Error("broken geometry");
    let rejectBroken!: (error: unknown) => void;
    let resolveLate!: (bytes: Uint8Array) => void;
    const signals: AbortSignal[] = [];
    vi.mocked(fetchBinaryBytes).mockImplementation((url, _expected, signal) => {
        signals.push(signal!);
        if (url === "broken")
            return new Promise((_resolve, reject) => {
                rejectBroken = reject;
            });
        if (url === "late")
            return new Promise((resolve) => {
                resolveLate = resolve;
            });
        return Promise.resolve(geometryBytes());
    });
    const geometryDispose = vi.spyOn(BufferGeometry.prototype, "dispose");
    const materialDispose = vi.spyOn(MeshStandardMaterial.prototype, "dispose");
    const pending = buildPayloadModel(
        transport([mesh("ready"), mesh("broken"), mesh("late")]),
        { state: {}, meshes: [] },
        true,
        { load: vi.fn() },
    );
    const rejected = expect(pending).rejects.toBe(failure);
    rejectBroken(failure);
    await vi.waitFor(() => expect(signals.every((signal) => signal.aborted)).toBe(true));
    resolveLate(geometryBytes());
    await rejected;
    expect(geometryDispose).toHaveBeenCalledOnce();
    expect(materialDispose).toHaveBeenCalledOnce();
});

it("starts no requests for an already cancelled load", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
        buildPayloadModel(
            transport([mesh("mesh")]),
            { state: {}, meshes: [] },
            true,
            { load: vi.fn() },
            false,
            controller.signal,
        ),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchBinaryBytes).not.toHaveBeenCalled();
});

function mockTextureURLs() {
    vi.stubGlobal(
        "URL",
        class extends URL {
            static createObjectURL = vi.fn(() => "blob:texture");
            static revokeObjectURL = vi.fn();
        },
    );
}

function bc1DDS(): ArrayBuffer {
    const buffer = new ArrayBuffer(136);
    const view = new DataView(buffer);
    view.setUint32(0, 0x20534444, true);
    view.setUint32(4, 124, true);
    view.setUint32(12, 4, true);
    view.setUint32(16, 4, true);
    view.setUint32(28, 1, true);
    view.setUint32(76, 32, true);
    view.setUint32(84, 0x31545844, true);
    return buffer;
}

it("loads a supported DDS as a GPU compressed texture", async () => {
    const fetchTexture = vi.fn().mockResolvedValue(new Response(bc1DDS()));
    vi.stubGlobal("fetch", fetchTexture);
    const input = transport([mesh("mesh")]);
    input.textures = {
        body: {
            url: "body.dds",
            fallbackUrl: "body.png",
            role: "diffuse",
            encoding: "dds",
            format: "bc1-unorm",
            width: 4,
            height: 4,
            mipCount: 1,
        },
    };

    const root = await buildPayloadModel(
        input,
        { state: {}, meshes: [] },
        true,
        { load: vi.fn() },
        false,
        undefined,
        { s3tc: true, s3tcSRGB: true, rgtc: true, bptc: true },
    );
    const texture = (root.userData.payloadTextures as Map<string, Texture>).get("body");
    expect(texture?.isCompressedTexture).toBe(true);
    expect(texture?.colorSpace).toBe(SRGBColorSpace);
    expect(texture?.userData.modelViewerDDS).toEqual({ format: "bc1-unorm", direct: true });
    expect(fetchTexture).toHaveBeenCalledWith("body.dds", expect.anything());
    clearPayloadModelData(root);
});

it("skips the DDS request and loads its lazy fallback when the GPU format is unsupported", async () => {
    mockTextureURLs();
    const fetchTexture = vi.fn().mockResolvedValue(new Response(new Blob(["image"])));
    vi.stubGlobal("fetch", fetchTexture);
    vi.spyOn(TextureLoader.prototype, "load").mockImplementation((_url, onLoad) => {
        const texture = new Texture();
        queueMicrotask(() => onLoad?.(texture));
        return texture;
    });
    const input = transport([mesh("mesh")]);
    input.textures = {
        body: {
            url: "body.dds",
            fallbackUrl: "body.png",
            role: "diffuse",
            encoding: "dds",
            format: "bc7-unorm",
            width: 4,
            height: 4,
            mipCount: 1,
            invertAlpha: true,
        },
    };

    const root = await buildPayloadModel(input, { state: {}, meshes: [] }, true, {
        load: vi.fn(),
    });
    const texture = (root.userData.payloadTextures as Map<string, Texture>).get("body");
    expect(fetchTexture).toHaveBeenCalledOnce();
    expect(fetchTexture).toHaveBeenCalledWith("body.png", expect.anything());
    expect(texture?.userData.modelViewerDDS).toEqual({ format: "bc7-unorm", direct: false });
    expect(texture?.userData.modelViewerInvertAlpha).toBe(true);
    clearPayloadModelData(root);
});

it("keeps DDS normal and alpha correction metadata when the fallback format is unknown", async () => {
    mockTextureURLs();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new Blob(["image"]))));
    vi.spyOn(TextureLoader.prototype, "load").mockImplementation((_url, onLoad) => {
        const texture = new Texture();
        queueMicrotask(() => onLoad?.(texture));
        return texture;
    });
    const input = transport([mesh("mesh")]);
    input.textures = {
        normal: {
            url: "normal.dds",
            fallbackUrl: "normal.png",
            role: "normal_map",
            encoding: "dds",
            invertAlpha: true,
        },
    };

    const root = await buildPayloadModel(input, { state: {}, meshes: [] }, true, {
        load: vi.fn(),
    });
    const texture = (root.userData.payloadTextures as Map<string, Texture>).get("normal");
    expect(texture?.userData.modelViewerDDS).toEqual({ format: "", direct: false });
    expect(texture?.userData.modelViewerInvertAlpha).toBe(true);
    clearPayloadModelData(root);
});

it("deduplicates texture transfers and preserves texture decoding settings", async () => {
    mockTextureURLs();
    const fetchTexture = vi.fn().mockResolvedValue(new Response(new Blob(["image"])));
    vi.stubGlobal("fetch", fetchTexture);
    vi.spyOn(TextureLoader.prototype, "load").mockImplementation((_url, onLoad) => {
        const texture = new Texture();
        queueMicrotask(() => onLoad?.(texture));
        return texture;
    });
    const input = transport([mesh("mesh")]);
    input.textures = {
        first: { url: "image", role: "diffuse" },
        second: { url: "image", role: "diffuse" },
    };
    const root = await buildPayloadModel(input, { state: {}, meshes: [] }, true, { load: vi.fn() });
    const textures = root.userData.payloadTextures as Map<string, Texture>;
    expect(fetchTexture).toHaveBeenCalledOnce();
    expect(textures.get("first")).toBe(textures.get("second"));
    expect(textures.get("first")?.flipY).toBe(true);
    expect(textures.get("first")?.colorSpace).toBe(SRGBColorSpace);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:texture");
    clearPayloadModelData(root);
});

it("cancels pending texture transfers and leaves queued textures unrequested", async () => {
    const controller = new AbortController();
    const signals: AbortSignal[] = [];
    const fetchTexture = vi.fn((_url: string, options: RequestInit) => {
        const signal = options.signal!;
        signals.push(signal);
        return new Promise<Response>((_resolve, reject) => {
            signal.addEventListener("abort", () => reject(signal.reason), { once: true });
        });
    });
    vi.stubGlobal("fetch", fetchTexture);
    const input = transport([mesh("mesh")]);
    input.textures = Object.fromEntries(
        Array.from({ length: 20 }, (_, i) => [String(i), { url: String(i), role: "diffuse" }]),
    );
    const pending = buildPayloadModel(
        input,
        { state: {}, meshes: [] },
        true,
        { load: vi.fn() },
        false,
        controller.signal,
    );
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchTexture).toHaveBeenCalledTimes(4);
    controller.abort();
    await rejected;
    expect(signals.every((signal) => signal.aborted)).toBe(true);
    expect(fetchTexture).toHaveBeenCalledTimes(4);
});

it("disposes an image decoded after cancellation and revokes its object URL", async () => {
    mockTextureURLs();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new Blob(["image"]))));
    const texture = new Texture();
    const dispose = vi.spyOn(texture, "dispose");
    let finishImage!: () => void;
    vi.spyOn(TextureLoader.prototype, "load").mockImplementation((_url, onLoad) => {
        finishImage = () => onLoad?.(texture);
        return texture;
    });
    const controller = new AbortController();
    const input = transport([mesh("mesh")]);
    input.textures = { image: { url: "image", role: "diffuse" } };
    const pending = buildPayloadModel(
        input,
        { state: {}, meshes: [] },
        true,
        { load: vi.fn() },
        false,
        controller.signal,
    );
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(finishImage).toBeTypeOf("function"));
    controller.abort();
    await rejected;
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:texture");
    finishImage();
    expect(dispose).toHaveBeenCalledOnce();
});

it("keeps the existing fallback when an optional texture cannot be loaded", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 404 })));
    const input = transport([mesh("mesh")]);
    input.textures = { missing: { url: "missing", role: "diffuse" } };
    const root = await buildPayloadModel(input, { state: {}, meshes: [] }, true, { load: vi.fn() });
    expect(root.children).toHaveLength(1);
    expect(root.userData.payloadTextures.size).toBe(0);
});

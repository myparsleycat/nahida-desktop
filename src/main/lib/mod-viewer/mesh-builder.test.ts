import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import { crc32 } from "node:zlib";

import fse from "fs-extra";
import sharp from "sharp";
import { afterEach, describe, it } from "vitest";

import type { DrawGroup, DrawRecord } from "./draw-groups";

import { DNF_TRUE } from "./dnf";
import { buildMeshResult, viewerPreviewTextureSize } from "./mesh-builder";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("buildMeshResult", () => {
    it("skips draws with negative start, count, or base instead of reading buffers", async () => {
        const root = await makeMeshDir();
        for (const draw of [
            makeDraw({ label: "neg-start", start: -1, count: 3, base: 0 }),
            makeDraw({ label: "neg-count", start: 0, count: -3, base: 0 }),
            makeDraw({ label: "neg-base", start: 0, count: 3, base: -1 }),
        ]) {
            const skipped = await buildMeshResult([makeGroup([draw])], root);
            assert.equal(skipped.meshes.length, 0, draw.label);
        }
        const result = await buildMeshResult(
            [makeGroup([makeDraw({ label: "ok", start: 0, count: 3, base: 0 })])],
            root,
        );
        assert.equal(result.meshes.length, 1);
        assert.equal(result.meshes[0]?.id, "ok");
        assert.deepEqual([...result.meshes[0].indices], [0, 1, 2]);
    });

    it("keeps valid png and jpeg textures with their mime types after buffer-limit reads", async () => {
        const root = await makeMeshDir();
        const png = await sharp({
            create: { width: 1, height: 1, channels: 3, background: { r: 12, g: 34, b: 56 } },
        })
            .png()
            .toBuffer();
        const jpeg = await sharp({
            create: { width: 1, height: 1, channels: 3, background: { r: 78, g: 90, b: 12 } },
        })
            .jpeg()
            .toBuffer();
        await fse.writeFile(path.join(root, "diffuse.png"), png);
        await fse.writeFile(path.join(root, "alt.jpg"), jpeg);
        const result = await buildMeshResult(
            [
                makeGroup([
                    makeDraw({
                        label: "ok",
                        textureDefaultFile: "diffuse.png",
                        textureAssignments: [
                            {
                                conditions: [[{ var: "color", value: "1", negate: false }]],
                                file: "alt.jpg",
                            },
                        ],
                    }),
                ]),
            ],
            root,
        );
        assert.equal(result.textures["diffuse::diffuse.png"]?.mimeType, "image/png");
        assert.deepEqual(result.textures["diffuse::diffuse.png"]?.bytes, png);
        assert.equal(result.textures["diffuse::alt.jpg"]?.mimeType, "image/jpeg");
        assert.deepEqual(result.textures["diffuse::alt.jpg"]?.bytes, jpeg);
    });

    it("omits unreadable png and jpeg textures instead of keeping original bytes", async () => {
        const root = await makeMeshDir();
        await fse.writeFile(path.join(root, "diffuse.png"), Buffer.from([0x89, 0x50, 0x4e, 0x47]));
        await fse.writeFile(path.join(root, "alt.jpg"), Buffer.from([0xff, 0xd8, 0xff]));
        const result = await buildMeshResult(
            [
                makeGroup([
                    makeDraw({
                        label: "ok",
                        textureDefaultFile: "diffuse.png",
                        textureAssignments: [
                            {
                                conditions: [[{ var: "color", value: "1", negate: false }]],
                                file: "alt.jpg",
                            },
                        ],
                    }),
                ]),
            ],
            root,
        );
        assert.equal(result.textures["diffuse::diffuse.png"], undefined);
        assert.equal(result.textures["diffuse::alt.jpg"], undefined);
        assert.equal(result.meshes[0]?.texKey, null);
        assert.deepEqual(result.meshes[0]?.textureVariants, []);
    });

    it("omits png textures whose declared size exceeds sharp limitInputPixels", async () => {
        const root = await makeMeshDir();
        const png = pngWithDeclaredSize(20_000, 20_000);
        await assert.rejects(() => sharp(png).metadata());
        await fse.writeFile(path.join(root, "diffuse.png"), png);
        const result = await buildMeshResult(
            [makeGroup([makeDraw({ label: "ok", textureDefaultFile: "diffuse.png" })])],
            root,
        );
        assert.equal(result.textures["diffuse::diffuse.png"], undefined);
        assert.equal(result.meshes[0]?.texKey, null);
    });

    it("downscales oversized png textures to the viewer pixel budget", async () => {
        const root = await makeMeshDir();
        await sharp({
            create: {
                width: 4096,
                height: 4096,
                channels: 4,
                background: { r: 32, g: 64, b: 96, alpha: 1 },
            },
        })
            .png()
            .toFile(path.join(root, "diffuse.png"));
        const result = await buildMeshResult(
            [makeGroup([makeDraw({ label: "ok", textureDefaultFile: "diffuse.png" })])],
            root,
        );
        const encoded = result.textures["diffuse::diffuse.png"];
        assert.equal(encoded?.mimeType, "image/png");
        assert.ok(encoded);
        const metadata = await sharp(encoded.bytes).metadata();
        assert.equal(metadata.width, 2048);
        assert.equal(metadata.height, 2048);
    });
});

describe("viewerPreviewTextureSize", () => {
    it("halves until the preview pixel budget is met", () => {
        assert.deepEqual(viewerPreviewTextureSize(8192, 8192), { width: 2048, height: 2048 });
        assert.deepEqual(viewerPreviewTextureSize(2048, 8192), { width: 1024, height: 4096 });
        assert.deepEqual(viewerPreviewTextureSize(4096, 4096), { width: 2048, height: 2048 });
        assert.deepEqual(viewerPreviewTextureSize(2048, 2048), { width: 2048, height: 2048 });
    });
});

async function makeMeshDir() {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-mesh-builder-"));
    tempRoots.push(root);
    const vertexCount = 8;
    const pos = Buffer.alloc(vertexCount * 40);
    for (let index = 0; index < vertexCount; index++) {
        pos.writeFloatLE(index, index * 40);
        pos.writeFloatLE(index, index * 40 + 4);
        pos.writeFloatLE(index, index * 40 + 8);
    }
    await fse.writeFile(path.join(root, "pos.buf"), pos);
    await fse.writeFile(path.join(root, "tc.buf"), Buffer.alloc(vertexCount * 20));
    await fse.writeFile(path.join(root, "body.ib"), Buffer.from(new Uint32Array([0, 1, 2]).buffer));
    return root;
}

function makeGroup(draws: DrawRecord[]): DrawGroup {
    return {
        name: "Body",
        displayName: "Body",
        positionFile: "pos.buf",
        texcoordFile: "tc.buf",
        positionStride: 40,
        texcoordStride: 20,
        texcoordUvOff: 4,
        ibFile: "body.ib",
        diffusePoolFiles: [],
        indexSize: 4,
        draws,
    };
}

function makeDraw(overrides: Partial<DrawRecord>): DrawRecord {
    return {
        label: "Body-1",
        count: 3,
        start: 0,
        base: 0,
        conditions: DNF_TRUE,
        sources: [],
        ...overrides,
    };
}

function pngWithDeclaredSize(width: number, height: number): Buffer {
    const ihdr = Buffer.alloc(13);
    ihdr.writeUInt32BE(width, 0);
    ihdr.writeUInt32BE(height, 4);
    ihdr[8] = 8;
    ihdr[9] = 6;
    return Buffer.concat([
        Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
        pngChunk("IHDR", ihdr),
        pngChunk("IEND", Buffer.alloc(0)),
    ]);
}

function pngChunk(type: string, data: Buffer): Buffer {
    const body = Buffer.concat([Buffer.from(type), data]);
    const length = Buffer.alloc(4);
    length.writeUInt32BE(data.length);
    const checksum = Buffer.alloc(4);
    checksum.writeUInt32BE(crc32(body));
    return Buffer.concat([length, body, checksum]);
}

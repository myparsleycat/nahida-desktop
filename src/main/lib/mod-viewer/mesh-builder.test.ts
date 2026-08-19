import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import type { DrawGroup, DrawRecord } from "./draw-groups";

import { DNF_TRUE } from "./dnf";
import { buildMeshResult } from "./mesh-builder";

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

    it("keeps png and jpeg textures with their mime types after buffer-limit reads", async () => {
        const root = await makeMeshDir();
        const png = Buffer.from([0x89, 0x50, 0x4e, 0x47]);
        const jpeg = Buffer.from([0xff, 0xd8, 0xff]);
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

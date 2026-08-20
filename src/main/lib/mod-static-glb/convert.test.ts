import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { convertModToGlbBuffer } from "./index";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("convertModToGlbBuffer", () => {
    it("still writes a non-empty GLB for a minimal mod", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-static-glb-"));
        tempRoots.push(root);
        const assets = path.join(root, "assets");
        await fse.ensureDir(assets);
        await fse.writeFile(
            path.join(root, "mod.ini"),
            `[TextureOverrideBody]
hash = 12345678
ib = ResourceBodyIB
drawindexed = 3, 0, 0
[ResourceBody]
filename = Body.buf
stride = 20
[ResourceBodyIB]
filename = Body.ib
format = DXGI_FORMAT_R32_UINT
`,
        );
        const vb = Buffer.alloc(60);
        vb.writeFloatLE(0, 0);
        vb.writeFloatLE(0, 4);
        vb.writeFloatLE(0, 8);
        vb.writeFloatLE(0, 12);
        vb.writeFloatLE(0, 16);
        vb.writeFloatLE(1, 20);
        vb.writeFloatLE(0, 24);
        vb.writeFloatLE(0, 28);
        vb.writeFloatLE(1, 32);
        vb.writeFloatLE(0, 36);
        vb.writeFloatLE(0, 40);
        vb.writeFloatLE(1, 44);
        vb.writeFloatLE(0, 48);
        vb.writeFloatLE(0, 52);
        vb.writeFloatLE(1, 56);
        await fse.writeFile(path.join(root, "Body.buf"), vb);
        await fse.writeFile(
            path.join(root, "Body.ib"),
            Buffer.from(new Uint32Array([0, 1, 2]).buffer),
        );
        await fse.writeFile(
            path.join(root, "Body.fmt"),
            `stride: 20
topology: trianglelist
format: DXGI_FORMAT_R32_UINT
element[0]:
  SemanticName: POSITION
  SemanticIndex: 0
  Format: DXGI_FORMAT_R32G32B32_FLOAT
  InputSlot: 0
  AlignedByteOffset: 0
  InputSlotClass: per-vertex
  InstanceDataStepRate: 0
element[1]:
  SemanticName: TEXCOORD
  SemanticIndex: 0
  Format: DXGI_FORMAT_R32G32_FLOAT
  InputSlot: 0
  AlignedByteOffset: 12
  InputSlotClass: per-vertex
  InstanceDataStepRate: 0
`,
        );

        const result = await convertModToGlbBuffer({
            modPath: root,
            assetPath: assets,
        });
        assert.ok(result.glb.length > 0);
        assert.ok(result.meshCount >= 1);
        assert.equal(result.glb.subarray(0, 4).toString("ascii"), "glTF");
    });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import {
    anisotropicScaleFromOriginal,
    applyBrushStroke,
    displacementMetrics,
    extractPositions,
    writePositionsIntoBuffer,
} from "@shared/body-shape";
import { describe, it } from "vitest";

import { bodyShapedFolderBaseName, exportBodyShapeMesh, loadBodyShapeMod } from "./body-shape";

function writeFloat3Buffer(filePath: string, positions: Float32Array): void {
    const raw = new Uint8Array(positions.length * 4);
    const written = writePositionsIntoBuffer(raw, 12, positions);
    fs.writeFileSync(filePath, Buffer.from(written));
}

describe("bodyShapedFolderBaseName", () => {
    it("appends (Body Shaped) suffix and strips DISABLED prefix", () => {
        assert.equal(
            bodyShapedFolderBaseName("Astral Modulator Rover NSFW Heavy"),
            "Astral Modulator Rover NSFW Heavy (Body Shaped)",
        );
        assert.equal(
            bodyShapedFolderBaseName("DISABLED Astral Modulator Rover NSFW Heavy"),
            "Astral Modulator Rover NSFW Heavy (Body Shaped)",
        );
        assert.equal(bodyShapedFolderBaseName("DISABLED_Foo"), "Foo (Body Shaped)");
    });
});

describe("BodyShapeEditor load/export", () => {
    it("loads mod.ini position buffer, paints, deforms, exports with size guards", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "body-shape-"));
        const meshesDir = path.join(root, "Meshes");
        fs.mkdirSync(meshesDir);

        const original = new Float32Array([
            0,
            0,
            0, // v0
            1,
            0,
            0, // v1
            0,
            1,
            0, // v2
            0,
            0,
            1, // v3
        ]);
        const positionPath = path.join(meshesDir, "Position.buf");
        writeFloat3Buffer(positionPath, original);

        const indexPath = path.join(meshesDir, "Index.buf");
        const indices = new Uint32Array([0, 1, 2, 0, 2, 3]);
        fs.writeFileSync(indexPath, Buffer.from(indices.buffer));

        // Blend: 4 verts × stride 16 — bone 3 on v1 (w=255), bone 9 on v2 (w=128)
        const blendPath = path.join(meshesDir, "Blend.buf");
        const blend = Buffer.alloc(4 * 16);
        blend[16] = 3;
        blend[16 + 8] = 255;
        blend[32] = 9;
        blend[32 + 8] = 128;
        fs.writeFileSync(blendPath, blend);

        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[ResourcePositionBuffer]",
                "type = Buffer",
                "stride = 12",
                "filename = Meshes/Position.buf",
                "",
                "[ResourceIndexBuffer]",
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/Index.buf",
                "",
                "[ResourceBlendBuffer]",
                "type = Buffer",
                "format = DXGI_FORMAT_R8_UINT",
                "stride = 16",
                "filename = Meshes/Blend.buf",
                "",
            ].join("\n"),
            "utf8",
        );

        const sizeBefore = fs.statSync(positionPath).size;
        const loaded = await loadBodyShapeMod(root);
        assert.equal(loaded.meshes.length, 1);
        const mesh = loaded.meshes[0];
        assert.equal(mesh.vertexCount, 4);
        assert.equal(mesh.positions.length, 12);
        assert.ok(mesh.blendBytes);
        assert.equal(mesh.blendStride, 16);
        assert.deepEqual(
            mesh.bones.map((b) => b.id),
            [3, 9],
        );

        const weights = new Float32Array(mesh.vertexCount);
        applyBrushStroke({
            positions: mesh.positions,
            weights,
            hitPoint: [1, 0, 0],
            radius: 0.6,
            strength: 1,
            mode: "paint",
        });
        assert.ok(weights[1] > 0.5);

        const preview = new Float32Array(mesh.positions.length);
        anisotropicScaleFromOriginal({
            originalPositions: mesh.positions,
            previewPositions: preview,
            weights,
            pivot: [0, 0, 0],
            amount: 0.5,
            axisScale: [1, 0, 0],
        });

        const metrics = displacementMetrics(mesh.positions, preview);
        assert.ok(metrics.movedVertices >= 1);
        assert.ok(Number.isFinite(metrics.maxDisplacement));
        assert.ok(metrics.maxDisplacement > 0);

        // Unpainted vertices must not move
        for (let i = 0; i < mesh.vertexCount; i++) {
            if (weights[i] <= 0) {
                assert.equal(preview[i * 3], mesh.positions[i * 3]);
                assert.equal(preview[i * 3 + 1], mesh.positions[i * 3 + 1]);
                assert.equal(preview[i * 3 + 2], mesh.positions[i * 3 + 2]);
            }
        }

        const indexBefore = fs.readFileSync(indexPath);

        const result = await exportBodyShapeMesh({
            modRoot: root,
            positionPath: mesh.positionPath,
            positionStride: mesh.positionStride,
            positions: preview,
            weights,
            amount: 0.5,
            axisScale: [1, 0, 0],
            writeChangeLog: true,
            changeSummary: {
                amount: 0.5,
                axisScale: [1, 0, 0],
                movedVertices: metrics.movedVertices,
                maxDisplacement: metrics.maxDisplacement,
            },
        });

        assert.equal(result.positionBytes, sizeBefore);
        assert.equal(fs.statSync(positionPath).size, sizeBefore);
        assert.deepEqual([...fs.readFileSync(indexPath)], [...indexBefore]);

        const after = extractPositions(new Uint8Array(fs.readFileSync(positionPath)), 12);
        assert.deepEqual([...after], [...preview]);

        const changeLog = path.join(root, "변경사항.txt");
        assert.ok(fs.existsSync(changeLog));
        assert.ok(fs.readFileSync(changeLog, "utf8").includes("체형"));

        const metricsPath = process.env.BODY_SHAPE_METRICS_PATH;
        if (metricsPath) {
            fs.writeFileSync(
                metricsPath,
                JSON.stringify(
                    {
                        ...metrics,
                        positionBytes: result.positionBytes,
                        sizeUnchanged: result.positionBytes === sizeBefore,
                        changeLogWritten: fs.existsSync(changeLog),
                        onlyPaintedMoved: Array.from({ length: mesh.vertexCount }, (_, i) => {
                            const w = weights[i];
                            const d = Math.hypot(
                                preview[i * 3] - mesh.positions[i * 3],
                                preview[i * 3 + 1] - mesh.positions[i * 3 + 1],
                                preview[i * 3 + 2] - mesh.positions[i * 3 + 2],
                            );
                            return w > 0 ? d > 1e-6 : d <= 1e-6;
                        }).every(Boolean),
                    },
                    null,
                    2,
                ),
                "utf8",
            );
        }
    });

    it("matches EFMI hash-style Position/Blend/ComponentN companions", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "body-shape-efmi-"));
        const bufferDir = path.join(root, "Buffer");
        fs.mkdirSync(bufferDir);

        const hash = "34b08b7f";
        const original = new Float32Array([
            0,
            0,
            0, // v0
            1,
            0,
            0, // v1
            0,
            1,
            0, // v2
            0,
            0,
            1, // v3
        ]);
        writeFloat3Buffer(path.join(bufferDir, `${hash}-Position.buf`), original);

        const indices = new Uint32Array([0, 1, 2, 0, 2, 3]);
        fs.writeFileSync(
            path.join(bufferDir, `${hash}-Component1.buf`),
            Buffer.from(indices.buffer),
        );

        // Blend: 4 verts × stride 16 — bone 3 on v1 (w=255), bone 9 on v2 (w=128)
        const blend = Buffer.alloc(4 * 16);
        blend[16] = 3;
        blend[16 + 8] = 255;
        blend[32] = 9;
        blend[32 + 8] = 128;
        fs.writeFileSync(path.join(bufferDir, `${hash}-Blend.buf`), blend);

        // Second mesh group so single-candidate fallback cannot apply
        const other = "aabbccdd";
        writeFloat3Buffer(
            path.join(bufferDir, `${other}-Position.buf`),
            new Float32Array([2, 0, 0, 3, 0, 0, 2, 1, 0, 2, 0, 1]),
        );
        fs.writeFileSync(
            path.join(bufferDir, `${other}-Component1.buf`),
            Buffer.from(new Uint32Array([0, 1, 2, 0, 2, 3]).buffer),
        );
        fs.writeFileSync(path.join(bufferDir, `${other}-Blend.buf`), Buffer.alloc(4 * 16));

        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                `[Resource${hash}Position]`,
                "type = Buffer",
                "stride = 12",
                `filename = Buffer/${hash}-Position.buf`,
                "",
                `[Resource${hash}Blend]`,
                "type = Buffer",
                "format = DXGI_FORMAT_R8_UINT",
                "stride = 16",
                `filename = Buffer/${hash}-Blend.buf`,
                "",
                `[Resource_${hash}_Component1]`,
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                `filename = Buffer/${hash}-Component1.buf`,
                "",
                `[Resource${other}Position]`,
                "type = Buffer",
                "stride = 12",
                `filename = Buffer/${other}-Position.buf`,
                "",
                `[Resource${other}Blend]`,
                "type = Buffer",
                "format = DXGI_FORMAT_R8_UINT",
                "stride = 16",
                `filename = Buffer/${other}-Blend.buf`,
                "",
                `[Resource_${other}_Component1]`,
                "type = Buffer",
                "format = DXGI_FORMAT_R32_UINT",
                `filename = Buffer/${other}-Component1.buf`,
                "",
            ].join("\n"),
            "utf8",
        );

        const loaded = await loadBodyShapeMod(root);
        assert.equal(loaded.meshes.length, 2);

        const mesh = loaded.meshes.find((m) => m.id === `${hash}Position`);
        assert.ok(mesh);
        assert.ok(mesh.indices);
        assert.equal(mesh.indices.length, 6);
        assert.deepEqual([...mesh.indices], [0, 1, 2, 0, 2, 3]);
        assert.equal(mesh.indexRelativePath, `Buffer/${hash}-Component1.buf`);
        assert.ok(mesh.blendBytes);
        assert.equal(mesh.blendRelativePath, `Buffer/${hash}-Blend.buf`);
        assert.equal(mesh.blendStride, 16);
        assert.deepEqual(
            mesh.bones.map((b) => b.id),
            [3, 9],
        );

        const otherMesh = loaded.meshes.find((m) => m.id === `${other}Position`);
        assert.ok(otherMesh);
        assert.ok(otherMesh.indices);
        assert.equal(otherMesh.indexRelativePath, `Buffer/${other}-Component1.buf`);
        assert.equal(otherMesh.blendRelativePath, `Buffer/${other}-Blend.buf`);
    });

    it("matches MiHoYo shared positions to all bound index buffers", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "body-shape-mihoyo-"));
        const meshesDir = path.join(root, "Meshes");
        fs.mkdirSync(meshesDir);

        const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1]);
        for (const name of ["body", "body2", "back"]) {
            writeFloat3Buffer(path.join(meshesDir, `${name}Position.buf`), positions);
        }
        fs.writeFileSync(
            path.join(meshesDir, "bodyA.ib"),
            Buffer.from(new Uint32Array([0, 1, 2]).buffer),
        );
        fs.writeFileSync(
            path.join(meshesDir, "bodyB.ib"),
            Buffer.from(new Uint32Array([0, 2, 3]).buffer),
        );
        fs.writeFileSync(
            path.join(meshesDir, "bodyUnexpected.ib"),
            Buffer.from(new Uint32Array([1, 3, 2]).buffer),
        );
        fs.writeFileSync(
            path.join(meshesDir, "body2A.ib"),
            Buffer.from(new Uint32Array([0, 1, 3]).buffer),
        );
        fs.writeFileSync(
            path.join(meshesDir, "backA.ib"),
            Buffer.from(new Uint32Array([1, 2, 3]).buffer),
        );

        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[ResourcebodyPosition]",
                "type = Buffer",
                "stride = 12",
                "filename = Meshes/bodyPosition.buf",
                "",
                "[ResourcebodyPositionCS]",
                "type = StructuredBuffer",
                "stride = 12",
                "filename = Meshes/bodyPosition.buf",
                "",
                "[ResourcebodyAIB]",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/bodyA.ib",
                "",
                "[ResourcebodyBIB]",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/bodyB.ib",
                "",
                "[ResourceUnexpectedIndex]",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/bodyUnexpected.ib",
                "",
                "[Resourcebody2Position]",
                "type = Buffer",
                "stride = 12",
                "filename = Meshes/body2Position.buf",
                "",
                "[Resourcebody2AIB]",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/body2A.ib",
                "",
                "[ResourcebackPosition]",
                "type = Buffer",
                "stride = 12",
                "filename = Meshes/backPosition.buf",
                "",
                "[ResourcebackAIB]",
                "format = DXGI_FORMAT_R32_UINT",
                "filename = Meshes/backA.ib",
                "",
                "[TextureOverridebody]",
                "vb0 = ResourcebodyPosition",
                "ib = ResourceUnexpectedIndex",
            ].join("\n"),
            "utf8",
        );

        const loaded = await loadBodyShapeMod(root);
        assert.equal(loaded.meshes.length, 3);
        const body = loaded.meshes.find((mesh) => mesh.id === "bodyPosition");
        const body2 = loaded.meshes.find((mesh) => mesh.id === "body2Position");
        const back = loaded.meshes.find((mesh) => mesh.id === "backPosition");
        assert.ok(body?.indices);
        assert.ok(body2?.indices);
        assert.ok(back?.indices);
        assert.deepEqual([...body.indices], [0, 1, 2, 0, 2, 3, 1, 3, 2]);
        assert.deepEqual([...body2.indices], [0, 1, 3]);
        assert.deepEqual([...back.indices], [1, 2, 3]);
    });

    it("loads native EFMI Component_VB0/IB/VB2 and skips LOD blend", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "body-shape-efmi-native-"));
        const meshesDir = path.join(root, "Meshes");
        fs.mkdirSync(meshesDir);

        const writePaddedPosition = (filePath: string, positions: Float32Array) => {
            const buf = Buffer.alloc((positions.length / 3) * 16);
            for (let i = 0; i < positions.length / 3; i++) {
                buf.writeFloatLE(positions[i * 3], i * 16);
                buf.writeFloatLE(positions[i * 3 + 1], i * 16 + 4);
                buf.writeFloatLE(positions[i * 3 + 2], i * 16 + 8);
            }
            fs.writeFileSync(filePath, buf);
        };

        writePaddedPosition(
            path.join(meshesDir, "Component0_VB0.buf"),
            new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1]),
        );
        fs.writeFileSync(
            path.join(meshesDir, "Component0_IB.buf"),
            Buffer.from(new Uint16Array([0, 1, 2, 0, 2, 3]).buffer),
        );
        // Blend stride 12: 4×u16 weights @0, 4×u8 indices @8 — bone 3 on v1, bone 9 on v2
        const blend0 = Buffer.alloc(4 * 12);
        blend0.writeUInt16LE(65535, 12);
        blend0[12 + 8] = 3;
        blend0.writeUInt16LE(32768, 24);
        blend0[24 + 8] = 9;
        fs.writeFileSync(path.join(meshesDir, "Component0_VB2.buf"), blend0);
        fs.writeFileSync(path.join(meshesDir, "Component0_VB2_LOD.buf"), Buffer.alloc(4 * 12));

        // Second component: ensure Component1 does not cross-match Component0
        writePaddedPosition(
            path.join(meshesDir, "Component1_VB0.buf"),
            new Float32Array([2, 0, 0, 3, 0, 0, 2, 1, 0, 2, 0, 1]),
        );
        fs.writeFileSync(
            path.join(meshesDir, "Component1_IB.buf"),
            Buffer.from(new Uint16Array([0, 1, 2, 0, 2, 3]).buffer),
        );
        const blend1 = Buffer.alloc(4 * 12);
        blend1.writeUInt16LE(65535, 0);
        blend1[8] = 7;
        fs.writeFileSync(path.join(meshesDir, "Component1_VB2.buf"), blend1);

        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[Resource_Component0_IB]",
                "format = DXGI_FORMAT_R16_UINT",
                "stride = 6",
                "filename = Meshes/Component0_IB.buf",
                "",
                "[Resource_Component0_VB0]",
                "stride = 16",
                "filename = Meshes/Component0_VB0.buf",
                "",
                "[Resource_Component0_VB1]",
                "stride = 12",
                "filename = Meshes/Component0_VB1.buf",
                "",
                "[Resource_Component0_VB2]",
                "stride = 12",
                "filename = Meshes/Component0_VB2.buf",
                "",
                "[Resource_Component0_VB2_LOD]",
                "stride = 12",
                "filename = Meshes/Component0_VB2_LOD.buf",
                "",
                "[Resource_Component1_IB]",
                "format = DXGI_FORMAT_R16_UINT",
                "stride = 6",
                "filename = Meshes/Component1_IB.buf",
                "",
                "[Resource_Component1_VB0]",
                "stride = 16",
                "filename = Meshes/Component1_VB0.buf",
                "",
                "[Resource_Component1_VB2]",
                "stride = 12",
                "filename = Meshes/Component1_VB2.buf",
                "",
            ].join("\n"),
            "utf8",
        );

        const loaded = await loadBodyShapeMod(root);
        assert.equal(loaded.meshes.length, 2);

        const mesh0 = loaded.meshes.find((m) => m.id === "_Component0_VB0");
        assert.ok(mesh0);
        assert.equal(mesh0.vertexCount, 4);
        assert.equal(mesh0.positionStride, 16);
        assert.ok(mesh0.indices);
        assert.equal(mesh0.indices.length, 6);
        assert.deepEqual([...mesh0.indices], [0, 1, 2, 0, 2, 3]);
        assert.equal(mesh0.indexRelativePath, "Meshes/Component0_IB.buf");
        assert.ok(mesh0.blendBytes);
        assert.equal(mesh0.blendRelativePath, "Meshes/Component0_VB2.buf");
        assert.equal(mesh0.blendStride, 12);
        assert.deepEqual(
            mesh0.bones.map((b) => b.id),
            [3, 9],
        );

        const mesh1 = loaded.meshes.find((m) => m.id === "_Component1_VB0");
        assert.ok(mesh1);
        assert.equal(mesh1.indexRelativePath, "Meshes/Component1_IB.buf");
        assert.equal(mesh1.blendRelativePath, "Meshes/Component1_VB2.buf");
        assert.deepEqual(
            mesh1.bones.map((b) => b.id),
            [7],
        );
    });

    it("loads compact 8-byte stride blend buffer and exposes bones", async () => {
        const root = fs.mkdtempSync(path.join(os.tmpdir(), "body-shape-compact-"));
        const meshesDir = path.join(root, "Meshes");
        fs.mkdirSync(meshesDir);

        const original = new Float32Array([
            0,
            0,
            0, // v0
            1,
            0,
            0, // v1
            0,
            1,
            0, // v2
            0,
            0,
            1, // v3
        ]);
        const positionPath = path.join(meshesDir, "Position.buf");
        writeFloat3Buffer(positionPath, original);

        // Blend: 4 verts × stride 8 (compact: indices@0 weights@4)
        // bone 3 on v1 (w=255), bone 9 on v2 (w=128)
        const blendPath = path.join(meshesDir, "Blend.buf");
        const blend = Buffer.alloc(4 * 8);
        blend[8] = 3;
        blend[8 + 4] = 255;
        blend[16] = 9;
        blend[16 + 4] = 128;
        fs.writeFileSync(blendPath, blend);

        fs.writeFileSync(
            path.join(root, "mod.ini"),
            [
                "[ResourcePositionBuffer]",
                "type = Buffer",
                "stride = 12",
                "filename = Meshes/Position.buf",
                "",
                "[ResourceBlendBuffer]",
                "type = Buffer",
                "format = DXGI_FORMAT_R8_UINT",
                "stride = 8",
                "filename = Meshes/Blend.buf",
                "",
            ].join("\n"),
            "utf8",
        );

        const loaded = await loadBodyShapeMod(root);
        assert.equal(loaded.meshes.length, 1);
        const mesh = loaded.meshes[0];
        assert.equal(mesh.vertexCount, 4);
        assert.ok(mesh.blendBytes);
        assert.equal(mesh.blendStride, 8);
        assert.deepEqual(
            mesh.bones.map((b) => b.id),
            [3, 9],
        );
        assert.equal(mesh.bones.find((b) => b.id === 3)?.vertexCount, 1);
        assert.equal(mesh.bones.find((b) => b.id === 9)?.vertexCount, 1);
    });
});

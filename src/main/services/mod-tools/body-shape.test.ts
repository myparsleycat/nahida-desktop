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

import {
    bodyShapedFolderBaseName,
    BODY_SHAPED_SUFFIX,
    exportBodyShapeMesh,
    loadBodyShapeMod,
} from "./body-shape";

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

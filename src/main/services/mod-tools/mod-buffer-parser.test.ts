import assert from "node:assert/strict";

import type { Resource } from "@main/lib/mod-static-glb/types";
import { describe, it } from "vitest";

import { matchIndexResources } from "./mod-buffer-parser";

describe("matchIndexResources", () => {
    it("matches LOD index resources to hash-named position resources", () => {
        const positions = [
            {
                name: "789ae812Position",
                filename: "789ae812-Position.buf",
                stride: 40,
                values: {},
            },
            {
                name: "785b21f5Position",
                filename: "785b21f5-Position.buf",
                stride: 40,
                values: {},
            },
        ] satisfies Resource[];
        const indices = [
            {
                name: "_LOD0.789ae812_16590_0_Index",
                filename: "LOD0.789ae812-16590-0-Index.buf",
                format: "DXGI_FORMAT_R32_UINT",
                values: {},
            },
            {
                name: "_LOD0.785b21f5_57612_0_Index",
                filename: "LOD0.785b21f5-57612-0-Index.buf",
                format: "DXGI_FORMAT_R32_UINT",
                values: {},
            },
        ] satisfies Resource[];

        const matches = matchIndexResources(positions, indices, []);

        assert.deepEqual(
            matches.get("789ae812position")?.map((resource) => resource.name),
            ["_LOD0.789ae812_16590_0_Index"],
        );
        assert.deepEqual(
            matches.get("785b21f5position")?.map((resource) => resource.name),
            ["_LOD0.785b21f5_57612_0_Index"],
        );
    });

    it("matches index resources when named variants follow the buffer kind", () => {
        const positions = [
            {
                name: "ZhaoBodyPosition_Default",
                filename: ".\\1Default\\ZhaoBodyPosition.buf",
                stride: 40,
                values: {},
            },
            {
                name: "ZhaoBodyPosition_Bodysuit",
                filename: ".\\2Bodysuit\\ZhaoBodyPosition.buf",
                stride: 40,
                values: {},
            },
        ] satisfies Resource[];
        const indices = [
            {
                name: "ZhaoBodyAIB_Default",
                filename: ".\\1Default\\ZhaoBodyA.ib",
                format: "DXGI_FORMAT_R32_UINT",
                values: {},
            },
            {
                name: "ZhaoBodyAIB_Bodysuit",
                filename: ".\\2Bodysuit\\ZhaoBodyA.ib",
                format: "DXGI_FORMAT_R32_UINT",
                values: {},
            },
        ] satisfies Resource[];

        const matches = matchIndexResources(positions, indices, []);

        assert.deepEqual(
            matches.get("zhaobodyposition_default")?.map((resource) => resource.name),
            ["ZhaoBodyAIB_Default"],
        );
        assert.deepEqual(
            matches.get("zhaobodyposition_bodysuit")?.map((resource) => resource.name),
            ["ZhaoBodyAIB_Bodysuit"],
        );
    });
});

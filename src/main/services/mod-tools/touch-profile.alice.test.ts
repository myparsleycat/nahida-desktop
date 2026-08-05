import assert from "node:assert/strict";

import fse from "fs-extra";
import { describe, it } from "vitest";

import { analyzeTouchMod } from "./touch-profile-analyzer";
import { bakeSampleOffsets } from "./touch-profile-assets";
import { inspectTouchProfileInput } from "./touch-profile-detection";

const ALICE_CANDIDATES = [
    "E:/ZZMI/Mods/Character/Alice/DISABLED_AliceMod_merged",
    "E:/ZZMI/Mods/Character/Alice/AliceMod_merged/body",
    "E:/ZZMI/Mods/Character/Alice/AliceMod_merged",
];

async function resolveAliceRoot() {
    for (const candidate of ALICE_CANDIDATES) {
        if (await fse.pathExists(candidate)) return candidate;
    }
    return null;
}

describe("alice fixture", () => {
    it("analyzes alice non-touch ranges", async () => {
        const alice = await resolveAliceRoot();
        if (!alice) return;

        if ((await inspectTouchProfileInput(alice)).status !== "none") return;

        const analysis = await analyzeTouchMod(alice);
        assert.equal(analysis.supportGrade, "A");

        const body = analysis.components.find((component) => component.kind === "body");
        const leg = analysis.components.find((component) => component.kind === "legs");
        assert.ok(body);
        assert.ok(leg);
        assert.equal(body.vertexCount, 34263);
        assert.equal(leg.vertexCount, 7766);

        const clothed = body.objectMaps.find((entry) => entry.label === "clothed");
        const nude = body.objectMaps.find((entry) => entry.label === "nude");
        assert.ok(clothed);
        assert.ok(nude);
        assert.equal(clothed.firstIndex, 48543);
        assert.equal(clothed.indexCount, 55830);
        assert.equal(nude.firstIndex, 104373);
        assert.equal(nude.indexCount, 55830);

        const skin = leg.objectMaps[0];
        assert.equal(skin.firstIndex, 5562);
        assert.equal(skin.indexCount, 34002);

        const samples = bakeSampleOffsets(nude.firstIndex, nude.indexCount);
        assert.equal(samples.length, 8);
        assert.equal(samples[0], 104373);
        assert.equal(samples[7], 160202);
        assert.ok(samples.every((value, index) => index === 0 || value >= samples[index - 1]));
    }, 60_000);
});

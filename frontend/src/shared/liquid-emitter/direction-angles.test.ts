import assert from "node:assert/strict";

import { describe, it } from "vitest";

import type { Vec3 } from "./flow-trajectory";

import { anglesToDirection, directionToAngles } from "./direction-angles";

const Y_DOWN: Vec3 = [0, -1, 0];
const Z_DOWN: Vec3 = [0, 0, -1];

describe("direction angles", () => {
    it("reads a saved vector as angles and builds the same direction back", () => {
        for (const down of [Y_DOWN, Z_DOWN]) {
            for (const angles of [
                { tilt: 30, azimuth: 0 },
                { tilt: 90, azimuth: 135 },
                { tilt: 150, azimuth: 300 },
            ]) {
                const read = directionToAngles(anglesToDirection(angles, down), down);
                assert.ok(
                    Math.abs(read.tilt - angles.tilt) < 1e-9,
                    `tilt ${JSON.stringify(angles)}`,
                );
                assert.ok(
                    Math.abs(read.azimuth - angles.azimuth) < 1e-9,
                    `azimuth ${JSON.stringify(angles)}`,
                );
            }
        }
    });

    it("measures tilt from the game's down axis, ignoring the vector's length", () => {
        assert.equal(directionToAngles([0, -180, 0], Y_DOWN).tilt, 0);
        assert.equal(directionToAngles([0, 0, -5], Z_DOWN).tilt, 0);
        assert.ok(Math.abs(directionToAngles([0, 0, -1], Y_DOWN).tilt - 90) < 1e-9);
    });

    it("turns counterclockwise seen from above", () => {
        const quarter = anglesToDirection({ tilt: 90, azimuth: 90 }, Z_DOWN);
        assert.ok(Math.abs(quarter[1] - 1) < 1e-9, `Z up quarter turn ${quarter}`);
        const yQuarter = anglesToDirection({ tilt: 90, azimuth: 90 }, Y_DOWN);
        assert.ok(Math.abs(yQuarter[2] + 1) < 1e-9, `Y up quarter turn ${yQuarter}`);
    });
});

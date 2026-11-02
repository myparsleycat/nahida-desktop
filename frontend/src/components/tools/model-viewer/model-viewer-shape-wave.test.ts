import { describe, expect, it } from "vitest";

import { ShapeWavePlayback } from "./model-viewer-shape-wave";

const target = {
    var: "freq",
    wave: {
        speedVar: "speed",
        rate: 1,
        angularScale: 30,
        amplitude: 0.5,
        bias: 0.5,
        when: [[{ var: "on", value: "1", negate: false }]],
    },
};

describe("ShapeWavePlayback", () => {
    it("advances only while the wave is on, at the speed set then", () => {
        const playback = new ShapeWavePlayback([target]);
        playback.advance("clip", 0, 10, { on: 1, speed: 0.5 });
        expect(playback.advance("clip", 1, 10, { on: 1, speed: 0.5 }).freq).toBeCloseTo(0.5);
        expect(playback.advance("clip", 2, 10, { on: 0, speed: 0.5 }).freq).toBeCloseTo(0.5);
        expect(playback.advance("clip", 3, 10, { on: 1, speed: 0.25 }).freq).toBeCloseTo(0.75);
    });

    it("keeps the phase when the clip loops", () => {
        const playback = new ShapeWavePlayback([target]);
        const state = { on: 1, speed: 1 };
        playback.advance("clip", 9.5, 10, state);
        expect(playback.advance("clip", 0.5, 10, state).freq).toBeCloseTo(1);
    });

    it("returns to the same phase when scrubbed back", () => {
        const playback = new ShapeWavePlayback([target]);
        const state = { on: 1, speed: 1 };
        playback.advance("clip", 1, 10, state);
        playback.advance("clip", 3, 10, state);
        expect(playback.advance("clip", 1, 10, state).freq).toBeCloseTo(0);
    });
});

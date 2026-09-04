import { describe, expect, it } from "vitest";

import { enqueueBrushPointerSample, type BrushPointerSample } from "./body-shape-viewport";

const sample = (clientX: number, paint: boolean): BrushPointerSample => ({
    clientX,
    clientY: 0,
    paint,
});

describe("body shape brush pointer queue", () => {
    it("retains every painting sample received before the next animation frame", () => {
        const pending: BrushPointerSample[] = [];

        enqueueBrushPointerSample(pending, sample(0, true));
        enqueueBrushPointerSample(pending, sample(10, true));
        enqueueBrushPointerSample(pending, sample(20, true));

        expect(pending.map(({ clientX }) => clientX)).toEqual([0, 10, 20]);
    });

    it("coalesces hover movement without discarding the preceding painted path", () => {
        const pending: BrushPointerSample[] = [];

        enqueueBrushPointerSample(pending, sample(0, true));
        enqueueBrushPointerSample(pending, sample(10, true));
        enqueueBrushPointerSample(pending, sample(20, false));
        enqueueBrushPointerSample(pending, sample(30, false));

        expect(pending).toEqual([sample(0, true), sample(10, true), sample(30, false)]);
    });
});

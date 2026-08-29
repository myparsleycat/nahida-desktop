import { extractBoneWeights } from "@shared/body-shape";
import { describe, expect, it } from "vitest";

import { toBodyShapeBytes } from "./body-shape-payload";

describe("toBodyShapeBytes", () => {
    it("decodes the base64 payload produced for Go byte slices", () => {
        expect(toBodyShapeBytes("AAECA/8=")).toEqual(new Uint8Array([0, 1, 2, 3, 255]));
    });

    it("restores blend bytes for bone weight selection", () => {
        const bytes = toBodyShapeBytes("AwAAAAAAAAD/AAAAAAAAAA==");
        expect(bytes).toBeDefined();
        expect(extractBoneWeights(bytes!, 3, 1, 16)).toEqual(new Float32Array([1]));
    });

    it("keeps Electron-compatible binary payloads working", () => {
        const bytes = new Uint8Array([4, 5, 6]);
        expect(toBodyShapeBytes(bytes)).toBe(bytes);
        expect(toBodyShapeBytes([7, 8, 9])).toEqual(new Uint8Array([7, 8, 9]));
    });

    it("ignores malformed base64 payloads", () => {
        expect(toBodyShapeBytes("not base64!")).toBeUndefined();
    });
});

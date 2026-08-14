import { isMinified, minify, unminify } from "@backend/utils/jsonMinify";
import { Encoder } from "cbor-x";
import { describe, expect, it } from "vitest";

import { jsonResponseFromBody, readApiBody } from "./cbor-response";

const encoder = new Encoder({ useRecords: false, mapsAsObjects: true });

function jsonResponse(body: unknown) {
    return new Response(JSON.stringify(body), {
        headers: { "Content-Type": "application/json" },
    });
}

function cborResponse(body: Uint8Array) {
    const bytes = new Uint8Array(body.byteLength);
    bytes.set(body);
    return new Response(bytes.buffer, {
        headers: { "Content-Type": "application/cbor" },
    });
}

function unminifyIfNeeded(data: unknown) {
    return isMinified(data) ? unminify(data) : data;
}

describe("jsonResponseFromBody", () => {
    it("returns a fresh JSON body after the original response is consumed", async () => {
        const response = jsonResponse({ ok: true });
        const rewritten = await jsonResponseFromBody(response);

        expect(response.bodyUsed).toBe(true);
        expect(await rewritten.json()).toEqual({ ok: true });
        expect(rewritten.headers.get("Content-Type")).toContain("application/json");
    });

    it("unminifies mapped JSON without leaving the original body locked", async () => {
        const payload = { role: "member", name: "member" };
        const response = jsonResponse(minify(payload));
        const rewritten = await jsonResponseFromBody(response, unminifyIfNeeded);

        expect(response.bodyUsed).toBe(true);
        expect(await rewritten.json()).toEqual(payload);
    });

    it("propagates mapping errors so CBOR callers can retry as JSON", async () => {
        const response = cborResponse(encoder.encode({ ok: true }));

        await expect(
            jsonResponseFromBody(response, () => {
                throw new Error("unminify failed");
            }),
        ).rejects.toThrow("unminify failed");
        expect(response.bodyUsed).toBe(true);
    });

    it("throws a decode error instead of returning the consumed CBOR response", async () => {
        const response = cborResponse(new Uint8Array([255, 255]));

        await expect(jsonResponseFromBody(response)).rejects.toThrow();
        expect(response.bodyUsed).toBe(true);
        await expect(response.arrayBuffer()).rejects.toThrow();
    });

    it("rewrites a successful CBOR body into readable JSON", async () => {
        const response = cborResponse(encoder.encode({ ok: true }));
        const rewritten = await jsonResponseFromBody(response);

        expect(response.bodyUsed).toBe(true);
        expect(await rewritten.json()).toEqual({ ok: true });
    });

    it("falls back to the already-read payload when mapped data cannot be serialized", async () => {
        const response = jsonResponse({ ok: true });
        const rewritten = await jsonResponseFromBody(response, () => ({ ok: 1n }));

        expect(response.bodyUsed).toBe(true);
        expect(await rewritten.json()).toEqual({ ok: true });
    });

    it("throws when the already-read payload cannot be serialized instead of returning a locked body", async () => {
        const response = cborResponse(encoder.encode({ ok: 2n ** 80n }));

        await expect(jsonResponseFromBody(response)).rejects.toThrow();
        expect(response.bodyUsed).toBe(true);
        await expect(response.arrayBuffer()).rejects.toThrow();
    });
});

describe("readApiBody", () => {
    it("consumes the original response before returning parsed JSON", async () => {
        const response = jsonResponse({ ok: true });
        const data = await readApiBody(response);

        expect(response.bodyUsed).toBe(true);
        expect(data).toEqual({ ok: true });
        await expect(response.text()).rejects.toThrow();
    });
});

import { describe, expect, it } from "vitest";

import {
    creditedLogicalBytesForMember,
    DIRECT_UPLOAD_THRESHOLD,
    logicalBytesForPackProgress,
    PACK_MEMBER_MAX,
    packUploadUrl,
    partitionPackedUploads,
} from "./upload-pack";

describe("partitionPackedUploads", () => {
    it("packs adjacent small files and keeps oversized files single", () => {
        const groups = partitionPackedUploads([
            { id: "a", payloadBytes: 10 },
            { id: "b", payloadBytes: 20 },
            { id: "c", payloadBytes: PACK_MEMBER_MAX + 1 },
            { id: "d", payloadBytes: 30 },
            { id: "e", payloadBytes: 40 },
        ]);

        expect(groups).toEqual([
            {
                kind: "pack",
                members: [
                    { id: "a", payloadBytes: 10 },
                    { id: "b", payloadBytes: 20 },
                ],
            },
            { kind: "single", member: { id: "c", payloadBytes: PACK_MEMBER_MAX + 1 } },
            {
                kind: "pack",
                members: [
                    { id: "d", payloadBytes: 30 },
                    { id: "e", payloadBytes: 40 },
                ],
            },
        ]);
    });

    it("flushes a pack when the payload budget is reached", () => {
        const members = Array.from({ length: 23 }, (_, index) => ({
            id: String(index),
            payloadBytes: PACK_MEMBER_MAX,
        }));
        const groups = partitionPackedUploads(members);

        expect(groups[0]).toEqual({
            kind: "pack",
            members: members.slice(0, 22),
        });
        expect(groups[1]).toEqual({ kind: "single", member: members[22] });
    });
});

describe("packUploadUrl", () => {
    it("rewrites an intent upload URL to the pack endpoint", () => {
        expect(packUploadUrl("https://api.nahida.live/akasha/v2/uploads/intent-1")).toBe(
            "https://api.nahida.live/akasha/v2/uploads:pack",
        );
    });

    it("preserves a query string and fragment", () => {
        expect(
            packUploadUrl("https://api.nahida.live/akasha/v2/uploads/intent-1?sig=abc#frag"),
        ).toBe("https://api.nahida.live/akasha/v2/uploads:pack?sig=abc#frag");
    });

    it("throws when the URL has no intent upload segment", () => {
        expect(() => packUploadUrl("https://api.nahida.live/akasha/v2/other/intent-1?x=1")).toThrow(
            "pack_url_unresolved",
        );
    });
});

describe("logicalBytesForPackProgress", () => {
    const members = [
        { logicalSize: 10, payloadBytes: 5 },
        { logicalSize: 20, payloadBytes: 10 },
    ];

    it("credits whole members as their payloads complete", () => {
        expect(logicalBytesForPackProgress(members, 5)).toBe(10);
        expect(logicalBytesForPackProgress(members, 15)).toBe(30);
    });

    it("credits a partial member by payload ratio", () => {
        expect(logicalBytesForPackProgress(members, 10)).toBe(20);
        expect(creditedLogicalBytesForMember(members, 1, 10)).toBe(10);
    });
});

describe("DIRECT_UPLOAD_THRESHOLD", () => {
    it("stays below the 100MiB request body limit", () => {
        expect(DIRECT_UPLOAD_THRESHOLD).toBe(80 * 1024 * 1024);
    });
});

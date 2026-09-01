import { describe, expect, it } from "vitest";

import {
    canRestoreDraft,
    detachDraftMedia,
    evictDrafts,
    restoreDraftMedia,
    type MenuMakerDraftMeta,
} from "./drafts";
import { acceptsIconResult, createStateToken, sanitizeIconifySVG } from "./icons";
import { MENU_MAKER_SHADER } from "./resources";
import { DEFAULT_MENU_MAKER_SETTINGS, type MenuMakerSlot } from "./types";

describe("Menu Maker managed resources", () => {
    it("preserves shader attribution", () => {
        expect(MENU_MAKER_SHADER).toContain("Contributors: SinsOfSeven");
    });
});

describe("Menu Maker icon safety", () => {
    it("rejects executable and externally-referencing SVG", () => {
        expect(sanitizeIconifySVG('<svg onload="alert(1)"><path d="M0 0" /></svg>')).toBeNull();
        expect(sanitizeIconifySVG("<svg><script>alert(1)</script></svg>")).toBeNull();
        expect(
            sanitizeIconifySVG('<svg><use href="https://evil.test/icon.svg" /></svg>'),
        ).toBeNull();
        expect(
            sanitizeIconifySVG('<svg><path fill="url(https://evil.test/a)" /></svg>'),
        ).toBeNull();
        expect(
            sanitizeIconifySVG(
                '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0" /></svg>',
            ),
        ).not.toBeNull();
    });

    it("drops stale asynchronous results", () => {
        const current = createStateToken("hash", "slot", 2);
        expect(acceptsIconResult(current, createStateToken("hash", "slot", 1))).toBe(false);
        expect(acceptsIconResult(current, current)).toBe(true);
    });
});

describe("Menu Maker drafts", () => {
    const draft = (index: number): MenuMakerDraftMeta => ({
        id: `draft-${index}`,
        sourcePath: "C:\\mod.ini",
        sourceName: "mod.ini",
        sourceSHA256: "hash",
        slotSignature: "signature",
        updatedAt: index,
        settings: DEFAULT_MENU_MAKER_SETTINGS,
        slots: [],
    });

    it("restores only matching source and slot signature", () => {
        expect(canRestoreDraft(draft(1), "hash", "signature")).toBe(true);
        expect(canRestoreDraft(draft(1), "other", "signature")).toBe(false);
        expect(canRestoreDraft(draft(1), "hash", "other")).toBe(false);
    });

    it("evicts metadata and blobs beyond 30 newest drafts", () => {
        const { kept, removed } = evictDrafts(
            Array.from({ length: 35 }, (_, index) => draft(index)),
        );
        expect(kept).toHaveLength(30);
        expect(kept[0].id).toBe("draft-34");
        expect(removed.map((item) => item.id)).toEqual([
            "draft-4",
            "draft-3",
            "draft-2",
            "draft-1",
            "draft-0",
        ]);
    });

    it("keeps uploaded media out of metadata and restores it from blobs", () => {
        const settings = {
            ...DEFAULT_MENU_MAKER_SETTINGS,
            panelImageDataUrl: "data:image/png;base64,panel",
        };
        const slots: MenuMakerSlot[] = [
            {
                id: "slot-0",
                key: "1",
                originalKeys: ["1"],
                name: "x",
                skip: false,
                handlers: [],
                icon: {
                    kind: "upload",
                    name: "upload",
                    color: "#ffffff",
                    dataUrl: "data:image/png;base64,slot",
                },
            },
        ];
        const detached = detachDraftMedia(settings, slots);
        const stored = { ...draft(1), settings: detached.settings, slots: detached.slots };

        expect(JSON.stringify(stored)).not.toContain("data:image/png");
        expect(restoreDraftMedia(stored, detached.blobs).settings.panelImageDataUrl).toBe(
            "data:image/png;base64,panel",
        );
        expect(restoreDraftMedia(stored, detached.blobs).slots[0].icon).toMatchObject({
            kind: "upload",
            dataUrl: "data:image/png;base64,slot",
        });
    });
});

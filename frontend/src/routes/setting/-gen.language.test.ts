import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./gen.tsx", import.meta.url), "utf8");

describe("general settings language flow", () => {
    it("persists the selection and delegates language application to the global event", () => {
        expect(source).toContain('void update("language", val);');
        expect(source).not.toContain("i18n.changeLanguage");
    });
});

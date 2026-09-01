import en from "@renderer/lib/i18n/locales/en.json";
import ja from "@renderer/lib/i18n/locales/ja.json";
import ko from "@renderer/lib/i18n/locales/ko.json";
import zh from "@renderer/lib/i18n/locales/zh.json";
import { describe, expect, it } from "vitest";

describe("Menu Maker locales", () => {
    it("keeps the same key set in all four locales", () => {
        const expected = Object.keys(en.page.tools.menu_maker).toSorted();
        expect(Object.keys(ko.page.tools.menu_maker).toSorted()).toEqual(expected);
        expect(Object.keys(zh.page.tools.menu_maker).toSorted()).toEqual(expected);
        expect(Object.keys(ja.page.tools.menu_maker).toSorted()).toEqual(expected);
    });
});

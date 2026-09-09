import { expect, it } from "vitest";

import { Route } from "./model-viewer-window";

it("validates model viewer search without accepting non-string paths", () => {
    const validate = Route.options.validateSearch;
    if (typeof validate !== "function") throw new Error("Missing search validator");
    expect(validate({ path: "C:/모드 (1) & #%+" })).toEqual({ path: "C:/모드 (1) & #%+" });
    for (const path of [undefined, null, 1, {}, ["C:/mod"]])
        expect(validate({ path })).toEqual({ path: "" });
});

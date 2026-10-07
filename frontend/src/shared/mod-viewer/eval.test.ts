import { expect, it } from "vitest";

import { applyStateRules } from "./eval";

it("sees a key added by an earlier rule through a case-insensitive lookup", () => {
    const rules = [
        { var: "Skirt", value: "1", conditions: [[{ var: "skirt", value: "1", negate: true }]] },
        { var: "Shoes", value: "1", conditions: [[{ var: "skirt", value: "2", negate: false }]] },
    ];
    expect(applyStateRules({ Outfit: 1 }, rules)).toEqual({ Outfit: 1, Skirt: "1" });
});

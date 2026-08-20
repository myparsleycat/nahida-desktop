import assert from "node:assert/strict";

import { describe, it } from "vitest";

import { parseIniText } from "./ini";
import { extractMenuToggles } from "./menu";

function menuFromIni(text: string) {
    return extractMenuToggles(parseIniText(text, "mod.ini"));
}

function slotByVar(menu: ReturnType<typeof extractMenuToggles>, name: string) {
    return Object.values(menu).find((slot) => slot.var.toLowerCase() === name.toLowerCase());
}

function commandList(firstBody: string, secondBody = "$other = ($other + 1) % 2") {
    return `[Constants]
global persist $swapvar = 0
global persist $other = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
${firstBody}
elif $clickedSlot == 2
${secondBody}
endif
`;
}

describe("extractMenuToggles parseBranch variable names", () => {
    it("still parses same-case increment-mod cycles", () => {
        const swap = slotByVar(
            menuFromIni(commandList("    $swapvar = ($swapvar + 1) % 3")),
            "swapvar",
        );
        assert.ok(swap);
        assert.deepEqual(swap.values, ["0", "1", "2"]);
    });

    it("parses increment-mod when the rhs variable differs only by case", () => {
        const swap = slotByVar(
            menuFromIni(commandList("    $swapvar = ($SwapVar + 1) % 3")),
            "swapvar",
        );
        assert.ok(swap);
        assert.deepEqual(swap.values, ["0", "1", "2"]);
    });

    it("parses increment then modulo when the rhs variables differ only by case", () => {
        const swap = slotByVar(
            menuFromIni(commandList("    $swapvar = $SwapVar + 1\n    $swapvar = $SwapVar % 4")),
            "swapvar",
        );
        assert.ok(swap);
        assert.deepEqual(swap.values, ["0", "1", "2", "3"]);
    });

    it("parses wrap-else increment cycles when guard and rhs differ only by case", () => {
        const swap = slotByVar(
            menuFromIni(
                commandList(`    if $SwapVar < 2
        $swapvar = $SwapVar + 1
    else
        $swapvar = 0
    endif`),
            ),
            "swapvar",
        );
        assert.ok(swap);
        assert.deepEqual(swap.values, ["0", "1", "2"]);
    });

    it("parses increment reset cycles when the later guard differs only by case", () => {
        const swap = slotByVar(
            menuFromIni(
                commandList(`    $swapvar = $SwapVar + 1
    if $SwapVar > 2
        $swapvar = 0
    endif`),
            ),
            "swapvar",
        );
        assert.ok(swap);
        assert.deepEqual(swap.values, ["0", "1", "2"]);
    });

    it("rejects increment-mod cycles that exceed the value limit", () => {
        const menu = menuFromIni(`[Constants]
global persist $swapvar = 0
global persist $other = 0
global persist $fine = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $swapvar = ($swapvar + 1) % 100000
elif $clickedSlot == 2
    $other = ($other + 1) % 2
elif $clickedSlot == 3
    $fine = ($fine + 1) % 2
endif
`);
        assert.equal(slotByVar(menu, "swapvar"), undefined);
        assert.deepEqual(slotByVar(menu, "other")?.values, ["0", "1"]);
        assert.deepEqual(slotByVar(menu, "fine")?.values, ["0", "1"]);
    });

    it("rejects increment reset cycles that exceed the value limit", () => {
        const menu = menuFromIni(`[Constants]
global persist $swapvar = 0
global persist $other = 0
global persist $fine = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $swapvar = $swapvar + 1
    if $swapvar > 100000
        $swapvar = 0
    endif
elif $clickedSlot == 2
    $other = ($other + 1) % 2
elif $clickedSlot == 3
    $fine = ($fine + 1) % 2
endif
`);
        assert.equal(slotByVar(menu, "swapvar"), undefined);
        assert.deepEqual(slotByVar(menu, "other")?.values, ["0", "1"]);
        assert.deepEqual(slotByVar(menu, "fine")?.values, ["0", "1"]);
    });

    it("rejects arrow-button ranges that exceed the value limit", () => {
        const menu = menuFromIni(`[Constants]
global persist $swapvar = 0
global persist $other = 0
global persist $fine = 0
[CommandListButton1Right]
$swapvar = $swapvar + 1
if $swapvar > 100000
    $swapvar = 0
endif
[CommandListButton1Left]
$swapvar = $swapvar - 1
if $swapvar < 0
    $swapvar = 100000
endif
[CommandListButton2Right]
$other = $other + 1
if $other > 1
    $other = 0
endif
[CommandListButton2Left]
$other = $other - 1
if $other < 0
    $other = 1
endif
[CommandListButton3Right]
$fine = $fine + 1
if $fine > 1
    $fine = 0
endif
[CommandListButton3Left]
$fine = $fine - 1
if $fine < 0
    $fine = 1
endif
`);
        assert.equal(slotByVar(menu, "swapvar"), undefined);
        assert.deepEqual(slotByVar(menu, "other")?.values, ["0", "1"]);
        assert.deepEqual(slotByVar(menu, "fine")?.values, ["0", "1"]);
    });
});

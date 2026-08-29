package tools

import (
	"fmt"
	"testing"
)

func TestModelViewerSlotBindingsFindCycle(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $dress = 1

[KeyDress]
type = cycle
$dress = 0, 1, 2`)
	bindings := collectModelViewerSlotBindings(sections, collectModelViewerDefaultVariables(sections))
	if len(bindings) != 1 || bindings[0].Variable != "dress" || len(bindings[0].Values) != 3 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestModelViewerMenuParsesCaseInsensitiveIncrementCycles(t *testing.T) {
	tests := []struct {
		name, body string
		count      int
	}{
		{name: "increment modulo", body: `$swapvar = ($SwapVar + 1) % 3`, count: 3},
		{name: "separate modulo", body: "$swapvar = $SwapVar + 1\n$swapvar = $SwapVar % 4", count: 4},
		{name: "wrap else", body: "if $SwapVar < 2\n$swapvar = $SwapVar + 1\nelse\n$swapvar = 0\nendif", count: 3},
		{name: "increment reset", body: "$swapvar = $SwapVar + 1\nif $SwapVar > 2\n$swapvar = 0\nendif", count: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sections := parseModINI(fmt.Sprintf(`[Constants]
global persist $swapvar = 0
global persist $other = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
%s
elif $clickedSlot == 2
$other = ($other + 1) %% 2
endif`, test.body))
			bindings := collectModelViewerSlotBindings(sections, collectModelViewerDefaultVariables(sections))
			var found *modelViewerSlotBinding
			for index := range bindings {
				if bindings[index].Variable == "swapvar" {
					found = &bindings[index]
					break
				}
			}
			if found == nil || len(found.Values) != test.count {
				t.Fatalf("bindings = %#v", bindings)
			}
		})
	}
}

func TestModelViewerMenuRejectsExcessiveCycleAndKeepsOtherSlots(t *testing.T) {
	sections := parseModINI(`[Constants]
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
endif`)
	bindings := collectModelViewerSlotBindings(sections, collectModelViewerDefaultVariables(sections))
	found := make(map[string]int)
	for _, binding := range bindings {
		found[binding.Variable] = len(binding.Values)
	}
	if found["swapvar"] != 0 || found["other"] != 2 || found["fine"] != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestModelViewerMenuParsesBoundedArrowButtons(t *testing.T) {
	sections := parseModINI(`[CommandListButton1Right]
$swapvar = $SwapVar + 1
if $swapvar > 2
$swapvar = 0
endif
[CommandListButton1Left]
$swapvar = $SwapVar - 1
if $swapvar < 0
$swapvar = 2
endif
[CommandListButton2Right]
$other = $other + 1
if $other >= 2
$other = 0
endif
[CommandListButton2Left]
$other = $other - 1
if $other <= -1
$other = 1
endif`)
	bindings := collectModelViewerSlotBindings(sections, nil)
	if len(bindings) != 2 || bindings[0].Variable != "swapvar" || len(bindings[0].Values) != 3 || bindings[1].Variable != "other" || len(bindings[1].Values) != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestModelViewerMenuRejectsExcessiveIncrementResetAndKeepsOtherSlots(t *testing.T) {
	sections := parseModINI(`[Constants]
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
endif`)
	bindings := collectModelViewerSlotBindings(sections, collectModelViewerDefaultVariables(sections))
	found := make(map[string]int)
	for _, binding := range bindings {
		found[binding.Variable] = len(binding.Values)
	}
	if found["swapvar"] != 0 || found["other"] != 2 || found["fine"] != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestModelViewerMenuRejectsExcessiveArrowButtonRanges(t *testing.T) {
	sections := parseModINI(`[Constants]
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
endif`)
	bindings := collectModelViewerSlotBindings(sections, collectModelViewerDefaultVariables(sections))
	found := make(map[string]int)
	for _, binding := range bindings {
		found[binding.Variable] = len(binding.Values)
	}
	if found["swapvar"] != 0 || found["other"] != 2 || found["fine"] != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

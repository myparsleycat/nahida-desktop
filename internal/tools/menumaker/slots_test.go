package menumaker

import (
	"errors"
	"testing"
)

func testSlot(id, key string, handlers int, extraKeys []string, firstIndex int) MenuMakerSlot {
	slot := MenuMakerSlot{ID: id, Key: key, Name: id, OriginalKeys: []string{key}}
	for i := range handlers {
		slot.Handlers = append(slot.Handlers, MenuMakerHandler{
			Section: "Key" + id, SourceIndex: firstIndex + i, Key: key, Keys: append([]string{key}, extraKeys...),
			Assignments: []MenuMakerEntry{{Kind: "assign", Variable: "$Swap"}},
		})
	}
	return slot
}

func TestSetSlotKeyUpdatesEveryHandlerAndRejectsMultiKeyHandlers(t *testing.T) {
	t.Parallel()
	updated, err := SetSlotKey(testSlot("swap", "5", 2, nil, 0), "7")
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range updated.Handlers {
		if handler.Key != "7" || len(handler.Keys) != 1 || handler.Keys[0] != "7" {
			t.Fatalf("handler = %+v, want key 7", handler)
		}
	}

	if _, err := SetSlotKey(testSlot("swap", "5", 1, []string{"6"}, 0), "7"); !errors.Is(err, ErrMultiKeyHandler) {
		t.Fatalf("err = %v, want ErrMultiKeyHandler", err)
	}
}

func TestMergeSlotsKeepsUniqueHandlersAtFirstPosition(t *testing.T) {
	t.Parallel()
	slots := []MenuMakerSlot{
		testSlot("a", "1", 1, nil, 0), testSlot("b", "2", 1, nil, 1), testSlot("c", "3", 1, nil, 2),
	}
	slots[2].Handlers = append(slots[2].Handlers, slots[0].Handlers[0])

	merged := MergeSlots(slots, []string{"c", "a"}, "allKeys")
	if len(merged) != 2 || merged[0].ID != "merged-a-c" || merged[1].ID != "b" {
		t.Fatalf("merged ids = %v", merged)
	}
	if merged[0].Name != "a + c" || merged[0].MergeMode != "allKeys" || len(merged[0].Handlers) != 2 {
		t.Fatalf("merged slot = %+v", merged[0])
	}
}

// Drafts stored by the React frontend are restored only when this signature matches theirs byte for byte.
func TestSlotSignatureMatchesFrontendJSON(t *testing.T) {
	t.Parallel()
	slot := testSlot("a", " No_Modifiers  VK_F1 ", 1, nil, 4)
	slot.Handlers[0].Condition = " $active == 1 && $x < 2 "
	want := `[{"key":"vk_f1","handlers":[{"section":"keya","sourceIndex":4,` +
		`"condition":"$active == 1 && $x < 2","variables":["$swap"]}]}]`
	if got := SlotSignature([]MenuMakerSlot{slot}); got != want {
		t.Fatalf("signature =\n%s\nwant\n%s", got, want)
	}
	if got := SlotSignature(nil); got != "[]" {
		t.Fatalf("empty signature = %s", got)
	}
}

func TestSanitizeIconifySVG(t *testing.T) {
	t.Parallel()
	for _, unsafe := range []string{
		`<svg onload="alert(1)"><path d="M0 0" /></svg>`,
		`<svg><script>alert(1)</script></svg>`,
		`<svg><use href="https://evil.test/icon.svg" /></svg>`,
		`<svg><path fill="url(https://evil.test/a)" /></svg>`,
		`<g><path d="M0 0"/></g>`,
	} {
		if svg, ok := SanitizeIconifySVG(unsafe); ok {
			t.Errorf("SanitizeIconifySVG(%q) = %q, want rejection", unsafe, svg)
		}
	}

	svg, ok := SanitizeIconifySVG(
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" ` +
			`viewBox="0 0 24 24" class="x"><defs><path id="p" d="M1 1"/></defs>` +
			`<g fill="currentColor" data-x="1"><path d="M0 0h24" xlink:title="t"/></g></svg>`,
	)
	want := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<g fill="currentColor"><path d="M0 0h24"></path></g></svg>`
	if !ok || svg != want {
		t.Fatalf("SanitizeIconifySVG = %q, %v; want %q", svg, ok, want)
	}
}

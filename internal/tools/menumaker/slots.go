package menumaker

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// ErrMultiKeyHandler rejects a key change on a slot whose handler listens to several keys.
var ErrMultiKeyHandler = errors.New("MENU_MAKER_MULTI_KEY_HANDLER")

// MergeSlots replaces the selected slots with one slot at the position of the first of them, carrying all of
// their handlers. Fewer than two selected slots leave slots unchanged.
func MergeSlots(slots []MenuMakerSlot, selectedIDs []string, mode string) []MenuMakerSlot {
	selected := lo.Filter(slots, func(slot MenuMakerSlot, _ int) bool { return slices.Contains(selectedIDs, slot.ID) })
	if len(selected) < 2 {
		return slots
	}
	merged := selected[0]
	merged.ID = "merged-" + strings.Join(
		lo.Map(selected, func(slot MenuMakerSlot, _ int) string { return slot.ID }),
		"-",
	)
	merged.Handlers = uniqueHandlers(selected)
	merged.OriginalKeys = uniqueNormalizedKeys(lo.FlatMap(selected, func(slot MenuMakerSlot, _ int) []string {
		return slot.OriginalKeys
	}))
	merged.Name = strings.Join(lo.Map(selected, func(slot MenuMakerSlot, _ int) string { return slot.Name }), " + ")
	merged.MergeMode = mode

	out := make([]MenuMakerSlot, 0, len(slots)-len(selected)+1)
	for _, slot := range slots {
		switch {
		case slot.ID == selected[0].ID:
			out = append(out, merged)
		case !slices.Contains(selectedIDs, slot.ID):
			out = append(out, slot)
		}
	}
	return out
}

// SetSlotKey gives the slot and every handler in it the key.
func SetSlotKey(slot MenuMakerSlot, key string) (MenuMakerSlot, error) {
	if slices.ContainsFunc(slot.Handlers, func(handler MenuMakerHandler) bool { return len(handler.Keys) > 1 }) {
		return slot, ErrMultiKeyHandler
	}
	slot.Key = key
	slot.Handlers = slices.Clone(slot.Handlers)
	for i := range slot.Handlers {
		slot.Handlers[i].Key = key
		slot.Handlers[i].Keys = []string{key}
	}
	return slot, nil
}

// SlotSignature identifies the key handlers a document parses into, so that a saved draft is restored only
// onto the same handlers. It matches the JSON the React frontend stored with its drafts.
func SlotSignature(slots []MenuMakerSlot) string {
	type handlerSignature struct {
		Section     string   `json:"section"`
		SourceIndex int      `json:"sourceIndex"`
		Condition   string   `json:"condition"`
		Variables   []string `json:"variables"`
	}
	type slotSignature struct {
		Key      string             `json:"key"`
		Handlers []handlerSignature `json:"handlers"`
	}
	signature := lo.Map(slots, func(slot MenuMakerSlot, _ int) slotSignature {
		return slotSignature{
			Key: normalizeMenuMakerKey(slot.Key),
			Handlers: lo.Map(slot.Handlers, func(handler MenuMakerHandler, _ int) handlerSignature {
				return handlerSignature{
					Section:     strings.ToLower(handler.Section),
					SourceIndex: handler.SourceIndex,
					Condition:   strings.TrimSpace(handler.Condition),
					Variables:   AssignmentVariables(handler),
				}
			}),
		}
	})
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(signature)
	return strings.TrimSuffix(out.String(), "\n")
}

// AssignmentVariables returns the lower-cased variables the handler assigns.
func AssignmentVariables(handler MenuMakerHandler) []string {
	variables := []string{}
	for _, entry := range handler.Assignments {
		if entry.Variable != "" {
			variables = append(variables, strings.ToLower(entry.Variable))
		}
	}
	return variables
}

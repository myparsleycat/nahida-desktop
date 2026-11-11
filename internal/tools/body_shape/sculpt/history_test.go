package sculpt

import "testing"

func TestSelectionHistoryEntryStoresOnlyChanges(t *testing.T) {
	entry, ok := NewSelectionHistoryEntry([]float32{0, 0.25, 0.5, 0.75}, []float32{0, 0.5, 0.5, 1})
	if !ok {
		t.Fatal("no entry")
	}
	assertEqual(t, "indices", entry.Indices, []uint32{1, 3})
	assertEqual(t, "before", entry.Before, []float32{0.25, 0.75})
	assertEqual(t, "after", entry.After, []float32{0.5, 1})
}

func TestSelectionHistoryUndoRedo(t *testing.T) {
	var history SelectionHistory
	weights := []float32{1, 0.25, 0.75}
	if !history.Commit([]float32{0, 0.25, 0.5}, weights) {
		t.Fatal("no change committed")
	}

	history.UndoInto(weights)
	assertEqual(t, "undone", weights, []float32{0, 0.25, 0.5})
	history.RedoInto(weights)
	assertEqual(t, "redone", weights, []float32{1, 0.25, 0.75})
	if !history.CanUndo() || history.CanRedo() {
		t.Fatalf("undo %d, redo %d", len(history.Undo), len(history.Redo))
	}
}

func TestSelectionHistoryEviction(t *testing.T) {
	entry := func(index uint32) SelectionHistoryEntry {
		return SelectionHistoryEntry{Indices: []uint32{index}, Before: []float32{0}, After: []float32{1}}
	}

	byCount := SelectionHistory{Limit: 2}
	for index := range uint32(4) {
		byCount.Push(entry(index))
	}
	if len(byCount.Undo) != 2 || byCount.Undo[0].Indices[0] != 2 || byCount.Undo[1].Indices[0] != 3 {
		t.Fatalf("undo = %+v, want the last two entries", byCount.Undo)
	}

	oversized := SelectionHistory{ByteLimit: 1}
	oversized.Push(entry(0))
	if len(oversized.Undo) != 1 {
		t.Fatalf("undo = %+v, want the latest entry kept", oversized.Undo)
	}
}

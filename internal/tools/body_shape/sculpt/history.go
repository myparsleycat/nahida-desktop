package sculpt

const (
	SelectionHistoryLimit     = 50
	SelectionHistoryByteLimit = 64 * 1024 * 1024
)

// SelectionHistoryEntry stores only the vertices one edit changed, with their
// values before and after it.
type SelectionHistoryEntry struct {
	Indices []uint32
	Before  []float32
	After   []float32
}

func (e SelectionHistoryEntry) bytes() int {
	return (len(e.Indices) + len(e.Before) + len(e.After)) * 4
}

// NewSelectionHistoryEntry diffs two selections; it returns false when nothing changed.
func NewSelectionHistoryEntry(before, after []float32) (SelectionHistoryEntry, bool) {
	var entry SelectionHistoryEntry
	for index := range min(len(before), len(after)) {
		if before[index] != after[index] {
			entry.Indices = append(entry.Indices, uint32(index))
			entry.Before = append(entry.Before, before[index])
			entry.After = append(entry.After, after[index])
		}
	}
	return entry, len(entry.Indices) > 0
}

// ApplySelectionHistoryValues writes values back to the entry's vertices.
func ApplySelectionHistoryValues(weights []float32, indices []uint32, values []float32) {
	for index, vertex := range indices {
		weights[vertex] = values[index]
	}
}

// SelectionHistory is the undo and redo stack of selection edits. Limit and
// ByteLimit cap the undo stack; zero means SelectionHistoryLimit and
// SelectionHistoryByteLimit. The latest entry is always kept.
type SelectionHistory struct {
	Undo      []SelectionHistoryEntry
	Redo      []SelectionHistoryEntry
	Limit     int
	ByteLimit int
}

// Push records an edit, clears redo and evicts the oldest entries over the caps.
func (h *SelectionHistory) Push(entry SelectionHistoryEntry) {
	h.Undo = append(h.Undo, entry)
	h.Redo = nil
	limit := h.Limit
	if limit == 0 {
		limit = SelectionHistoryLimit
	}
	byteLimit := h.ByteLimit
	if byteLimit == 0 {
		byteLimit = SelectionHistoryByteLimit
	}

	total := 0
	for _, item := range h.Undo {
		total += item.bytes()
	}
	evicted := 0
	for len(h.Undo)-evicted > 1 && (len(h.Undo)-evicted > limit || total > byteLimit) {
		total -= h.Undo[evicted].bytes()
		evicted++
	}
	h.Undo = append(h.Undo[:0:0], h.Undo[evicted:]...)
}

// Commit pushes the difference between a selection before an edit and after it,
// and reports whether anything changed.
func (h *SelectionHistory) Commit(before, after []float32) bool {
	entry, ok := NewSelectionHistoryEntry(before, after)
	if ok {
		h.Push(entry)
	}
	return ok
}

// UndoInto restores the previous selection into weights.
func (h *SelectionHistory) UndoInto(weights []float32) bool {
	if len(h.Undo) == 0 {
		return false
	}
	entry := h.Undo[len(h.Undo)-1]
	h.Undo = h.Undo[:len(h.Undo)-1]
	ApplySelectionHistoryValues(weights, entry.Indices, entry.Before)
	h.Redo = append(h.Redo, entry)
	return true
}

// RedoInto reapplies the last undone edit into weights.
func (h *SelectionHistory) RedoInto(weights []float32) bool {
	if len(h.Redo) == 0 {
		return false
	}
	entry := h.Redo[len(h.Redo)-1]
	h.Redo = h.Redo[:len(h.Redo)-1]
	ApplySelectionHistoryValues(weights, entry.Indices, entry.After)
	h.Undo = append(h.Undo, entry)
	return true
}

func (h *SelectionHistory) CanUndo() bool { return len(h.Undo) > 0 }

func (h *SelectionHistory) CanRedo() bool { return len(h.Redo) > 0 }

// Reset drops all history, as when another mesh is loaded.
func (h *SelectionHistory) Reset() {
	h.Undo, h.Redo = nil, nil
}

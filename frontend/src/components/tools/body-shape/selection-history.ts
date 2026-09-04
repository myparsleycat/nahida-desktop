export type SelectionHistoryEntry = {
    indices: Uint32Array;
    before: Float32Array;
    after: Float32Array;
};

export type SelectionHistory = {
    undo: SelectionHistoryEntry[];
    redo: SelectionHistoryEntry[];
};

export const SELECTION_HISTORY_LIMIT = 50;
export const SELECTION_HISTORY_BYTE_LIMIT = 64 * 1024 * 1024;

export function createSelectionHistoryEntry(
    before: Float32Array,
    after: Float32Array,
): SelectionHistoryEntry | undefined {
    const count = Math.min(before.length, after.length);
    const changedIndices: number[] = [];
    for (let index = 0; index < count; index++) {
        if (before[index] !== after[index]) changedIndices.push(index);
    }
    if (changedIndices.length === 0) return undefined;

    const indices = Uint32Array.from(changedIndices);
    const beforeValues = new Float32Array(indices.length);
    const afterValues = new Float32Array(indices.length);
    for (let index = 0; index < indices.length; index++) {
        const vertexIndex = indices[index];
        beforeValues[index] = before[vertexIndex];
        afterValues[index] = after[vertexIndex];
    }
    return { indices, before: beforeValues, after: afterValues };
}

export function applySelectionHistoryValues(
    weights: Float32Array,
    indices: Uint32Array,
    values: Float32Array,
): void {
    for (let index = 0; index < indices.length; index++) {
        weights[indices[index]] = values[index];
    }
}

export function pushSelectionHistory(
    history: SelectionHistory,
    entry: SelectionHistoryEntry,
    options: { limit?: number; byteLimit?: number } = {},
): void {
    history.undo.push(entry);
    history.redo = [];
    const limit = options.limit ?? SELECTION_HISTORY_LIMIT;
    const byteLimit = options.byteLimit ?? SELECTION_HISTORY_BYTE_LIMIT;
    let totalBytes = history.undo.reduce((total, item) => total + entryBytes(item), 0);
    while (history.undo.length > 1 && (history.undo.length > limit || totalBytes > byteLimit)) {
        totalBytes -= entryBytes(history.undo.shift()!);
    }
}

function entryBytes(entry: SelectionHistoryEntry): number {
    return entry.indices.byteLength + entry.before.byteLength + entry.after.byteLength;
}

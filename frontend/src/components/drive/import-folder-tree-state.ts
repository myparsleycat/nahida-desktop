export type ParentLookup = (id: string) => string | null | undefined;

export function collectSelectedAncestorIds(
    selected: ReadonlySet<string>,
    getParentId: ParentLookup,
): Set<string> {
    const ancestors = new Set<string>();
    for (const id of selected) {
        walkParents(id, getParentId, (parentId) => ancestors.add(parentId));
    }
    return ancestors;
}

export function hasSelectedAncestor(
    id: string,
    selected: ReadonlySet<string>,
    getParentId: ParentLookup,
): boolean {
    let found = false;
    walkParents(id, getParentId, (parentId) => {
        if (selected.has(parentId)) found = true;
    });
    return found;
}

export function isDescendantOf(id: string, ancestorId: string, getParentId: ParentLookup): boolean {
    let found = false;
    walkParents(id, getParentId, (parentId) => {
        if (parentId === ancestorId) found = true;
    });
    return found;
}

export function toggleSubtreeSelection(
    selected: ReadonlySet<string>,
    id: string,
    getParentId: ParentLookup,
): Set<string> {
    if (selected.has(id)) {
        const next = new Set(selected);
        next.delete(id);
        return next;
    }
    if (hasSelectedAncestor(id, selected, getParentId)) return new Set(selected);

    const next = new Set(selected);
    for (const selectedId of selected) {
        if (isDescendantOf(selectedId, id, getParentId)) next.delete(selectedId);
    }
    next.add(id);
    return next;
}

export function pruneSubtreeSelection(
    selected: ReadonlySet<string>,
    getParentId: ParentLookup,
): Set<string> {
    const pruned = new Set(selected);
    for (const id of selected) {
        if (hasSelectedAncestor(id, selected, getParentId)) pruned.delete(id);
    }
    return pruned;
}

function walkParents(
    id: string,
    getParentId: ParentLookup,
    visit: (parentId: string) => void,
): void {
    const visited = new Set([id]);
    let currentId = id;
    while (true) {
        const parentId = getParentId(currentId);
        if (!parentId || visited.has(parentId)) return;
        visited.add(parentId);
        visit(parentId);
        currentId = parentId;
    }
}

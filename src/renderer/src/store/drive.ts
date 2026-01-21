import { LayoutType, SortType } from "@renderer/types";
import { Content } from "@shared/types";
import { useNavigate } from "@tanstack/react-router";
import { createStore, useStore } from "zustand";

interface ViewState {
    layout: "list" | "grid";
    setLayout: (layout: "list" | "grid") => void;
    sortType: "NAME:ASC" | "NAME:DESC" | "SIZE:ASC" | "SIZE:DESC" | "DATE:ASC" | "DATE:DESC";
    setSortType: (
        sortType: "NAME:ASC" | "NAME:DESC" | "SIZE:ASC" | "SIZE:DESC" | "DATE:ASC" | "DATE:DESC",
    ) => void;
    searchInDirQuery: string;
    setSearchInDirQuery: (query: string) => void;
    isfocusSearchInput: boolean;
    setFocusSearchInputState: (state: boolean) => void;
    lastDriveId: string;
    setLastDriveId: (id: string) => void;
    lastShareId: string;
    setLastShareId: (id: string) => void;
}

export const viewStore = createStore<ViewState>((set) => ({
    layout: "list",
    setLayout: (layout) => set({ layout }),
    sortType: "NAME:ASC",
    setSortType: (sortType) => set({ sortType }),
    searchInDirQuery: "",
    setSearchInDirQuery: (searchInDirQuery) => set({ searchInDirQuery }),
    isfocusSearchInput: false,
    setFocusSearchInputState: (isfocusSearchInput) => set({ isfocusSearchInput }),
    lastDriveId: "root",
    setLastDriveId: (lastDriveId) => set({ lastDriveId }),
    lastShareId: "share",
    setLastShareId: (lastShareId) => set({ lastShareId }),
}));

export function useViewStore<T>(selector: (state: ViewState) => T): T {
    return useStore(viewStore, selector);
}

interface DragStore {
    uploadDragging: boolean;
    setUploadDragging: (dragging: boolean) => void;
    currentDragOver: Content | null;
    setCurrentDragOver: (content: Content | null) => void;
}

export const dragStore = createStore<DragStore>((set) => ({
    uploadDragging: false,
    setUploadDragging: (uploadDragging) => set({ uploadDragging }),
    currentDragOver: null,
    setCurrentDragOver: (currentDragOver) => set({ currentDragOver }),
}));

export function useDragStore<T>(selector: (state: DragStore) => T): T {
    return useStore(dragStore, selector);
}

type DialogResolve = (result: any) => void;

interface BaseDialogState {
    open: boolean;
    data?: any;
}

interface ClearPrefixData {
    id: string | null;
    name: string;
    inProgress: boolean;
}

interface DialogStates {
    gamebananaDialog: BaseDialogState;
    emptyTrashDialog: BaseDialogState;
    createDirDialog: BaseDialogState;
    renameDialog: BaseDialogState;
    previewDialog: BaseDialogState;
    shareDialog: BaseDialogState & { data: { id: string } };
    searchCommand: BaseDialogState;
    conflictNameDialog: BaseDialogState;
    clearPrefixDialog: BaseDialogState & { data: ClearPrefixData };
    searchDialog: BaseDialogState;
    notiDialog: BaseDialogState;
}

type DialogName = keyof DialogStates;
type DialogData<T extends DialogName> = DialogStates[T]["data"];

interface DialogActions {
    anyDialogOpen: () => boolean;
    getDialogState: <T extends DialogName>(dialogName: T) => DialogStates[T];
    setOpen: <T extends DialogName>(
        dialogName: T,
        isOpen: boolean,
        data?: Partial<DialogData<T>>,
    ) => void;
    toggleDialog: <T extends DialogName>(dialogName: T, data?: Partial<DialogData<T>>) => void;
    updateDialogData: <T extends DialogName>(dialogName: T, data: Partial<DialogData<T>>) => void;
    showDialog: <R = boolean, T extends DialogName = DialogName>(
        dialogName: T,
        data?: Partial<DialogData<T>>,
    ) => Promise<R>;
    resolveDialog: <R = boolean>(dialogName: DialogName, result: R) => void;
    updateDialogField: <T extends DialogName, K extends keyof DialogData<T>>(
        dialogName: T,
        field: K,
        value: DialogData<T>[K],
    ) => void;
}

const activeDialogs: Record<DialogName, DialogResolve | null> = {} as Record<
    DialogName,
    DialogResolve | null
>;
export const dialogStore = createStore<DialogStates & DialogActions>((set, get) => ({
    gamebananaDialog: { open: false, data: {} },
    emptyTrashDialog: { open: false, data: {} },
    createDirDialog: { open: false, data: {} },
    renameDialog: { open: false, data: {} },
    previewDialog: { open: false, data: {} },
    shareDialog: { open: false, data: { id: "" } },
    searchCommand: { open: false, data: {} },
    conflictNameDialog: { open: false, data: {} },
    clearPrefixDialog: {
        open: false,
        data: { id: null, name: "", inProgress: false },
    },
    searchDialog: { open: false, data: {} },
    notiDialog: { open: false, data: {} },

    anyDialogOpen: () => {
        const state = get();
        return Object.values(state).some(
            (dialogState) =>
                typeof dialogState === "object" &&
                dialogState !== null &&
                "open" in dialogState &&
                dialogState.open,
        );
    },

    getDialogState: (dialogName) => {
        return get()[dialogName];
    },

    setOpen: (dialogName, isOpen, data) =>
        set((state) => ({
            ...state,
            [dialogName]: {
                ...state[dialogName],
                open: isOpen,
                data: isOpen
                    ? data
                        ? { ...state[dialogName].data, ...data }
                        : state[dialogName].data
                    : undefined,
            },
        })),

    toggleDialog: (dialogName, data) =>
        set((state) => {
            const isOpen = !state[dialogName].open;
            return {
                ...state,
                [dialogName]: {
                    ...state[dialogName],
                    open: isOpen,
                    data:
                        isOpen && data
                            ? {
                                  ...state[dialogName].data,
                                  ...data,
                              }
                            : state[dialogName].data,
                },
            };
        }),

    updateDialogData: (dialogName, data) =>
        set((state) => ({
            ...state,
            [dialogName]: {
                ...state[dialogName],
                data: {
                    ...state[dialogName].data,
                    ...data,
                },
            },
        })),

    showDialog: (dialogName, data) => {
        return new Promise((resolve) => {
            activeDialogs[dialogName] = resolve as DialogResolve;

            set((state) => ({
                ...state,
                [dialogName]: {
                    ...state[dialogName],
                    open: true,
                    data: {
                        ...state[dialogName].data,
                        ...(data || {}),
                    },
                },
            }));
        });
    },

    resolveDialog: (dialogName, result) => {
        if (activeDialogs[dialogName]) {
            activeDialogs[dialogName]!(result);
            activeDialogs[dialogName] = null;
        }
    },

    updateDialogField: (dialogName, field, value) =>
        set((state) => ({
            ...state,
            [dialogName]: {
                ...state[dialogName],
                data: {
                    ...state[dialogName].data,
                    [field]: value,
                },
            },
        })),
}));
export const useDialogStore = () => useStore(dialogStore);

interface SelectionState {
    selectedItems: Content[];
    setSelectedItems: (items: Content[]) => void;
    lastSelectedIdx: number | null;
    setLastSelectedIdx: (idx: number | null) => void;
    copyOrCuts: {
        action: "cut" | "copy" | null;
        items: Content[];
    };
    setCopyOrCuts: (action: "cut" | "copy" | null, items: Content[]) => void;
}

export const selectionStore = createStore<SelectionState>((set) => ({
    selectedItems: [],
    setSelectedItems: (selectedItems) => set({ selectedItems }),
    lastSelectedIdx: null,
    setLastSelectedIdx: (lastSelectedIdx) => set({ lastSelectedIdx }),
    copyOrCuts: { action: null, items: [] },
    setCopyOrCuts: (action, items) => set({ copyOrCuts: { action, items } }),
}));

export const useSelectionStore = () => useStore(selectionStore);

export function useContentMenu(sortedContents?: Content[]) {
    const dialog = dialogStore.getState();
    const selection = useSelectionStore();
    const { currentDragOver } = dragStore.getState();
    const navi = useNavigate();
    // const setItemId = useDriveStore((state) => state.setItemId);

    const handleItemClick = async (item: Content, index: number, event: React.MouseEvent) => {
        if (event.shiftKey && selection.lastSelectedIdx !== null && sortedContents) {
            const start = Math.min(selection.lastSelectedIdx, index);
            const end = Math.max(selection.lastSelectedIdx, index);
            const newSelections = sortedContents.slice(start, end + 1);

            if (event.metaKey || event.ctrlKey) {
                selection.setSelectedItems(
                    Array.from(new Set([...selection.selectedItems, ...newSelections])),
                );
            } else {
                selection.setSelectedItems(newSelections);
            }
        } else if (event.metaKey || event.ctrlKey) {
            if (selection.selectedItems.includes(item)) {
                selection.setSelectedItems(
                    selection.selectedItems.filter((selected) => selected.id !== item.id),
                );
            } else {
                selection.setSelectedItems([...selection.selectedItems, item]);
            }
            selection.setLastSelectedIdx(index);
        } else {
            selection.setSelectedItems([item]);
            selection.setLastSelectedIdx(index);
        }
    };

    const handleItemRightClick = async (e: React.MouseEvent, item: Content) => {
        if (selection.selectedItems.length <= 1) {
            selection.setSelectedItems([item]);
        }
    };

    const handleClickOutside = (e: React.MouseEvent) => {
        selection.setSelectedItems([]);
        selection.setLastSelectedIdx(null);
    };

    const handleItemDoubleClick = async (item: Content, navi2?: (str: string) => void) => {
        if (item.isDir) {
            if (navi2) {
                navi2(item.id);
            } else {
                // setItemId(item.id);
                navi({ to: item.id });
            }
        } else {
            if (item.mimeType?.startsWith("text")) {
                // textViewerStore.openTextViewer(item);
            } else {
                await window.api.invoke("drive:fn:startDownload", item.id);
            }
        }
    };

    const getDoubleClickHandler = (item: Content, navi?: (str: string) => void) => () => {
        if (!dialog.anyDialogOpen()) {
            handleItemDoubleClick(item, navi);
        }
    };

    return {
        dialog,
        selection,
        currentDragOver,
        handleItemClick,
        handleItemRightClick,
        handleClickOutside,
        getDoubleClickHandler,
    };
}

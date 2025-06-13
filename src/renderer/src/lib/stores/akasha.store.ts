
import { writable, get, derived } from 'svelte/store';
import { _ } from 'svelte-i18n';

export const isTransferSheetOpen = writable(false);
export const openChangeTransferSheet = (open: boolean) => isTransferSheetOpen.set(open);

function createLoadingStateStore() {
  const state = {
    createDirLoading: false,
    renameLoading: false,
    makePubLinkLoading: false
  }

  const store = writable(state);

  return {
    subscribe: store.subscribe,
    set: store.set,
    setLoading: (key: keyof typeof state, value: boolean) => {
      store.update(state => ({
        ...state,
        [key]: value
      }))
    }
  }
}

export interface BaseDialogState {
  open: boolean;
  data?: any;
}

export interface ClearPrefixData {
  id: string | null;
  name: string;
  inProgress: boolean;
}

function createDialogStateStore() {
  type DialogResolve = (result: any) => void;

  const state = {
    // layout
    gamebananaDialog: { open: false, data: {} } as BaseDialogState,
    emptyTrashDialog: { open: false, data: {} } as BaseDialogState,

    // drive/[uuid]
    createDirDialog: { open: false, data: {} } as BaseDialogState,
    renameDialog: { open: false, data: {} } as BaseDialogState,
    previewDialog: { open: false, data: {} } as BaseDialogState,
    shareDialog: { open: false, data: {} as { id: string } },
    searchCommand: { open: false, data: {} } as BaseDialogState,
    conflictNameDialog: { open: false, data: {} } as BaseDialogState,

    clearPrefixDialog: {
      open: false,
      data: { id: null, name: '', inProgress: false } as ClearPrefixData
    }
  } as const;

  type DialogState = typeof state;
  type DialogName = keyof DialogState;
  type DialogData<T extends DialogName> = DialogState[T]['data'];
  const activeDialogs: Record<DialogName, DialogResolve | null> = {} as Record<DialogName, DialogResolve | null>;

  const store = writable(state);

  const anyDialogOpen = derived(store, $state =>
    Object.values($state).some(dialogState => dialogState.open)
  );

  return {
    subscribe: store.subscribe,
    set: store.set,
    anyDialogOpen,

    getDialogState: <T extends DialogName>(dialogName: T): DialogState[T] => {
      return get(store)[dialogName];
    },

    setOpen: <T extends DialogName>(
      dialogName: T,
      isOpen: boolean,
      data?: Partial<DialogData<T>>
    ) => {
      store.update(state => ({
        ...state,
        [dialogName]: {
          ...state[dialogName],
          open: isOpen,
          data: isOpen && data ? {
            ...state[dialogName].data,
            ...data
          } : state[dialogName].data
        }
      }));
    },
    toggleDialog: <T extends DialogName>(
      dialogName: T,
      data?: Partial<DialogData<T>>
    ) => {
      store.update(state => {
        const isOpen = !state[dialogName].open;
        return {
          ...state,
          [dialogName]: {
            ...state[dialogName],
            open: isOpen,
            data: isOpen && data ? {
              ...state[dialogName].data,
              ...data
            } : state[dialogName].data
          }
        };
      });
    },

    updateDialogData: <T extends DialogName>(
      dialogName: T,
      data: Partial<DialogData<T>>
    ) => {
      store.update(state => ({
        ...state,
        [dialogName]: {
          ...state[dialogName],
          data: {
            ...state[dialogName].data,
            ...data
          }
        }
      }));
    },

    showDialog: <R = boolean, T extends DialogName = DialogName>(
      dialogName: T,
      data?: Partial<DialogData<T>>
    ) => {
      return new Promise<R>((resolve) => {
        activeDialogs[dialogName] = resolve as DialogResolve;

        store.update(state => ({
          ...state,
          [dialogName]: {
            ...state[dialogName],
            open: true,
            data: {
              ...state[dialogName].data,
              ...(data || {})
            }
          }
        }));
      });
    },

    resolveDialog: <R = boolean>(dialogName: DialogName, result: R) => {
      if (activeDialogs[dialogName]) {
        activeDialogs[dialogName]!(result);
        activeDialogs[dialogName] = null;
      }

      // store.update(state => ({
      //   ...state,
      //   [dialogName]: {
      //     ...state[dialogName],
      //     open: false
      //   }
      // }));
    },

    updateDialogField: <
      T extends DialogName,
      K extends keyof DialogData<T>
    >(
      dialogName: T,
      field: K,
      value: DialogData<T>[K]
    ) => {
      store.update(state => ({
        ...state,
        [dialogName]: {
          ...state[dialogName],
          data: {
            ...state[dialogName].data,
            [field]: value
          }
        }
      }));
    }
  };
}

export const DialogStateStore = createDialogStateStore();
export const LoadingStateStore = createLoadingStateStore();
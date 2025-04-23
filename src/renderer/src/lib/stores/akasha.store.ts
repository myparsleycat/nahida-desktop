
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

type DialogResolve = (value: boolean) => void;

// 기본 다이얼로그 상태 인터페이스를 함수 외부로 이동
export interface BaseDialogState {
  open: boolean;
  data?: any;
}

// clearPrefixDialog 데이터 타입 정의를 함수 외부로 이동
export interface ClearPrefixData {
  id: string | null;
  name: string;
  inProgress: boolean;
}

function createDialogStateStore() {
  type DialogResolve = (result: any) => void;

  // 모든 다이얼로그 상태를 저장하는 객체
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

    // 여러 데이터 필드가 있는 다이얼로그 예시
    clearPrefixDialog: {
      open: false,
      data: { id: null, name: '', inProgress: false } as ClearPrefixData
    }
  } as const;

  // 상태 타입 정의
  type DialogState = typeof state;

  // 다이얼로그 이름에 대한 타입 정의 (자동완성 지원)
  type DialogName = keyof DialogState;

  // 특정 다이얼로그의 데이터 타입 가져오기
  type DialogData<T extends DialogName> = DialogState[T]['data'];

  // 현재 활성화된 다이얼로그와 그 해결자를 저장
  const activeDialogs: Record<DialogName, DialogResolve | null> = {} as Record<DialogName, DialogResolve | null>;

  const store = writable(state);

  // 하나라도 열린 다이얼로그가 있는지 확인
  const anyDialogOpen = derived(store, $state =>
    Object.values($state).some(dialogState => dialogState.open)
  );

  return {
    subscribe: store.subscribe,
    set: store.set,
    anyDialogOpen,

    // 다이얼로그 상태 조회
    getDialogState: <T extends DialogName>(dialogName: T): DialogState[T] => {
      return get(store)[dialogName];
    },

    // 다이얼로그 열림/닫힘 상태 설정 (boolean 파라미터 사용)
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
          // data는 isOpen이 true이고 data가 제공된 경우에만 업데이트
          data: isOpen && data ? {
            ...state[dialogName].data,
            ...data
          } : state[dialogName].data
        }
      }));
    },

    // 다이얼로그 토글 (단순히 열고 닫기)
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
            // data는 isOpen이 true이고 data가 제공된 경우에만 업데이트
            data: isOpen && data ? {
              ...state[dialogName].data,
              ...data
            } : state[dialogName].data
          }
        };
      });
    },

    // 다이얼로그 데이터 업데이트
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

    // 프로미스와 함께 다이얼로그 표시 (결과 반환)
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

    // 다이얼로그 결과 처리 및 닫기
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

    // 특정 다이얼로그 데이터 필드 업데이트 유틸리티
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
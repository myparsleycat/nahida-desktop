import { ElectronAPI } from '@electron-toolkit/preload'
import type { DirCreateManyResp, MoveManyResp, TrashManyResp } from '@shared/types/drive.types';
import type { DirectChildren } from '@shared/types/mods.types';
import type { HelloModsRespSuccessResp, Mod } from '@shared/types/nahida.types';

declare global {
  interface Window {
    electron: ElectronAPI;
    webUtils: {
      getPathForFile: (file: File) => string;
    }
    api: {
      auth: {
        onAuthStateChanged: (callback: (value: boolean) => void) => () => void
        checkSessionState: () => Promise<boolean>
        startOAuth2Login: () => Promise<void>
        logout: () => Promise<void>
      }

      fss: {
        select: (opt: ElectronAPI.OpenDialogOptions) => Promise<string | null>
        readDir: (path: string, options: ReadDirectoryOptions) => Promise<FileInfo[]>
        readFile: (path: string) => Promise<ArrayBufferLike>
        writeFile: (path: string, data: ArrayBuffer) => Promise<boolean>
        getStat: (path: string) => Promise<Stats>
        openPath: (path: string) => Promise<void>
        deletePath: (path: string) => Promise<boolean>
        getImgBase64: (path: string) => Promise<string>
        watchFolder: (path: string, options?: { recursive?: boolean, depth?: number }) => Promise<boolean>
        unwatchFolder: (path: string) => Promise<boolean>
        getWatchedFolders: () => Promise<string[]>
        folderEvent: (callback: (data: FolderEventData) => void) => () => void
      }

      mods: {
        clearPath: () => void
        ui: {
          resizable: {
            get: () => Promise<number>
            set: (size: number) => Promise<void>
          }
          layout: {
            get: () => Promise<'grid' | 'list'>
            set: (layout: 'grid' | 'list') => Promise<void>
          }
        }
        folder: {
          getAll: () => Promise<ModFolders[]>
          create: (name: string, path: string) => Promise<boolean>
          delete: (path: string) => Promise<void>
          changeSeq: (path: string, newSeq: number) => Promise<boolean>
          dir: {
            read: (path: string, options?: ReadDirectoryOptions) => Promise<FileInfo[]>
            disableAll: (path: string) => Promise<boolean>
            enableAll: (path: string) => Promise<boolean>
          }
          read: (path: string) => Promise<DirectChildren[]>
        },
        mod: {
          read: (path: string) => Promise<DirectChildren[]>
          toggle: (path: string) => Promise<boolean>
        },
        ini: {
          parse: (path: string) => Promise<IniParseResult[]>
          update: (path: string, section: string, key: 'key' | 'back', value: string) => Promise<boolean>
        }
        intx: {
          drop: (data: string[]) => Promise<boolean>
        }
        msg: {
          currentCharPathChanged: (callback: (path: string) => void) => () => void
          currentFolderPathChanged: (callback: (path: string) => void) => () => void
        }
      }

      nahida: {
        get: {
          mods: (params: NahidaIPCHelloModsParams) => Promise<HelloModsRespSuccessResp>
        }
        startDownload: (mod: Mod, path: string) => Promise<boolean>
      }

      drive: {
        item: {
          get: (id: string) => Promise<GetContentsResp>
          move: (current: string, ids: string[], newParentId: string) => Promise<MoveManyResp>
          dir: {
            create: (parentId: string, dirs: { name: string; path: string; }[]) => Promise<DirCreateManyResp>
          },
          rename: (id: string, name: string) => Promise<void>
          download: {
            enqueue: (id: string, name: string) => Promise<void>
          },
          trash_many: (ids: string[]) => Promise<TrashManyResp>
        },
        util: {
          imageCache: {
            get: (url: string) => Promise<string>
            sizes: () => Promise<number>
            clear: () => Promise<void>
            getStates: () => Promise<{ state: boolean; sizes: number; }>
            change: (v) => Promise<void>
          }
        }
      }

      overlay: {
        activate: () => Promise<void>
        focusTarget: () => Promise<void>
        screenshot: (width: number, height: number) => Promise<Buffer>
        overlayEvent: (callback: (event: {
          type: 'attach' | 'detach' | 'focus' | 'blur' | 'moveresize';
          data?: {
            bounds?: {
              x: number;
              y: number;
              width: number;
              height: number;
            };
            hasAccess?: boolean;
            isFullscreen?: boolean;
          };
        }) => void) => () => void
      }

      toast: {
        toastShow: (callback: (params: any) => void) => () => void
      }

      renderer: {
        requestCharPath: (callback: (data: any) => void) => () => any
        charPathResponse: (reqId: string, ob: { success: boolean; data: string }) => void
      }

      window: WindowControls
    }
  }
}
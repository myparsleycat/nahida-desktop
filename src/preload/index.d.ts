import { ElectronAPI } from '@electron-toolkit/preload'
import type { DirCreateManyResp } from '../types/drive.types';

declare global {
  interface Window {
    electron: ElectronAPI;
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
        saveFile: (path: string, data: ArrayBuffer) => Promise<void>
        getStat: (path: string) => Promise<Stats>
        openPath: (path: string) => Promise<void>
        deletePath: (path: string) => Promise<void>
        getImgBase64: (path: string) => Promise<string>
        watchFolder: (path: string, options?: { recursive?: boolean, depth?: number }) => Promise<boolean>
        unwatchFolder: (path: string) => Promise<boolean>
        getWatchedFolders: () => Promise<string[]>
        folderEvent: (callback: (data: FolderEventData) => void) => () => void
      }

      mods: {
        ui: {
          resizable: {
            get: () => Promise<number>
            set: (size: number) => Promise<void>
          }
        },
        folder: {
          getAll: () => Promise<ModFolders[]>
          create: (name: string, path: string) => Promise<void>
          delete: (path: string) => Promise<void>
          dir: {
            read: (path: string, options?: ReadDirectoryOptions) => Promise<FileInfo[]>
          }
        },
        mod: {
          toggle: (path: string) => Promise<boolean>
        },
        ini: {
          parse: (path: string) => Promise<IniParseResult[]>
          update: (path: string, section: string, key: 'key', value: string) => Promise<boolean>
        }
      }

      drive: {
        item: {
          get: (id: string) => Promise<GetContentsResp>
          create_dirs: (parentId: string, dirs: { name: string; path: string; }[]) => Promise<DirCreateManyResp>
          rename: (id: string, name: string) => Promise<void>
          download: {
            enqueue: (id: string) => Promise<void>
          }
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

      window: WindowControls
    }
  }
}
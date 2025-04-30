// src/renderer/src/env.d.ts

/// <reference types="svelte" />
/// <reference types="vite/client" />
import { ElectronAPI } from "@electron-toolkit/preload";
import { GetContentsResp } from "../../types/drive.types";
import type { FileInfo, ReadDirectoryOptions } from "../../types/fs.types";
import type { ModFolders } from "../../types/fs.types";
import type { Stats } from "node:fs";
import type { IniParseResult } from "../../core/lib/iniParser";

interface WindowControls {
  minimize: () => void
  maximize: () => void
  close: () => void
}

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

      window: WindowControls
    }
  }
}
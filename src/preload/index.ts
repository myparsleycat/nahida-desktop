// src/preload/index.ts

import { contextBridge, webUtils } from 'electron'
import { electronAPI } from '@electron-toolkit/preload'
import { createApiInterface } from '@core/ipc';

const api = createApiInterface();

if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld('electron', electronAPI)
    contextBridge.exposeInMainWorld('webUtils', {
      getPathForFile: (file: File) => webUtils.getPathForFile(file)
    });
    contextBridge.exposeInMainWorld('api', api)
    console.log('APIs exposed via contextBridge');
  } catch (error) {
    console.error(error)
  }
} else {
  // @ts-ignore (define in dts)
  window.electron = electronAPI
  // @ts-ignore (define in dts)
  window.api = api
  console.log('APIs exposed directly to window');
}
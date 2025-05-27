// src/core/ipc-channels.ts

import { ipcRenderer, ipcMain } from 'electron';
import { Toast, ToastMessage } from './services/toast.service';
import { ReadDirectoryOptions } from '@shared/types/fs.types';
import type { ExternalToast } from "svelte-sonner";
import { NahidaIPCHelloModsParams } from '@shared/types/nahida.types';

type ServiceHandler = (...args: any[]) => Promise<any> | any;
type EventCallback = (state: any) => boolean | void;

interface ChannelDefinition {
  name: string;
  handler?: ServiceHandler;
  isEvent?: boolean;
  eventName?: string;
}

interface Services {
  auth?: any;
  fss?: any;
  ADS?: any;
  mods?: any;
  NahidaService?: any;
}

class ChannelGroup {
  private channels: Map<string, ChannelDefinition> = new Map();
  private subGroups: Map<string, ChannelGroup> = new Map();
  private parent?: ChannelGroup;
  private path: string[] = [];

  constructor(private name: string, parent?: ChannelGroup) {
    this.parent = parent;
    this.path = parent ? [...parent.path, name] : [name];
  }

  addChannel(name: string, handler?: ServiceHandler, isEvent: boolean = false, eventName?: string): ChannelDefinition {
    const channelName = name;
    const definition = { name: channelName, handler, isEvent, eventName };
    this.channels.set(name, definition);
    return definition;
  }

  addGroup(name: string): ChannelGroup {
    if (!this.subGroups.has(name)) {
      const group = new ChannelGroup(name, this);
      this.subGroups.set(name, group);
      return group;
    }
    return this.subGroups.get(name)!;
  }

  getFullChannelId(name: string): string {
    return [...this.path, name].join('.');
  }

  registerAllChannels(ipcMainInstance: typeof ipcMain): void {
    this.channels.forEach((definition, name) => {
      const fullChannelId = this.getFullChannelId(name);
      if (definition.handler && !definition.isEvent) {
        ipcMainInstance.handle(fullChannelId,
          (_event: any, ...args: any[]) => definition.handler!(...args));
      }
    });

    this.subGroups.forEach(group => {
      group.registerAllChannels(ipcMainInstance);
    });
  }

  createApiMethods(ipcRendererInstance: typeof ipcRenderer): any {
    const methods: Record<string, any> = {};

    this.channels.forEach((definition, name) => {
      const fullChannelId = this.getFullChannelId(name);

      if (definition.isEvent) {
        const eventChannelId = definition.eventName || fullChannelId;

        if (this.name === 'auth' && name === 'authStateChanged') {
          methods['onAuthStateChanged'] = (callback: EventCallback) => {
            ipcRendererInstance.on(eventChannelId, (_event: any, state: any) => callback(state));
            return () => ipcRendererInstance.removeListener(eventChannelId, callback);
          };
        } else {
          methods[name] = (callback: EventCallback) => {
            ipcRendererInstance.on(eventChannelId, (_event: any, state: any) => callback(state));
            return () => ipcRendererInstance.removeListener(eventChannelId, callback);
          };
        }
      } else {
        methods[name] = (...args: any[]) => ipcRendererInstance.invoke(fullChannelId, ...args);
      }
    });

    this.subGroups.forEach((group, groupName) => {
      methods[groupName] = group.createApiMethods(ipcRendererInstance);
    });

    return methods;
  }

  generateChannelConstants(): Record<string, any> {
    const constants: Record<string, any> = {};

    this.channels.forEach((definition, name) => {
      if (definition.isEvent && definition.eventName) {
        constants[definition.eventName.replace(/-/g, '_').toUpperCase()] = definition.eventName;
      } else {
        constants[name.toUpperCase()] = this.getFullChannelId(name);
      }

      if (this.name === 'auth' && name === 'authStateChanged') {
        constants['AUTH_STATE_CHANGED'] = 'auth-state-changed';
      }
    });

    this.subGroups.forEach((group, groupName) => {
      constants[groupName.toUpperCase()] = group.generateChannelConstants();
    });

    if (this.name === 'window') {
      constants['CONTROL'] = 'window-control';
    }

    return constants;
  }

  clearChannels(): void {
    this.channels.clear();
    this.subGroups.clear();
  }

  getSubGroup(name: string): ChannelGroup | undefined {
    return this.subGroups.get(name);
  }

  setChannelHandler(channelName: string, handler: ServiceHandler): void {
    const channel = this.channels.get(channelName);
    if (channel) {
      channel.handler = handler;
    } else {
      console.warn(`Channel not found: ${channelName}`);
    }
  }
}

export class ServiceRegistry {
  private rootGroup: ChannelGroup;

  constructor() {
    this.rootGroup = new ChannelGroup('root');
    this.defineChannelStructure();
  }

  private defineChannelStructure(): void {
    // AUTH
    const authGroup = this.rootGroup.addGroup('auth');
    authGroup.addChannel('startOAuth2Login');
    authGroup.addChannel('checkSessionState');
    authGroup.addChannel('logout');
    authGroup.addChannel('authStateChanged', undefined, true, 'auth-state-changed');

    // FS
    const fssGroup = this.rootGroup.addGroup('fss');
    fssGroup.addChannel('readDir');
    fssGroup.addChannel('readFile');
    fssGroup.addChannel('saveFile');
    fssGroup.addChannel('getStat');
    fssGroup.addChannel('openPath');
    fssGroup.addChannel('deletePath');
    fssGroup.addChannel('getImgBase64');
    fssGroup.addChannel('watchFolder');
    fssGroup.addChannel('unwatchFolder');
    fssGroup.addChannel('getWatchedFolders');
    fssGroup.addChannel('folderEvent', undefined, true, 'fss-folder-event');
    fssGroup.addChannel('select');

    const fsDirGroup = fssGroup.addGroup('dir');
    fsDirGroup.addChannel('read');

    // MODS
    const modsGroup = this.rootGroup.addGroup('mods');
    modsGroup.addChannel('clearPath');
    
    const modsUiGroup = modsGroup.addGroup('ui');
    const modsUiResizableGroup = modsUiGroup.addGroup('resizable');
    const modsUiLayoutGroup = modsUiGroup.addGroup('layout');
    modsUiResizableGroup.addChannel('get');
    modsUiResizableGroup.addChannel('set');
    modsUiLayoutGroup.addChannel('get');
    modsUiLayoutGroup.addChannel('set');
    
    const modsFolderGroup = modsGroup.addGroup('folder');
    modsFolderGroup.addChannel('getAll');
    modsFolderGroup.addChannel('create');
    modsFolderGroup.addChannel('delete');
    modsFolderGroup.addChannel('changeSeq');
    modsFolderGroup.addChannel('read');
    
    const modsFolderDirGroup = modsFolderGroup.addGroup('dir');
    modsFolderDirGroup.addChannel('read');
    modsFolderDirGroup.addChannel('disableAll');
    modsFolderDirGroup.addChannel('enableAll');
    
    const modGroup = modsGroup.addGroup('mod');
    modGroup.addChannel('read');
    modGroup.addChannel('toggle');
    
    const iniGroup = modsGroup.addGroup('ini');
    iniGroup.addChannel('parse');
    iniGroup.addChannel('update');
    
    const intxGroup = modsGroup.addGroup('intx');
    intxGroup.addChannel('drop');
    
    const modsMsgGroup = modsGroup.addGroup('msg');
    modsMsgGroup.addChannel('currentCharPathChanged', undefined, true, 'current-char-path-changed');
    modsMsgGroup.addChannel('currentFolderPathChanged', undefined, true, 'current-folder-path-changed');

    // NAHIDA
    const nahidaGroup = this.rootGroup.addGroup('nahida');
    const nahidaGetGroup = nahidaGroup.addGroup('get');
    nahidaGetGroup.addChannel('mods');

    // DRIVE
    const driveGroup = this.rootGroup.addGroup('drive');
    const driveItemGroup = driveGroup.addGroup('item');
    driveItemGroup.addChannel('get');
    driveItemGroup.addChannel('move');
    driveItemGroup.addChannel('rename');
    driveItemGroup.addChannel('trash_many');
    
    const driveItemDirGroup = driveItemGroup.addGroup('dir');
    driveItemDirGroup.addChannel('create');
    
    const driveItemDownloadGroup = driveItemGroup.addGroup('download');
    driveItemDownloadGroup.addChannel('enqueue');

    const driveUtilGroup = driveGroup.addGroup('util');
    const driveUtilImageCacheGroup = driveUtilGroup.addGroup('imageCache');
    driveUtilImageCacheGroup.addChannel('get');
    driveUtilImageCacheGroup.addChannel('sizes');
    driveUtilImageCacheGroup.addChannel('clear');
    driveUtilImageCacheGroup.addChannel('getStates');
    driveUtilImageCacheGroup.addChannel('change');

    // TOAST
    const toastGroup = this.rootGroup.addGroup('toast');
    toastGroup.addChannel('show');
    toastGroup.addChannel('success');
    toastGroup.addChannel('error');
    toastGroup.addChannel('warning');
    toastGroup.addChannel('info');
    toastGroup.addChannel('toastShow', undefined, true, 'toast-show');

    // WINDOW
    const windowGroup = this.rootGroup.addGroup('window');
    windowGroup.addChannel('windowControl', undefined, true, 'window-control');
  }

  injectServiceHandlers(services: Services): void {
    const { auth, fss, ADS, mods, NahidaService } = services;

    this.injectHandlers('auth', {
      startOAuth2Login: () => auth.StartOAuth2Login(),
      checkSessionState: () => auth.CheckSessionState(),
      logout: () => auth.Logout()
    });

    this.injectHandlers('fss', {
      readDir: (path: string, options: ReadDirectoryOptions) => fss.readDirectory(path, options),
      readFile: (path: string) => fss.readFile(path, "arrbuf"),
      saveFile: (path: string, data: ArrayBuffer) => fss.writeFile(path, Buffer.from(data)),
      getStat: (path: string) => fss.getStat(path),
      openPath: (path: string) => fss.openPath(path),
      deletePath: (path: string) => fss.deletePath(path),
      getImgBase64: (path: string) => fss.getImgBase64(path),
      watchFolder: (path: string, options: any) => fss.watchFolderChanges(path, options),
      unwatchFolder: (path: string) => fss.unwatchFolder(path),
      getWatchedFolders: () => fss.getWatchedFolders(),
      select: (opt: Electron.OpenDialogOptions) => fss.select(opt)
    });

    this.injectHandlers('fss.dir', {
      read: (path: string) => fss.readDirectory(path)
    });

    this.injectHandlers('mods', {
      clearPath: () => mods.clearPath()
    });

    this.injectHandlers('mods.ui.resizable', {
      get: () => mods.ui.resizable.get(),
      set: (size: number) => mods.ui.resizable.set(size)
    });

    this.injectHandlers('mods.ui.layout', {
      get: () => mods.ui.layout.get(),
      set: (layout: 'grid' | 'list') => mods.ui.layout.set(layout)
    });

    this.injectHandlers('mods.folder', {
      getAll: () => mods.folder.getAll(),
      create: (name: string, path: string) => mods.folder.create(path, name),
      delete: (path: string) => mods.folder.delete(path),
      changeSeq: (path: string, newSeq: number) => mods.folder.changeSeq(path, newSeq),
      read: (path: string) => mods.folder.read(path)
    });

    this.injectHandlers('mods.folder.dir', {
      read: (path: string, options?: ReadDirectoryOptions) => mods.folder.dir.read(path, options),
      disableAll: (path: string) => mods.folder.dir.disableAll(path),
      enableAll: (path: string) => mods.folder.dir.enableAll(path)
    });

    this.injectHandlers('mods.mod', {
      read: (path: string) => mods.mod.read(path),
      toggle: (path: string) => mods.mod.toggle(path)
    });

    this.injectHandlers('mods.ini', {
      parse: (path: string) => mods.ini.parse(path),
      update: (path: string, section: string, key: 'key', value: string) => mods.ini.update(path, section, key, value)
    });

    this.injectHandlers('mods.intx', {
      drop: (data: string[]) => mods.intx.drop(data)
    });

    this.injectHandlers('nahida.get', {
      mods: (params: NahidaIPCHelloModsParams) => NahidaService.get.mods(params)
    });

    this.injectHandlers('drive.item', {
      get: (id: string) => ADS.item.get(id),
      move: (current: string, ids: string[], newParentId: string) => ADS.item.move(current, ids, newParentId),
      rename: (id: string, rename: string) => ADS.item.rename(id, rename),
      trash_many: (ids: string[]) => ADS.item.trash_many(ids)
    });

    this.injectHandlers('drive.item.dir', {
      create: (parentId: string, dirs: { name: string; path: string; }[]) => ADS.item.dir.create(parentId, dirs)
    });

    this.injectHandlers('drive.item.download', {
      enqueue: (id: string, _name: string) => ADS.item.download(id)
    });

    this.injectHandlers('drive.util.imageCache', {
      get: (url: string) => ADS.util.imageCache.get(url),
      sizes: () => ADS.util.imageCache.sizes(),
      clear: () => ADS.util.imageCache.clear(),
      getStates: () => ADS.util.imageCache.getStates(),
      change: (v: boolean) => ADS.util.imageCache.change(v)
    });

    this.injectHandlers('toast', {
      show: (message: string, type?: ToastMessage['type'], data?: ExternalToast) => Toast.show(message, type, data),
      success: (message: string, data?: ExternalToast) => Toast.success(message, data),
      error: (message: string, data?: ExternalToast) => Toast.error(message, data),
      warning: (message: string, data?: ExternalToast) => Toast.warning(message, data),
      info: (message: string, data?: ExternalToast) => Toast.info(message, data)
    });
  }

  private injectHandlers(groupPath: string, handlers: Record<string, ServiceHandler>): void {
    const pathParts = groupPath.split('.');
    let currentGroup = this.rootGroup;

    for (const part of pathParts) {
      const subGroup = currentGroup.getSubGroup(part);
      if (subGroup) {
        currentGroup = subGroup;
      } else {
        console.warn(`Group not found: ${groupPath}`);
        return;
      }
    }

    Object.entries(handlers).forEach(([channelName, handler]) => {
      currentGroup.setChannelHandler(channelName, handler);
    });
  }

  registerServices(ipcMainInstance: typeof ipcMain): void {
    this.rootGroup.registerAllChannels(ipcMainInstance);

    ipcMainInstance.on('window-control', (_event, _command) => {
      // window-control 이벤트 처리
    });
  }

  createApiInterface(ipcRendererInstance: typeof ipcRenderer): Record<string, any> {
    const api: Record<string, any> = this.rootGroup.createApiMethods(ipcRendererInstance);

    api.window = {
      minimize: () => ipcRendererInstance.send('window-control', 'minimize'),
      maximize: () => ipcRendererInstance.send('window-control', 'maximize'),
      close: () => ipcRendererInstance.send('window-control', 'close')
    };

    return api;
  }

  getChannelConstants(): Record<string, any> {
    return this.rootGroup.generateChannelConstants();
  }
}

export const serviceRegistry = new ServiceRegistry();
export const IPC_CHANNELS = serviceRegistry.getChannelConstants();

// main.ts
export const registerServices = async (ipcMainInstance: typeof ipcMain) => {
  const { auth, fss, ADS } = await import('@core/services');
  const { mods } = await import('./services/mods.service');
  const { NahidaService } = await import('./services/nahida.service');

  serviceRegistry.injectServiceHandlers({ auth, fss, ADS, mods, NahidaService });
  serviceRegistry.registerServices(ipcMainInstance);
};

// preload.ts
export const createApiInterface = () => {
  return serviceRegistry.createApiInterface(ipcRenderer);
};
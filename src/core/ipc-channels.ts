// src/core/ipc-channels.ts

import { ipcRenderer, ipcMain } from 'electron'
import { auth, fss } from '../core/services'
import { drive } from '../core/services'
import { mods } from './services/mods.service';
import { ReadDirectoryOptions } from '../types/fs.types';

type ServiceHandler = (...args: any[]) => Promise<any> | any;
type EventCallback = (state: any) => boolean | void;
type EventEmitter = (channel: string, ...args: any[]) => void;

interface ChannelDefinition {
  name: string;
  handler?: ServiceHandler;
  isEvent?: boolean;
  eventName?: string; // 이벤트 채널 이름 (실제 사용되는 이름이 다를 경우)
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
      // 이벤트 채널인 경우 eventName을 사용 (있는 경우)
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
}

export class ServiceRegistry {
  private rootGroup: ChannelGroup;

  constructor() {
    this.rootGroup = new ChannelGroup('root');
    this.defineChannels();
  }

  private defineChannels(): void {
    // AUTH
    const authGroup = this.rootGroup.addGroup('auth');
    authGroup.addChannel('startOAuth2Login', () => auth.StartOAuth2Login());
    authGroup.addChannel('checkSessionState', () => auth.CheckSessionState());
    authGroup.addChannel('logout', () => auth.Logout());
    authGroup.addChannel('authStateChanged', undefined, true, 'auth-state-changed');

    // FS
    const fssGroup = this.rootGroup.addGroup('fss');
    fssGroup.addChannel('readDir', (path: string, options: ReadDirectoryOptions) => fss.readDirectory(path, options))
    fssGroup.addChannel('readFile', (path: string) => fss.readFile(path, "arrbuf"))
    fssGroup.addChannel('saveFile', (path: string, data: ArrayBuffer) => fss.saveFile(path, data))
    fssGroup.addChannel('getStat', (path: string) => fss.getStat(path))
    fssGroup.addChannel('openPath', (path: string) => fss.openPath(path))
    fssGroup.addChannel('deletePath', (path: string) => fss.deletePath(path))
    fssGroup.addChannel('getImgBase64', (path: string) => fss.getImgBase64(path))

    fssGroup.addChannel('watchFolder', (path: string, options: any) => fss.watchFolderChanges(path, options))
    fssGroup.addChannel('unwatchFolder', (path: string) => fss.unwatchFolder(path))
    fssGroup.addChannel('getWatchedFolders', () => fss.getWatchedFolders())
    fssGroup.addChannel('folderEvent', undefined, true, 'fss-folder-event')

    const fsDirGroup = fssGroup.addGroup('dir');
    fssGroup.addChannel('select', (opt: Electron.OpenDialogOptions) => fss.select(opt));
    fsDirGroup.addChannel('read', (path: string) => fss.readDirectory(path))

    // mods
    const modsGroup = this.rootGroup.addGroup('mods');
    const modsUiGroup = modsGroup.addGroup('ui');
    const modsUiResizableGroup = modsUiGroup.addGroup('resizable');
    modsUiResizableGroup.addChannel('get', () => mods.ui.resizable.get());
    modsUiResizableGroup.addChannel('set', (size: number) => mods.ui.resizable.set(size));
    const modsFolderGroup = modsGroup.addGroup('folder');
    modsFolderGroup.addChannel('getAll', () => mods.folder.getAll());
    modsFolderGroup.addChannel('create', (name: string, path: string) => mods.folder.create(path, name));
    modsFolderGroup.addChannel('delete', (path: string) => mods.folder.delete(path))
    const modsFolderDirGroup = modsFolderGroup.addGroup('dir');
    const modGroup = modsGroup.addGroup('mod');
    modGroup.addChannel('toggle', (path: string) => mods.mod.toggle(path));
    modsFolderDirGroup.addChannel('read', (path: string, options?: ReadDirectoryOptions) => mods.folder.dir.read(path, options));
    // modsPathGroup.addChannel('delete', () => {});
    const iniGroup = modsGroup.addGroup('ini');
    iniGroup.addChannel('parse', (path: string) => mods.ini.parse(path));
    iniGroup.addChannel('update', (path: string, section: string, key: 'key', value: string) => mods.ini.update(path, section, key, value));

    // DRIVE
    const driveGroup = this.rootGroup.addGroup('drive');
    const driveItemGroup = driveGroup.addGroup('item');
    driveItemGroup.addChannel('get', (id: string) => drive.item.get(id));
    driveItemGroup.addChannel('rename', (id: string, rename: string) => drive.item.rename(id, rename));

    const driveItemDownloadGroup = driveItemGroup.addGroup('download');
    driveItemDownloadGroup.addChannel('enqueue', (id: string) => drive.item.download.enqueue(id));

    const driveUtilGroup = driveGroup.addGroup('util');
    const driveUtilImageCacheGroup = driveUtilGroup.addGroup('imageCache');
    driveUtilImageCacheGroup.addChannel('get', (url: string) => drive.util.imageCache.get(url));
    driveUtilImageCacheGroup.addChannel('sizes', () => drive.util.imageCache.sizes());
    driveUtilImageCacheGroup.addChannel('clear', () => drive.util.imageCache.clear());
    driveUtilImageCacheGroup.addChannel('getStates', () => drive.util.imageCache.getStates());
    driveUtilImageCacheGroup.addChannel('change', (v: boolean) => drive.util.imageCache.change(v));

    // WINDOW
    const windowGroup = this.rootGroup.addGroup('window');
    windowGroup.addChannel('windowControl', undefined, true, 'window-control');
  }

  registerServices(ipcMainInstance: typeof ipcMain): void {
    this.rootGroup.registerAllChannels(ipcMainInstance);

    ipcMainInstance.on('window-control', (_event, _command) => {
      // window-control 이벤트는 main 프로세스에서 처리됨
      // 여기서는 이벤트가 등록되어 있음을 확인하는 용도
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
export const registerServices = (ipcMainInstance: typeof ipcMain) => {
  serviceRegistry.registerServices(ipcMainInstance);
};

// preload.ts
export const createApiInterface = () => {
  return serviceRegistry.createApiInterface(ipcRenderer);
};
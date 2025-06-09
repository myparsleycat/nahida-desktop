// src/core/ipc/index.ts

import { ipcRenderer, ipcMain } from 'electron';
import { ServiceRegistry } from './registry';
import {
    defineAuthChannels, injectAuthHandlers,
    defineFsChannels, injectFsHandlers,
    defineModsChannels, injectModsHandlers,
    defineNahidaChannels, injectNahidaHandlers,
    defineDriveChannels, injectDriveHandlers,
    defineToastChannels, injectToastHandlers,
    defineSettingChannels, injectSettingHandlers,
    injectRendererHandlers, defineRendererChannels,
    defineWindowChannels,
} from './channels';
import type { Services } from './types';

export class IPCManager {
    private registry: ServiceRegistry;

    constructor() {
        this.registry = new ServiceRegistry();
        this.defineChannelStructure();
    }

    private defineChannelStructure(): void {
        const rootGroup = this.registry.getRootGroup();

        defineAuthChannels(rootGroup);
        defineFsChannels(rootGroup);
        defineModsChannels(rootGroup);
        defineNahidaChannels(rootGroup);
        defineDriveChannels(rootGroup);
        defineToastChannels(rootGroup);
        defineWindowChannels(rootGroup);
        defineRendererChannels(rootGroup);
        defineSettingChannels(rootGroup);
    }

    injectServiceHandlers(services: Services): void {
        const {
            AuthService,
            FSService,
            DriveService,
            ModsService,
            NahidaService,
            SettingService
        } = services;

        injectAuthHandlers(this.registry, AuthService);
        injectFsHandlers(this.registry, FSService);
        injectModsHandlers(this.registry, ModsService);
        injectNahidaHandlers(this.registry, NahidaService);
        injectDriveHandlers(this.registry, DriveService);
        injectToastHandlers(this.registry);
        injectSettingHandlers(this.registry, SettingService)
    }

    registerServices(ipcMainInstance: typeof ipcMain): void {
        injectRendererHandlers(this.registry);
        this.registry.registerServices(ipcMainInstance);
    }

    createApiInterface(ipcRendererInstance: typeof ipcRenderer): Record<string, any> {
        return this.registry.createApiInterface(ipcRendererInstance);
    }

    getChannelConstants(): Record<string, any> {
        return this.registry.getChannelConstants();
    }
}

const ipcManager = new IPCManager();
export { ipcManager };
export const IPC_CHANNELS = ipcManager.getChannelConstants();
export { RendererCallManager } from './channels/renderer';

// main.ts 에서 사용
export const registerServices = async (ipcMainInstance: typeof ipcMain) => {
    const {
        AuthService,
        FSService,
        DriveService,
        ModsService,
        NahidaService,
        SettingService
    } = await import('@core/services');

    ipcManager.injectServiceHandlers({
        AuthService,
        FSService,
        DriveService,
        ModsService,
        NahidaService,
        SettingService
    });
    ipcManager.registerServices(ipcMainInstance);
};

// preload.ts 에서 사용
export const createApiInterface = () => {
    return ipcManager.createApiInterface(ipcRenderer);
};
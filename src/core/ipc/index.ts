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
    defineWindowChannels
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
    }

    injectServiceHandlers(services: Services): void {
        const { AuthService, FSService, DriveService, ModsService, NahidaService } = services;

        injectAuthHandlers(this.registry, AuthService);
        injectFsHandlers(this.registry, FSService);
        injectModsHandlers(this.registry, ModsService);
        injectNahidaHandlers(this.registry, NahidaService);
        injectDriveHandlers(this.registry, DriveService);
        injectToastHandlers(this.registry);
    }

    registerServices(ipcMainInstance: typeof ipcMain): void {
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

// main.ts
export const registerServices = async (ipcMainInstance: typeof ipcMain) => {
    const {
        AuthService,
        FSService,
        DriveService,
        ModsService,
        NahidaService
    } = await import('@core/services');

    ipcManager.injectServiceHandlers({
        AuthService,
        FSService,
        DriveService,
        ModsService,
        NahidaService
    });
    ipcManager.registerServices(ipcMainInstance);
};

// preload.ts
export const createApiInterface = () => {
    return ipcManager.createApiInterface(ipcRenderer);
};
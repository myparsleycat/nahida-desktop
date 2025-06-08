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
        const { auth, fss, ADS, mods, NahidaService } = services;

        injectAuthHandlers(this.registry, auth);
        injectFsHandlers(this.registry, fss);
        injectModsHandlers(this.registry, mods);
        injectNahidaHandlers(this.registry, NahidaService);
        injectDriveHandlers(this.registry, ADS);
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

// 싱글톤 인스턴스
const ipcManager = new IPCManager();
export { ipcManager };
export const IPC_CHANNELS = ipcManager.getChannelConstants();

// main.ts에서 사용
export const registerServices = async (ipcMainInstance: typeof ipcMain) => {
    const { auth, fss, ADS } = await import('@core/services');
    const { mods } = await import('@core/services/mods.service');
    const { NahidaService } = await import('@core/services/nahida.service');

    ipcManager.injectServiceHandlers({ auth, fss, ADS, mods, NahidaService });
    ipcManager.registerServices(ipcMainInstance);
};

// preload.ts에서 사용
export const createApiInterface = () => {
    return ipcManager.createApiInterface(ipcRenderer);
};
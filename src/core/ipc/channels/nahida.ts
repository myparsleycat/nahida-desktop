// src/core/ipc/channels/nahida.ts
import { ChannelGroup } from '../registry';
import type { Mod, NahidaIPCHelloModsParams } from '@shared/types/nahida.types';

export function defineNahidaChannels(rootGroup: ChannelGroup) {
    const nahidaGroup = rootGroup.addGroup('nahida');
    nahidaGroup.addChannel('startDownload');
    const nahidaGetGroup = nahidaGroup.addGroup('get');
    nahidaGetGroup.addChannel('mods');
}

export function injectNahidaHandlers(registry: any, NahidaService: any) {
    registry.injectHandlers('nahida', {
        startDownload: (mod: Mod, path: string) => NahidaService.startDownload(mod, path)
    });

    registry.injectHandlers('nahida.get', {
        mods: (params: NahidaIPCHelloModsParams) => NahidaService.get.mods(params)
    });
}
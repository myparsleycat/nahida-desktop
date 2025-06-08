import type { Mod, NahidaIPCHelloModsParams } from '@shared/types/nahida.types';
import { CharPathSelector } from '../stores/global.store';
import { get } from 'svelte/store';

class NahidaHelperClass {
    get = {
        mods: async (params: NahidaIPCHelloModsParams) => window.api.nahida.get.mods(params)
    }

    async startDownload(mod: Mod) {
        if (mod.password) {

        }
        
        try {
            const selectedPath = await CharPathSelector.open();
            await window.api.nahida.startDownload(mod, selectedPath);
        } catch (error) {
            console.error('startDownload Error:', error);
        }
    }
}

export const NahidaHelper = new NahidaHelperClass();
import type { NahidaIPCHelloModsParams } from '@shared/types/nahida.types';

class NahidaHelperClass {
    get = {
        mods: async (params: NahidaIPCHelloModsParams) => window.api.nahida.get.mods(params)
    }
}

export const NahidaHelper = new NahidaHelperClass();
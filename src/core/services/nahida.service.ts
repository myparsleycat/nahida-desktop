import { HelloModsUrl } from "@core/const";
import { fetcher } from "@core/lib/fetcher";
import { HelloModsResp, Mod, NahidaIPCHelloModsParams } from "@shared/types/nahida.types";
import { Toast } from "./toast.service";


class NahidaServiceClass {
  get = {
    mods: async (params: NahidaIPCHelloModsParams) => {
      const { page = 1, size = 102 } = params;

      const url = HelloModsUrl + '?' + `p=${page}&ps=${size}`;

      const resp = await fetcher<HelloModsResp>(url);

      if (!resp.data.success) {
        Toast.error('모드 데이터를 가져오는데 실패했습니다', {
          description: resp.data.error.message
        });
        return false;
      }
      return resp.data;
    }
  }

  async startDownload(mod: Mod, path: string) {
    console.log('title', mod.uuid);
    return true;
  }
}

export const NahidaService = new NahidaServiceClass();